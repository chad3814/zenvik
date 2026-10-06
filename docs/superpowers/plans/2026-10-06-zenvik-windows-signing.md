# zenvik Windows code signing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Final releases' Windows `zenvik.exe` and `zenvik-gui.exe` are Authenticode-signed through SignPath Foundation, after a human approval, and dormant until `SIGNPATH_ORGANIZATION_ID` is set.

**Architecture:**
- **Artifacts:** release artifacts get a `release-` prefix, and `publish` downloads only those. The Windows zips are built as `windows-unsigned-*`.
- **`windows-sign`** either passes them through, as decided by `scripts/windows-sign-mode.sh`, or submits them to SignPath and waits. Either way it uploads `release-windows`.
- **`windows-sign-check`** runs them on Windows and, when signing ran, verifies the signatures.
- **Configuration:** the SignPath artifact configuration and the setup steps live in `signing/`. The README carries SignPath's required "Code signing policy".

**Tech Stack:** GitHub Actions; bash; Ruby's standard YAML (structural workflow test); PowerShell 7; SignPath action `signpath/github-action-submit-signing-request` v3.0 (pinned SHA).

**Spec:** `docs/superpowers/specs/2026-10-06-zenvik-windows-signing-design.md`

## Global Constraints

- Sign only final tags `^v[0-9]+\.[0-9]+\.[0-9]+$`, and only when the repository variable `SIGNPATH_ORGANIZATION_ID` is non-empty. Otherwise pass the zips through unchanged.
- SignPath settings:
  - project slug `zenvik`, signing policy slug `release-signing`, the default artifact configuration;
  - action `signpath/github-action-submit-signing-request@f6d04783b4569d051e0c80105fe66e82819d0092 # v3.0`;
  - `wait-for-completion-timeout-in-seconds: 14400`.
- The secret `SIGNPATH_API_TOKEN` (`release` environment) is read only by `windows-sign`. Never print it.
- Sign `zenvik.exe` and `zenvik-gui.exe`. Never sign `mkvmerge.exe`.
- Artifact names:
  - `release-cli` (Linux `.tar.gz`), `windows-unsigned-cli` (Windows `.zip`);
  - `release-cli-darwin`, `release-gui-darwin-<i>`;
  - `release-gui-linux`, `windows-unsigned-gui`;
  - `windows-to-sign`, `release-windows`.

  `publish` downloads `pattern: release-*` with `merge-multiple: true`.
- `windows-sign-check` always runs (no `if:`), so `publish`'s `needs` never sees a skipped job.
- README policy text includes: "Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org)."
- Confirm with the user immediately before merging or pushing zenvik, and before tagging a release.

## Review Focus

1. **Unsigned zips leaking into the release.** Same file names; if `publish` downloaded the unsigned zips, `merge-multiple` would keep whichever came last. Task 3's structural test asserts `publish` has exactly one download, of `release-*`, and that the Windows zips are uploaded only as `windows-unsigned-*`.
2. **A skipped job blocking `publish`.** If `windows-sign-check` had an `if:` and were skipped, GitHub would skip `publish` too. Task 3's test asserts it has no `if:`.
3. **The SignPath token reaching other jobs.** Task 3's test asserts only `windows-sign` mentions `SIGNPATH_API_TOKEN`.
4. **SignPath returns something not runnable or not signed**, such as a corrupted zip or a signature from the wrong signer. `windows-sign-check` runs `zenvik.exe --version` and checks `Valid` plus "SignPath Foundation". It first runs signed when the user sets up SignPath; the reviewer reads it.
5. **The artifact configuration doesn't match the zips' layout**, for example the wildcard folder names. Task 2's test parses it and checks the two `pe-file` targets sit inside `zenvik_*_windows_amd64` and `zenvik-gui_*_windows_amd64` folders matching `release-build.sh` and `release-gui.sh`. SignPath validates it for real when it's pasted in.

---

### Task 1: `scripts/windows-sign-mode.sh`

**Files:**
- Create: `scripts/windows-sign-mode.sh`, `scripts/windows-sign-mode_test.sh`

**Interfaces:**
- Produces: `scripts/windows-sign-mode.sh <tag> <organization-id>`, which prints `sign` or `pass` on stdout, with a reason on stderr for `pass`. Exit 0, or exit 2 for a usage error.

- [ ] **Step 1: Write the failing test**

`scripts/windows-sign-mode_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/windows-sign-mode.sh.
#
#   scripts/windows-sign-mode_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

# expect NAME WANT_STDOUT WANT_STDERR_PART TAG ORG
expect() {
	local out err
	err=$({ out=$("$here/windows-sign-mode.sh" "$4" "$5"); } 2>&1)
	out=$("$here/windows-sign-mode.sh" "$4" "$5" 2>/dev/null)
	if [[ $out == "$2" && $err == *"$3"* ]]; then ok "$1"; else bad "$1" "stdout [$out] stderr [$err]"; fi
}
expect "a final tag with SignPath configured signs" sign "" v1.4.0 0123-org
expect "a final tag without SignPath passes through" pass "not set" v1.4.0 ""
expect "a pre-release passes through even when configured" pass "pre-release" v1.4.0-rc1 0123-org
expect "a non-version tag passes through" pass "pre-release" nightly 0123-org

if out=$("$here/windows-sign-mode.sh" v1.4.0 2>&1); then bad "one argument is a usage error" "$out"; else ok "one argument is a usage error"; fi
if out=$("$here/windows-sign-mode.sh" "" org 2>&1); then bad "an empty tag is a usage error" "$out"; else ok "an empty tag is a usage error"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/windows-sign-mode_test.sh && scripts/windows-sign-mode_test.sh`
Expected: FAIL lines, non-zero exit.

- [ ] **Step 3: Implement**

`scripts/windows-sign-mode.sh`:

```bash
#!/usr/bin/env bash
# Decide whether a release's Windows zips are signed through SignPath (the
# release workflow's windows-sign job runs this):
#
#   scripts/windows-sign-mode.sh <tag> <signpath-organization-id>
#
# prints "sign" for a final tag (vX.Y.Z) when the organization ID is set;
# otherwise "pass" (publish unsigned), with the reason on stderr.
set -euo pipefail

if [[ $# -ne 2 || -z $1 ]]; then
	echo "usage: $0 <tag> <signpath-organization-id>" >&2
	exit 2
fi
if [[ ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "windows-sign: $1 is a pre-release (not vX.Y.Z); publishing the Windows zips unsigned" >&2
	echo pass
	exit 0
fi
if [[ -z $2 ]]; then
	echo "windows-sign: SIGNPATH_ORGANIZATION_ID is not set; publishing the Windows zips unsigned" >&2
	echo pass
	exit 0
fi
echo sign
```

- [ ] **Step 4: Run the tests and lint**

Run: `chmod +x scripts/windows-sign-mode.sh && scripts/windows-sign-mode_test.sh && shellcheck -x scripts/windows-sign-mode*.sh`
Expected: `6 passed, 0 failed`, and no lint output. If shellcheck flags the test's `out=` inside the brace group (SC2034), keep the line and add `# shellcheck disable=SC2034` above it: it captures stdout so that only stderr reaches `err`.

- [ ] **Step 5: Commit**

```bash
git add scripts/windows-sign-mode.sh scripts/windows-sign-mode_test.sh
git commit -m "windows-sign-mode.sh: sign final tags once SignPath is configured, else pass the zips through"
```

---

### Task 2: `signing/` (artifact configuration and setup guide)

**Files:**
- Create: `signing/signpath-artifact-configuration.xml`, `signing/README.md`
- Create: `scripts/signpath-config_test.sh`

**Interfaces:**
- Produces: the artifact configuration the user pastes into SignPath. It must match the zips' layout: `zenvik_<ver>_windows_amd64/zenvik.exe`, from `scripts/release-build.sh`, and `zenvik-gui_<ver>_windows_amd64/zenvik-gui.exe`, from `scripts/release-gui.sh`.

- [ ] **Step 1: Write the failing test**

`scripts/signpath-config_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for signing/signpath-artifact-configuration.xml: it parses, signs
# exactly zenvik.exe and zenvik-gui.exe in the release zips' folders, and
# never mkvmerge.exe.
#
#   scripts/signpath-config_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
cfg="$here/../signing/signpath-artifact-configuration.xml"
python3 - "$cfg" <<'PY'
import sys, xml.etree.ElementTree as ET
ns = {"s": "http://signpath.io/artifact-configuration/v1"}
passed = failed = 0
def check(name, ok, detail=""):
    global passed, failed
    if ok:
        passed += 1; print("ok   " + name)
    else:
        failed += 1; print("FAIL " + name + (("\n     " + detail) if detail else ""))
try:
    root = ET.parse(sys.argv[1]).getroot()
except Exception as e:
    check("parses as XML", False, str(e)); print(f"{passed} passed, {failed} failed"); sys.exit(1)
check("parses as XML", True)
check("root is artifact-configuration in SignPath's namespace", root.tag == "{%s}artifact-configuration" % ns["s"], root.tag)
outer = root.findall("s:zip-file", ns)
check("one outer zip-file (the GitHub artifact)", len(outer) == 1 and "path" not in outer[0].attrib)
signed = []
for z in root.iter("{%s}zip-file" % ns["s"]):
    for d in z.findall("s:directory", ns):
        for pe in d.findall("s:pe-file", ns):
            if pe.find("s:authenticode-sign", ns) is not None:
                signed.append((z.get("path"), d.get("path"), pe.get("path")))
want = [("zenvik_*_windows_amd64.zip", "zenvik_*_windows_amd64", "zenvik.exe"),
        ("zenvik-gui_*_windows_amd64.zip", "zenvik-gui_*_windows_amd64", "zenvik-gui.exe")]
check("signs zenvik.exe and zenvik-gui.exe in the release zips' folders", sorted(signed) == sorted(want), repr(signed))
text = open(sys.argv[1]).read()
check("never names mkvmerge.exe", "mkvmerge" not in text)
print(f"{passed} passed, {failed} failed")
sys.exit(0 if failed == 0 else 1)
PY
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/signpath-config_test.sh && scripts/signpath-config_test.sh`
Expected: `FAIL parses as XML` (no such file), non-zero exit.

- [ ] **Step 3: Write the files**

`signing/signpath-artifact-configuration.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<!-- SignPath artifact configuration for zenvik (project "zenvik", default
     configuration). Paste it into SignPath; keep this copy in sync.
     The outer zip-file is the GitHub artifact windows-sign uploads (the two
     release zips at its root). mkvmerge.exe is MKVToolNix's own build and is
     deliberately not listed, so it passes through unsigned. -->
<artifact-configuration xmlns="http://signpath.io/artifact-configuration/v1">
  <zip-file>
    <zip-file path="zenvik_*_windows_amd64.zip">
      <directory path="zenvik_*_windows_amd64">
        <pe-file path="zenvik.exe">
          <authenticode-sign />
        </pe-file>
      </directory>
    </zip-file>
    <zip-file path="zenvik-gui_*_windows_amd64.zip">
      <directory path="zenvik-gui_*_windows_amd64">
        <pe-file path="zenvik-gui.exe">
          <authenticode-sign />
        </pe-file>
      </directory>
    </zip-file>
  </zip-file>
</artifact-configuration>
```

`signing/README.md`:

````markdown
# Windows code signing (SignPath Foundation)

Final releases' Windows `zenvik.exe` and `zenvik-gui.exe` are signed through [SignPath.io](https://about.signpath.io), with a certificate from [SignPath Foundation](https://signpath.org). The release workflow's `windows-sign` job does it once the repository variable `SIGNPATH_ORGANIZATION_ID` is set; until then releases publish unsigned. Pre-releases are never signed.

## One-time setup

1. Make sure the README's "Code signing policy" section is published on `main`. SignPath's reviewers look for it.
2. Apply at https://signpath.org/apply.
3. Once accepted, in SignPath:
   - create the project with slug `zenvik`;
   - paste [`signpath-artifact-configuration.xml`](signpath-artifact-configuration.xml) as its default artifact configuration;
   - use the signing policy with slug `release-signing`, with yourself as approver;
   - link the predefined **GitHub.com** trusted build system to the project, and install the SignPath GitHub app on `chad3814/zenvik`;
   - create an API token for a user with submitter permissions.
4. In GitHub (`chad3814/zenvik` → Settings):
   - Environments → `release` → add the secret `SIGNPATH_API_TOKEN`;
   - Variables → add the repository variable `SIGNPATH_ORGANIZATION_ID` (your SignPath organization ID). This turns signing on.

## Every final release

1. `windows-sign` submits the Windows zips and waits.
2. SignPath emails you: approve the request in its web app within **4 hours**.
3. The signed zips go to `windows-sign-check`, which verifies the signatures on Windows, and then to `publish`.

If the request isn't approved in time, or is rejected, `windows-sign` fails and nothing publishes. Re-run the failed jobs from the release run to submit again.

To fall back to unsigned releases (for example during a SignPath outage), delete or empty `SIGNPATH_ORGANIZATION_ID`.

If you change `scripts/release-build.sh` or `scripts/release-gui.sh` so the zips' folder names change, update the artifact configuration here and in SignPath. `scripts/signpath-config_test.sh` checks this copy.
````

- [ ] **Step 4: Run the tests**

Run: `scripts/signpath-config_test.sh && shellcheck -x scripts/signpath-config_test.sh`
Expected: `5 passed, 0 failed`, and no lint output. Also confirm the folder names match the builders: `grep -n 'name="zenvik' scripts/release-build.sh scripts/release-gui.sh` should show `zenvik_${ver}_${goos}_${goarch}` and `zenvik-gui_${ver}_${goos}_${goarch}`.

- [ ] **Step 5: Commit**

```bash
git add signing/ scripts/signpath-config_test.sh
git commit -m "signing: SignPath artifact configuration (zenvik.exe, zenvik-gui.exe; not mkvmerge.exe) and setup guide"
```

---

### Task 3: Release workflow, README policy and CLAUDE.md

**Files:**
- Modify: `.github/workflows/release.yml` (`cli`, `cli-darwin`, `gui-darwin`, `gui-other` and `publish`, plus the new `windows-sign` and `windows-sign-check` jobs)
- Create: `scripts/release-workflow_test.sh`
- Modify: `README.md` (the new "Code signing policy" section), `CLAUDE.md`

**Interfaces:**
- Consumes: `scripts/windows-sign-mode.sh` (Task 1).
- Produces: the jobs `windows-sign` (output `signed`) and `windows-sign-check`, and the artifacts `windows-unsigned-cli`, `windows-unsigned-gui` and `release-windows`.

- [ ] **Step 1: Write the failing structural test**

`scripts/release-workflow_test.sh`:

```bash
#!/usr/bin/env bash
# Structural checks of .github/workflows/release.yml: Windows signing, and
# the release- artifact prefix publish relies on. Uses Ruby's built-in YAML.
#
#   scripts/release-workflow_test.sh [path/to/release.yml]
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
wf=${1:-$here/../.github/workflows/release.yml}
ruby -ryaml - "$wf" <<'RUBY'
w = YAML.load_file(ARGV[0])
jobs = w['jobs']
$pass = 0
$fail = 0
def check(name, ok, detail = '')
  if ok
    $pass += 1
    puts "ok   #{name}"
  else
    $fail += 1
    puts "FAIL #{name}"
    puts "     #{detail}" unless detail.to_s.empty?
  end
end
steps = ->(j) { jobs.dig(j, 'steps') || [] }
uses = ->(s, a) { s['uses'].to_s.start_with?(a) }

pub = jobs['publish'] || {}
check 'publish needs windows-sign and windows-sign-check', (Array(pub['needs']) & %w[windows-sign windows-sign-check]).size == 2, pub['needs'].inspect
dl = steps.call('publish').select { |s| uses.call(s, 'actions/download-artifact') }
check 'publish downloads only release-* artifacts', dl.size == 1 && dl[0].dig('with', 'pattern') == 'release-*' && dl[0].dig('with', 'merge-multiple') == true, dl.inspect

build = %w[cli cli-darwin gui-darwin gui-other windows-sign]
names = build.flat_map { |j| steps.call(j).select { |s| uses.call(s, 'actions/upload-artifact') }.map { |s| [j, s.dig('with', 'name').to_s] } }
bad = names.reject { |_, n| n.start_with?('release-', 'windows-unsigned-') || n == 'windows-to-sign' || n == '${{ matrix.artifact }}' }
check 'build jobs upload only release-*, windows-unsigned-* or windows-to-sign', bad.empty?, bad.inspect

inc = jobs.dig('gui-other', 'strategy', 'matrix', 'include') || []
check 'gui-other names its artifacts per matrix entry', inc.map { |e| e['artifact'] }.compact.sort == %w[release-gui-linux windows-unsigned-gui], inc.inspect
cli_up = steps.call('cli').select { |s| uses.call(s, 'actions/upload-artifact') }.map { |s| [s.dig('with', 'name'), s.dig('with', 'path')] }
check 'cli uploads the Windows zip only as windows-unsigned-cli', cli_up.include?(['windows-unsigned-cli', 'dist/*.zip']) && cli_up.include?(['release-cli', 'dist/*.tar.gz']) && cli_up.size == 2, cli_up.inspect

ws = jobs['windows-sign'] || {}
check 'windows-sign runs in the release environment', ws['environment'] == 'release', ws['environment'].inspect
check 'windows-sign needs cli and gui-other', (Array(ws['needs']) & %w[cli gui-other]).size == 2, ws['needs'].inspect
check 'windows-sign outputs signed', ws.dig('outputs', 'signed').to_s.include?('steps.mode.outputs.signed'), ws['outputs'].inspect
sp = steps.call('windows-sign').find { |s| uses.call(s, 'signpath/github-action-submit-signing-request@') }
check 'the SignPath action is pinned to a commit SHA', !sp.nil? && sp['uses'] =~ /@[0-9a-f]{40}\z/, sp.inspect
check 'signing waits up to 4 hours', !sp.nil? && sp.dig('with', 'wait-for-completion-timeout-in-seconds').to_s == '14400'
check 'signing uses project zenvik and policy release-signing', !sp.nil? && sp.dig('with', 'project-slug') == 'zenvik' && sp.dig('with', 'signing-policy-slug') == 'release-signing'
users = jobs.select { |_, j| j.to_s.include?('SIGNPATH_API_TOKEN') }.keys
check 'only windows-sign reads SIGNPATH_API_TOKEN', users == ['windows-sign'], users.inspect

wc = jobs['windows-sign-check'] || {}
check 'windows-sign-check needs windows-sign and always runs', Array(wc['needs']) == ['windows-sign'] && !wc.key?('if'), wc.slice('needs', 'if').inspect
check 'windows-sign-check runs on Windows', wc['runs-on'] == 'windows-latest', wc['runs-on'].inspect

puts "#{$pass} passed, #{$fail} failed"
exit($fail.zero? ? 0 : 1)
RUBY
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/release-workflow_test.sh && scripts/release-workflow_test.sh`
Expected: FAIL lines (no `windows-sign`; `publish` downloads everything; `cli` uploads `cli`), non-zero exit.

- [ ] **Step 3: Edit `.github/workflows/release.yml`**

1. **`cli`:** replace its `upload-artifact` step with:

```yaml
      - uses: actions/upload-artifact@v4
        with:
          name: release-cli
          path: dist/*.tar.gz
      # The Windows zip goes through windows-sign before publish.
      - uses: actions/upload-artifact@v4
        with:
          name: windows-unsigned-cli
          path: dist/*.zip
```

2. **`cli-darwin`:** artifact `name: cli-darwin` → `name: release-cli-darwin`.
3. **`gui-darwin`:** `name: gui-darwin-${{ strategy.job-index }}` → `name: release-gui-darwin-${{ strategy.job-index }}`.
4. **`gui-other`:** add the artifact name to each matrix entry, and use it:

```yaml
        include:
          - { os: windows-latest, target: windows/amd64, artifact: windows-unsigned-gui }
          - { os: ubuntu-latest, target: linux/amd64, artifact: release-gui-linux }
```

```yaml
      - uses: actions/upload-artifact@v4
        with:
          name: ${{ matrix.artifact }}
          path: dist/zenvik-gui_*
```

5. **`publish`:** set `needs: [cli, cli-darwin, gui-darwin, gui-other, windows-sign, windows-sign-check]`, and change its download step to:

```yaml
      - uses: actions/download-artifact@v5
        with:
          pattern: release-*
          path: dist
          merge-multiple: true
```

6. **New jobs:** insert these two before `publish`:

```yaml
  # Windows code signing (SignPath Foundation): final releases only, once the
  # repository variable SIGNPATH_ORGANIZATION_ID is set; otherwise the zips
  # pass through unsigned. Signing waits for a person to approve the request
  # in SignPath (up to 4 hours). Only this job reads SIGNPATH_API_TOKEN. See
  # signing/README.md.
  windows-sign:
    needs: [cli, gui-other]
    runs-on: ubuntu-latest
    environment: release
    timeout-minutes: 300
    outputs:
      signed: ${{ steps.mode.outputs.signed }}
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
      - id: mode
        env:
          ORG: ${{ vars.SIGNPATH_ORGANIZATION_ID }}
        run: |
          mode=$(scripts/windows-sign-mode.sh "$GITHUB_REF_NAME" "$ORG")
          echo "mode=$mode" >>"$GITHUB_OUTPUT"
          if [[ $mode == sign ]]; then
            echo "signed=true" >>"$GITHUB_OUTPUT"
          else
            echo "signed=false" >>"$GITHUB_OUTPUT"
          fi
      - uses: actions/download-artifact@v5
        with:
          pattern: windows-unsigned-*
          path: unsigned
          merge-multiple: true
      - if: steps.mode.outputs.mode == 'pass'
        uses: actions/upload-artifact@v4
        with:
          name: release-windows
          path: unsigned/*.zip
      - if: steps.mode.outputs.mode == 'sign'
        env:
          TOKEN: ${{ secrets.SIGNPATH_API_TOKEN }}
        run: |
          if [[ -z $TOKEN ]]; then
            echo "SIGNPATH_ORGANIZATION_ID is set but the release environment has no SIGNPATH_API_TOKEN" >&2
            exit 1
          fi
      - if: steps.mode.outputs.mode == 'sign'
        id: to-sign
        uses: actions/upload-artifact@v4
        with:
          name: windows-to-sign
          path: unsigned/*.zip
      - if: steps.mode.outputs.mode == 'sign'
        uses: signpath/github-action-submit-signing-request@f6d04783b4569d051e0c80105fe66e82819d0092 # v3.0
        with:
          api-token: ${{ secrets.SIGNPATH_API_TOKEN }}
          organization-id: ${{ vars.SIGNPATH_ORGANIZATION_ID }}
          project-slug: zenvik
          signing-policy-slug: release-signing
          github-artifact-id: ${{ steps.to-sign.outputs.artifact-id }}
          wait-for-completion: true
          wait-for-completion-timeout-in-seconds: 14400
          output-artifact-directory: signed
      - if: steps.mode.outputs.mode == 'sign'
        uses: actions/upload-artifact@v4
        with:
          name: release-windows
          path: signed/*.zip

  # Always runs (publish needs it): the Windows binaries must run, and when
  # windows-sign signed them, carry valid SignPath Foundation signatures.
  windows-sign-check:
    needs: windows-sign
    runs-on: windows-latest
    defaults:
      run:
        shell: pwsh
    steps:
      - uses: actions/download-artifact@v5
        with:
          name: release-windows
          path: ${{ runner.temp }}/win
      - name: The Windows binaries run, and are signed when signing ran
        env:
          SIGNED: ${{ needs.windows-sign.outputs.signed }}
        run: |
          $ErrorActionPreference = 'Stop'
          $root = "$env:RUNNER_TEMP\win"
          Get-ChildItem $root -Filter *.zip | ForEach-Object { Expand-Archive $_.FullName -DestinationPath "$root\x" -Force }
          $cli = Get-ChildItem "$root\x" -Recurse -Filter zenvik.exe | Select-Object -First 1
          $gui = Get-ChildItem "$root\x" -Recurse -Filter zenvik-gui.exe | Select-Object -First 1
          $mk = @(Get-ChildItem "$root\x" -Recurse -Filter mkvmerge.exe)
          if (-not $cli -or -not $gui -or $mk.Count -ne 2) { throw "missing files: zenvik.exe=$cli zenvik-gui.exe=$gui mkvmerge.exe count=$($mk.Count)" }
          $out = (& $cli.FullName --version) -join "`n"
          if ($LASTEXITCODE -ne 0 -or $out -notlike 'zenvik v*') { throw "zenvik --version printed: $out" }
          if ($env:SIGNED -eq 'true') {
            foreach ($f in $cli, $gui) {
              $s = Get-AuthenticodeSignature $f.FullName
              if ($s.Status -ne 'Valid') { throw "$($f.Name): signature status $($s.Status)" }
              if ($s.SignerCertificate.Subject -notmatch 'SignPath Foundation') { throw "$($f.Name): signed by $($s.SignerCertificate.Subject)" }
              Write-Host "$($f.Name): signed by $($s.SignerCertificate.Subject)"
            }
          } else {
            Write-Host 'not signed this release (a pre-release, or SignPath is not configured)'
          }
          Write-Host 'windows-sign-check: all checks passed'
```

- [ ] **Step 4: Run the structural test and actionlint**

Run: `scripts/release-workflow_test.sh && go run github.com/rhysd/actionlint/cmd/actionlint@latest`
Expected: `14 passed, 0 failed`, and no actionlint output.

- [ ] **Step 5: README "Code signing policy" section**

Add this section to `README.md`, before the License or last section; if there is no such section, add it at the end:

```markdown
## Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).

- **What's signed:** `zenvik.exe` and `zenvik-gui.exe` in each final release's Windows downloads, built by this repository's GitHub Actions release workflow from the tagged source. The bundled `mkvmerge.exe` is MKVToolNix's own build, included unchanged and not signed by this project.
- **Team roles:** Chad Walker ([@chad3814](https://github.com/chad3814)): committer and author, reviewer, approver. Every signing request is approved by hand.
- **Privacy:** zenvik and the Zenvik app send nothing about you, your files or your discs. Once a day they ask for the latest version number (the request carries the program's version in its User-Agent): from GitHub's API (`api.github.com/repos/chad3814/zenvik/releases/latest`), or, for winget and Chocolatey installs, from that package manager's own listing (winget's manifest folder on GitHub, or Chocolatey's community feed). `update_check = false` in the config file, or the `ZENVIK_NO_UPDATE_CHECK` environment variable, turns this off. Nothing else connects to the network unless you ask it to.
```

Check the privacy text against `internal/update`. `grep -rn 'https://' internal/update/*.go | grep -v _test` should show only GitHub's `releases/latest`, winget-pkgs' manifests path and Chocolatey's feed. If anything else is contacted, add it to the text.

- [ ] **Step 6: CLAUDE.md**

Add after the Chocolatey bullet:

```markdown
- Windows code signing (SignPath Foundation, free OSS program; `signing/README.md`): for final tags, once the repository variable `SIGNPATH_ORGANIZATION_ID` is set, the release workflow's `windows-sign` job submits the Windows zips (`windows-unsigned-cli`, `windows-unsigned-gui`) to SignPath (project `zenvik`, policy `release-signing`, artifact configuration `signing/signpath-artifact-configuration.xml`, tested by `scripts/signpath-config_test.sh`) and waits up to 4 h for the user's approval; otherwise (`scripts/windows-sign-mode.sh`, tests `scripts/windows-sign-mode_test.sh`) the zips pass through unsigned. Either way it uploads `release-windows`; `windows-sign-check` runs them on Windows and verifies signatures when signing ran. `publish` downloads only `release-*` artifacts (`scripts/release-workflow_test.sh` checks the convention); only `windows-sign` reads the `release` environment secret `SIGNPATH_API_TOKEN`. Never print it. The README's "Code signing policy" section is required by SignPath.
```

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/release.yml scripts/release-workflow_test.sh README.md CLAUDE.md
git commit -m "Release: sign the Windows binaries through SignPath for final tags once configured (else pass through), verify them on Windows, and publish only release-* artifacts; README code signing policy"
```

---

### Task 4: Review, merge, and prove the pass-through

- [ ] **Step 1: Full local checks:**
  - `go test -race ./...`
  - every `scripts/*_test.sh` (`ZENVIK_NET_TESTS=1` where they download), then `brew developer off`
  - `shellcheck -x scripts/*.sh`
  - actionlint

  Expected: all pass.
- [ ] **Step 2: Final whole-branch review** (the executor's review step), then the fix pass, if any.
- [ ] **Step 3: Ask the user, then merge and push** `main`, and watch CI.
- [ ] **Step 4: Ask the user, then tag the next release** (for example `v1.3.3`) to prove the pass-through:
  - `windows-sign` logs "SIGNPATH_ORGANIZATION_ID is not set" and uploads `release-windows`;
  - `windows-sign-check` passes;
  - the release has the usual assets, and its `SHA256SUMS` lists each Windows zip exactly once;
  - the package-manager jobs pass, with Chocolatey's moderation skip as before.
- [ ] **Step 5: Hand the user the setup steps** in `signing/README.md`: apply now that the policy section is live, then configure SignPath and add the secret and variable. The first final release after that is the first signed one; watch `windows-sign`, approve, and check `windows-sign-check`'s signature lines.
