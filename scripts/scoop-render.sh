#!/usr/bin/env bash
# Write the Scoop manifests for a final zenvik release, for
# chad3814/scoop-bucket (the release workflow's scoop jobs run this):
#
#   scripts/scoop-render.sh v1.2.0 <outdir>
#
# writes <outdir>/bucket/zenvik.json (the CLI zip, which bundles
# mkvmerge.exe) and <outdir>/bucket/zenvik-gui.json (the desktop app zip),
# hashes from the release's SHA256SUMS. Nothing in <outdir> changes unless
# both render. Downloads come from ZENVIK_RELEASE_BASE_URL (default the
# GitHub repo; tests use file://); the URLs written are always GitHub's.
# Needs jq.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 vX.Y.Z <outdir>" >&2
	exit 2
fi
tag=$1
out=$2
if [[ ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "scoop-render: $tag is not a final release tag (vX.Y.Z)" >&2
	exit 2
fi
ver=${tag#v}
base=${ZENVIK_RELEASE_BASE_URL:-https://github.com/chad3814/zenvik}
die() { echo "scoop-render: $*" >&2; exit 1; }
command -v jq >/dev/null || die "jq is required"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL "$base/releases/download/$tag/SHA256SUMS" -o "$work/SHA256SUMS" ||
	die "couldn't download $tag's SHA256SUMS"
sum() { awk -v f="$1" '$2 == f || $2 == "*" f { print $1; exit }' "$work/SHA256SUMS"; }
cli=$(sum "zenvik_${ver}_windows_amd64.zip")
gui=$(sum "zenvik-gui_${ver}_windows_amd64.zip")
[[ -n $cli ]] || die "SHA256SUMS has no zenvik_${ver}_windows_amd64.zip"
[[ -n $gui ]] || die "SHA256SUMS has no zenvik-gui_${ver}_windows_amd64.zip"
for h in "$cli" "$gui"; do
	[[ $h =~ ^[0-9a-f]{64}$ ]] || die "not a SHA-256: $h"
done

mkdir -p "$work/out/bucket"
# The jq programs are single-quoted: $version, $sha256 and $basename are
# Scoop's autoupdate placeholders, not shell or jq variables.
jq -n --indent 4 --arg ver "$ver" --arg hash "$cli" '{
    version: $ver,
    description: "Remux Blu-ray and DVD disc images to MKV",
    homepage: "https://github.com/chad3814/zenvik",
    license: "MIT",
    architecture: {"64bit": {
        url: "https://github.com/chad3814/zenvik/releases/download/v\($ver)/zenvik_\($ver)_windows_amd64.zip",
        hash: $hash,
        extract_dir: "zenvik_\($ver)_windows_amd64"
    }},
    bin: "zenvik.exe",
    checkver: "github",
    autoupdate: {
        architecture: {"64bit": {
            url: "https://github.com/chad3814/zenvik/releases/download/v$version/zenvik_$version_windows_amd64.zip",
            extract_dir: "zenvik_$version_windows_amd64"
        }},
        hash: {
            url: "https://github.com/chad3814/zenvik/releases/download/v$version/SHA256SUMS",
            regex: "$sha256\\s+$basename"
        }
    }
}' >"$work/out/bucket/zenvik.json"
jq -n --indent 4 --arg ver "$ver" --arg hash "$gui" '{
    version: $ver,
    description: "Desktop app to remux Blu-ray and DVD disc images to MKV",
    homepage: "https://github.com/chad3814/zenvik",
    license: "MIT,GPL-2.0-only",
    notes: "Zenvik needs the Microsoft Edge WebView2 runtime, which Windows 11 includes. It bundles mkvmerge from MKVToolNix (GPLv2; see MKVTOOLNIX-NOTICE.txt in the app folder).",
    architecture: {"64bit": {
        url: "https://github.com/chad3814/zenvik/releases/download/v\($ver)/zenvik-gui_\($ver)_windows_amd64.zip",
        hash: $hash,
        extract_dir: "zenvik-gui_\($ver)_windows_amd64"
    }},
    shortcuts: [["zenvik-gui.exe", "Zenvik"]],
    checkver: "github",
    autoupdate: {
        architecture: {"64bit": {
            url: "https://github.com/chad3814/zenvik/releases/download/v$version/zenvik-gui_$version_windows_amd64.zip",
            extract_dir: "zenvik-gui_$version_windows_amd64"
        }},
        hash: {
            url: "https://github.com/chad3814/zenvik/releases/download/v$version/SHA256SUMS",
            regex: "$sha256\\s+$basename"
        }
    }
}' >"$work/out/bucket/zenvik-gui.json"

mkdir -p "$out/bucket"
mv "$work/out/bucket/zenvik.json" "$out/bucket/zenvik.json"
mv "$work/out/bucket/zenvik-gui.json" "$out/bucket/zenvik-gui.json"
echo "rendered zenvik $ver into $out/bucket" >&2
