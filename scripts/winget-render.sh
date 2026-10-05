#!/usr/bin/env bash
# Write the winget manifests (schema 1.12.0) for a final zenvik release:
# chad3814.Zenvik (the CLI zip, which bundles mkvmerge.exe) and
# chad3814.ZenvikGUI (the desktop app zip), both zip-wrapped portable
# programs. The release workflow's winget jobs run this:
#
#   scripts/winget-render.sh v1.3.0 <outdir>
#
# writes <outdir>/manifests/c/chad3814/<Package>/<ver>/*.yaml, hashes from
# the release's SHA256SUMS. Nothing in <outdir> changes unless all render.
# Downloads come from ZENVIK_RELEASE_BASE_URL (default the GitHub repo).
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 vX.Y.Z <outdir>" >&2
	exit 2
fi
tag=$1
out=$2
if [[ ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "winget-render: $tag is not a final release tag (vX.Y.Z)" >&2
	exit 2
fi
ver=${tag#v}
base=${ZENVIK_RELEASE_BASE_URL:-https://github.com/chad3814/zenvik}
repo=https://github.com/chad3814/zenvik
die() { echo "winget-render: $*" >&2; exit 1; }

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
today=$(date -u +%Y-%m-%d)

# package PKG ZIPBASE EXE ALIAS HASH NAME LICENSE SHORT DESCRIPTION MONIKER
package() {
	local pkg=$1 zipbase=$2 exe=$3 alias=$4 hash=$5 name=$6 license=$7 short=$8 desc=$9 moniker=${10}
	local id="chad3814.$pkg" dir="$work/out/manifests/c/chad3814/$pkg/$ver"
	local up
	up=$(tr 'a-f' 'A-F' <<<"$hash")
	mkdir -p "$dir"
	cat >"$dir/$id.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.version.1.12.0.schema.json

PackageIdentifier: $id
PackageVersion: $ver
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.12.0
EOF
	cat >"$dir/$id.installer.yaml" <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.installer.1.12.0.schema.json

PackageIdentifier: $id
PackageVersion: $ver
InstallerType: zip
NestedInstallerType: portable
ReleaseDate: $today
Installers:
- Architecture: x64
  NestedInstallerFiles:
  - RelativeFilePath: ${zipbase}\\$exe
    PortableCommandAlias: $alias
  InstallerUrl: $repo/releases/download/$tag/$zipbase.zip
  InstallerSha256: $up
ManifestType: installer
ManifestVersion: 1.12.0
EOF
	{
		cat <<EOF
# yaml-language-server: \$schema=https://aka.ms/winget-manifest.defaultLocale.1.12.0.schema.json

PackageIdentifier: $id
PackageVersion: $ver
PackageLocale: en-US
Publisher: Chad Walker
PublisherUrl: https://github.com/chad3814
PublisherSupportUrl: $repo/issues
Author: Chad Walker
PackageName: $name
PackageUrl: $repo
License: $license
LicenseUrl: $repo/blob/main/LICENSE
ShortDescription: $short
Description: $desc
EOF
		if [[ -n $moniker ]]; then echo "Moniker: $moniker"; fi
		cat <<EOF
Tags:
- bluray
- dvd
- mkv
- mkvmerge
- remux
ReleaseNotesUrl: $repo/releases/tag/$tag
ManifestType: defaultLocale
ManifestVersion: 1.12.0
EOF
	} >"$dir/$id.locale.en-US.yaml"
}

package Zenvik "zenvik_${ver}_windows_amd64" zenvik.exe zenvik "$cli" zenvik "MIT" \
	"Remux Blu-ray and DVD disc images to MKV" \
	"zenvik remuxes unencrypted Blu-ray and DVD disc images and folders to MKV with mkvmerge, which it includes (MKVToolNix, GPLv2)." \
	zenvik
package ZenvikGUI "zenvik-gui_${ver}_windows_amd64" zenvik-gui.exe zenvik-gui "$gui" Zenvik "MIT, GPL-2.0 (bundled mkvmerge)" \
	"Desktop app to remux Blu-ray and DVD disc images to MKV" \
	"Zenvik is a desktop app that queues and remuxes unencrypted Blu-ray and DVD disc images to MKV. It bundles mkvmerge from MKVToolNix (GPLv2) and needs the Microsoft Edge WebView2 runtime, which Windows 11 includes." \
	""

mkdir -p "$out/manifests/c/chad3814"
for pkg in Zenvik ZenvikGUI; do
	rm -rf "$out/manifests/c/chad3814/$pkg/$ver"
	mkdir -p "$out/manifests/c/chad3814/$pkg"
	mv "$work/out/manifests/c/chad3814/$pkg/$ver" "$out/manifests/c/chad3814/$pkg/$ver"
done
echo "rendered winget manifests for zenvik $ver into $out" >&2
