#Requires -Version 7
# Push zenvik's packed Chocolatey packages to the community feed. Reads the
# API key from CHOCO_API_KEY. Without -Resubmit, a version the feed already
# has (or has in moderation) is skipped, as is a version older than the
# feed's newest; with -Resubmit, both packages are pushed again regardless,
# which is how a version a moderator sent back is corrected.
#
#   ./scripts/choco-push.ps1 -Nupkg <dir>/nupkg -Version 1.3.0 [-Resubmit]
param(
  [Parameter(Mandatory)][string]$Nupkg,
  [Parameter(Mandatory)][string]$Version,
  [switch]$Resubmit
)
$ErrorActionPreference = 'Stop'
if (-not $env:CHOCO_API_KEY) { throw 'CHOCO_API_KEY is not set' }
$ver = [version]$Version
$feed = 'https://community.chocolatey.org/api/v2'
foreach ($id in 'zenvik', 'zenvik-gui') {
  $file = "$Nupkg\$id.$ver.nupkg"
  $have = @()
  if (-not $Resubmit) {
    try {
      Invoke-WebRequest "$feed/Packages(Id='$id',Version='$ver')" -UseBasicParsing | Out-Null
      Write-Host "$id $ver is already on the feed (or in moderation); skipping"
      continue
    } catch { }
    $xml = [xml](Invoke-WebRequest "$feed/FindPackagesById()?id='$id'" -UseBasicParsing).Content
    $have = @($xml.feed.entry | ForEach-Object { $_.properties.Version } | Where-Object { $_ -match '^\d+\.\d+\.\d+$' } | ForEach-Object { [version]$_ } | Sort-Object)
    if ($have.Count -gt 0 -and $have[-1] -ge $ver) {
      Write-Host "${id}: the feed already has $($have[-1]); skipping"
      continue
    }
  }
  $out = choco push $file --source https://push.chocolatey.org/ --api-key $env:CHOCO_API_KEY 2>&1 | Out-String
  Write-Host $out
  if ($LASTEXITCODE -ne 0) {
    if ($Resubmit) { throw "choco push $id (resubmit) exited with $LASTEXITCODE" }
    if ($out -match 'already exists|409|Conflict') { Write-Host "$id $ver was already submitted; skipping"; continue }
    # A new package's first version must be approved before the feed takes
    # another: a 403 while nothing is approved yet means waiting on
    # moderation (the feed hides unapproved versions).
    if ($out -match '403' -and $have.Count -eq 0) {
      Write-Warning "$id $ver was refused (403) while $id has no approved version yet; it's waiting on moderation of an earlier version: https://community.chocolatey.org/packages/$id"
      continue
    }
    throw "choco push $id exited with $LASTEXITCODE"
  }
}
# Every package was pushed or deliberately skipped (real failures threw
# above); don't let a skipped push's exit code fail the step.
exit 0
