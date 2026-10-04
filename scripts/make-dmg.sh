#!/usr/bin/env bash
# Build a compressed, read-only disk image for drag-and-drop installs: the
# folder's contents at the volume's root, plus an "Applications" symlink to
# /Applications to drag the app onto. The folder itself is left unchanged.
#
#   scripts/make-dmg.sh <folder> <volume-name> <out.dmg>
#
# macOS only (hdiutil). Signing and notarizing the image is the caller's job
# (see macos_sign and macos_notarize in scripts/macos-sign.sh).
set -euo pipefail

if [[ $# -ne 3 ]]; then
	echo "usage: $0 <folder> <volume-name> <out.dmg>" >&2
	exit 2
fi
src=$1
vol=$2
out=$3
if [[ ! -d $src ]]; then
	echo "make-dmg: $src is not a folder" >&2
	exit 1
fi

work=$(mktemp -d "${TMPDIR:-/tmp}/zenvik-dmg.XXXXXX")
trap 'rm -rf "$work"' EXIT
root="$work/root"
ditto "$src" "$root"
ln -s /Applications "$root/Applications"
rm -f "$out"
hdiutil create -quiet -volname "$vol" -srcfolder "$root" -fs HFS+ -format UDZO -ov "$out"
