#!/usr/bin/env bash
# Tests for scripts/scoop-render.sh. Offline: SHA256SUMS comes from a
# file:// fixture. Needs jq.
#
#   scripts/scoop-render_test.sh
# The expected $version, $sha256 and $basename are Scoop placeholders,
# compared literally.
# shellcheck disable=SC2016
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }
command -v jq >/dev/null || { echo "scoop-render_test.sh needs jq" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cli=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
gui=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
decoy=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc

# fixture TAG SUMS: a file:// release with SHA256SUMS
fixture() {
	local fx="$work/fx-$RANDOM"
	mkdir -p "$fx/releases/download/$1"
	printf '%s' "$2" >"$fx/releases/download/$1/SHA256SUMS"
	echo "file://$fx"
}

good="$gui  zenvik-gui_9.9.9_windows_amd64.zip
$decoy  zenvik_9.9.90_windows_amd64.zip
$decoy  zenvik-gui_9.9.9_darwin_arm64.dmg
$cli  zenvik_9.9.9_windows_amd64.zip
$decoy  zenvik-gui_9.9.90_windows_amd64.zip
"

out="$work/out"
mkdir -p "$out/bucket"
echo old >"$out/bucket/zenvik.json"
echo old >"$out/bucket/zenvik-gui.json"
if msg=$(ZENVIK_RELEASE_BASE_URL=$(fixture v9.9.9 "$good") "$here/scoop-render.sh" v9.9.9 "$out" 2>&1); then
	ok "renders a final release"
else
	bad "renders a final release" "$msg"
fi

# q FILE FILTER: jq -r on a rendered manifest
q() { jq -r "$2" "$out/bucket/$1.json" 2>&1; }
eq() { # eq NAME GOT WANT
	if [[ $2 == "$3" ]]; then ok "$1"; else bad "$1" "got [$2], want [$3]"; fi
}
eq "zenvik.json is valid JSON" "$(jq -e . "$out/bucket/zenvik.json" >/dev/null 2>&1 && echo yes)" yes
eq "zenvik-gui.json is valid JSON" "$(jq -e . "$out/bucket/zenvik-gui.json" >/dev/null 2>&1 && echo yes)" yes
eq "CLI version" "$(q zenvik .version)" 9.9.9
eq "CLI url" "$(q zenvik '.architecture."64bit".url')" https://github.com/chad3814/zenvik/releases/download/v9.9.9/zenvik_9.9.9_windows_amd64.zip
eq "CLI hash is the CLI zip's" "$(q zenvik '.architecture."64bit".hash')" "$cli"
eq "CLI extract_dir" "$(q zenvik '.architecture."64bit".extract_dir')" zenvik_9.9.9_windows_amd64
eq "CLI bin" "$(q zenvik .bin)" zenvik.exe
eq "CLI has no dependency (it bundles mkvmerge.exe)" "$(q zenvik '.depends // "none"')" none
eq "CLI checkver" "$(q zenvik .checkver)" github
eq "CLI autoupdate url" "$(q zenvik '.autoupdate.architecture."64bit".url')" 'https://github.com/chad3814/zenvik/releases/download/v$version/zenvik_$version_windows_amd64.zip'
eq "autoupdate hash regex is literal" "$(q zenvik .autoupdate.hash.regex)" '$sha256\s+$basename'
eq "autoupdate hash url" "$(q zenvik .autoupdate.hash.url)" 'https://github.com/chad3814/zenvik/releases/download/v$version/SHA256SUMS'
eq "GUI version" "$(q zenvik-gui .version)" 9.9.9
eq "GUI hash is the GUI zip's" "$(q zenvik-gui '.architecture."64bit".hash')" "$gui"
eq "GUI extract_dir" "$(q zenvik-gui '.architecture."64bit".extract_dir')" zenvik-gui_9.9.9_windows_amd64
eq "GUI shortcut" "$(q zenvik-gui '.shortcuts[0] | join(",")')" zenvik-gui.exe,Zenvik
eq "GUI license" "$(q zenvik-gui .license)" MIT,GPL-2.0-only
eq "GUI has no dependency" "$(q zenvik-gui '.depends // "none"')" none
eq "GUI autoupdate regex" "$(q zenvik-gui .autoupdate.hash.regex)" '$sha256\s+$basename'
if grep -q file: "$out/bucket/zenvik.json" "$out/bucket/zenvik-gui.json"; then bad "written URLs never use the download override"; else ok "written URLs never use the download override"; fi

# expect_fail NAME WANT TAG BASE: that status, and a fresh outdir left empty
expect_fail() {
	local o="$work/fail-$RANDOM" st msg
	mkdir -p "$o"
	msg=$(ZENVIK_RELEASE_BASE_URL=$4 "$here/scoop-render.sh" "$3" "$o" 2>&1)
	st=$?
	if [[ $st -ne $2 ]]; then
		bad "$1" "status $st (want $2): $msg"
	elif [[ -n $(ls -A "$o") ]]; then
		bad "$1" "wrote into outdir"
	else
		ok "$1"
	fi
}
expect_fail "pre-release tag is refused" 2 v9.9.9-rc1 "$(fixture v9.9.9-rc1 "$good")"
expect_fail "missing GUI line fails" 1 v9.9.9 "$(fixture v9.9.9 "$cli  zenvik_9.9.9_windows_amd64.zip
")"
expect_fail "malformed hash fails" 1 v9.9.9 "$(fixture v9.9.9 "XYZ  zenvik_9.9.9_windows_amd64.zip
$gui  zenvik-gui_9.9.9_windows_amd64.zip
")"
expect_fail "missing SHA256SUMS fails" 1 v9.9.9 "file://$work/nowhere"

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
