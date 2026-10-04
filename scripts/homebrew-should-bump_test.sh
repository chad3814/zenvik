#!/usr/bin/env bash
# Tests for scripts/homebrew-should-bump.sh.
#
#   scripts/homebrew-should-bump_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# tap VERSION: a tap checkout whose cask is at VERSION ("" = no cask yet)
tap() {
	local d="$work/tap-$RANDOM"
	mkdir -p "$d/Casks"
	if [[ -n $1 ]]; then
		printf 'cask "zenvik-gui" do\n  arch arm: "arm64", intel: "amd64"\n\n  version "%s"\nend\n' "$1" >"$d/Casks/zenvik-gui.rb"
	fi
	echo "$d"
}

# expect NAME WANT TAG TAP_VERSION
expect() {
	local msg st
	msg=$("$here/homebrew-should-bump.sh" "$3" "$(tap "$4")" 2>&1)
	st=$?
	if [[ $st -eq $2 ]]; then ok "$1"; else bad "$1" "status $st (want $2): $msg"; fi
}
expect "a newer release bumps" 0 v1.2.0 1.1.1
expect "a newer minor compares numerically (1.10 > 1.9)" 0 v1.10.0 1.9.0
expect "an empty tap bumps" 0 v1.2.0 ""
expect "the same version is skipped" 3 v1.2.0 1.2.0
expect "an older release never downgrades the tap" 3 v1.2.0 1.2.1
expect "an older patch compares numerically (1.9 < 1.10)" 3 v1.9.0 1.10.0
expect "a pre-release tag is a usage error" 2 v1.2.0-rc1 1.1.1

msg=$("$here/homebrew-should-bump.sh" v1.2.0 "$(tap 1.2.1)" 2>&1)
if [[ $msg == *1.2.1* ]]; then ok "a skip names the tap's version"; else bad "a skip names the tap's version" "$msg"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
