#!/usr/bin/env bash
# Tests for scripts/homebrew-render.sh. Offline: downloads come from file://
# fixtures. When brew is installed, the rendered files must pass brew style
# (which turns on Homebrew developer mode; run `brew developer off` after).
#
#   scripts/homebrew-render_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

sha() {
	if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

arm=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
intel=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
decoy=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc

# fixture TAG SUMS-CONTENT: a file:// release with a source tarball and SHA256SUMS
fixture() {
	local tag=$1 sums=$2 fx="$work/fx-$1"
	rm -rf "$fx"
	mkdir -p "$fx/archive/refs/tags" "$fx/releases/download/$tag"
	echo "source of $tag" >"$fx/archive/refs/tags/$tag.tar.gz"
	printf '%s' "$sums" >"$fx/releases/download/$tag/SHA256SUMS"
	echo "file://$fx"
}

good_sums="$decoy  zenvik-gui_9.9.90_darwin_arm64.dmg
$intel  zenvik-gui_9.9.9_darwin_amd64.dmg
$decoy  zenvik_9.9.9_darwin_arm64.tar.gz
$arm  zenvik-gui_9.9.9_darwin_arm64.dmg
$decoy  zenvik-gui_9.9.90_darwin_amd64.dmg
"

out="$work/out"
mkdir -p "$out/Formula" "$out/Casks"
echo old >"$out/Formula/zenvik.rb"
echo old >"$out/Casks/zenvik-gui.rb"
base=$(fixture v9.9.9 "$good_sums")
src_sha=$(sha "$work/fx-v9.9.9/archive/refs/tags/v9.9.9.tar.gz")
if msg=$(ZENVIK_RELEASE_BASE_URL=$base "$here/homebrew-render.sh" v9.9.9 "$out" 2>&1); then
	ok "renders a final release"
else
	bad "renders a final release" "$msg"
fi
formula=$(cat "$out/Formula/zenvik.rb" 2>/dev/null)
cask=$(cat "$out/Casks/zenvik-gui.rb" 2>/dev/null)

has() { # has NAME HAYSTACK NEEDLE
	if [[ $2 == *"$3"* ]]; then ok "$1"; else bad "$1" "missing: $3"; fi
}
has "formula points at the tag's source" "$formula" 'url "https://github.com/chad3814/zenvik/archive/refs/tags/v9.9.9.tar.gz"'
has "formula pins the source hash" "$formula" "sha256 \"$src_sha\""
has "formula depends on mkvtoolnix" "$formula" 'depends_on "mkvtoolnix"'
has "formula stamps the version" "$formula" '-X main.version=v#{version}'
has "cask has the version" "$cask" 'version "9.9.9"'
has "cask has the arm64 hash on arm" "$cask" "sha256 arm:   \"$arm\","
has "cask has the amd64 hash on intel" "$cask" "intel: \"$intel\""
has "cask downloads the DMG" "$cask" 'url "https://github.com/chad3814/zenvik/releases/download/v#{version}/zenvik-gui_#{version}_darwin_#{arch}.dmg"'
if [[ $formula == *old* || $cask == *old* || $cask == *"$decoy"* ]]; then
	bad "replaces old files and ignores decoys"
else
	ok "replaces old files and ignores decoys"
fi
if [[ $formula == *"$work"* || $cask == *"$work"* || $formula == *file:* ]]; then
	bad "written URLs never use the download override"
else
	ok "written URLs never use the download override"
fi

# expect_fail NAME WANT_STATUS TAG BASE: fails with that status and leaves a
# fresh outdir empty
expect_fail() {
	local name=$1 want=$2 tag=$3 b=$4 o="$work/fail-$RANDOM" st
	mkdir -p "$o"
	msg=$(ZENVIK_RELEASE_BASE_URL=$b "$here/homebrew-render.sh" "$tag" "$o" 2>&1)
	st=$?
	if [[ $st -ne $want ]]; then
		bad "$name" "status $st (want $want): $msg"
	elif [[ -n $(ls -A "$o") ]]; then
		bad "$name" "wrote into outdir: $(ls -R "$o")"
	else
		ok "$name"
	fi
}
expect_fail "pre-release tag is refused" 2 v9.9.9-rc1 "$base"
expect_fail "tag without v is refused" 2 9.9.9 "$base"
expect_fail "missing arm64 DMG line fails" 1 v9.9.9 "$(fixture v9.9.9 "$intel  zenvik-gui_9.9.9_darwin_amd64.dmg
")"
expect_fail "malformed hash fails" 1 v9.9.9 "$(fixture v9.9.9 "xyz  zenvik-gui_9.9.9_darwin_arm64.dmg
$intel  zenvik-gui_9.9.9_darwin_amd64.dmg
")"
b=$(fixture v9.9.9 "$good_sums")
rm "$work/fx-v9.9.9/archive/refs/tags/v9.9.9.tar.gz"
expect_fail "missing source tarball fails" 1 v9.9.9 "$b"
b=$(fixture v9.9.9 "$good_sums")
rm "$work/fx-v9.9.9/releases/download/v9.9.9/SHA256SUMS"
expect_fail "missing SHA256SUMS fails" 1 v9.9.9 "$b"

if msg=$("$here/homebrew-render.sh" 2>&1); then
	bad "no arguments is a usage error" "$msg"
else
	has "no arguments is a usage error" "$msg" usage
fi

if command -v brew >/dev/null; then
	if msg=$(brew style "$out/Formula/zenvik.rb" "$out/Casks/zenvik-gui.rb" 2>&1); then
		ok "brew style accepts the rendered files"
	else
		bad "brew style accepts the rendered files" "$msg"
	fi
else
	echo "skip brew style (no brew)"
fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
