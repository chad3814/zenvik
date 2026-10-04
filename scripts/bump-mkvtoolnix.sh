#!/usr/bin/env bash
# Point the bundled mkvmerge at another MKVToolNix release: rewrite
# third_party/mkvtoolnix.env from upstream's .sha256 files, and (with
# --publish, which creates a public GitHub release) host that version's
# source as release mkvtoolnix-src-<version>.
#
#   scripts/bump-mkvtoolnix.sh 103.0 1
#   scripts/bump-mkvtoolnix.sh 103.0 1 --publish
set -euo pipefail

publish=0
args=()
for a in "$@"; do
	if [[ $a == --publish ]]; then publish=1; else args+=("$a"); fi
done
if [[ ${#args[@]} -lt 1 || ${#args[@]} -gt 2 ]]; then
	echo "usage: $0 <version> [<macos-build>] [--publish]" >&2
	exit 2
fi
v=${args[0]}
build=${args[1]:-1}
root=$(cd "$(dirname "$0")/.." && pwd)
base=${MKVTOOLNIX_BASE_URL:-https://mkvtoolnix.download}

# upstream_sum prints the SHA-256 from upstream's .sha256 file for url.
upstream_sum() {
	local s
	s=$(curl -fsSL "$1.sha256" | cut -d' ' -f1)
	if [[ ! $s =~ ^[0-9a-f]{64}$ ]]; then
		echo "no SHA-256 at $1.sha256" >&2
		return 1
	fi
	echo "$s"
}

arm=$(upstream_sum "$base/macos/releases/$v/MKVToolNix-$v-$build-arm64.dmg")
x86=$(upstream_sum "$base/macos/releases/$v/MKVToolNix-$v-$build-x86_64.dmg")
win=$(upstream_sum "$base/windows/releases/$v/mkvtoolnix-64-bit-$v.7z")
src=$(upstream_sum "$base/sources/mkvtoolnix-$v.tar.xz")

cat >"$root/third_party/mkvtoolnix.env" <<EOF
# MKVToolNix release whose mkvmerge the desktop app bundles (macOS, Windows).
# Update with scripts/bump-mkvtoolnix.sh; the checksums are upstream's own
# .sha256 values for each file.
MKVTOOLNIX_VERSION=$v
MKVTOOLNIX_MACOS_BUILD=$build
MKVTOOLNIX_SHA256_MACOS_ARM64=$arm
MKVTOOLNIX_SHA256_MACOS_X86_64=$x86
MKVTOOLNIX_SHA256_WINDOWS_64=$win
MKVTOOLNIX_SHA256_SOURCE=$src
EOF
echo "wrote third_party/mkvtoolnix.env for MKVToolNix $v (macOS build $build)" >&2

tag="mkvtoolnix-src-$v"
tarball="mkvtoolnix-$v.tar.xz"
notes="Source of MKVToolNix $v, whose mkvmerge is bundled with the Zenvik desktop app (GPLv2). Unmodified from $base/sources/$tarball."
if [[ $publish != 1 ]]; then
	echo "next, host the source (creates a public release; run with --publish to do it here):" >&2
	echo "  gh release create $tag $tarball $tarball.sha256 --title 'MKVToolNix $v source' --notes '$notes' --latest=false" >&2
	exit 0
fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL -o "$work/$tarball" "$base/sources/$tarball"
got=$(if command -v sha256sum >/dev/null; then sha256sum "$work/$tarball"; else shasum -a 256 "$work/$tarball"; fi | cut -d' ' -f1)
if [[ $got != "$src" ]]; then
	echo "checksum mismatch for $tarball: got $got, want $src" >&2
	exit 1
fi
printf '%s  %s\n' "$src" "$tarball" >"$work/$tarball.sha256"
(cd "$work" && gh release create "$tag" "$tarball" "$tarball.sha256" \
	--repo chad3814/zenvik --title "MKVToolNix $v source" --notes "$notes" --latest=false)
