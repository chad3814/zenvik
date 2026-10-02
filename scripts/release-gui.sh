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

tags=()
[[ $goos == linux ]] && tags=(-tags webkit2_41)
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
	cp -R "$bin/Zenvik.app" "$stage/"
	(cd "$dist" && rm -f "$name.zip" && ditto -c -k --keepParent "$name" "$name.zip")
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
