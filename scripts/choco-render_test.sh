#!/usr/bin/env bash
# Tests for scripts/choco-render.sh (offline, file:// fixtures).
#
#   scripts/choco-render_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cli=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
gui=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
decoy=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
fixture() {
	local fx="$work/fx-$RANDOM"
	mkdir -p "$fx/releases/download/$1"
	printf '%s' "$2" >"$fx/releases/download/$1/SHA256SUMS"
	echo "file://$fx"
}
good="$gui  zenvik-gui_9.9.9_windows_amd64.zip
$decoy  zenvik_9.9.90_windows_amd64.zip
$cli  zenvik_9.9.9_windows_amd64.zip
"
out="$work/out"
mkdir -p "$out/zenvik" && echo old >"$out/zenvik/zenvik.nuspec"
if msg=$(ZENVIK_RELEASE_BASE_URL=$(fixture v9.9.9 "$good") "$here/choco-render.sh" v9.9.9 "$out" 2>&1); then ok "renders a final release"; else bad "renders a final release" "$msg"; fi

contains() { # contains NAME FILE TEXT
	if grep -qF -- "$3" "$2" 2>/dev/null; then ok "$1"; else bad "$1" "missing [$3] in $2"; fi
}
xmlok() {
	if python3 -c 'import sys,xml.dom.minidom as m; m.parse(sys.argv[1])' "$2" 2>/dev/null; then ok "$1"; else bad "$1"; fi
}
xmlok "zenvik.nuspec is XML" "$out/zenvik/zenvik.nuspec"
xmlok "zenvik-gui.nuspec is XML" "$out/zenvik-gui/zenvik-gui.nuspec"
contains "CLI id" "$out/zenvik/zenvik.nuspec" "<id>zenvik</id>"
contains "CLI version" "$out/zenvik/zenvik.nuspec" "<version>9.9.9</version>"
contains "packageSourceUrl" "$out/zenvik/zenvik.nuspec" "<packageSourceUrl>https://github.com/chad3814/zenvik/blob/main/scripts/choco-render.sh</packageSourceUrl>"
contains "authors" "$out/zenvik/zenvik.nuspec" "<authors>Chad Walker</authors>"
contains "GUI id" "$out/zenvik-gui/zenvik-gui.nuspec" "<id>zenvik-gui</id>"
# The moderators reject raw.githubusercontent.com icons; jsDelivr serves the
# repo at the tag, so each release's icon is pinned.
contains "CLI icon from the jsDelivr CDN at the tag" "$out/zenvik/zenvik.nuspec" "<iconUrl>https://cdn.jsdelivr.net/gh/chad3814/zenvik@v9.9.9/gui/build/appicon.png</iconUrl>"
contains "GUI icon from the jsDelivr CDN at the tag" "$out/zenvik-gui/zenvik-gui.nuspec" "<iconUrl>https://cdn.jsdelivr.net/gh/chad3814/zenvik@v9.9.9/gui/build/appicon.png</iconUrl>"
copyright=$(grep -m1 '^Copyright' "$here/../LICENSE")
contains "CLI copyright is LICENSE's line" "$out/zenvik/zenvik.nuspec" "<copyright>$copyright</copyright>"
contains "GUI copyright is LICENSE's line" "$out/zenvik-gui/zenvik-gui.nuspec" "<copyright>$copyright</copyright>"
if grep -rq 'raw.githubusercontent.com' "$out"; then bad "no raw.githubusercontent.com URLs"; else ok "no raw.githubusercontent.com URLs"; fi
I="$out/zenvik/tools/chocolateyinstall.ps1"
GI="$out/zenvik-gui/tools/chocolateyinstall.ps1"
contains "CLI url" "$I" "https://github.com/chad3814/zenvik/releases/download/v9.9.9/zenvik_9.9.9_windows_amd64.zip"
contains "CLI checksum, the CLI's" "$I" "$cli"
contains "CLI hides the bundled mkvmerge from shims" "$I" "mkvmerge.exe.ignore"
contains "CLI unpack folder" "$I" "zenvik_9.9.9_windows_amd64"
contains "GUI checksum, the GUI's" "$GI" "$gui"
contains "GUI hides the bundled mkvmerge from shims" "$GI" "mkvmerge.exe.ignore"
contains "GUI gets a windowed shim" "$GI" "zenvik-gui.exe.gui"
contains "GUI Start-menu shortcut" "$GI" "Zenvik.lnk"
contains "GUI uninstall removes the shortcut" "$out/zenvik-gui/tools/chocolateyuninstall.ps1" "Zenvik.lnk"
if grep -qx old "$out/zenvik/zenvik.nuspec" || grep -rq "$decoy" "$out" || grep -rq '@@' "$out" || grep -rq 'file:' "$out"; then bad "no old files, decoys, placeholders or override URLs left"; else ok "no old files, decoys, placeholders or override URLs left"; fi

expect_fail() {
	local o="$work/fail-$RANDOM" st msg
	mkdir -p "$o"
	msg=$(ZENVIK_RELEASE_BASE_URL=$4 "$here/choco-render.sh" "$3" "$o" 2>&1)
	st=$?
	if [[ $st -ne $2 ]]; then bad "$1" "status $st (want $2): $msg"; elif [[ -n $(ls -A "$o") ]]; then bad "$1" "wrote into outdir"; else ok "$1"; fi
}
expect_fail "pre-release tag is refused" 2 v9.9.9-rc1 "$(fixture v9.9.9-rc1 "$good")"
expect_fail "missing CLI line fails" 1 v9.9.9 "$(fixture v9.9.9 "$gui  zenvik-gui_9.9.9_windows_amd64.zip
")"
expect_fail "malformed hash fails" 1 v9.9.9 "$(fixture v9.9.9 "XYZ  zenvik_9.9.9_windows_amd64.zip
$gui  zenvik-gui_9.9.9_windows_amd64.zip
")"

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
