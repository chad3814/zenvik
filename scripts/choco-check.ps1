#Requires -Version 7
# Prove zenvik's Chocolatey packages install and work on this Windows
# machine: pack both from the rendered sources, install them from the local
# .nupkg files, check that zenvik runs and finds its bundled mkvmerge through
# Chocolatey's shim, that each package's mkvmerge runs, that the app has its
# Start-menu shortcut, then uninstall. The release workflow and the
# choco-resubmit workflow run it before pushing.
#
#   ./scripts/choco-check.ps1 -Source <dir>/choco -Version 1.3.0 -Nupkg <dir>/nupkg
#
# -Source holds choco-render.sh's output (zenvik/ and zenvik-gui/); the
# packed .nupkg files land in -Nupkg. Needs admin (choco install).
param(
  [Parameter(Mandatory)][string]$Source,
  [Parameter(Mandatory)][string]$Version,
  [Parameter(Mandatory)][string]$Nupkg
)
$ErrorActionPreference = 'Stop'
function Run { $cmd = $args[0]; $rest = @($args | Select-Object -Skip 1); & $cmd @rest; if ($LASTEXITCODE -ne 0) { throw "$($args -join ' ') exited with $LASTEXITCODE" } }
New-Item -ItemType Directory -Force $Nupkg | Out-Null
foreach ($id in 'zenvik', 'zenvik-gui') {
  Run choco pack "$Source\$id\$id.nuspec" --outputdirectory $Nupkg
  Run choco install $id --source $Nupkg -y --no-progress
}
$out = (Run zenvik --version) -join "`n"
if ($out -notlike "zenvik v$Version*") { throw "zenvik --version printed: $out" }
if (Test-Path "$env:ChocolateyInstall\bin\mkvmerge.exe") { throw 'the bundled mkvmerge.exe got a shim on PATH' }
# Through Chocolatey's shim, zenvik must find the mkvmerge.exe bundled
# beside its real executable (doctor names the source).
$doc = (& zenvik doctor 2>&1) -join "`n"
if ($doc -notmatch 'mkvmerge: .*, bundled\)') { throw "zenvik doctor didn't use the bundled mkvmerge: $doc" }
foreach ($id in 'zenvik', 'zenvik-gui') {
  $mk = Get-ChildItem "$env:ChocolateyInstall\lib\$id\tools" -Recurse -Filter mkvmerge.exe | Select-Object -First 1
  if (-not $mk) { throw "no bundled mkvmerge.exe in $id" }
  $out = (Run $mk.FullName --version) -join "`n"
  if ($out -notlike 'mkvmerge v*') { throw "$id's mkvmerge.exe printed: $out" }
}
$lnk = Join-Path ([Environment]::GetFolderPath('CommonPrograms')) 'Zenvik.lnk'
if (-not (Test-Path $lnk)) { throw "no Start-menu shortcut at $lnk" }
foreach ($id in 'zenvik-gui', 'zenvik') { Run choco uninstall $id -y }
if (Test-Path $lnk) { throw 'uninstall left the Start-menu shortcut' }
Write-Host 'choco-check: all checks passed'
