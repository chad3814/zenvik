#!/usr/bin/env bash
# Write the Chocolatey package sources for a final zenvik release: zenvik
# (the CLI zip, which bundles mkvmerge.exe) and zenvik-gui (the desktop app
# zip). The release workflow's choco jobs run this:
#
#   scripts/choco-render.sh v1.3.0 <outdir>
#
# Each package downloads its release zip, checks its SHA-256 (from the
# release's SHA256SUMS), and unpacks it; the bundled mkvmerge.exe gets no
# shim, and the app gets a windowed shim and a Start-menu shortcut. Nothing
# in <outdir> changes unless both render. Downloads come from
# ZENVIK_RELEASE_BASE_URL (default the GitHub repo). The icon is served by
# jsDelivr at the tag (the community repository rejects
# raw.githubusercontent.com), and <copyright> comes from LICENSE.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 vX.Y.Z <outdir>" >&2
	exit 2
fi
tag=$1
out=$2
if [[ ! $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "choco-render: $tag is not a final release tag (vX.Y.Z)" >&2
	exit 2
fi
ver=${tag#v}
base=${ZENVIK_RELEASE_BASE_URL:-https://github.com/chad3814/zenvik}
repo=https://github.com/chad3814/zenvik
here=$(cd "$(dirname "$0")" && pwd)
die() { echo "choco-render: $*" >&2; exit 1; }

# The nuspec's <copyright> is LICENSE's own line, so the year never goes
# stale here.
copyright=$(grep -m1 '^Copyright' "$here/../LICENSE") || die "LICENSE has no Copyright line"

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

# fill FILE NAME=VALUE...: replace @@NAME@@ placeholders in FILE
fill() {
	local f=$1 kv
	shift
	for kv in "$@"; do
		sed -i.bak "s|@@${kv%%=*}@@|${kv#*=}|g" "$f" && rm -f "$f.bak"
	done
}

nuspec() { # nuspec ID TITLE SUMMARY DESCRIPTION FILE
	cat >"$5" <<'EOF'
<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd">
  <metadata>
    <id>@@ID@@</id>
    <version>@@VER@@</version>
    <title>@@TITLE@@</title>
    <authors>Chad Walker</authors>
    <owners>chad3814</owners>
    <projectUrl>@@REPO@@</projectUrl>
    <iconUrl>https://cdn.jsdelivr.net/gh/chad3814/zenvik@@@TAG@@/gui/build/appicon.png</iconUrl>
    <copyright>@@COPYRIGHT@@</copyright>
    <licenseUrl>@@REPO@@/blob/main/LICENSE</licenseUrl>
    <requireLicenseAcceptance>false</requireLicenseAcceptance>
    <projectSourceUrl>@@REPO@@</projectSourceUrl>
    <packageSourceUrl>@@REPO@@/blob/main/scripts/choco-render.sh</packageSourceUrl>
    <docsUrl>@@REPO@@#readme</docsUrl>
    <bugTrackerUrl>@@REPO@@/issues</bugTrackerUrl>
    <tags>bluray dvd mkv remux mkvmerge</tags>
    <summary>@@SUMMARY@@</summary>
    <description>@@DESCRIPTION@@</description>
    <releaseNotes>@@REPO@@/releases/tag/@@TAG@@</releaseNotes>
  </metadata>
  <files>
    <file src="tools\**" target="tools" />
  </files>
</package>
EOF
	fill "$5" "ID=$1" "VER=$ver" "TITLE=$2" "REPO=$repo" "TAG=$tag" "COPYRIGHT=$copyright" "SUMMARY=$3" "DESCRIPTION=$4"
}

install_ps1() { # install_ps1 FILE ZIPBASE HASH
	cat >"$1" <<'EOF'
$ErrorActionPreference = 'Stop'
$toolsDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Install-ChocolateyZipPackage -PackageName $env:ChocolateyPackageName `
  -Url64bit '@@URL@@' -Checksum64 '@@SHA@@' -ChecksumType64 'sha256' `
  -UnzipLocation $toolsDir
$app = Join-Path $toolsDir '@@DIR@@'
# The bundled mkvmerge.exe is zenvik's own: no shim for it on PATH.
New-Item -ItemType File -Force -Path (Join-Path $app 'mkvmerge.exe.ignore') | Out-Null
EOF
	fill "$1" "URL=$repo/releases/download/$tag/$2.zip" "SHA=$3" "DIR=$2"
}

c="$work/out/zenvik"
g="$work/out/zenvik-gui"
mkdir -p "$c/tools" "$g/tools"
nuspec zenvik zenvik "Remux Blu-ray and DVD disc images to MKV" \
	"zenvik remuxes unencrypted Blu-ray and DVD disc images and folders to MKV. It includes mkvmerge from MKVToolNix (GPLv2; see MKVTOOLNIX-NOTICE.txt in the package folder)." \
	"$c/zenvik.nuspec"
install_ps1 "$c/tools/chocolateyinstall.ps1" "zenvik_${ver}_windows_amd64" "$cli"
nuspec zenvik-gui Zenvik "Desktop app to remux Blu-ray and DVD disc images to MKV" \
	"Zenvik is a desktop app that queues and remuxes unencrypted Blu-ray and DVD disc images to MKV. It bundles mkvmerge from MKVToolNix (GPLv2) and needs the Microsoft Edge WebView2 runtime, which Windows 11 includes." \
	"$g/zenvik-gui.nuspec"
install_ps1 "$g/tools/chocolateyinstall.ps1" "zenvik-gui_${ver}_windows_amd64" "$gui"
cat >>"$g/tools/chocolateyinstall.ps1" <<'EOF'
# A windowed shim, and a Start-menu shortcut.
New-Item -ItemType File -Force -Path (Join-Path $app 'zenvik-gui.exe.gui') | Out-Null
$lnk = Join-Path ([Environment]::GetFolderPath('CommonPrograms')) 'Zenvik.lnk'
Install-ChocolateyShortcut -ShortcutFilePath $lnk -TargetPath (Join-Path $app 'zenvik-gui.exe') -WorkingDirectory $app
EOF
cat >"$g/tools/chocolateyuninstall.ps1" <<'EOF'
$lnk = Join-Path ([Environment]::GetFolderPath('CommonPrograms')) 'Zenvik.lnk'
Remove-Item -Force -ErrorAction SilentlyContinue $lnk
EOF

mkdir -p "$out"
rm -rf "$out/zenvik" "$out/zenvik-gui"
mv "$c" "$out/zenvik"
mv "$g" "$out/zenvik-gui"
echo "rendered Chocolatey packages for zenvik $ver into $out" >&2
