#!/usr/bin/env bash
# Tests for scripts/release-build.sh's Windows bundle. It downloads
# MKVToolNix, so it runs only with ZENVIK_NET_TESTS=1 (MKVTOOLNIX_CACHE
# reuses downloads).
#
#   ZENVIK_NET_TESTS=1 scripts/release-build_test.sh
set -uo pipefail

if [[ ${ZENVIK_NET_TESTS:-} != 1 ]]; then
	echo "skip: set ZENVIK_NET_TESTS=1 (downloads MKVToolNix)"
	exit 0
fi
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

tag=v0.0.0-bundletest
trap 'rm -f "$root"/dist/zenvik_0.0.0-bundletest_*' EXIT
if out=$("$here/release-build.sh" "$tag" windows/amd64 linux/amd64 2>&1); then
	ok "builds windows/amd64 and linux/amd64"
else
	bad "builds windows/amd64 and linux/amd64" "$out"
fi
zip="$root/dist/zenvik_0.0.0-bundletest_windows_amd64.zip"
list=$(unzip -Z1 "$zip" 2>&1)
for f in zenvik.exe mkvmerge.exe LICENSE README.md MKVTOOLNIX-COPYING.txt MKVTOOLNIX-NOTICE.txt MKVTOOLNIX-LICENSES/; do
	if grep -qx "zenvik_0.0.0-bundletest_windows_amd64/$f" <<<"$list"; then ok "zip has $f"; else bad "zip has $f" "$list"; fi
done
tgz=$(tar -tzf "$root/dist/zenvik_0.0.0-bundletest_linux_amd64.tar.gz" 2>&1)
if grep -q -i mkv <<<"$tgz"; then bad "linux tarball has no mkvmerge or notices" "$tgz"; else ok "linux tarball has no mkvmerge or notices"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
