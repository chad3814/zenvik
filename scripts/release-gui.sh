#!/usr/bin/env bash
# Build one zenvik GUI release archive for this host into dist/.
#
#   scripts/release-gui.sh v1.1.0 darwin/arm64
#
# Targets: darwin/arm64 and darwin/amd64 (on macOS; clang builds both),
# windows/amd64 (on Windows, with 7-Zip), linux/amd64 (on Linux, needs
# libgtk-3-dev and libwebkit2gtk-4.1-dev). The GUI uses cgo, so each OS
# builds its own; the CLI archives come from scripts/release-build.sh.
# The darwin download is a disk image (Zenvik.app beside an Applications
# link to drag it onto), the others are archives. The darwin and windows
# builds bundle MKVToolNix's mkvmerge and its license notices (see
# scripts/fetch-mkvmerge.sh and third_party/mkvtoolnix.env).
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
mtx=""
dmgmnt=""
restore() {
	if [[ -n $dmgmnt ]]; then
		hdiutil detach "$dmgmnt" -quiet 2>/dev/null || hdiutil detach "$dmgmnt" -force -quiet 2>/dev/null || true
		rm -rf "$dmgmnt"
	fi
	rm -rf "${mtx:-}"
	# Signing material first, so it's gone even if a restore step fails.
	if declare -F macos_sign_cleanup >/dev/null; then
		macos_sign_cleanup
	fi
	cp "$saved" "$wailsjson"
	rm -f "$saved"
	touch "$root/gui/frontend/dist/gitkeep"
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
# The desktop app bundles MKVToolNix's mkvmerge on macOS and Windows (see
# third_party/mkvtoolnix.env); the app learns which release from this flag.
bundle=0
ldextra=""
if [[ $goos == darwin || $goos == windows ]]; then
	bundle=1
	# shellcheck source=third_party/mkvtoolnix.env
	source "$root/third_party/mkvtoolnix.env"
	ldextra=" -X main.mkvtoolnixVersion=$MKVTOOLNIX_VERSION"
fi
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
	macos_sign_preflight # fail fast on missing credentials, before the build
fi
# The build runs npm install scripts and vite plugins, so it gets none of the
# signing credentials, and the keychain isn't set up until it has finished.
(cd "$root/gui" && env -u MACOS_CERT_P12 -u MACOS_CERT_PASSWORD -u APPLE_API_KEY_P8 \
	-u APPLE_API_KEY_ID -u APPLE_API_ISSUER_ID \
	go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build \
	-clean -trimpath -platform "$target" -ldflags "-s -w -X main.version=$tag$ldextra" ${tags[@]+"${tags[@]}"})

if [[ $bundle == 1 ]]; then
	mtx=$(mktemp -d)
	"$root/scripts/fetch-mkvmerge.sh" "$target" "$mtx"
	# The notices link this source release; a CI release must not ship without it.
	if ! "$root/scripts/check-mkvtoolnix-source.sh" "$MKVTOOLNIX_VERSION"; then
		if [[ ${CI:-} == true || ${SIGN_REQUIRED:-} == 1 ]]; then
			exit 1
		fi
		echo "warning: building anyway (not CI); don't publish this build" >&2
	fi
fi

bin="$root/gui/build/bin"
stage="$dist/$name"
rm -rf "$stage"
mkdir -p "$stage"
cp "$root/LICENSE" "$stage/"
cp "$root/gui/README.md" "$stage/README.md"
case $goos in
darwin)
	app="$bin/Zenvik.app"
	mkdir -p "$app/Contents/Helpers/libs"
	cp "$mtx/mkvmerge" "$app/Contents/Helpers/mkvmerge"
	cp "$mtx/libs/libQt6Core.6.dylib" "$app/Contents/Helpers/libs/libQt6Core.6.dylib"
	# The notices travel inside the app too, for whoever drags only the app
	# out of the disk image.
	cp "$root/LICENSE" "$mtx"/MKVTOOLNIX-*.txt "$app/Contents/Resources/"
	cp -R "$mtx/MKVTOOLNIX-LICENSES" "$app/Contents/Resources/"
	# check_mkvmerge proves the bundled helper still runs (and loads its Qt)
	# at each step. An x86_64 helper can only run here through Rosetta; without
	# it the check is skipped (the signature checks still apply, and the arm64
	# build always runs it).
	runnable=1
	if [[ $goarch == amd64 && $(uname -m) == arm64 ]] && ! /usr/bin/arch -x86_64 /usr/bin/true 2>/dev/null; then
		if [[ ${SIGN_REQUIRED:-} == 1 ]]; then
			echo "no Rosetta on this host, so the x86_64 mkvmerge can't be checked (SIGN_REQUIRED=1)" >&2
			exit 1
		fi
		runnable=0
		echo "warning: no Rosetta on this host; can't run the x86_64 mkvmerge to check it" >&2
	fi
	# check_mkvmerge REASON [APP]
	check_mkvmerge() {
		local out
		[[ $runnable == 1 ]] || return 0
		out=$("${2:-$app}/Contents/Helpers/mkvmerge" --version 2>&1) || true
		if [[ $out != "mkvmerge v$MKVTOOLNIX_VERSION"* ]]; then
			echo "the bundled mkvmerge doesn't run ($1): $out" >&2
			exit 1
		fi
	}
	check_mkvmerge "as fetched"
	macos_sign_setup
	if [[ $MACOS_SIGN_ENABLED == 1 ]]; then
		macos_sign "$app"
		macos_verify_signature "$app"
		check_mkvmerge "after signing"
		ditto -c -k --keepParent "$app" "$MACOS_SIGN_TMP/$name-notary.zip"
		macos_notarize "$MACOS_SIGN_TMP/$name-notary.zip"
		macos_staple "$app"
		check_mkvmerge "after stapling"
		macos_verify_signature "$app"
		macos_verify_gatekeeper "$app"
	fi
	cp -R "$app" "$stage/"
	cp "$mtx"/MKVTOOLNIX-*.txt "$stage/"
	cp -R "$mtx/MKVTOOLNIX-LICENSES" "$stage/"
	dmg="$dist/$name.dmg"
	"$root/scripts/make-dmg.sh" "$stage" Zenvik "$dmg"
	macos_sign "$dmg"
	macos_verify_signature "$dmg"
	macos_notarize "$dmg"
	macos_staple "$dmg"
	macos_verify_gatekeeper "$dmg"
	# Check what a user gets: the image opens, the link points at
	# /Applications, and the app in it is intact and still runs its mkvmerge.
	dmgmnt=$(mktemp -d)
	hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$dmgmnt" "$dmg" -quiet
	if [[ ! -L $dmgmnt/Applications || $(readlink "$dmgmnt/Applications") != /Applications ]]; then
		echo "$name.dmg has no Applications link" >&2
		exit 1
	fi
	for f in Zenvik.app/Contents/Resources/MKVTOOLNIX-NOTICE.txt MKVTOOLNIX-NOTICE.txt LICENSE README.md; do
		if [[ ! -f $dmgmnt/$f ]]; then
			echo "$name.dmg is missing $f" >&2
			exit 1
		fi
	done
	macos_verify_signature "$dmgmnt/Zenvik.app"
	check_mkvmerge "from the disk image" "$dmgmnt/Zenvik.app"
	hdiutil detach "$dmgmnt" -quiet
	rm -rf "$dmgmnt"
	dmgmnt=""
	;;
windows)
	cp "$bin/zenvik-gui.exe" "$stage/"
	cp "$mtx/mkvmerge.exe" "$stage/"
	cp "$mtx"/MKVTOOLNIX-*.txt "$stage/"
	cp -R "$mtx/MKVTOOLNIX-LICENSES" "$stage/"
	if [[ $(uname -s) == MINGW* || $(uname -s) == MSYS* ]]; then
		out=$("$stage/mkvmerge.exe" --version 2>&1) || true
		if [[ $out != "mkvmerge v$MKVTOOLNIX_VERSION"* ]]; then
			echo "the bundled mkvmerge.exe doesn't run: $out" >&2
			exit 1
		fi
	fi
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
