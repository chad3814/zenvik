#Requires -Version 7
# Prove zenvik's winget manifests install and work on this Windows machine:
# validate both packages, install them from the local manifests, check that
# zenvik runs and finds its bundled mkvmerge through winget's link, that the
# app's bundled mkvmerge runs, then uninstall. The release workflow runs it
# before opening winget-pkgs PRs; CI runs it on the latest release's
# manifests so a fix is proven before a tag.
#
#   ./scripts/winget-check.ps1 -Manifests <dir>/manifests/c/chad3814 -Version 1.3.1
#
# Needs admin (it enables local manifests and updates winget).
param(
  [Parameter(Mandatory)][string]$Manifests,
  [Parameter(Mandatory)][string]$Version
)
$ErrorActionPreference = 'Stop'
function Run { $cmd = $args[0]; $rest = @($args | Select-Object -Skip 1); & $cmd @rest; if ($LASTEXITCODE -ne 0) { throw "$($args -join ' ') exited with $LASTEXITCODE" } }
# Always bring winget up to date: the runner's can predate the
# manifests' schema (1.12), and it then warns on every header. PowerShell 7
# installs modules from the Gallery without a NuGet provider bootstrap.
Set-PSRepository -Name PSGallery -InstallationPolicy Trusted
Install-Module Microsoft.WinGet.Client -Scope AllUsers -Force -AllowClobber
Import-Module Microsoft.WinGet.Client
Repair-WinGetPackageManager -Latest -Force
if (-not (Get-Command winget -ErrorAction SilentlyContinue)) { throw 'winget is not on PATH after Repair-WinGetPackageManager' }
Run winget --version
Run winget settings --enable LocalManifestFiles
$ver = $Version
$root = $Manifests
# Portable installs go to user or machine scope; look in both.
$links = @("$env:LOCALAPPDATA\Microsoft\WinGet\Links", "$env:ProgramFiles\WinGet\Links")
$pkgs = @("$env:LOCALAPPDATA\Microsoft\WinGet\Packages", "$env:ProgramFiles\WinGet\Packages")
foreach ($p in 'Zenvik', 'ZenvikGUI') {
  $dir = "$root\$p\$ver"
  # winget validate exits non-zero on mere warnings; what counts
  # is that validation succeeded.
  $v = (& winget validate --manifest $dir 2>&1) -join "`n"
  Write-Host $v
  if ($v -notmatch 'Manifest validation succeeded') { throw "winget validate failed for ${p}: $v" }
  Run winget install --manifest $dir --accept-package-agreements --accept-source-agreements --disable-interactivity
}
$cli = $links | ForEach-Object { Join-Path $_ 'zenvik.exe' } | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $cli) { throw 'no zenvik.exe link after install' }
$out = (Run $cli --version) -join "`n"
if ($out -notlike "zenvik v$ver*") { throw "zenvik --version printed: $out" }
# Through winget's link, zenvik must find the mkvmerge.exe bundled
# beside its real executable (doctor names the source).
$doc = (& $cli doctor 2>&1) -join "`n"
if ($doc -notmatch 'mkvmerge: .*, bundled\)') { throw "zenvik doctor didn't use the bundled mkvmerge: $doc" }
foreach ($id in 'chad3814.Zenvik', 'chad3814.ZenvikGUI') {
  $home_ = $pkgs | ForEach-Object { Get-ChildItem $_ -Directory -Filter "${id}_*" -ErrorAction SilentlyContinue } | Select-Object -First 1
  if (-not $home_) { throw "no package folder for $id" }
  $mk = Get-ChildItem $home_.FullName -Recurse -Filter mkvmerge.exe | Select-Object -First 1
  if (-not $mk) { throw "no bundled mkvmerge.exe for $id" }
  $out = (Run $mk.FullName --version) -join "`n"
  if ($out -notlike 'mkvmerge v*') { throw "$id's mkvmerge.exe printed: $out" }
}
# Packages installed from a local manifest aren't listed under their winget
# ID, so uninstall by the same manifest.
foreach ($p in 'ZenvikGUI', 'Zenvik') {
  Run winget uninstall --manifest "$root\$p\$ver" --accept-source-agreements --disable-interactivity
}
Write-Host 'winget-check: all checks passed'
