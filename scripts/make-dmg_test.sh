#!/usr/bin/env bash
# Tests for scripts/make-dmg.sh (macOS only; they skip elsewhere).
#
#   scripts/make-dmg_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0

ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

if [[ $(uname -s) != Darwin ]]; then
	echo "skip: make-dmg.sh needs macOS"
	exit 0
fi

work=$(mktemp -d)
mnt=""
cleanup() {
	if [[ -n $mnt ]]; then
		hdiutil detach "$mnt" -quiet 2>/dev/null || hdiutil detach "$mnt" -force -quiet 2>/dev/null || true
	fi
	rm -rf "$work"
}
trap cleanup EXIT

if out=$("$here/make-dmg.sh" 2>&1); then
	bad "no arguments is a usage error" "$out"
elif [[ $out == *usage* ]]; then
	ok "no arguments is a usage error"
else
	bad "no arguments is a usage error" "$out"
fi

if out=$("$here/make-dmg.sh" "$work/missing" Zenvik "$work/x.dmg" 2>&1); then
	bad "a missing source folder fails" "$out"
else
	ok "a missing source folder fails"
fi

src="$work/zenvik-gui_9.9.9_darwin_arm64"
mkdir -p "$src/Fake.app/Contents/MacOS" "$src/MKVTOOLNIX-LICENSES"
cp /usr/bin/true "$src/Fake.app/Contents/MacOS/fake"
echo license >"$src/LICENSE"
echo notice >"$src/MKVTOOLNIX-NOTICE.txt"
echo lgpl >"$src/MKVTOOLNIX-LICENSES/lgpl.txt"
dmg="$work/out.dmg"
if out=$("$here/make-dmg.sh" "$src" "Zenvik Test" "$dmg" 2>&1); then
	ok "builds a DMG"
else
	bad "builds a DMG" "$out"
fi

if [[ -f $dmg ]]; then
	mnt="$work/mnt"
	mkdir -p "$mnt"
	if hdiutil attach -readonly -nobrowse -noautoopen -mountpoint "$mnt" "$dmg" -quiet; then
		if [[ -L $mnt/Applications && $(readlink "$mnt/Applications") == /Applications ]]; then
			ok "Applications is a symlink to /Applications"
		else
			bad "Applications is a symlink to /Applications" "$(ls -l "$mnt")"
		fi
		if [[ -x $mnt/Fake.app/Contents/MacOS/fake && -f $mnt/LICENSE && -f $mnt/MKVTOOLNIX-NOTICE.txt &&
			-f $mnt/MKVTOOLNIX-LICENSES/lgpl.txt ]]; then
			ok "the folder's contents are at the volume's root"
		else
			bad "the folder's contents are at the volume's root" "$(ls -lR "$mnt")"
		fi
		info=$(hdiutil imageinfo "$dmg" 2>&1)
		if grep -q 'Format: UDZO' <<<"$info"; then
			ok "the image is compressed (UDZO)"
		else
			bad "the image is compressed (UDZO)" "$info"
		fi
		vol=$(diskutil info "$mnt" 2>/dev/null | sed -n 's/^ *Volume Name: *//p')
		if [[ $vol == "Zenvik Test" ]]; then
			ok "the volume is named"
		else
			bad "the volume is named" "got '$vol'"
		fi
		hdiutil detach "$mnt" -quiet || hdiutil detach "$mnt" -force -quiet
		mnt=""
	else
		bad "the DMG attaches"
	fi
fi

if [[ -e $src/Applications ]]; then
	bad "the source folder is left as it was" "$(ls -l "$src")"
else
	ok "the source folder is left as it was"
fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
