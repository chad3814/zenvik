#!/usr/bin/env bash
# Fetch the pinned MKVToolNix mkvmerge that the desktop app bundles, with its
# license files, into outdir. Every download is checked against the SHA-256
# pinned in third_party/mkvtoolnix.env.
#
#   scripts/fetch-mkvmerge.sh darwin/arm64 /tmp/mtx
#
# darwin/arm64, darwin/amd64: outdir/mkvmerge, outdir/libs/libQt6Core.6.dylib
# windows/amd64:              outdir/mkvmerge.exe
# both: outdir/MKVTOOLNIX-NOTICE.txt, MKVTOOLNIX-COPYING.txt, MKVTOOLNIX-LICENSES/
#
# MKVTOOLNIX_CACHE=dir uses (and fills) dir instead of a temporary folder, so
# files already there aren't downloaded again; MKVTOOLNIX_BASE_URL overrides
# https://mkvtoolnix.download.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 os/arch outdir" >&2
	exit 2
fi
target=$1
out=$2
root=$(cd "$(dirname "$0")/.." && pwd)
# shellcheck source=third_party/mkvtoolnix.env
source "$root/third_party/mkvtoolnix.env"
v=$MKVTOOLNIX_VERSION
base=${MKVTOOLNIX_BASE_URL:-https://mkvtoolnix.download}

case $target in
darwin/arm64)
	file="MKVToolNix-$v-$MKVTOOLNIX_MACOS_BUILD-arm64.dmg"
	url="$base/macos/releases/$v/$file"
	sum=$MKVTOOLNIX_SHA256_MACOS_ARM64
	;;
darwin/amd64)
	file="MKVToolNix-$v-$MKVTOOLNIX_MACOS_BUILD-x86_64.dmg"
	url="$base/macos/releases/$v/$file"
	sum=$MKVTOOLNIX_SHA256_MACOS_X86_64
	;;
windows/amd64)
	file="mkvtoolnix-64-bit-$v.7z"
	url="$base/windows/releases/$v/$file"
	sum=$MKVTOOLNIX_SHA256_WINDOWS_64
	;;
*)
	echo "mkvmerge is not bundled for $target" >&2
	exit 2
	;;
esac
src="mkvtoolnix-$v.tar.xz"

work=$(mktemp -d)
mnt=""
cleanup() {
	if [[ -n $mnt ]]; then hdiutil detach "$mnt" -quiet || true; fi
	rm -rf "$work"
}
trap cleanup EXIT
cache=${MKVTOOLNIX_CACHE:-$work/cache}
mkdir -p "$cache" "$out"

sha256() {
	if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1
}

# fetch downloads url into the cache (unless it's already there) and checks
# its SHA-256.
fetch() {
	local url=$1 name=$2 want=$3 got
	if [[ ! -f $cache/$name ]]; then
		echo "downloading $name" >&2
		curl -fsSL -o "$cache/$name.part" "$url"
		mv "$cache/$name.part" "$cache/$name"
	fi
	got=$(sha256 "$cache/$name")
	if [[ $got != "$want" ]]; then
		echo "checksum mismatch for $name: got $got, want $want" >&2
		return 1
	fi
}

fetch "$url" "$file" "$sum"
fetch "$base/sources/$src" "$src" "$MKVTOOLNIX_SHA256_SOURCE"

qt=""
case $target in
darwin/*)
	mnt="$work/mnt"
	mkdir -p "$mnt"
	hdiutil attach -nobrowse -readonly -mountpoint "$mnt" "$cache/$file" >/dev/null
	bin="$mnt/MKVToolNix.app/Contents/MacOS"
	cp "$bin/mkvmerge" "$out/mkvmerge"
	mkdir -p "$out/libs"
	real=$(readlink "$bin/libs/libQt6Core.6.dylib" || basename "$bin/libs/libQt6Core.6.dylib")
	cp -L "$bin/libs/libQt6Core.6.dylib" "$out/libs/libQt6Core.6.dylib"
	qt=${real#libQt6Core.}
	qt=${qt%.dylib}
	hdiutil detach "$mnt" -quiet
	mnt=""
	;;
windows/*)
	if command -v 7z >/dev/null; then
		7z e -y -bso0 -o"$work/w" "$cache/$file" mkvtoolnix/mkvmerge.exe
	else
		mkdir -p "$work/w"
		tar -xf "$cache/$file" -C "$work/w" mkvtoolnix/mkvmerge.exe
		mv "$work/w/mkvtoolnix/mkvmerge.exe" "$work/w/mkvmerge.exe"
	fi
	cp "$work/w/mkvmerge.exe" "$out/mkvmerge.exe"
	;;
esac

tar -xJf "$cache/$src" -C "$work" "mkvtoolnix-$v/COPYING" "mkvtoolnix-$v/doc/licenses"
cp "$work/mkvtoolnix-$v/COPYING" "$out/MKVTOOLNIX-COPYING.txt"
rm -rf "$out/MKVTOOLNIX-LICENSES"
cp -R "$work/mkvtoolnix-$v/doc/licenses" "$out/MKVTOOLNIX-LICENSES"
cp "$root/third_party/licenses/GPL-3.0.txt" "$out/MKVTOOLNIX-LICENSES/GPL-3.0.txt"

{
	echo "This download includes mkvmerge from MKVToolNix $v"
	echo "(https://mkvtoolnix.download/), (c) Moritz Bunkus and contributors,"
	echo "licensed under the GNU General Public License version 2 (MKVTOOLNIX-COPYING.txt)."
	if [[ -n $qt ]]; then
		echo
		echo "On macOS it is accompanied by the Qt $qt Core library (libQt6Core),"
		echo "licensed under the GNU Lesser General Public License version 3"
		echo "(MKVTOOLNIX-LICENSES/LGPL-3.0.txt and GPL-3.0.txt). It is dynamically linked:"
		echo "you may replace Zenvik.app/Contents/Helpers/libs/libQt6Core.6.dylib with your own build."
		echo "Qt source: https://download.qt.io/archive/qt/${qt%.*}/$qt/submodules/qtbase-everywhere-src-$qt.tar.xz"
	fi
	echo
	echo "The files are unmodified from the official MKVToolNix $v release, except that"
	echo "on macOS they are re-signed with Zenvik's Developer ID."
	echo
	echo "Source for MKVToolNix $v:"
	echo "https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-$v/mkvtoolnix-$v.tar.xz"
	echo
	echo "Licenses of the libraries built into mkvmerge: MKVTOOLNIX-LICENSES/."
} >"$out/MKVTOOLNIX-NOTICE.txt"
echo "fetched mkvmerge $v for $target into $out" >&2
