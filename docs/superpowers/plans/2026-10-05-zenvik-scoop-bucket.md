# zenvik Scoop bucket Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish `chad3814/scoop-bucket` with `zenvik` (CLI, depends on `extras/mkvtoolnix`) and `zenvik-gui` manifests, and have every final zenvik release update them after proving they install on Windows.

**Architecture:**
- **Rendering:** `scripts/scoop-render.sh` writes both manifests with `jq` from the release's `SHA256SUMS`.
- **Checking:** the bucket owns `check.ps1`, the single definition of "this bucket works", which its CI runs.
- **Release jobs:** render; run the bucket's `check.ps1` on the render on `windows-latest`; push with a deploy key.
- **Shared guard:** the downgrade check moves into `scripts/newer-version.sh`, which both the tap and the bucket use.

**Tech Stack:** bash (macOS bash 3.2 and Linux), `jq`, PowerShell 7 (`check.ps1`), Scoop, GitHub Actions, `gh`.

**Spec:** `docs/superpowers/specs/2026-10-05-zenvik-scoop-bucket-design.md`

## Global Constraints

- Bucket repo: `chad3814/scoop-bucket` (public), with manifests in `bucket/`.
- Users install with:
  - `scoop bucket add extras`
  - `scoop bucket add chad3814 https://github.com/chad3814/scoop-bucket`
  - `scoop install chad3814/zenvik` and `scoop install chad3814/zenvik-gui`
- Manifest URLs:
  - CLI: `https://github.com/chad3814/zenvik/releases/download/v<ver>/zenvik_<ver>_windows_amd64.zip`, `extract_dir` `zenvik_<ver>_windows_amd64`
  - GUI: `…/zenvik-gui_<ver>_windows_amd64.zip`, `extract_dir` `zenvik-gui_<ver>_windows_amd64`
- `autoupdate.hash`: `{"url": "https://github.com/chad3814/zenvik/releases/download/v$version/SHA256SUMS", "regex": "$sha256\\s+$basename"}` (JSON text; the regex value is `$sha256\s+$basename`).
- CLI: `bin: "zenvik.exe"`, `depends: "extras/mkvtoolnix"`, `license: "MIT"`.
- GUI:
  - `license: "MIT,GPL-2.0-only"`
  - `shortcuts: [["zenvik-gui.exe", "Zenvik"]]`
  - `notes`: `Zenvik needs the Microsoft Edge WebView2 runtime, which Windows 11 includes. It bundles mkvmerge from MKVToolNix (GPLv2; see MKVTOOLNIX-NOTICE.txt in the app folder).`
- Only final tags `^v[0-9]+\.[0-9]+\.[0-9]+$` bump the bucket.
- The secret `SCOOP_BUCKET_DEPLOY_KEY` (zenvik's `release` environment) is readable only by `scoop-push`. Never print, log, write to the workspace, or commit key material.
- Scoop on GitHub's Windows runners is installed with `iex "& {$(irm get.scoop.sh)} -RunAsAdmin"`.
- Scripts: `set -euo pipefail`; capture output into variables rather than piping into `grep -q`; `shellcheck -x` clean.
- Creating the bucket repo, pushing to it, adding the deploy key and secret, and merging or pushing zenvik are outward-facing: confirm with the user immediately before each.

## Review Focus

1. **Swapped CLI/GUI hashes:** each zip's hash must land in its own manifest. Task 2's test lists the GUI line before the CLI line in `SHA256SUMS` and asserts each manifest's hash.
2. **Decoy lines in `SHA256SUMS`:** `zenvik_9.9.90_windows_amd64.zip` and the DMG lines must not match 9.9.9. Task 2's test includes them.
3. **Escaping in `autoupdate`:** the JSON must hold the regex `$sha256\s+$basename` literally, with no shell expansion and with the backslash correctly escaped in JSON. Task 2's test reads it back with `jq` and compares it exactly.
4. **Failures `check.ps1` might miss:** Scoop is a script, and a failed `scoop install` can leave PowerShell's error state clean. `check.ps1` must check `$LASTEXITCODE` after every native or Scoop command. This can't run on macOS: the final reviewer reads it, and the bucket's first CI run (Task 3) exercises it for real.
5. **Re-running `scoop-push` after a successful push** must succeed as a no-op. The push script checks `git diff --cached --quiet`, and the downgrade guard runs first. Task 4 dry-runs the no-op path.

---

### Task 1: `scripts/newer-version.sh`, shared by the tap and the bucket

**Files:**
- Create: `scripts/newer-version.sh`, `scripts/newer-version_test.sh`
- Modify: `scripts/homebrew-should-bump.sh` (delegate the comparison)

**Interfaces:**
- Produces: `scripts/newer-version.sh vX.Y.Z <have-version>`. Exit 0 means newer, or `<have-version>` is empty. Exit 3 means not newer, with a stderr message containing `<have-version>`. Exit 2 means a usage error or a non-final tag.
- Keeps: `scripts/homebrew-should-bump.sh vX.Y.Z <tap checkout>`, unchanged in interface and exit codes, and its test, `scripts/homebrew-should-bump_test.sh`, unchanged.

- [ ] **Step 1: Write the failing test**

`scripts/newer-version_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/newer-version.sh.
#
#   scripts/newer-version_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

# expect NAME WANT TAG HAVE
expect() {
	local msg st
	msg=$("$here/newer-version.sh" "$3" "$4" 2>&1)
	st=$?
	if [[ $st -eq $2 ]]; then ok "$1"; else bad "$1" "status $st (want $2): $msg"; fi
}
expect "a newer release" 0 v1.2.0 1.1.1
expect "1.10 is newer than 1.9" 0 v1.10.0 1.9.0
expect "nothing yet" 0 v1.2.0 ""
expect "the same version" 3 v1.2.0 1.2.0
expect "an older release" 3 v1.2.0 1.2.1
expect "1.9 is older than 1.10" 3 v1.9.0 1.10.0
expect "a pre-release tag" 2 v1.2.0-rc1 1.1.1
expect "no v prefix" 2 1.2.0 1.1.1
if msg=$("$here/newer-version.sh" v1.2.0 2>&1); then bad "one argument is a usage error"; else ok "one argument is a usage error"; fi
msg=$("$here/newer-version.sh" v1.2.0 1.2.1 2>&1)
if [[ $msg == *1.2.1* ]]; then ok "a skip names the current version"; else bad "a skip names the current version" "$msg"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/newer-version_test.sh && scripts/newer-version_test.sh`
Expected: FAIL lines ("No such file or directory"), non-zero exit.

- [ ] **Step 3: Implement**

`scripts/newer-version.sh`:

```bash
#!/usr/bin/env bash
# Whether a release is newer than what a package repository (the Homebrew
# tap, the Scoop bucket) already has, so a bump never downgrades it.
#
#   scripts/newer-version.sh vX.Y.Z <have-version>
#
# Exit 0: newer, or <have-version> is empty. Exit 3: not newer (the message
# names <have-version>). Exit 2: usage, or a non-final tag.
set -euo pipefail

if [[ $# -ne 2 || ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 vX.Y.Z <have-version>" >&2
	exit 2
fi
new=${1#v}
have=$2
if [[ -z $have ]]; then
	exit 0
fi
newest=$(printf '%s\n%s\n' "$have" "$new" | sort -V | tail -1)
if [[ $new == "$have" || $newest != "$new" ]]; then
	echo "the package repository already has zenvik $have; not replacing it with $new" >&2
	exit 3
fi
```

In `scripts/homebrew-should-bump.sh`, replace everything from `if [[ -z $have ]]; then` to the end of the file with:

```bash
exec "$(dirname "$0")/newer-version.sh" "$1" "$have"
```

Keep its usage check and the `have=` extraction above that line. Update its header comment's last line to say the comparison is in `newer-version.sh`.

- [ ] **Step 4: Run the tests and lint**

Run: `chmod +x scripts/newer-version.sh && scripts/newer-version_test.sh && scripts/homebrew-should-bump_test.sh && shellcheck -x scripts/newer-version*.sh scripts/homebrew-should-bump.sh`
Expected: `10 passed, 0 failed`, then `8 passed, 0 failed`, and no shellcheck output.

- [ ] **Step 5: Commit**

```bash
git add scripts/newer-version.sh scripts/newer-version_test.sh scripts/homebrew-should-bump.sh
git commit -m "newer-version.sh: the shared never-downgrade check, used by the Homebrew guard"
```

---

### Task 2: `scripts/scoop-render.sh` and its test

**Files:**
- Create: `scripts/scoop-render.sh`, `scripts/scoop-render_test.sh`

**Interfaces:**
- Produces: `scripts/scoop-render.sh <tag> <outdir>`.
  - It writes `<outdir>/bucket/zenvik.json` and `<outdir>/bucket/zenvik-gui.json`.
  - Exit 0 on success. Exit 2 for usage or a non-final tag. Exit 1 when the download, a missing line or a malformed hash stops it; then nothing in `<outdir>` changes.
  - Env `ZENVIK_RELEASE_BASE_URL` (default `https://github.com/chad3814/zenvik`) sets where it downloads `releases/download/<tag>/SHA256SUMS` from.
  - It requires `jq`.

- [ ] **Step 1: Write the failing test**

`scripts/scoop-render_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/scoop-render.sh. Offline: SHA256SUMS comes from a
# file:// fixture. Needs jq.
#
#   scripts/scoop-render_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }
command -v jq >/dev/null || { echo "scoop-render_test.sh needs jq" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cli=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
gui=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
decoy=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc

# fixture TAG SUMS: a file:// release with SHA256SUMS
fixture() {
	local fx="$work/fx-$RANDOM"
	mkdir -p "$fx/releases/download/$1"
	printf '%s' "$2" >"$fx/releases/download/$1/SHA256SUMS"
	echo "file://$fx"
}

good="$gui  zenvik-gui_9.9.9_windows_amd64.zip
$decoy  zenvik_9.9.90_windows_amd64.zip
$decoy  zenvik-gui_9.9.9_darwin_arm64.dmg
$cli  zenvik_9.9.9_windows_amd64.zip
$decoy  zenvik-gui_9.9.90_windows_amd64.zip
"

out="$work/out"
mkdir -p "$out/bucket"
echo old >"$out/bucket/zenvik.json"
echo old >"$out/bucket/zenvik-gui.json"
if msg=$(ZENVIK_RELEASE_BASE_URL=$(fixture v9.9.9 "$good") "$here/scoop-render.sh" v9.9.9 "$out" 2>&1); then
	ok "renders a final release"
else
	bad "renders a final release" "$msg"
fi

# q FILE FILTER: jq -r on a rendered manifest
q() { jq -r "$2" "$out/bucket/$1.json" 2>&1; }
eq() { # eq NAME GOT WANT
	if [[ $2 == "$3" ]]; then ok "$1"; else bad "$1" "got [$2], want [$3]"; fi
}
eq "zenvik.json is valid JSON" "$(jq -e . "$out/bucket/zenvik.json" >/dev/null 2>&1 && echo yes)" yes
eq "zenvik-gui.json is valid JSON" "$(jq -e . "$out/bucket/zenvik-gui.json" >/dev/null 2>&1 && echo yes)" yes
eq "CLI version" "$(q zenvik .version)" 9.9.9
eq "CLI url" "$(q zenvik '.architecture."64bit".url')" https://github.com/chad3814/zenvik/releases/download/v9.9.9/zenvik_9.9.9_windows_amd64.zip
eq "CLI hash is the CLI zip's" "$(q zenvik '.architecture."64bit".hash')" "$cli"
eq "CLI extract_dir" "$(q zenvik '.architecture."64bit".extract_dir')" zenvik_9.9.9_windows_amd64
eq "CLI bin" "$(q zenvik .bin)" zenvik.exe
eq "CLI depends on Extras' mkvtoolnix" "$(q zenvik .depends)" extras/mkvtoolnix
eq "CLI checkver" "$(q zenvik .checkver)" github
eq "CLI autoupdate url" "$(q zenvik '.autoupdate.architecture."64bit".url')" 'https://github.com/chad3814/zenvik/releases/download/v$version/zenvik_$version_windows_amd64.zip'
eq "autoupdate hash regex is literal" "$(q zenvik .autoupdate.hash.regex)" '$sha256\s+$basename'
eq "autoupdate hash url" "$(q zenvik .autoupdate.hash.url)" 'https://github.com/chad3814/zenvik/releases/download/v$version/SHA256SUMS'
eq "GUI version" "$(q zenvik-gui .version)" 9.9.9
eq "GUI hash is the GUI zip's" "$(q zenvik-gui '.architecture."64bit".hash')" "$gui"
eq "GUI extract_dir" "$(q zenvik-gui '.architecture."64bit".extract_dir')" zenvik-gui_9.9.9_windows_amd64
eq "GUI shortcut" "$(q zenvik-gui '.shortcuts[0] | join(",")')" zenvik-gui.exe,Zenvik
eq "GUI license" "$(q zenvik-gui .license)" MIT,GPL-2.0-only
eq "GUI has no dependency" "$(q zenvik-gui '.depends // "none"')" none
eq "GUI autoupdate regex" "$(q zenvik-gui .autoupdate.hash.regex)" '$sha256\s+$basename'
if grep -q file: "$out/bucket/zenvik.json" "$out/bucket/zenvik-gui.json"; then bad "written URLs never use the download override"; else ok "written URLs never use the download override"; fi

# expect_fail NAME WANT TAG BASE: that status, and a fresh outdir left empty
expect_fail() {
	local o="$work/fail-$RANDOM" st msg
	mkdir -p "$o"
	msg=$(ZENVIK_RELEASE_BASE_URL=$4 "$here/scoop-render.sh" "$3" "$o" 2>&1)
	st=$?
	if [[ $st -ne $2 ]]; then
		bad "$1" "status $st (want $2): $msg"
	elif [[ -n $(ls -A "$o") ]]; then
		bad "$1" "wrote into outdir"
	else
		ok "$1"
	fi
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/scoop-render_test.sh && scripts/scoop-render_test.sh`
Expected: FAIL lines ("No such file or directory"), non-zero exit.

- [ ] **Step 3: Implement**

`scripts/scoop-render.sh`:

```bash
#!/usr/bin/env bash
# Write the Scoop manifests for a final zenvik release, for
# chad3814/scoop-bucket (the release workflow's scoop jobs run this):
#
#   scripts/scoop-render.sh v1.2.0 <outdir>
#
# writes <outdir>/bucket/zenvik.json (the CLI zip; depends on Extras'
# mkvtoolnix) and <outdir>/bucket/zenvik-gui.json (the desktop app zip),
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
    depends: "extras/mkvtoolnix",
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
```

Inside a jq string literal, `$version` is plain text: jq interpolates only `\(…)`. `"\\s"` in jq source is the two characters `\s`, which jq writes into JSON as `"\\s"`. If jq reports an error on a `$name` inside a string, the test will show it, and the fix is to keep those words as written. Here they're string text, not variable references.

- [ ] **Step 4: Run the tests and lint**

Run: `chmod +x scripts/scoop-render.sh && scripts/scoop-render_test.sh && shellcheck -x scripts/scoop-render*.sh`
Expected: `25 passed, 0 failed`. If shellcheck warns SC2016 about `$version` in single quotes, add `# shellcheck disable=SC2016` above each `jq -n` line, with a comment that those are Scoop placeholders.

- [ ] **Step 5: Render the real v1.2.0 as a smoke test**

Run: `scripts/scoop-render.sh v1.2.0 "$TMPDIR/scoop-v120" && jq -r '.architecture."64bit".hash' "$TMPDIR/scoop-v120"/bucket/*.json`
Expected: two hashes equal to the `zenvik-gui_1.2.0_windows_amd64.zip` and `zenvik_1.2.0_windows_amd64.zip` lines of `gh release download v1.2.0 -R chad3814/zenvik -p SHA256SUMS -O -` (GUI `0caa31d8…`, CLI `4980a75d…`). Keep `$TMPDIR/scoop-v120` for Task 3.

- [ ] **Step 6: Commit**

```bash
git add scripts/scoop-render.sh scripts/scoop-render_test.sh
git commit -m "scoop-render.sh: write the bucket's zenvik and zenvik-gui manifests for a final release"
```

---

### Task 3: The bucket repo `chad3814/scoop-bucket`

**Files** (in a scratch checkout `$TMPDIR/scoop-bucket`):
- Create: `check.ps1`, `.github/workflows/ci.yml`, `README.md`
- Copy from Task 2: `bucket/zenvik.json`, `bucket/zenvik-gui.json` (v1.2.0)

**Interfaces:**
- Consumes: Task 2's render in `$TMPDIR/scoop-v120/bucket/`.
- Produces: `check.ps1` at the bucket root. Exit 0 when all checks pass. Otherwise it exits 1, printing `check.ps1: failed at: <step>`. Task 4's `scoop-check` job runs it from a clone with the rendered `bucket/` copied over.

- [ ] **Step 1: Create the scratch checkout**

```bash
b="$TMPDIR/scoop-bucket"
rm -rf "$b" && mkdir -p "$b/.github/workflows" && cd "$b" && git init -q -b main
cp -R "$TMPDIR/scoop-v120/bucket" "$b/"
```

- [ ] **Step 2: Write `check.ps1`**

```powershell
#Requires -Version 7
# Prove this bucket's manifests install and work. CI runs it on every push;
# zenvik's release workflow runs it on freshly rendered manifests before
# pushing them here. Needs Scoop on Windows.
#
#   ./check.ps1
#
# Every native or Scoop command's exit code is checked: Scoop is a script,
# and a failed install doesn't always raise a PowerShell error.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$script:step = 'setup'
function Step([string]$name) { $script:step = $name; Write-Host "==> $name" }
function Run {
    & $args[0] @($args | Select-Object -Skip 1)
    if ($LASTEXITCODE -ne 0) { throw "$($args -join ' ') exited with $LASTEXITCODE" }
}
trap {
    Write-Host "check.ps1: failed at: $script:step"
    Write-Host $_
    exit 1
}

Step 'add the extras bucket'
$buckets = @(scoop bucket list | ForEach-Object { $_.Name })
if ($buckets -notcontains 'extras') { Run scoop bucket add extras }

Step 'read the manifests'
$m = @{}
foreach ($n in 'zenvik', 'zenvik-gui') {
    $j = Get-Content (Join-Path $PSScriptRoot "bucket/$n.json") -Raw | ConvertFrom-Json
    $a = $j.architecture.'64bit'
    foreach ($v in $j.version, $a.url, $a.hash, $a.extract_dir) {
        if (-not $v) { throw "$n.json lacks version, url, hash or extract_dir" }
    }
    $m[$n] = $j
}
if ($m['zenvik'].version -ne $m['zenvik-gui'].version) { throw 'the two manifests have different versions' }
$ver = $m['zenvik'].version

Step 'install zenvik'
Run scoop install (Join-Path $PSScriptRoot 'bucket/zenvik.json')
Step 'zenvik --version'
$out = (Run zenvik --version) -join "`n"
if ($out -notlike "zenvik v$ver*") { throw "zenvik --version printed: $out" }
Step 'mkvmerge from the Extras dependency'
$out = (Run mkvmerge --version) -join "`n"
if ($out -notlike 'mkvmerge v*') { throw "mkvmerge --version printed: $out" }

Step 'install zenvik-gui'
Run scoop install (Join-Path $PSScriptRoot 'bucket/zenvik-gui.json')
$dir = ((Run scoop prefix zenvik-gui) | Select-Object -Last 1).Trim()
Step "the app's files in $dir"
if (-not (Test-Path (Join-Path $dir 'zenvik-gui.exe'))) { throw "no zenvik-gui.exe in $dir" }
$out = (Run (Join-Path $dir 'mkvmerge.exe') --version) -join "`n"
if ($out -notlike 'mkvmerge v*') { throw "the bundled mkvmerge.exe printed: $out" }

Step 'uninstall both'
Run scoop uninstall zenvik-gui
Run scoop uninstall zenvik
Write-Host 'check.ps1: all checks passed'
```

If `pwsh` is installed locally, run `pwsh -NoProfile -Command '$e=$null; [void][System.Management.Automation.Language.Parser]::ParseFile("check.ps1",[ref]$null,[ref]$e); if ($e) { $e; exit 1 }'`. Expected: no output, meaning it parses. If `pwsh` isn't installed, CI in Step 6 is the first run.

- [ ] **Step 3: Write `.github/workflows/ci.yml`**

```yaml
name: ci

on:
  push:
  pull_request:

permissions:
  contents: read

jobs:
  check:
    runs-on: windows-latest
    defaults:
      run:
        shell: pwsh
    steps:
      - uses: actions/checkout@v5
      - name: Install Scoop
        run: |
          iex "& {$(irm get.scoop.sh)} -RunAsAdmin"
          "$env:USERPROFILE\scoop\shims" | Out-File -FilePath $env:GITHUB_PATH -Append -Encoding utf8
      - name: Does Scoop resolve extras/mkvtoolnix without the extras bucket? (informational)
        continue-on-error: true
        run: |
          scoop install .\bucket\zenvik.json
          Write-Host "scoop install without extras exited with $LASTEXITCODE"
          scoop list
          scoop uninstall zenvik
          scoop uninstall mkvtoolnix
      - run: .\check.ps1
```

Run `actionlint .github/workflows/ci.yml` (or `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/ci.yml` from inside the checkout). Expected: no output.

- [ ] **Step 4: Write `README.md`**

````markdown
# chad3814/scoop-bucket

[Scoop](https://scoop.sh) manifests for [zenvik](https://github.com/chad3814/zenvik), which remuxes Blu-ray and DVD disc images to MKV.

```powershell
scoop bucket add extras                                             # for MKVToolNix
scoop bucket add chad3814 https://github.com/chad3814/scoop-bucket
scoop install chad3814/zenvik        # the zenvik command-line tool
scoop install chad3814/zenvik-gui    # the Zenvik desktop app
```

- `zenvik` installs `zenvik.exe` on your PATH and [MKVToolNix](https://mkvtoolnix.download) (for `mkvmerge`) from the Extras bucket.
- `zenvik-gui` installs the Zenvik app, with a Start-menu shortcut. It includes its own `mkvmerge`, and needs the Microsoft Edge WebView2 runtime, which Windows 11 includes.

zenvik's release workflow updates these manifests on every release, after `check.ps1` has proved they install. To check by hand on Windows with Scoop: `./check.ps1`.
````

- [ ] **Step 5: Commit, then ask the user before creating and pushing the public repo**

```bash
cd "$b" && git add -A && git commit -m "zenvik 1.2.0"
```

After the user confirms:

```bash
cd "$b" && gh repo create chad3814/scoop-bucket --public \
  --description "Scoop manifests for zenvik" --source . --push
```

- [ ] **Step 6: Watch its CI, and read the Extras answer**

```bash
sleep 15
run=$(gh run list -R chad3814/scoop-bucket -L 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$run" -R chad3814/scoop-bucket --exit-status
gh run view "$run" -R chad3814/scoop-bucket --log | grep -E 'without extras exited|check.ps1:|==>' | head -30
```

Expected:
- the `check` job succeeds and the log shows `check.ps1: all checks passed`;
- the informational step's `exited with` line answers the Extras question.

Record that answer in the ledger. If CI fails, fix the cause in the scratch checkout and push again. If the fix belongs in the manifest text, fix `scripts/scoop-render.sh` and its test in the zenvik worktree too, then re-render.

- [ ] **Step 7: Replace the scratch checkout with the standard local layout**

```bash
cd ~/Projects && "$HOME/.claude/skills/project-setup/project-setup.sh" git@github.com:chad3814/scoop-bucket.git
rm -rf "$TMPDIR/scoop-bucket" "$TMPDIR/scoop-v120"
```

Expected: `~/Projects/scoop-bucket/worktrees/main` holds the five files.

---

### Task 4: Release jobs and docs (zenvik repo)

**Files:**
- Modify: `.github/workflows/release.yml` (append three jobs)
- Modify: `README.md` (install section), `CLAUDE.md` (Commands list)

**Interfaces:**
- Consumes:
  - `scripts/scoop-render.sh <tag> <outdir>` (Task 2);
  - `scripts/newer-version.sh vX.Y.Z <have>` (Task 1);
  - the bucket's `check.ps1` (Task 3);
  - secret `SCOOP_BUCKET_DEPLOY_KEY` (Task 5).
- Produces: jobs `scoop-render`, `scoop-check` and `scoop-push`, and an artifact named `scoop`.

- [ ] **Step 1: Append the jobs to `.github/workflows/release.yml`**

```yaml
  # Scoop (chad3814/scoop-bucket), final releases only: render the manifests
  # from what was just published, prove them with the bucket's own check.ps1
  # on Windows, then push. Only scoop-push can read the deploy key. A failure
  # leaves the release published and the bucket on the previous version;
  # re-run the failed job to retry.
  scoop-render:
    if: ${{ !contains(github.ref_name, '-') }}
    needs: publish
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
      - run: scripts/scoop-render.sh "$GITHUB_REF_NAME" scoop
      - uses: actions/upload-artifact@v4
        with:
          name: scoop
          path: scoop

  scoop-check:
    needs: scoop-render
    runs-on: windows-latest
    defaults:
      run:
        shell: pwsh
    steps:
      - name: Install Scoop
        run: |
          iex "& {$(irm get.scoop.sh)} -RunAsAdmin"
          "$env:USERPROFILE\scoop\shims" | Out-File -FilePath $env:GITHUB_PATH -Append -Encoding utf8
      - run: git clone --depth 1 https://github.com/chad3814/scoop-bucket.git "$env:RUNNER_TEMP\bucket"
      - uses: actions/download-artifact@v5
        with:
          name: scoop
          path: ${{ runner.temp }}/rendered
      - run: |
          Copy-Item -Recurse -Force "$env:RUNNER_TEMP\rendered\bucket\*" "$env:RUNNER_TEMP\bucket\bucket\"
          & "$env:RUNNER_TEMP\bucket\check.ps1"
          if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

  scoop-push:
    needs: scoop-check
    runs-on: ubuntu-latest
    environment: release
    concurrency: { group: scoop-bucket-push, cancel-in-progress: false }
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
      - uses: actions/download-artifact@v5
        with:
          name: scoop
          path: ${{ runner.temp }}/rendered
      - name: Push the bump to chad3814/scoop-bucket
        env:
          DEPLOY_KEY: ${{ secrets.SCOOP_BUCKET_DEPLOY_KEY }}
        run: |
          if [[ -z $DEPLOY_KEY ]]; then
            echo "SCOOP_BUCKET_DEPLOY_KEY is not set in the release environment" >&2
            exit 1
          fi
          eval "$(ssh-agent -s)" >/dev/null
          trap 'ssh-agent -k >/dev/null' EXIT
          ssh-add - <<<"$DEPLOY_KEY" >/dev/null
          unset DEPLOY_KEY
          mkdir -p ~/.ssh
          curl -fsSL https://api.github.com/meta | jq -r '.ssh_keys[] | "github.com " + .' >~/.ssh/known_hosts
          git clone git@github.com:chad3814/scoop-bucket.git "$RUNNER_TEMP/bucket"
          # Never downgrade: a re-run of an older release's push (or two
          # releases racing) leaves a newer bucket alone.
          have=$(jq -r '.version // empty' "$RUNNER_TEMP/bucket/bucket/zenvik.json" 2>/dev/null || true)
          st=0
          scripts/newer-version.sh "$GITHUB_REF_NAME" "$have" || st=$?
          if [[ $st -eq 3 ]]; then
            exit 0
          elif [[ $st -ne 0 ]]; then
            exit "$st"
          fi
          cp -R "$RUNNER_TEMP/rendered/bucket/." "$RUNNER_TEMP/bucket/bucket/"
          cd "$RUNNER_TEMP/bucket"
          git add bucket
          if git diff --cached --quiet; then
            echo "the bucket already has zenvik ${GITHUB_REF_NAME#v}"
            exit 0
          fi
          git -c user.name='github-actions[bot]' \
            -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
            commit -m "zenvik ${GITHUB_REF_NAME#v}"
          git push origin HEAD:main
```

- [ ] **Step 2: Lint**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest`
Expected: no output.

- [ ] **Step 3: Dry-run the push's no-op path (Review Focus 5)**

```bash
d=$(mktemp -d) && git clone -q https://github.com/chad3814/scoop-bucket.git "$d/bucket"
scripts/scoop-render.sh v1.2.0 "$d/rendered" && cp -R "$d/rendered/bucket/." "$d/bucket/bucket/"
(cd "$d/bucket" && git add bucket && git diff --cached --quiet && echo "no-op: nothing to push")
scripts/newer-version.sh v1.2.0 "$(jq -r .version "$d/bucket/bucket/zenvik.json")"; echo "guard exit $?"
rm -rf "$d"
```

Expected: `no-op: nothing to push`, which also proves the render is reproducible. Then `guard exit 3`, because the same version must not bump.

- [ ] **Step 4: Docs**

`README.md`: in the install section, after the Homebrew paragraph, add:

```markdown
With [Scoop](https://scoop.sh) on Windows: `scoop bucket add extras`, `scoop bucket add chad3814 https://github.com/chad3814/scoop-bucket`, then `scoop install chad3814/zenvik` (it installs MKVToolNix with it) or `scoop install chad3814/zenvik-gui` for the desktop app.
```

`CLAUDE.md`: add after the Homebrew tap bullet:

```markdown
- Scoop bucket (`chad3814/scoop-bucket`: manifests `bucket/zenvik.json` with `depends: extras/mkvtoolnix` and `bucket/zenvik-gui.json`; its `check.ps1` is the test): `scripts/scoop-render.sh vX.Y.Z outdir` writes both from a published release's `SHA256SUMS` (tests: `scripts/scoop-render_test.sh`, needs jq). For final tags only, the release workflow's `scoop-render`, `scoop-check` (runs the bucket's `check.ps1` on `windows-latest`) and `scoop-push` jobs update the bucket; `scripts/newer-version.sh` (tests: `scripts/newer-version_test.sh`) is the never-downgrade check both the tap and the bucket use. Only `scoop-push` reads the `release` environment secret `SCOOP_BUCKET_DEPLOY_KEY`, a write deploy key on the bucket. Never print or commit it.
```

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yml README.md CLAUDE.md
git commit -m "Release: bump the Scoop bucket's zenvik and zenvik-gui manifests on final releases, after the bucket's check.ps1 passes on them"
```

---

### Task 5: Deploy key and secret

**Files:** none.

**Interfaces:**
- Produces:
  - a write-enabled deploy key titled `zenvik release` on `chad3814/scoop-bucket`;
  - secret `SCOOP_BUCKET_DEPLOY_KEY` in `chad3814/zenvik`'s `release` environment.

- [ ] **Step 1: Ask the user, then create and install the key in one step**

```bash
k=$(mktemp -d) && chmod 700 "$k" \
  && ssh-keygen -q -t ed25519 -N '' -C zenvik-release -f "$k/key" \
  && gh repo deploy-key add "$k/key.pub" -R chad3814/scoop-bucket --title "zenvik release" --allow-write \
  && gh secret set SCOOP_BUCKET_DEPLOY_KEY --env release -R chad3814/zenvik <"$k/key"; \
  rm -rf "$k"
```

- [ ] **Step 2: Verify without reading the secret**

```bash
gh repo deploy-key list -R chad3814/scoop-bucket
gh secret list --env release -R chad3814/zenvik | cut -f1
```

Expected: a `zenvik release` read-write key, and `SCOOP_BUCKET_DEPLOY_KEY` among the secret names.

---

### Task 6: Merge, push, and the first automated bump

- [ ] **Step 1: Full local checks**

Run:
- `scripts/newer-version_test.sh`
- `scripts/homebrew-should-bump_test.sh`
- `scripts/scoop-render_test.sh`
- `scripts/homebrew-render_test.sh` (then `brew developer off`)
- `shellcheck -x scripts/*.sh`
- `go run github.com/rhysd/actionlint/cmd/actionlint@latest`

Expected: all pass.

- [ ] **Step 2: Ask the user, then merge and push** `feat/scoop-bucket` into `main`, and watch CI on main.

- [ ] **Step 3: First automated bump**, at the user's next final release:
  - watch `scoop-render`, `scoop-check` and `scoop-push`;
  - confirm the bucket has a `zenvik X.Y.Z` commit by `github-actions[bot]` and that its CI passes.

  The user can then run `scoop update; scoop install chad3814/zenvik` on Windows.
