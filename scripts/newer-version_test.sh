#!/usr/bin/env bash
# Tests for scripts/newer-version.sh.
#
#   scripts/newer-version_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

# expect NAME WANT TAG HAVE
expect() {
	local msg st
	msg=$("$here/newer-version.sh" "$3" "$4" 2>&1)
	st=$?
	if [[ $st -eq $2 ]]; then ok "$1"; else bad "$1" "status $st (want $2): $msg"; fi
}
expect "a newer release" 0 v1.2.0 1.1.1
expect "1.10 is newer than 1.9" 0 v1.10.0 1.9.0
expect "nothing yet" 0 v1.2.0 ""
expect "the same version" 3 v1.2.0 1.2.0
expect "an older release" 3 v1.2.0 1.2.1
expect "1.9 is older than 1.10" 3 v1.9.0 1.10.0
expect "a pre-release tag" 2 v1.2.0-rc1 1.1.1
expect "no v prefix" 2 1.2.0 1.1.1
if msg=$("$here/newer-version.sh" v1.2.0 2>&1); then bad "one argument is a usage error"; else ok "one argument is a usage error"; fi
msg=$("$here/newer-version.sh" v1.2.0 1.2.1 2>&1)
if [[ $msg == *1.2.1* ]]; then ok "a skip names the current version"; else bad "a skip names the current version" "$msg"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
