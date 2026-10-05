#!/usr/bin/env bash
# Tests for scripts/winget-render.sh (offline, file:// fixtures).
#
#   scripts/winget-render_test.sh
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
$decoy  zenvik-gui_9.9.9_darwin_arm64.dmg
"
out="$work/out"
d="$out/manifests/c/chad3814"
mkdir -p "$d/Zenvik/9.9.9" && echo old >"$d/Zenvik/9.9.9/chad3814.Zenvik.yaml"
if msg=$(ZENVIK_RELEASE_BASE_URL=$(fixture v9.9.9 "$good") "$here/winget-render.sh" v9.9.9 "$out" 2>&1); then ok "renders a final release"; else bad "renders a final release" "$msg"; fi

has() { # has NAME FILE LINE: FILE contains exactly LINE
	if grep -qxF -- "$3" "$2" 2>/dev/null; then ok "$1"; else bad "$1" "missing [$3] in $2"; fi
}
C="$d/Zenvik/9.9.9/chad3814.Zenvik"
G="$d/ZenvikGUI/9.9.9/chad3814.ZenvikGUI"
for f in "$C.yaml" "$C.installer.yaml" "$C.locale.en-US.yaml" "$G.yaml" "$G.installer.yaml" "$G.locale.en-US.yaml"; do
	if [[ -f $f ]]; then ok "wrote ${f#"$out"/}"; else bad "wrote ${f#"$out"/}"; fi
done
has "CLI id" "$C.installer.yaml" "PackageIdentifier: chad3814.Zenvik"
has "CLI version" "$C.installer.yaml" "PackageVersion: 9.9.9"
has "CLI zip/portable" "$C.installer.yaml" "NestedInstallerType: portable"
has "CLI nested path" "$C.installer.yaml" '  - RelativeFilePath: zenvik_9.9.9_windows_amd64\zenvik.exe'
has "CLI alias" "$C.installer.yaml" "    PortableCommandAlias: zenvik"
has "CLI url" "$C.installer.yaml" "  InstallerUrl: https://github.com/chad3814/zenvik/releases/download/v9.9.9/zenvik_9.9.9_windows_amd64.zip"
has "CLI hash uppercase, the CLI's" "$C.installer.yaml" "  InstallerSha256: $(tr 'a-f' 'A-F' <<<"$cli")"
has "schema 1.12.0" "$C.installer.yaml" "ManifestVersion: 1.12.0"
has "CLI moniker" "$C.locale.en-US.yaml" "Moniker: zenvik"
has "publisher" "$C.locale.en-US.yaml" "Publisher: Chad Walker"
has "version manifest locale" "$C.yaml" "DefaultLocale: en-US"
has "GUI hash, the GUI's" "$G.installer.yaml" "  InstallerSha256: $(tr 'a-f' 'A-F' <<<"$gui")"
has "GUI nested path" "$G.installer.yaml" '  - RelativeFilePath: zenvik-gui_9.9.9_windows_amd64\zenvik-gui.exe'
has "GUI alias" "$G.installer.yaml" "    PortableCommandAlias: zenvik-gui"
has "GUI name" "$G.locale.en-US.yaml" "PackageName: Zenvik"
if grep -q old "$C.yaml" || grep -rq "$decoy" "$d" || grep -rq 'file:' "$d"; then bad "replaces old files, ignores decoys, never writes the override URL"; else ok "replaces old files, ignores decoys, never writes the override URL"; fi

expect_fail() {
	local o="$work/fail-$RANDOM" st msg
	mkdir -p "$o"
	msg=$(ZENVIK_RELEASE_BASE_URL=$4 "$here/winget-render.sh" "$3" "$o" 2>&1)
	st=$?
	if [[ $st -ne $2 ]]; then bad "$1" "status $st (want $2): $msg"; elif [[ -n $(ls -A "$o") ]]; then bad "$1" "wrote into outdir"; else ok "$1"; fi
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
