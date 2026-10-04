#!/usr/bin/env bash
# Tests for scripts/fetch-mkvmerge.sh. The offline cases run anywhere; the
# real downloads (about 70 MB) run only with ZENVIK_NET_TESTS=1, the darwin
# one only on macOS.
#
#   scripts/fetch-mkvmerge_test.sh
#   ZENVIK_NET_TESTS=1 scripts/fetch-mkvmerge_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
# shellcheck source=third_party/mkvtoolnix.env
source "$root/third_party/mkvtoolnix.env"
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() {
	fail=$((fail + 1))
	echo "FAIL $1"
	if [[ -n ${2:-} ]]; then echo "     $2"; fi
}

out=$("$here/fetch-mkvmerge.sh" linux/amd64 "$(mktemp -d)" 2>&1)
status=$?
if [[ $status -eq 2 && $out == *"mkvmerge is not bundled for linux/amd64"* ]]; then
	ok "unsupported target exits 2"
else
	bad "unsupported target exits 2" "status $status: $out"
fi

cache=$(mktemp -d)
echo junk >"$cache/mkvtoolnix-64-bit-$MKVTOOLNIX_VERSION.7z"
out=$(MKVTOOLNIX_CACHE=$cache "$here/fetch-mkvmerge.sh" windows/amd64 "$(mktemp -d)" 2>&1)
status=$?
if [[ $status -eq 1 && $out == *"checksum mismatch for mkvtoolnix-64-bit-$MKVTOOLNIX_VERSION.7z"* ]]; then
	ok "checksum mismatch fails naming the file"
else
	bad "checksum mismatch fails naming the file" "status $status: $out"
fi
rm -rf "$cache"

out=$("$here/fetch-mkvmerge.sh" darwin/arm64 2>&1)
status=$?
if [[ $status -eq 2 ]]; then ok "missing outdir is a usage error"; else bad "missing outdir is a usage error" "status $status: $out"; fi

if [[ ${ZENVIK_NET_TESTS:-} == 1 ]]; then
	cache=$(mktemp -d)
	if [[ $(uname -s) == Darwin ]]; then
		d=$(mktemp -d)
		if MKVTOOLNIX_CACHE=$cache "$here/fetch-mkvmerge.sh" darwin/arm64 "$d" >/dev/null 2>&1; then
			got=$(cd "$d" && find . -type f -o -type l | sort | tr '\n' ' ')
			want_files=("./MKVTOOLNIX-COPYING.txt" "./MKVTOOLNIX-NOTICE.txt" "./libs/libQt6Core.6.dylib" "./mkvmerge")
			missing=""
			for f in "${want_files[@]}"; do
				if [[ $got != *"$f "* ]]; then missing="$missing $f"; fi
			done
			if [[ -z $missing && -f $d/MKVTOOLNIX-LICENSES/GPL-3.0.txt && -f $d/MKVTOOLNIX-LICENSES/LGPL-3.0.txt ]]; then
				ok "darwin fetch writes the helper, Qt and the license files"
			else
				bad "darwin fetch writes the helper, Qt and the license files" "missing:$missing; got: $got"
			fi
			ver=$("$d/mkvmerge" --version 2>&1)
			if [[ $ver == "mkvmerge v$MKVTOOLNIX_VERSION"* ]]; then
				ok "fetched mkvmerge runs from its new place"
			else
				bad "fetched mkvmerge runs from its new place"
			fi
			if grep -q "Qt 6\." "$d/MKVTOOLNIX-NOTICE.txt" && grep -q "mkvtoolnix-src-$MKVTOOLNIX_VERSION" "$d/MKVTOOLNIX-NOTICE.txt" &&
				grep -qF "codesign --force --sign -" "$d/MKVTOOLNIX-NOTICE.txt"; then
				ok "darwin notice names Qt, the source release and how to replace Qt"
			else
				bad "darwin notice names Qt, the source release and how to replace Qt" "$(cat "$d/MKVTOOLNIX-NOTICE.txt")"
			fi
		else
			bad "darwin fetch succeeds"
		fi
		rm -rf "$d"
	fi
	w=$(mktemp -d)
	if MKVTOOLNIX_CACHE=$cache "$here/fetch-mkvmerge.sh" windows/amd64 "$w" >/dev/null 2>&1 &&
		[[ $(head -c 2 "$w/mkvmerge.exe") == MZ ]] && [[ -f $w/MKVTOOLNIX-COPYING.txt ]] &&
		grep -qE "Qt 6\.[0-9]+\.[0-9]+" "$w/MKVTOOLNIX-NOTICE.txt" && grep -q "statically linked" "$w/MKVTOOLNIX-NOTICE.txt" &&
		grep -q "qtbase-everywhere-src-" "$w/MKVTOOLNIX-NOTICE.txt"; then
		ok "windows fetch writes mkvmerge.exe and notices naming its statically linked Qt"
	else
		bad "windows fetch writes mkvmerge.exe and notices naming its statically linked Qt" "$(cat "$w/MKVTOOLNIX-NOTICE.txt" 2>&1)"
	fi
	rm -rf "$w" "$cache"
	if "$here/check-mkvtoolnix-source.sh" "$MKVTOOLNIX_VERSION" >/dev/null 2>&1; then
		ok "the pinned version's source release exists"
	else
		bad "the pinned version's source release exists"
	fi
	out=$("$here/check-mkvtoolnix-source.sh" 0.0 2>&1)
	status=$?
	if [[ $status -eq 1 && $out == *"mkvtoolnix-src-0.0"* ]]; then
		ok "a missing source release fails naming it"
	else
		bad "a missing source release fails naming it" "status $status: $out"
	fi
else
	echo "skip network fetches (set ZENVIK_NET_TESTS=1)"
fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
