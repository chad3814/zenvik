# zenvik on winget and Chocolatey Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bundle `mkvmerge.exe` with the Windows CLI and publish zenvik to winget (`chad3814.Zenvik`, `chad3814.ZenvikGUI`) and Chocolatey (`zenvik`, `zenvik-gui`), each updated by the release workflow after a Windows install check.

**Architecture:**
- **CLI lookup:** on Windows, `internal/mux.Find` looks for `mkvmerge.exe` beside the symlink-resolved executable before PATH, and reports which one it used.
- **Release build:** `release-build.sh` adds the pinned `mkvmerge.exe` and MKVToolNix notices to the Windows CLI zip.
- **Render scripts:** `winget-render.sh` and `choco-render.sh` write manifests and packages from `SHA256SUMS`, like the existing tap and bucket scripts.
- **winget submission:** `winget-submit.sh` opens PRs through GitHub's API.
- **Release jobs:** render → check on `windows-latest` → publish, for each channel.

**Tech Stack:** Go 1.27; bash (macOS 3.2 and Linux); PowerShell 7 (release jobs); `gh`; `jq`; winget; Chocolatey; GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-10-05-zenvik-winget-choco-design.md`

## Global Constraints

- winget IDs: `chad3814.Zenvik` (CLI) and `chad3814.ZenvikGUI` (app). Manifests go under `manifests/c/chad3814/<Zenvik|ZenvikGUI>/<ver>/` with schema **1.12.0**. Publisher: `Chad Walker`.
- winget PR titles: `New package: <ID> version <ver>` for the first, `Update: <ID> to <ver>` after. Branch: `<ID>-<ver>` on `chad3814/winget-pkgs`.
- Chocolatey IDs: `zenvik` and `zenvik-gui`. Push source: `https://push.chocolatey.org/`.
- Secrets live in the `release` environment: `WINGET_TOKEN` (classic token; read only by `winget-submit`) and `CHOCO_API_KEY` (read only by `choco-push`). Never print, log, write to disk or commit either.
- Only final tags `^v[0-9]+\.[0-9]+\.[0-9]+$` publish.
- Windows CLI zip layout, `zenvik_<ver>_windows_amd64/`:
  - `zenvik.exe`, `mkvmerge.exe`, `LICENSE`, `README.md`;
  - `MKVTOOLNIX-COPYING.txt`, `MKVTOOLNIX-NOTICE.txt`, `MKVTOOLNIX-LICENSES/`.
- mkvmerge lookup order: the config's `mkvmerge_path`, then (Windows only) `mkvmerge.exe` beside the symlink-resolved executable, then PATH. `Mkvmerge.Source` is `"config" | "bundled" | "PATH"`.
- Go rules: no cgo; dependencies limited to cobra and go-toml; sentinel errors wrapped with `%w`.
- Scripts: `set -euo pipefail`; capture command output into variables rather than piping into `grep -q`; `shellcheck -x` clean.
- These steps reach outside the repo, so confirm with the user immediately before each:
  - committing to `chad3814/scoop-bucket`;
  - merging or pushing zenvik;
  - tagging a release.

## Review Focus

1. **winget's portable link.** `zenvik.exe` is started through a symlink in `WinGet\Links`, so the bundled lookup must use the link's target folder. Task 1 tests a symlinked executable; the test is skipped where symlinks aren't permitted.
2. **Chocolatey shims the bundled `mkvmerge.exe`.** Without `mkvmerge.exe.ignore`, a `mkvmerge` shim on PATH would shadow a real MKVToolNix. Task 6's test asserts the install script writes the `.ignore` file, and Task 7's `choco-check` asserts that no `bin\mkvmerge.exe` shim exists. That second check first runs for real at v1.3.0.
3. **Leaking `WINGET_TOKEN`.** `winget-submit.sh` must never print it. Task 5's test runs it with a sentinel token through a fake `gh` and checks neither the output nor the fake's log contains it.
4. **Release ordering for the Scoop bucket.** The bucket's `check.ps1` runs during v1.3.0's `scoop-check` on *new* manifests, which have no `depends`, and in the bucket's own CI on *current* manifests, which have `depends`. It must accept both. Task 3 makes it check the bundled `mkvmerge.exe` when one exists, and the PATH one otherwise. The bucket's CI proves the current-manifest path; v1.3.0 proves the new one.
5. **Where winget puts a portable install** under an elevated runner: user scope (`%LOCALAPPDATA%`) or machine scope (`%ProgramFiles%`). `winget-check` searches both for the Links and Packages folders. It first runs at v1.3.0, so the reviewer reads it. If it fails, nothing is submitted.
6. **A Chocolatey version still in moderation.** The feed lookup may not find it, and `choco push` then reports a conflict. Task 7's push step treats an "already exists" or 409 response as a skip, not a failure.

---

### Task 1: mkvmerge lookup beside the executable, with `Source`

**Files:**
- Modify: `internal/mux/mkvmerge.go` (`Find`, `Mkvmerge`, a new `locate`, and test hooks)
- Modify: `cmd/zenvik/doctor.go:62` (print `Source`)
- Test: `internal/mux/locate_test.go` (`package mux`)

**Interfaces:**
- Produces: the field `Mkvmerge.Source string` and an unexported `locate() (path, source string, err error)`; the test hooks `goos`, `executable` and `lookPath` are package variables. `Find(ctx, path)` keeps its signature.

- [ ] **Step 1: Write the failing test**

`internal/mux/locate_test.go`:

```go
package mux

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func hooks(t *testing.T, os_ string, exe string, onPath string) {
	t.Helper()
	g, e, l := goos, executable, lookPath
	t.Cleanup(func() { goos, executable, lookPath = g, e, l })
	goos = os_
	executable = func() (string, error) { return exe, nil }
	lookPath = func(string) (string, error) {
		if onPath == "" {
			return "", errors.New("not on PATH")
		}
		return onPath, nil
	}
}

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLocateBundledBeatsPathOnWindows(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "zenvik.exe"))
	touch(t, filepath.Join(dir, "mkvmerge.exe"))
	hooks(t, "windows", filepath.Join(dir, "zenvik.exe"), `C:\Tools\mkvmerge.exe`)
	p, src, err := locate()
	if err != nil || src != "bundled" || p != filepath.Join(dir, "mkvmerge.exe") {
		t.Fatalf("locate = %q, %q, %v; want the bundled mkvmerge.exe", p, src, err)
	}
}

func TestLocateFallsBackToPath(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "zenvik.exe")) // no mkvmerge.exe beside it
	hooks(t, "windows", filepath.Join(dir, "zenvik.exe"), `C:\Tools\mkvmerge.exe`)
	if p, src, err := locate(); err != nil || src != "PATH" || p != `C:\Tools\mkvmerge.exe` {
		t.Fatalf("no bundled copy: locate = %q, %q, %v; want PATH", p, src, err)
	}
}

func TestLocateIgnoresBundledOffWindows(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "zenvik"))
	touch(t, filepath.Join(dir, "mkvmerge.exe"))
	hooks(t, "linux", filepath.Join(dir, "zenvik"), "/usr/bin/mkvmerge")
	if p, src, err := locate(); err != nil || src != "PATH" || p != "/usr/bin/mkvmerge" {
		t.Fatalf("linux: locate = %q, %q, %v; want PATH", p, src, err)
	}
}

// winget's portable installs start zenvik.exe through a symlink in
// WinGet\Links; the bundled copy is beside the link's target.
func TestLocateFollowsTheExecutablesSymlink(t *testing.T) {
	pkg := t.TempDir()
	touch(t, filepath.Join(pkg, "zenvik.exe"))
	touch(t, filepath.Join(pkg, "mkvmerge.exe"))
	links := t.TempDir()
	link := filepath.Join(links, "zenvik.exe")
	if err := os.Symlink(filepath.Join(pkg, "zenvik.exe"), link); err != nil {
		t.Skipf("symlinks not permitted here: %v", err)
	}
	hooks(t, "windows", link, "")
	want, _ := filepath.EvalSymlinks(filepath.Join(pkg, "mkvmerge.exe"))
	p, src, err := locate()
	if err != nil || src != "bundled" {
		t.Fatalf("symlinked exe: locate = %q, %q, %v; want bundled", p, src, err)
	}
	if got, _ := filepath.EvalSymlinks(p); got != want {
		t.Fatalf("symlinked exe: got %q, want %q", got, want)
	}
}

func TestLocateNotFound(t *testing.T) {
	hooks(t, "windows", filepath.Join(t.TempDir(), "zenvik.exe"), "")
	if _, _, err := locate(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/mux/ -run Locate`
Expected: compile errors, `undefined: goos`, `executable`, `lookPath` and `locate`.

- [ ] **Step 3: Implement**

In `internal/mux/mkvmerge.go`, add `path/filepath` and `runtime` to the imports, add a `Source` field, and replace `Find`'s lookup:

```go
// Mkvmerge is a located, version-checked mkvmerge executable.
type Mkvmerge struct {
	Path    string
	Version Version
	Source  string   // "config" (an explicit path), "bundled" or "PATH"
	pre     []string // leading arguments (tests run the test binary as a fake)
	env     []string // extra environment (tests)
}

// Test hooks.
var (
	goos       = runtime.GOOS
	executable = os.Executable
	lookPath   = exec.LookPath
)

// Find locates mkvmerge at path, or when path is empty, beside the running
// program on Windows (the release zip bundles it) and then on PATH. It
// checks that it is at least MinVersion.
func Find(ctx context.Context, path string) (*Mkvmerge, error) {
	source := "config"
	if path == "" {
		var err error
		if path, source, err = locate(); err != nil {
			return nil, err
		}
	}
	m := &Mkvmerge{Path: path, Source: source}
	if err := m.checkVersion(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// locate finds mkvmerge when no path is configured: on Windows, the
// mkvmerge.exe beside the running program, following symlinks because
// winget's portable installs start it through one; otherwise PATH.
func locate() (string, string, error) {
	if goos == "windows" {
		if exe, err := executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			p := filepath.Join(filepath.Dir(exe), "mkvmerge.exe")
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
				return p, "bundled", nil
			}
		}
	}
	p, err := lookPath("mkvmerge")
	if err != nil {
		return "", "", fmt.Errorf("%w: %s", ErrNotFound, err.Error())
	}
	return p, "PATH", nil
}
```

In `cmd/zenvik/doctor.go`, change the success line to:

```go
		fmt.Fprintf(out, "✓ mkvmerge: %s (v%s, %s)\n", mk.Path, mk.Version, mk.Source)
```

If any doctor test pins the old line exactly, update its expected text to include `, PATH)` or `, config)`, matching how that test configures mkvmerge.

- [ ] **Step 4: Run the tests**

Run: `go test -race ./... && go vet ./... && golangci-lint run && CGO_ENABLED=0 go build ./...`
Expected: PASS. On macOS, the symlink test runs.

- [ ] **Step 5: Commit**

```bash
git add internal/mux/ cmd/zenvik/doctor.go
git commit -m "mux: on Windows, use the mkvmerge.exe beside zenvik.exe (following links) before PATH, and report which mkvmerge doctor found"
```

---

### Task 2: The Windows CLI zip bundles mkvmerge

**Files:**
- Modify: `scripts/release-build.sh` (the Windows staging)
- Create: `scripts/release-build_test.sh`
- Modify: `.github/workflows/release.yml` (`cli` job: install 7zip; check the zip)

**Interfaces:**
- Consumes: `scripts/fetch-mkvmerge.sh windows/amd64 <dir>`, `scripts/check-mkvtoolnix-source.sh <ver>` and `third_party/mkvtoolnix.env` (`MKVTOOLNIX_VERSION`), all existing.
- Produces: the Windows CLI zip layout from Global Constraints.

- [ ] **Step 1: Write the test (network-gated)**

`scripts/release-build_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/release-build.sh's Windows bundle. It downloads
# MKVToolNix, so it runs only with ZENVIK_NET_TESTS=1 (MKVTOOLNIX_CACHE
# reuses downloads).
#
#   ZENVIK_NET_TESTS=1 scripts/release-build_test.sh
set -uo pipefail

if [[ ${ZENVIK_NET_TESTS:-} != 1 ]]; then
	echo "skip: set ZENVIK_NET_TESTS=1 (downloads MKVToolNix)"
	exit 0
fi
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }

tag=v0.0.0-bundletest
trap 'rm -f "$root"/dist/zenvik_0.0.0-bundletest_*' EXIT
if out=$("$here/release-build.sh" "$tag" windows/amd64 linux/amd64 2>&1); then
	ok "builds windows/amd64 and linux/amd64"
else
	bad "builds windows/amd64 and linux/amd64" "$out"
fi
zip="$root/dist/zenvik_0.0.0-bundletest_windows_amd64.zip"
list=$(unzip -Z1 "$zip" 2>&1)
for f in zenvik.exe mkvmerge.exe LICENSE README.md MKVTOOLNIX-COPYING.txt MKVTOOLNIX-NOTICE.txt MKVTOOLNIX-LICENSES/; do
	if grep -qx "zenvik_0.0.0-bundletest_windows_amd64/$f" <<<"$list"; then ok "zip has $f"; else bad "zip has $f" "$list"; fi
done
tgz=$(tar -tzf "$root/dist/zenvik_0.0.0-bundletest_linux_amd64.tar.gz" 2>&1)
if grep -q -i mkv <<<"$tgz"; then bad "linux tarball has no mkvmerge or notices" "$tgz"; else ok "linux tarball has no mkvmerge or notices"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/release-build_test.sh && ZENVIK_NET_TESTS=1 scripts/release-build_test.sh`
Expected: `zip has mkvmerge.exe` and the four `MKVTOOLNIX-*` lines FAIL; `zenvik.exe`, `LICENSE` and `README.md` pass.

- [ ] **Step 3: Implement**

In `scripts/release-build.sh`, insert this after the line `cp "$root/README.md" "$root/LICENSE" "$stage/"`:

```bash
	# The Windows CLI bundles MKVToolNix's mkvmerge.exe, as the desktop app
	# does (see third_party/mkvtoolnix.env); zenvik finds it beside itself.
	if [[ $goos == windows ]]; then
		# shellcheck source=third_party/mkvtoolnix.env
		source "$root/third_party/mkvtoolnix.env"
		mtx=$(mktemp -d)
		"$root/scripts/fetch-mkvmerge.sh" "$target" "$mtx"
		if ! "$root/scripts/check-mkvtoolnix-source.sh" "$MKVTOOLNIX_VERSION"; then
			if [[ ${CI:-} == true ]]; then
				rm -rf "$mtx"
				exit 1
			fi
			echo "warning: building anyway (not CI); don't publish this build" >&2
		fi
		cp "$mtx/mkvmerge.exe" "$mtx"/MKVTOOLNIX-*.txt "$stage/"
		cp -R "$mtx/MKVTOOLNIX-LICENSES" "$stage/"
		rm -rf "$mtx"
	fi
```

In `.github/workflows/release.yml`'s `cli` job, add this before the `scripts/release-build.sh` step:

```yaml
      - run: sudo apt-get update && sudo apt-get install -y 7zip
```

Add this after it:

```yaml
      - name: The Windows CLI zip bundles mkvmerge.exe and its notices
        run: |
          list=$(unzip -Z1 dist/zenvik_*_windows_amd64.zip)
          for f in mkvmerge.exe MKVTOOLNIX-NOTICE.txt MKVTOOLNIX-COPYING.txt; do
            grep -q "/$f\$" <<<"$list" || { echo "missing $f"; exit 1; }
          done
```

- [ ] **Step 4: Run the tests and lint**

Run: `ZENVIK_NET_TESTS=1 scripts/release-build_test.sh && shellcheck -x scripts/release-build*.sh && go run github.com/rhysd/actionlint/cmd/actionlint@latest`
Expected: `9 passed, 0 failed`, and no lint output. On macOS, `fetch-mkvmerge.sh` unpacks the `.7z` with `7z` or bsdtar.

- [ ] **Step 5: Commit**

```bash
git add scripts/release-build.sh scripts/release-build_test.sh .github/workflows/release.yml
git commit -m "release-build: bundle MKVToolNix's mkvmerge.exe and its notices in the Windows CLI zip"
```

---

### Task 3: Scoop drops the Extras dependency

**Files:**
- Modify: `scripts/scoop-render.sh` (drop `depends`) and `scripts/scoop-render_test.sh` (assert there is none)
- Modify (bucket repo `~/Projects/scoop-bucket/worktrees/main`): `check.ps1`

**Interfaces:**
- Produces: `bucket/zenvik.json` without `depends`, and a `check.ps1` that accepts both the old and new CLI manifests (Review Focus 4).

- [ ] **Step 1: Change the test first**

In `scripts/scoop-render_test.sh`, replace the line:

```bash
eq "CLI depends on Extras' mkvtoolnix" "$(q zenvik .depends)" extras/mkvtoolnix
```

with:

```bash
eq "CLI has no dependency (it bundles mkvmerge.exe)" "$(q zenvik '.depends // "none"')" none
```

- [ ] **Step 2: Run it to verify it fails**

Run: `scripts/scoop-render_test.sh`
Expected: `FAIL CLI has no dependency … got [extras/mkvtoolnix], want [none]`, `24 passed, 1 failed`.

- [ ] **Step 3: Implement**

In `scripts/scoop-render.sh`, delete the `depends: "extras/mkvtoolnix",` line from the CLI's jq program. Update the header comment's description of `zenvik.json` to `(the CLI zip, which bundles mkvmerge.exe)`.

- [ ] **Step 4: Run the tests**

Run: `scripts/scoop-render_test.sh && shellcheck -x scripts/scoop-render*.sh`
Expected: `25 passed, 0 failed`, and no lint output.

- [ ] **Step 5: Commit (zenvik)**

```bash
git add scripts/scoop-render.sh scripts/scoop-render_test.sh
git commit -m "scoop-render: the CLI manifest no longer depends on Extras' mkvtoolnix (the zip bundles mkvmerge.exe)"
```

- [ ] **Step 6: Make the bucket's `check.ps1` accept both manifests**

In `~/Projects/scoop-bucket/worktrees/main/check.ps1`, replace:

```powershell
Step 'mkvmerge from the Extras dependency'
$out = (Run mkvmerge --version) -join "`n"
if ($out -notlike 'mkvmerge v*') { throw "mkvmerge --version printed: $out" }
```

with:

```powershell
Step "the CLI's mkvmerge"
# From v1.3.0 the CLI zip bundles mkvmerge.exe beside zenvik.exe; older
# manifests got it from Extras instead.
$cli = ((Run scoop prefix zenvik) | Select-Object -Last 1).Trim()
$bundled = Join-Path $cli 'mkvmerge.exe'
if (Test-Path $bundled) {
    $out = (Run $bundled --version) -join "`n"
} else {
    $out = (Run mkvmerge --version) -join "`n"
}
if ($out -notlike 'mkvmerge v*') { throw "mkvmerge --version printed: $out" }
```

Keep the `add the extras bucket` step: the current v1.2.1 manifests still depend on Extras.

- [ ] **Step 7: Ask the user, then commit and push to the bucket, and watch its CI**

After the user confirms:

```bash
cd ~/Projects/scoop-bucket/worktrees/main && git commit -qam "check.ps1: accept the CLI's bundled mkvmerge.exe as well as Extras'" && git push -q origin main
sleep 15; run=$(gh run list -R chad3814/scoop-bucket -L 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$run" -R chad3814/scoop-bucket --exit-status
```

Expected: success, which proves the PATH branch against the current manifests. The bundled branch runs for real in v1.3.0's `scoop-check`.

---

### Task 4: `scripts/winget-render.sh`

**Files:**
- Create: `scripts/winget-render.sh`, `scripts/winget-render_test.sh`

**Interfaces:**
- Produces: `scripts/winget-render.sh <tag> <outdir>`, which writes six files at `<outdir>/manifests/c/chad3814/{Zenvik,ZenvikGUI}/<ver>/chad3814.{Zenvik,ZenvikGUI}{,.installer,.locale.en-US}.yaml`.
  - Exit 2 for usage or a non-final tag.
  - Exit 1 when the download or a hash stops it; then nothing in `<outdir>` changes.
  - It honours `ZENVIK_RELEASE_BASE_URL`.

- [ ] **Step 1: Write the failing test**

`scripts/winget-render_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/winget-render.sh (offline, file:// fixtures).
#
#   scripts/winget-render_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cli=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
gui=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
decoy=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
fixture() {
	local fx="$work/fx-$RANDOM"
	mkdir -p "$fx/releases/download/$1"
	printf '%s' "$2" >"$fx/releases/download/$1/SHA256SUMS"
	echo "file://$fx"
}
good="$gui  zenvik-gui_9.9.9_windows_amd64.zip
$decoy  zenvik_9.9.90_windows_amd64.zip
$cli  zenvik_9.9.9_windows_amd64.zip
$decoy  zenvik-gui_9.9.9_darwin_arm64.dmg
"
out="$work/out"
d="$out/manifests/c/chad3814"
mkdir -p "$d/Zenvik/9.9.9" && echo old >"$d/Zenvik/9.9.9/chad3814.Zenvik.yaml"
if msg=$(ZENVIK_RELEASE_BASE_URL=$(fixture v9.9.9 "$good") "$here/winget-render.sh" v9.9.9 "$out" 2>&1); then ok "renders a final release"; else bad "renders a final release" "$msg"; fi

has() { # has NAME FILE LINE: FILE contains exactly LINE
	if grep -qxF -- "$3" "$2" 2>/dev/null; then ok "$1"; else bad "$1" "missing [$3] in $2"; fi
}
C="$d/Zenvik/9.9.9/chad3814.Zenvik"
G="$d/ZenvikGUI/9.9.9/chad3814.ZenvikGUI"
for f in "$C.yaml" "$C.installer.yaml" "$C.locale.en-US.yaml" "$G.yaml" "$G.installer.yaml" "$G.locale.en-US.yaml"; do
	if [[ -f $f ]]; then ok "wrote ${f#"$out"/}"; else bad "wrote ${f#"$out"/}"; fi
done
has "CLI id" "$C.installer.yaml" "PackageIdentifier: chad3814.Zenvik"
has "CLI version" "$C.installer.yaml" "PackageVersion: 9.9.9"
has "CLI zip/portable" "$C.installer.yaml" "NestedInstallerType: portable"
has "CLI nested path" "$C.installer.yaml" '  - RelativeFilePath: zenvik_9.9.9_windows_amd64\zenvik.exe'
has "CLI alias" "$C.installer.yaml" "    PortableCommandAlias: zenvik"
has "CLI url" "$C.installer.yaml" "  InstallerUrl: https://github.com/chad3814/zenvik/releases/download/v9.9.9/zenvik_9.9.9_windows_amd64.zip"
has "CLI hash uppercase, the CLI's" "$C.installer.yaml" "  InstallerSha256: $(tr 'a-f' 'A-F' <<<"$cli")"
has "schema 1.12.0" "$C.installer.yaml" "ManifestVersion: 1.12.0"
has "CLI moniker" "$C.locale.en-US.yaml" "Moniker: zenvik"
has "publisher" "$C.locale.en-US.yaml" "Publisher: Chad Walker"
has "version manifest locale" "$C.yaml" "DefaultLocale: en-US"
has "GUI hash, the GUI's" "$G.installer.yaml" "  InstallerSha256: $(tr 'a-f' 'A-F' <<<"$gui")"
has "GUI nested path" "$G.installer.yaml" '  - RelativeFilePath: zenvik-gui_9.9.9_windows_amd64\zenvik-gui.exe'
has "GUI alias" "$G.installer.yaml" "    PortableCommandAlias: zenvik-gui"
has "GUI name" "$G.locale.en-US.yaml" "PackageName: Zenvik"
if grep -q old "$C.yaml" || grep -rq "$decoy" "$d" || grep -rq 'file:' "$d"; then bad "replaces old files, ignores decoys, never writes the override URL"; else ok "replaces old files, ignores decoys, never writes the override URL"; fi

expect_fail() {
	local o="$work/fail-$RANDOM" st msg
	mkdir -p "$o"
	msg=$(ZENVIK_RELEASE_BASE_URL=$4 "$here/winget-render.sh" "$3" "$o" 2>&1)
	st=$?
	if [[ $st -ne $2 ]]; then bad "$1" "status $st (want $2): $msg"; elif [[ -n $(ls -A "$o") ]]; then bad "$1" "wrote into outdir"; else ok "$1"; fi
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

Run: `chmod +x scripts/winget-render_test.sh && scripts/winget-render_test.sh`
Expected: FAIL lines, non-zero exit.

- [ ] **Step 3: Implement**

`scripts/winget-render.sh`:

```bash
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
```

`${zipbase}\\$exe` in an unquoted heredoc writes one backslash, which is what the test expects: `zenvik_9.9.9_windows_amd64\zenvik.exe`.

- [ ] **Step 4: Run the tests and lint**

Run: `scripts/winget-render_test.sh && shellcheck -x scripts/winget-render*.sh`
Expected: `27 passed, 0 failed`, and no lint output.

- [ ] **Step 5: Commit**

```bash
git add scripts/winget-render.sh scripts/winget-render_test.sh
git commit -m "winget-render.sh: write the chad3814.Zenvik and chad3814.ZenvikGUI manifests for a final release"
```

---

### Task 5: `scripts/winget-submit.sh`

**Files:**
- Create: `scripts/winget-submit.sh`, `scripts/winget-submit_test.sh`

**Interfaces:**
- Consumes: Task 4's rendered tree, and `scripts/newer-version.sh vX.Y.Z <have>` (0 newer, 3 skip, 2 usage).
- Produces: `GH_TOKEN=… scripts/winget-submit.sh vX.Y.Z <rendered dir>`. Exit 0 when every package is submitted or skipped; non-zero on an API error. Env overrides for tests: `WINGET_UPSTREAM` (default `microsoft/winget-pkgs`) and `WINGET_FORK` (default `chad3814/winget-pkgs`).

- [ ] **Step 1: Write the failing test**

`scripts/winget-submit_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/winget-submit.sh, with a fake gh that logs its calls
# and answers from FAKE_* variables. Offline.
#
#   scripts/winget-submit_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }
command -v jq >/dev/null || { echo "needs jq" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
# fake gh: logs "gh <args>" (and any --input body) to $FAKE_LOG
args="$*"
echo "gh $args" >>"$FAKE_LOG"
if [[ $args == *"--input -"* ]]; then jq -c . >>"$FAKE_LOG"; fi
case $args in
*"repos/microsoft/winget-pkgs/contents/manifests/c/chad3814/Zenvik "*|*"contents/manifests/c/chad3814/Zenvik --jq"*)
	v=${FAKE_ZENVIK:-404}
	if [[ $v == 404 ]]; then echo "gh: Not Found (HTTP 404)" >&2; exit 1; fi
	printf '%s\n' $v ;;
*"contents/manifests/c/chad3814/ZenvikGUI"*)
	v=${FAKE_GUI:-404}
	if [[ $v == 404 ]]; then echo "gh: Not Found (HTTP 404)" >&2; exit 1; fi
	printf '%s\n' $v ;;
*"merge-upstream"*) echo '{}' ;;
*"pulls?state=open"*) echo "${FAKE_OPEN:-0}" ;;
*"git/ref/heads/master"*) echo base123 ;;
*"git/commits/base123"*) echo tree123 ;;
*"git/blobs"*) echo "blob$RANDOM" ;;
*"git/trees"*) echo newtree ;;
*"git/commits"*) echo newcommit ;;
*"git/refs"*) echo '{}' ;;
*"repos/microsoft/winget-pkgs/pulls"*) echo "https://github.com/microsoft/winget-pkgs/pull/1" ;;
*) echo "fake gh: unexpected: $args" >&2; exit 9 ;;
esac
STUB
chmod +x "$work/bin/gh"

# rendered tree for 9.9.9
r="$work/rendered"
for p in Zenvik ZenvikGUI; do
	mkdir -p "$r/manifests/c/chad3814/$p/9.9.9"
	for f in "" .installer .locale.en-US; do echo "PackageIdentifier: chad3814.$p" >"$r/manifests/c/chad3814/$p/9.9.9/chad3814.$p$f.yaml"; done
done

# run NAME env...: run the submitter, leaving output in $out and the log in $log
run() {
	local name=$1
	shift
	rm -f "$work/log"
	out=$(env PATH="$work/bin:$PATH" FAKE_LOG="$work/log" GH_TOKEN=sekrit-token-value "$@" "$here/winget-submit.sh" v9.9.9 "$r" 2>&1)
	st=$?
	log=$(cat "$work/log" 2>/dev/null)
}

run "new" FAKE_ZENVIK=404 FAKE_GUI=404
if [[ $st -eq 0 ]]; then ok "new packages: exit 0"; else bad "new packages: exit 0" "$out"; fi
if grep -q 'title=New package: chad3814.Zenvik version 9.9.9' <<<"$log" && grep -q 'title=New package: chad3814.ZenvikGUI version 9.9.9' <<<"$log"; then ok "new packages get 'New package' titles"; else bad "new packages get 'New package' titles" "$log"; fi
if grep -q 'head=chad3814:chad3814.Zenvik-9.9.9' <<<"$log"; then ok "PR head is the fork branch"; else bad "PR head is the fork branch" "$log"; fi
if grep -q 'refs/heads/chad3814.Zenvik-9.9.9' <<<"$log"; then ok "branch named <ID>-<ver>"; else bad "branch named <ID>-<ver>" "$log"; fi
n=$(grep -E '^\{"base_tree' <<<"$log" | head -1 | jq '.tree | length' 2>/dev/null)
if [[ $n == 3 ]]; then ok "one tree with exactly three files"; else bad "one tree with exactly three files" "got $n; $log"; fi
if grep -q 'manifests/c/chad3814/Zenvik/9.9.9/chad3814.Zenvik.installer.yaml' <<<"$log"; then ok "tree paths are the manifest paths"; else bad "tree paths are the manifest paths" "$log"; fi
if grep -q sekrit-token-value <<<"$out$log"; then bad "the token is never printed"; else ok "the token is never printed"; fi

run "update" FAKE_ZENVIK="9.9.8" FAKE_GUI="9.9.7 9.9.8"
if grep -q 'title=Update: chad3814.Zenvik to 9.9.9' <<<"$log"; then ok "an older upstream version gets an 'Update' title"; else bad "an older upstream version gets an 'Update' title" "$log"; fi

run "same" FAKE_ZENVIK="9.9.9" FAKE_GUI="9.9.10"
if [[ $st -eq 0 ]] && ! grep -q 'repos/microsoft/winget-pkgs/pulls -f' <<<"$log"; then ok "same or newer upstream: skipped, no PR"; else bad "same or newer upstream: skipped, no PR" "$log"; fi

run "open" FAKE_ZENVIK=9.9.8 FAKE_GUI=9.9.8 FAKE_OPEN=1
if [[ $st -eq 0 ]] && ! grep -q 'git/blobs' <<<"$log"; then ok "an open PR: skipped before any commit"; else bad "an open PR: skipped before any commit" "$log"; fi

if msg=$(GH_TOKEN=x "$here/winget-submit.sh" v9.9.9-rc1 "$r" 2>&1); then bad "a pre-release tag is refused" "$msg"; else ok "a pre-release tag is refused"; fi

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/winget-submit_test.sh && scripts/winget-submit_test.sh`
Expected: FAIL lines, non-zero exit.

- [ ] **Step 3: Implement**

`scripts/winget-submit.sh`:

```bash
#!/usr/bin/env bash
# Open microsoft/winget-pkgs pull requests for the manifests winget-render.sh
# wrote (the release workflow's winget-submit job runs this):
#
#   GH_TOKEN=… scripts/winget-submit.sh v1.3.0 <rendered dir>
#
# For each package under <dir>/manifests/c/chad3814/: skip it if winget-pkgs
# already has this version or a newer one, or an open PR from our branch;
# otherwise sync the fork's master with upstream, commit the package's three
# files to <ID>-<ver> on the fork in one commit, and open a PR. gh reads the
# token from GH_TOKEN; this script never prints it.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
upstream=${WINGET_UPSTREAM:-microsoft/winget-pkgs}
fork=${WINGET_FORK:-chad3814/winget-pkgs}
owner=${fork%%/*}
if [[ $# -ne 2 || ! $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 vX.Y.Z <rendered dir>" >&2
	exit 2
fi
tag=$1
dir=$2
ver=${tag#v}
[[ -n ${GH_TOKEN:-} ]] || { echo "winget-submit: GH_TOKEN is not set" >&2; exit 1; }

for pkgdir in "$dir"/manifests/c/chad3814/*/"$ver"; do
	[[ -d $pkgdir ]] || continue
	pkg=$(basename "$(dirname "$pkgdir")")
	id="chad3814.$pkg"
	path="manifests/c/chad3814/$pkg"

	# Versions upstream already has (a 404 means a new package).
	new=0
	if ! existing=$(gh api "repos/$upstream/contents/$path" --jq '.[] | select(.type == "dir") | .name' 2>"$dir/.err"); then
		if grep -q 'HTTP 404' "$dir/.err"; then
			new=1
			existing=""
		else
			cat "$dir/.err" >&2
			exit 1
		fi
	fi
	highest=$(printf '%s\n' "$existing" | grep -v '^$' | sort -V | tail -1 || true)
	st=0
	"$here/newer-version.sh" "$tag" "$highest" || st=$?
	if [[ $st -eq 3 ]]; then
		echo "$id: winget-pkgs already has $highest; skipping"
		continue
	elif [[ $st -ne 0 ]]; then
		exit "$st"
	fi

	branch="$id-$ver"
	open=$(gh api "repos/$upstream/pulls?state=open&head=$owner:$branch" --jq length)
	if [[ $open != 0 ]]; then
		echo "$id: a PR for $ver is already open; skipping"
		continue
	fi

	gh api -X POST "repos/$fork/merge-upstream" -f branch=master >/dev/null
	base=$(gh api "repos/$fork/git/ref/heads/master" --jq .object.sha)
	basetree=$(gh api "repos/$fork/git/commits/$base" --jq .tree.sha)
	entries="[]"
	for f in "$pkgdir"/*.yaml; do
		blob=$(base64 <"$f" | tr -d '\n' | jq -Rs '{encoding: "base64", content: .}' |
			gh api "repos/$fork/git/blobs" --input - --jq .sha)
		entries=$(jq --arg p "$path/$ver/$(basename "$f")" --arg s "$blob" \
			'. + [{path: $p, mode: "100644", type: "blob", sha: $s}]' <<<"$entries")
	done
	tree=$(jq -n --arg b "$basetree" --argjson t "$entries" '{base_tree: $b, tree: $t}' |
		gh api "repos/$fork/git/trees" --input - --jq .sha)
	if [[ $new == 1 ]]; then
		title="New package: $id version $ver"
	else
		title="Update: $id to $ver"
	fi
	commit=$(jq -n --arg m "$title" --arg t "$tree" --arg p "$base" '{message: $m, tree: $t, parents: [$p]}' |
		gh api "repos/$fork/git/commits" --input - --jq .sha)
	if ! gh api "repos/$fork/git/refs" -f ref="refs/heads/$branch" -f sha="$commit" >/dev/null 2>&1; then
		gh api -X PATCH "repos/$fork/git/refs/heads/$branch" -f sha="$commit" -F force=true >/dev/null
	fi
	body="## 📖 Description
$title, from https://github.com/chad3814/zenvik/releases/tag/$tag (zip-wrapped portable).

## 📦 Manifest Checklist

- [x] Checked that there aren't other open [pull requests](https://github.com/microsoft/winget-pkgs/pulls) for the same manifest update/change
- [x] This PR only modifies one (1) manifest
- [x] Validated manifest locally with \`winget validate --manifest <path>\`
- [x] Tested manifest locally with \`winget install --manifest <path>\`
- [x] Manifest conforms to the [1.12 schema](https://github.com/microsoft/winget-pkgs/tree/master/doc/manifest/schema/1.12.0)"
	url=$(gh api "repos/$upstream/pulls" -f title="$title" -f head="$owner:$branch" -f base=master -f body="$body" --jq .html_url)
	echo "$id: opened $url"
done
rm -f "$dir/.err"
```

The fake `gh` matches the commit-message call by `git/commits` *without* a SHA after it. The `git/commits/base123` lookup comes first in its `case`, which keeps the two apart.

- [ ] **Step 4: Run the tests and lint**

Run: `scripts/winget-submit_test.sh && shellcheck -x scripts/winget-submit*.sh`
Expected: `11 passed, 0 failed`, and no lint output. The fake logs each `--input` body as one compact JSON line, so the tree check reads the `{"base_tree":…}` line directly.

- [ ] **Step 5: Commit**

```bash
git add scripts/winget-submit.sh scripts/winget-submit_test.sh
git commit -m "winget-submit.sh: open microsoft/winget-pkgs PRs from the fork, one commit per package, skipping versions it has or PRs already open"
```

---

### Task 6: `scripts/choco-render.sh`

**Files:**
- Create: `scripts/choco-render.sh`, `scripts/choco-render_test.sh`

**Interfaces:**
- Produces: `scripts/choco-render.sh <tag> <outdir>`, which writes:
  - `<outdir>/zenvik/{zenvik.nuspec,tools/chocolateyinstall.ps1}`;
  - `<outdir>/zenvik-gui/{zenvik-gui.nuspec,tools/chocolateyinstall.ps1,tools/chocolateyuninstall.ps1}`.

  Exit codes match the other render scripts, and it honours `ZENVIK_RELEASE_BASE_URL`.

- [ ] **Step 1: Write the failing test**

`scripts/choco-render_test.sh`:

```bash
#!/usr/bin/env bash
# Tests for scripts/choco-render.sh (offline, file:// fixtures).
#
#   scripts/choco-render_test.sh
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
pass=0
fail=0
ok() { pass=$((pass + 1)); echo "ok   $1"; }
bad() { fail=$((fail + 1)); echo "FAIL $1"; [[ -n ${2:-} ]] && echo "     $2"; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cli=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
gui=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
decoy=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
fixture() {
	local fx="$work/fx-$RANDOM"
	mkdir -p "$fx/releases/download/$1"
	printf '%s' "$2" >"$fx/releases/download/$1/SHA256SUMS"
	echo "file://$fx"
}
good="$gui  zenvik-gui_9.9.9_windows_amd64.zip
$decoy  zenvik_9.9.90_windows_amd64.zip
$cli  zenvik_9.9.9_windows_amd64.zip
"
out="$work/out"
mkdir -p "$out/zenvik" && echo old >"$out/zenvik/zenvik.nuspec"
if msg=$(ZENVIK_RELEASE_BASE_URL=$(fixture v9.9.9 "$good") "$here/choco-render.sh" v9.9.9 "$out" 2>&1); then ok "renders a final release"; else bad "renders a final release" "$msg"; fi

contains() { # contains NAME FILE TEXT
	if grep -qF -- "$3" "$2" 2>/dev/null; then ok "$1"; else bad "$1" "missing [$3] in $2"; fi
}
xmlok() {
	if python3 -c 'import sys,xml.dom.minidom as m; m.parse(sys.argv[1])' "$2" 2>/dev/null; then ok "$1"; else bad "$1"; fi
}
xmlok "zenvik.nuspec is XML" "$out/zenvik/zenvik.nuspec"
xmlok "zenvik-gui.nuspec is XML" "$out/zenvik-gui/zenvik-gui.nuspec"
contains "CLI id" "$out/zenvik/zenvik.nuspec" "<id>zenvik</id>"
contains "CLI version" "$out/zenvik/zenvik.nuspec" "<version>9.9.9</version>"
contains "packageSourceUrl" "$out/zenvik/zenvik.nuspec" "<packageSourceUrl>https://github.com/chad3814/zenvik/blob/main/scripts/choco-render.sh</packageSourceUrl>"
contains "authors" "$out/zenvik/zenvik.nuspec" "<authors>Chad Walker</authors>"
contains "GUI id" "$out/zenvik-gui/zenvik-gui.nuspec" "<id>zenvik-gui</id>"
I="$out/zenvik/tools/chocolateyinstall.ps1"
GI="$out/zenvik-gui/tools/chocolateyinstall.ps1"
contains "CLI url" "$I" "https://github.com/chad3814/zenvik/releases/download/v9.9.9/zenvik_9.9.9_windows_amd64.zip"
contains "CLI checksum, the CLI's" "$I" "$cli"
contains "CLI hides the bundled mkvmerge from shims" "$I" "mkvmerge.exe.ignore"
contains "CLI unpack folder" "$I" "zenvik_9.9.9_windows_amd64"
contains "GUI checksum, the GUI's" "$GI" "$gui"
contains "GUI hides the bundled mkvmerge from shims" "$GI" "mkvmerge.exe.ignore"
contains "GUI gets a windowed shim" "$GI" "zenvik-gui.exe.gui"
contains "GUI Start-menu shortcut" "$GI" "Zenvik.lnk"
contains "GUI uninstall removes the shortcut" "$out/zenvik-gui/tools/chocolateyuninstall.ps1" "Zenvik.lnk"
if grep -q old "$out/zenvik/zenvik.nuspec" || grep -rq "$decoy" "$out" || grep -rq '@@' "$out" || grep -rq 'file:' "$out"; then bad "no old files, decoys, placeholders or override URLs left"; else ok "no old files, decoys, placeholders or override URLs left"; fi

expect_fail() {
	local o="$work/fail-$RANDOM" st msg
	mkdir -p "$o"
	msg=$(ZENVIK_RELEASE_BASE_URL=$4 "$here/choco-render.sh" "$3" "$o" 2>&1)
	st=$?
	if [[ $st -ne $2 ]]; then bad "$1" "status $st (want $2): $msg"; elif [[ -n $(ls -A "$o") ]]; then bad "$1" "wrote into outdir"; else ok "$1"; fi
}
expect_fail "pre-release tag is refused" 2 v9.9.9-rc1 "$(fixture v9.9.9-rc1 "$good")"
expect_fail "missing CLI line fails" 1 v9.9.9 "$(fixture v9.9.9 "$gui  zenvik-gui_9.9.9_windows_amd64.zip
")"
expect_fail "malformed hash fails" 1 v9.9.9 "$(fixture v9.9.9 "XYZ  zenvik_9.9.9_windows_amd64.zip
$gui  zenvik-gui_9.9.9_windows_amd64.zip
")"

echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
```

- [ ] **Step 2: Run it to verify it fails**

Run: `chmod +x scripts/choco-render_test.sh && scripts/choco-render_test.sh`
Expected: FAIL lines, non-zero exit.

- [ ] **Step 3: Implement**

`scripts/choco-render.sh`. The PowerShell and XML bodies are quoted heredocs with `@@NAME@@` placeholders, which `sed` fills in; that way bash never expands PowerShell's `$` variables:

```bash
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
# ZENVIK_RELEASE_BASE_URL (default the GitHub repo).
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
die() { echo "choco-render: $*" >&2; exit 1; }

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
    <iconUrl>https://raw.githubusercontent.com/chad3814/zenvik/main/gui/build/appicon.png</iconUrl>
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
	fill "$5" "ID=$1" "VER=$ver" "TITLE=$2" "REPO=$repo" "TAG=$tag" "SUMMARY=$3" "DESCRIPTION=$4"
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
```

`fill` uses `|` as the `sed` delimiter. None of the values contain `|`. URLs contain `/`, which is why `/` isn't the delimiter.

- [ ] **Step 4: Run the tests and lint**

Run: `scripts/choco-render_test.sh && shellcheck -x scripts/choco-render*.sh`
Expected: `21 passed, 0 failed`. If shellcheck flags SC2016 in a quoted heredoc, that's a false positive for PowerShell; add a comment explaining it rather than changing the quoting.

- [ ] **Step 5: Commit**

```bash
git add scripts/choco-render.sh scripts/choco-render_test.sh
git commit -m "choco-render.sh: write the zenvik and zenvik-gui Chocolatey packages for a final release"
```

---

### Task 7: Release jobs and docs

**Files:**
- Modify: `.github/workflows/release.yml` (append six jobs)
- Modify: `README.md`, `CLAUDE.md`

**Interfaces:**
- Consumes:
  - `scripts/winget-render.sh` (Task 4) and `scripts/winget-submit.sh` (Task 5);
  - `scripts/choco-render.sh` (Task 6);
  - secrets `WINGET_TOKEN` and `CHOCO_API_KEY` in the `release` environment.

- [ ] **Step 1: Append the jobs**

```yaml
  # winget (microsoft/winget-pkgs via the fork chad3814/winget-pkgs), final
  # releases only: render, install-test on Windows, then open one PR per
  # package. Only winget-submit reads WINGET_TOKEN.
  winget-render:
    if: ${{ !contains(github.ref_name, '-') }}
    needs: publish
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
      - run: scripts/winget-render.sh "$GITHUB_REF_NAME" winget
      - uses: actions/upload-artifact@v4
        with:
          name: winget
          path: winget

  winget-check:
    needs: winget-render
    runs-on: windows-latest
    defaults:
      run:
        shell: pwsh
    steps:
      - uses: actions/download-artifact@v5
        with:
          name: winget
          path: ${{ runner.temp }}/winget
      - name: Validate, install, check and uninstall both packages
        run: |
          $ErrorActionPreference = 'Stop'
          function Run { $cmd = $args[0]; $rest = @($args | Select-Object -Skip 1); & $cmd @rest; if ($LASTEXITCODE -ne 0) { throw "$($args -join ' ') exited with $LASTEXITCODE" } }
          if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
            Install-PackageProvider -Name NuGet -Force | Out-Null
            Install-Module Microsoft.WinGet.Client -Force -Scope AllUsers
            Repair-WinGetPackageManager -Latest -Force
          }
          Run winget settings --enable LocalManifestFiles
          $ver = "$env:GITHUB_REF_NAME".TrimStart('v')
          $root = "$env:RUNNER_TEMP\winget\manifests\c\chad3814"
          # Portable installs go to user or machine scope; look in both.
          $links = @("$env:LOCALAPPDATA\Microsoft\WinGet\Links", "$env:ProgramFiles\WinGet\Links")
          $pkgs = @("$env:LOCALAPPDATA\Microsoft\WinGet\Packages", "$env:ProgramFiles\WinGet\Packages")
          foreach ($p in 'Zenvik', 'ZenvikGUI') {
            $dir = "$root\$p\$ver"
            Run winget validate --manifest $dir
            Run winget install --manifest $dir --accept-package-agreements --accept-source-agreements --disable-interactivity
          }
          $cli = $links | ForEach-Object { Join-Path $_ 'zenvik.exe' } | Where-Object { Test-Path $_ } | Select-Object -First 1
          if (-not $cli) { throw 'no zenvik.exe link after install' }
          $out = (Run $cli --version) -join "`n"
          if ($out -notlike "zenvik v$ver*") { throw "zenvik --version printed: $out" }
          foreach ($id in 'chad3814.Zenvik', 'chad3814.ZenvikGUI') {
            $home_ = $pkgs | ForEach-Object { Get-ChildItem $_ -Directory -Filter "${id}_*" -ErrorAction SilentlyContinue } | Select-Object -First 1
            if (-not $home_) { throw "no package folder for $id" }
            $mk = Get-ChildItem $home_.FullName -Recurse -Filter mkvmerge.exe | Select-Object -First 1
            if (-not $mk) { throw "no bundled mkvmerge.exe for $id" }
            $out = (Run $mk.FullName --version) -join "`n"
            if ($out -notlike 'mkvmerge v*') { throw "$id's mkvmerge.exe printed: $out" }
          }
          foreach ($id in 'chad3814.ZenvikGUI', 'chad3814.Zenvik') {
            Run winget uninstall --id $id --disable-interactivity
          }
          Write-Host 'winget-check: all checks passed'

  winget-submit:
    needs: winget-check
    runs-on: ubuntu-latest
    environment: release
    concurrency: { group: winget-submit, cancel-in-progress: false }
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
      - uses: actions/download-artifact@v5
        with:
          name: winget
          path: ${{ runner.temp }}/winget
      - name: Open microsoft/winget-pkgs PRs
        env:
          GH_TOKEN: ${{ secrets.WINGET_TOKEN }}
        run: scripts/winget-submit.sh "$GITHUB_REF_NAME" "$RUNNER_TEMP/winget"

  # Chocolatey (community feed), final releases only: render, pack and
  # install-test on Windows, then push. Only choco-push reads CHOCO_API_KEY.
  choco-render:
    if: ${{ !contains(github.ref_name, '-') }}
    needs: publish
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
        with:
          persist-credentials: false
      - run: scripts/choco-render.sh "$GITHUB_REF_NAME" choco
      - uses: actions/upload-artifact@v4
        with:
          name: choco
          path: choco

  choco-check:
    needs: choco-render
    runs-on: windows-latest
    defaults:
      run:
        shell: pwsh
    steps:
      - uses: actions/download-artifact@v5
        with:
          name: choco
          path: ${{ runner.temp }}/choco
      - name: Pack, install, check and uninstall both packages
        run: |
          $ErrorActionPreference = 'Stop'
          function Run { $cmd = $args[0]; $rest = @($args | Select-Object -Skip 1); & $cmd @rest; if ($LASTEXITCODE -ne 0) { throw "$($args -join ' ') exited with $LASTEXITCODE" } }
          $ver = "$env:GITHUB_REF_NAME".TrimStart('v')
          $src = "$env:RUNNER_TEMP\choco"
          $nupkg = "$env:RUNNER_TEMP\nupkg"
          New-Item -ItemType Directory -Force $nupkg | Out-Null
          foreach ($id in 'zenvik', 'zenvik-gui') {
            Run choco pack "$src\$id\$id.nuspec" --outputdirectory $nupkg
            Run choco install $id --source $nupkg -y --no-progress
          }
          $out = (Run zenvik --version) -join "`n"
          if ($out -notlike "zenvik v$ver*") { throw "zenvik --version printed: $out" }
          if (Test-Path "$env:ChocolateyInstall\bin\mkvmerge.exe") { throw 'the bundled mkvmerge.exe got a shim on PATH' }
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
      - uses: actions/upload-artifact@v4
        with:
          name: nupkg
          path: ${{ runner.temp }}/nupkg/*.nupkg

  choco-push:
    needs: choco-check
    runs-on: windows-latest
    environment: release
    concurrency: { group: choco-push, cancel-in-progress: false }
    defaults:
      run:
        shell: pwsh
    steps:
      - uses: actions/download-artifact@v5
        with:
          name: nupkg
          path: ${{ runner.temp }}/nupkg
      - name: Push to the Chocolatey community feed
        env:
          CHOCO_API_KEY: ${{ secrets.CHOCO_API_KEY }}
        run: |
          $ErrorActionPreference = 'Stop'
          if (-not $env:CHOCO_API_KEY) { throw 'CHOCO_API_KEY is not set in the release environment' }
          $ver = [version]"$env:GITHUB_REF_NAME".TrimStart('v')
          foreach ($id in 'zenvik', 'zenvik-gui') {
            $file = "$env:RUNNER_TEMP\nupkg\$id.$ver.nupkg"
            $feed = 'https://community.chocolatey.org/api/v2'
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
            $out = choco push $file --source https://push.chocolatey.org/ --api-key $env:CHOCO_API_KEY 2>&1 | Out-String
            Write-Host $out
            if ($LASTEXITCODE -ne 0) {
              if ($out -match 'already exists|409|Conflict') { Write-Host "$id $ver was already submitted; skipping"; continue }
              throw "choco push $id exited with $LASTEXITCODE"
            }
          }
```

- [ ] **Step 2: Lint**

Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest`
Expected: no output.

- [ ] **Step 3: Docs**

**README.md:** in the install section, replace the Scoop paragraph with:

```markdown
On Windows, with [winget](https://learn.microsoft.com/windows/package-manager/): `winget install chad3814.Zenvik` (the CLI) or `winget install chad3814.ZenvikGUI` (the desktop app). With [Chocolatey](https://chocolatey.org): `choco install zenvik` or `choco install zenvik-gui`. With [Scoop](https://scoop.sh): `scoop bucket add chad3814 https://github.com/chad3814/scoop-bucket`, then `scoop install chad3814/zenvik` or `chad3814/zenvik-gui`. The Windows CLI includes `mkvmerge.exe` from MKVToolNix (GPLv2; the notices and a source link are in the download), so it needs nothing else installed.
```

**CLAUDE.md:**
- Change the Bundled mkvmerge bullet's opening to `- Bundled mkvmerge (desktop app on macOS and Windows; the Windows CLI zip too):`. Add, after its `release-gui.sh` clause: `; release-build.sh adds it beside zenvik.exe in the Windows CLI zip, where internal/mux finds it (config, then beside the symlink-resolved executable on Windows, then PATH)`.
- In the Scoop bullet, change `with depends: extras/mkvtoolnix` to `(the CLI zip bundles mkvmerge.exe, so no dependency)`.
- Add after the Scoop bullet:

```markdown
- winget (`chad3814.Zenvik`, `chad3814.ZenvikGUI`, zip-wrapped portable, schema 1.12.0): `scripts/winget-render.sh vX.Y.Z outdir` writes the manifests (tests: `scripts/winget-render_test.sh`); `scripts/winget-submit.sh` opens the microsoft/winget-pkgs PRs from the fork `chad3814/winget-pkgs` (tests: `scripts/winget-submit_test.sh`, a fake gh). Release jobs `winget-render`, `winget-check` (validate + install-test on `windows-latest`) and `winget-submit`; only `winget-submit` reads the `release` environment secret `WINGET_TOKEN` (a classic token). Never print or commit it.
- Chocolatey (`zenvik`, `zenvik-gui`): `scripts/choco-render.sh vX.Y.Z outdir` writes the package sources (tests: `scripts/choco-render_test.sh`). Release jobs `choco-render`, `choco-check` (pack + install-test on `windows-latest`) and `choco-push`; only `choco-push` reads `CHOCO_API_KEY`. New versions wait in Chocolatey's moderation. Never print or commit the key.
```

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml README.md CLAUDE.md
git commit -m "Release: publish to winget (PRs into microsoft/winget-pkgs) and Chocolatey after install checks on Windows"
```

---

### Task 8: Setup, review, merge and v1.3.0

- [ ] **Step 1: Check the secrets exist** with `gh secret list --env release -R chad3814/zenvik | cut -f1`. Expected: `WINGET_TOKEN` and `CHOCO_API_KEY` among the names. If `CHOCO_API_KEY` is missing, ask the user to add it, and continue with Steps 2–3 while waiting.
- [ ] **Step 2: Full local checks:**
  - `go test -race ./...`, `go vet ./...`, `golangci-lint run`
  - every `scripts/*_test.sh`, with `ZENVIK_NET_TESTS=1` for `release-build_test.sh` and `fetch-mkvmerge_test.sh`, then `brew developer off`
  - `shellcheck -x scripts/*.sh`
  - actionlint

  Expected: all pass.
- [ ] **Step 3: Final whole-branch review** (executor's review step), then the fix pass, if any.
- [ ] **Step 4: Ask the user, then merge and push** to `main`, and watch CI.
- [ ] **Step 5: Ask the user, then tag `v1.3.0` and push it.** Watch every job. Then:
  - the bucket gets `zenvik 1.3.0` without `depends`;
  - two winget PRs open: link them, and remind the user about Microsoft's CLA bot on the first;
  - two Chocolatey packages enter moderation: link `https://community.chocolatey.org/packages/zenvik/1.3.0` and the zenvik-gui equivalent.
- [ ] **Step 6: Ask the user, then clean up the bucket** now that its manifests bundle `mkvmerge.exe`:
  - in `check.ps1`, drop the `add the extras bucket` step;
  - in `README.md`, drop the `scoop bucket add extras` line;
  - in `.github/workflows/ci.yml`, drop the informational Extras step.

  Push, and watch the bucket's CI.
