#!/usr/bin/env bash
# Build one zenvik GUI release archive for this host into dist/.
#
#   scripts/release-gui.sh v1.1.0 darwin/arm64
#
# Targets: darwin/arm64 and darwin/amd64 (on macOS; clang builds both),
# windows/amd64 (on Windows, with 7-Zip), linux/amd64 (on Linux, needs
# libgtk-3-dev and libwebkit2gtk-4.1-dev). The GUI uses cgo, so each OS
# builds its own; the CLI archives come from scripts/release-build.sh.
set -euo pipefail

if [[ $# -ne 2 || ! $1 =~ ^v[0-9] ]]; then
	echo "usage: $0 vX.Y.Z os/arch" >&2
	exit 2
fi
tag=$1
target=$2
ver=${tag#v}
goos=${target%/*}
goarch=${target#*/}

root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
mkdir -p "$dist"
name="zenvik-gui_${ver}_${goos}_${goarch}"

# The app's own version fields (Info.plist's CFBundleShortVersionString and
# CFBundleVersion, the Windows version resource) come from wails.json's
# info.productVersion. They must be numeric, so a pre-release tag (v1.2.0-rc1)
# stamps 1.2.0. wails.json is changed for this build only and restored on exit;
# so is frontend/dist/gitkeep, which `wails build -clean` deletes.
numver=${ver%%[-+]*}
if [[ ! $numver =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "can't make an X.Y.Z app version from $tag" >&2
	exit 2
fi
wailsjson="$root/gui/wails.json"
saved=$(mktemp)
cp "$wailsjson" "$saved"
restore() {
	cp "$saved" "$wailsjson"
	rm -f "$saved"
	touch "$root/gui/frontend/dist/gitkeep"
	if declare -F macos_sign_cleanup >/dev/null; then
		macos_sign_cleanup
	fi
}
trap restore EXIT
node -e '
const fs = require("fs");
const [file, version] = process.argv.slice(1);
const project = JSON.parse(fs.readFileSync(file, "utf8"));
project.info = { ...project.info, productVersion: version };
fs.writeFileSync(file, JSON.stringify(project, null, 2) + "\n");
' "$wailsjson" "$numver"

tags=()
[[ $goos == linux ]] && tags=(-tags webkit2_41)
if [[ $goos == darwin ]]; then
	# macOS 13 or later (Go 1.27 itself needs 12). Wails adds
	# -mmacosx-version-min=10.13 to the cgo flags unless they already set a
	# minimum, and that flag would win over MACOSX_DEPLOYMENT_TARGET.
	export MACOSX_DEPLOYMENT_TARGET=13.0
	export CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }-mmacosx-version-min=13.0"
	export CGO_LDFLAGS="${CGO_LDFLAGS:+$CGO_LDFLAGS }-mmacosx-version-min=13.0"
fi
if [[ $goos == darwin ]]; then
	# shellcheck source=scripts/macos-sign.sh
	source "$root/scripts/macos-sign.sh"
	macos_sign_setup
fi
(cd "$root/gui" && go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build \
	-clean -trimpath -platform "$target" -ldflags "-s -w -X main.version=$tag" ${tags[@]+"${tags[@]}"})

bin="$root/gui/build/bin"
stage="$dist/$name"
rm -rf "$stage"
mkdir -p "$stage"
cp "$root/LICENSE" "$stage/"
cp "$root/gui/README.md" "$stage/README.md"
case $goos in
darwin)
	app="$bin/Zenvik.app"
	if [[ $MACOS_SIGN_ENABLED == 1 ]]; then
		macos_sign "$app"
		macos_verify_signature "$app"
		ditto -c -k --keepParent "$app" "$MACOS_SIGN_TMP/$name-notary.zip"
		macos_notarize "$MACOS_SIGN_TMP/$name-notary.zip"
		macos_staple "$app"
		macos_verify_signature "$app"
		macos_verify_gatekeeper "$app"
	fi
	cp -R "$app" "$stage/"
	(cd "$dist" && rm -f "$name.zip" && ditto -c -k --norsrc --noextattr --noacl --keepParent "$name" "$name.zip")
	if stray=$(unzip -l "$dist/$name.zip" | grep -E '/\._|__MACOSX'); then
		echo "AppleDouble entries in $name.zip:" >&2
		echo "$stray" >&2
		exit 1
	fi
	;;
windows)
	cp "$bin/zenvik-gui.exe" "$stage/"
	(cd "$dist" && rm -f "$name.zip" && 7z a -tzip -bso0 "$name.zip" "$name")
	;;
linux)
	cp "$bin/zenvik-gui" "$stage/"
	tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
	;;
*)
	echo "unsupported target $target" >&2
	exit 2
	;;
esac
rm -rf "$stage"
ls -l "$dist/$name".*
