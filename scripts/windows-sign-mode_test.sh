#!/usr/bin/env bash
# Tests for scripts/windows-sign-mode.sh.
#
#   scripts/windows-sign-mode_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

# expect NAME WANT_STDOUT WANT_STDERR_PART TAG ORG
expect() {
	local out err
	err=$({ out=$("$here/windows-sign-mode.sh" "$4" "$5"); } 2>&1)
	out=$("$here/windows-sign-mode.sh" "$4" "$5" 2>/dev/null)
	if [[ $out == "$2" && $err == *"$3"* ]]; then ok "$1"; else bad "$1" "stdout [$out] stderr [$err]"; fi
}
expect "a final tag with SignPath configured signs" sign "" v1.4.0 0123-org
expect "a final tag without SignPath passes through" pass "not set" v1.4.0 ""
expect "a pre-release passes through even when configured" pass "pre-release" v1.4.0-rc1 0123-org
expect "a non-version tag passes through" pass "pre-release" nightly 0123-org

if out=$("$here/windows-sign-mode.sh" v1.4.0 2>&1); then bad "one argument is a usage error" "$out"; else ok "one argument is a usage error"; fi
if out=$("$here/windows-sign-mode.sh" "" org 2>&1); then bad "an empty tag is a usage error" "$out"; else ok "an empty tag is a usage error"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
