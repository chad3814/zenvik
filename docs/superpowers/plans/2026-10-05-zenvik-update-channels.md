# zenvik update check: install channels Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the update notice name the upgrade command for the package manager zenvik was installed with, and for winget and Chocolatey installs ask that manager's own feed so the notice appears only once the package is installable there.

**Architecture:** `internal/update` gains a `Channel` (detected from the binary's own path, no network) and two more sources behind the existing `Checker`: the winget-pkgs directory listing and the Chocolatey community Atom feed. The cache becomes one file per source. The CLI and GUI build their checker from `DetectSelf` and read the checker's `Channel` for wording; the GUI adds one bound method, `UpgradeHint`, for the About dialog.

**Tech Stack:** Go 1.27 (`net/http`, `encoding/json`, `encoding/xml`, `httptest`), cobra, Wails v2.16 + React 19 / TypeScript / Vitest.

**Spec:** `docs/superpowers/specs/2026-10-05-zenvik-update-check-design.md`, section "Install channels (amendment, 2026-10-05)". The rest of that spec still applies.

## Global Constraints

- Root module: no cgo; third-party deps limited to `spf13/cobra` and `pelletier/go-toml/v2`; `internal/update` imports only the standard library; sentinel errors wrapped with `%w`; `golangci-lint run` clean (errorlint, misspell, unconvert, gofmt, goimports).
- `gui/` bound methods take and return only primitives, string slices and `error`; TypeScript strict, never `any`; never hand-edit `gui/frontend/wailsjs/`.
- Tests never touch real config/state (`TestMain`s set XDG dirs; checker tests set `CachePath` or `XDG_STATE_HOME` to temp dirs) and never reach the network unless `ZENVIK_NET_TESTS=1`.
- `Checker`'s zero value keeps today's behaviour exactly: GitHub source, `update-check.json`, so every existing test passes unchanged.
- Detection matches, on the lower-cased, forward-slashed executable path: CLI `"/cellar/zenvik/"`, `"/scoop/apps/zenvik/"`, `"/winget/packages/chad3814.zenvik_"`, `"/chocolatey/lib/zenvik/"`; GUI `"/zenvik.app/"` plus a `Caskroom/zenvik-gui` directory under `/opt/homebrew` or `/usr/local`, `"/scoop/apps/zenvik-gui/"`, `"/winget/packages/chad3814.zenvikgui_"`, `"/chocolatey/lib/zenvik-gui/"`.
- Channel names: `Homebrew`, `Scoop`, `winget`, `Chocolatey`. Upgrade commands: `brew upgrade zenvik`, `brew upgrade --cask zenvik-gui`, `scoop update zenvik`, `scoop update zenvik-gui`, `winget upgrade chad3814.Zenvik`, `winget upgrade chad3814.ZenvikGUI`, `choco upgrade zenvik`, `choco upgrade zenvik-gui`.
- Sources: winget `GET https://api.github.com/repos/microsoft/winget-pkgs/contents/manifests/c/chad3814/<Zenvik|ZenvikGUI>` (GitHub headers; JSON array of `{name,type}`; highest `dir` whose `v`+name parses; 404 = "not on winget yet"); Chocolatey `GET https://community.chocolatey.org/api/v2/FindPackagesById()?id='<zenvik|zenvik-gui>'` (`Accept: application/atom+xml`; highest `entry/properties/Version` with `IsApproved` `true`; empty feed = "not on Chocolatey yet"; none approved = "no approved version on Chocolatey"). `Result.URL` is empty for both.
- Cache files: GitHub `update-check.json`; winget `update-check-winget-<id>.json`; Chocolatey `update-check-chocolatey-<id>.json`. Each records `"source"`: `"github"`, `"winget:<id>"`, `"chocolatey:<id>"`; a missing `source` means `github` (files written before this change); any other mismatch counts as absent.
- Copy, verbatim:
  - CLI managed: `zenvik: v1.3.0 is available (you have v1.2.0); upgrade with: brew upgrade zenvik`; direct unchanged.
  - doctor managed: `✓ zenvik v1.2.0 is the latest on Chocolatey`, `! zenvik v1.3.0 is available on winget: winget upgrade chad3814.Zenvik`, `! update check on winget: <detail>`; direct lines and the dev line unchanged.
  - GUI banner managed: `Zenvik v1.3.0 is available. Upgrade with: brew upgrade --cask zenvik-gui`, no action, no URL; direct unchanged.
  - About managed: `v1.3.0 is available` then ` · upgrade with: ` and the command in a `<code>` element; direct keeps the Download link.

## Review Focus

1. **A Scoop or Chocolatey user whose binary is launched through the manager's shim**: the real process's `os.Executable` is the unpacked path under `scoop/apps` or `chocolatey/lib`, so detection holds. Task 1 pins both shapes, including the Scoop `current` junction form.
2. **A user who copies the binary out of a managed location**: detection says direct and the link is shown; no wrong command. Task 1's "direct" rows.
3. **A cache written by the previous release of zenvik** (no `source` field): must be honoured as GitHub, not refetched forever. Task 2's `TestCacheIsPerSource` back-compat case.
4. **A Chocolatey version in moderation that is newer than the approved one**: the approved one is reported as latest. Task 2's feed fixture.
5. **The GUI from the cask next to a direct-download CLI**: they ask the same GitHub source and share `update-check.json`, as before. Covered by the zero-value guarantee and Task 2's per-source test.

---

### Task 1: `Channel` and `Detect`

**Files:**
- Create: `internal/update/channel.go`
- Create: `internal/update/channel_test.go`

**Interfaces:**
- Produces: `type Product int` with `CLI`, `GUI`; `type Channel struct{ Name, Upgrade string; source sourceKind; pkg string }`; `func (c Channel) Managed() bool`; `func Detect(p Product, exe string, dirExists func(string) bool) Channel`; `func DetectSelf(p Product) Channel`; unexported `sourceKind` with `sourceGitHub`, `sourceWinget`, `sourceChocolatey`; `func (c Channel) sourceKey() string`; `func (c Channel) cacheName() string`. Tasks 2–4 use these.

- [ ] **Step 1: Write the failing tests**

`internal/update/channel_test.go`:

```go
package update

import "testing"

func TestDetect(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	cases := []struct {
		name      string
		p         Product
		exe       string
		dirExists func(string) bool
		wantName  string
		wantCmd   string
		wantKey   string
		wantCache string
	}{
		{"brew formula arm", CLI, "/opt/homebrew/Cellar/zenvik/1.2.1/bin/zenvik", no, "Homebrew", "brew upgrade zenvik", "github", "update-check.json"},
		{"brew formula intel", CLI, "/usr/local/Cellar/zenvik/1.2.1/bin/zenvik", no, "Homebrew", "brew upgrade zenvik", "github", "update-check.json"},
		{"scoop user", CLI, `C:\Users\chad\scoop\apps\zenvik\1.2.1\zenvik.exe`, no, "Scoop", "scoop update zenvik", "github", "update-check.json"},
		{"scoop current junction", CLI, `C:\Users\chad\scoop\apps\zenvik\current\zenvik.exe`, no, "Scoop", "scoop update zenvik", "github", "update-check.json"},
		{"scoop global gui", GUI, `C:\ProgramData\scoop\apps\zenvik-gui\1.2.1\zenvik-gui.exe`, no, "Scoop", "scoop update zenvik-gui", "github", "update-check.json"},
		{"winget cli", CLI, `C:\Users\chad\AppData\Local\Microsoft\WinGet\Packages\chad3814.Zenvik_Microsoft.Winget.Source_8wekyb3d8bbwe\zenvik_1.2.1_windows_amd64\zenvik.exe`, no, "winget", "winget upgrade chad3814.Zenvik", "winget:chad3814.Zenvik", "update-check-winget-chad3814.Zenvik.json"},
		{"winget gui", GUI, `C:\Users\chad\AppData\Local\Microsoft\WinGet\Packages\chad3814.ZenvikGUI_Microsoft.Winget.Source_8wekyb3d8bbwe\zenvik-gui_1.2.1_windows_amd64\zenvik-gui.exe`, no, "winget", "winget upgrade chad3814.ZenvikGUI", "winget:chad3814.ZenvikGUI", "update-check-winget-chad3814.ZenvikGUI.json"},
		{"choco cli", CLI, `C:\ProgramData\chocolatey\lib\zenvik\tools\zenvik_1.2.1_windows_amd64\zenvik.exe`, no, "Chocolatey", "choco upgrade zenvik", "chocolatey:zenvik", "update-check-chocolatey-zenvik.json"},
		{"choco gui", GUI, `C:\ProgramData\chocolatey\lib\zenvik-gui\tools\zenvik-gui_1.2.1_windows_amd64\zenvik-gui.exe`, no, "Chocolatey", "choco upgrade zenvik-gui", "chocolatey:zenvik-gui", "update-check-chocolatey-zenvik-gui.json"},
		{"cask", GUI, "/Applications/Zenvik.app/Contents/MacOS/zenvik-gui", yes, "Homebrew", "brew upgrade --cask zenvik-gui", "github", "update-check.json"},
		{"app without caskroom", GUI, "/Applications/Zenvik.app/Contents/MacOS/zenvik-gui", no, "", "", "github", "update-check.json"},
		{"app with nil dirExists", GUI, "/Applications/Zenvik.app/Contents/MacOS/zenvik-gui", nil, "", "", "github", "update-check.json"},
		{"direct unix", CLI, "/usr/local/bin/zenvik", no, "", "", "github", "update-check.json"},
		{"direct windows", CLI, `C:\Tools\zenvik\zenvik.exe`, no, "", "", "github", "update-check.json"},
		{"empty", CLI, "", yes, "", "", "github", "update-check.json"},
		{"cli in gui package", CLI, `C:\ProgramData\chocolatey\lib\zenvik-gui\tools\zenvik-gui_1.2.1_windows_amd64\zenvik.exe`, no, "", "", "github", "update-check.json"},
		{"gui in cli package", GUI, `C:\ProgramData\chocolatey\lib\zenvik\tools\zenvik_1.2.1_windows_amd64\zenvik-gui.exe`, no, "", "", "github", "update-check.json"},
		{"cli does not use cask rule", CLI, "/Applications/Zenvik.app/Contents/MacOS/zenvik", yes, "", "", "github", "update-check.json"},
		{"mixed case", CLI, `c:\programdata\CHOCOLATEY\LIB\Zenvik\tools\x\zenvik.exe`, no, "Chocolatey", "choco upgrade zenvik", "chocolatey:zenvik", "update-check-chocolatey-zenvik.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := Detect(tc.p, tc.exe, tc.dirExists)
			if ch.Name != tc.wantName || ch.Upgrade != tc.wantCmd {
				t.Errorf("Detect = %+v, want name %q, upgrade %q", ch, tc.wantName, tc.wantCmd)
			}
			if ch.Managed() != (tc.wantName != "") {
				t.Errorf("Managed() = %v for %+v", ch.Managed(), ch)
			}
			if ch.sourceKey() != tc.wantKey || ch.cacheName() != tc.wantCache {
				t.Errorf("source %q cache %q, want %q %q", ch.sourceKey(), ch.cacheName(), tc.wantKey, tc.wantCache)
			}
		})
	}
}

func TestCaskPrefixes(t *testing.T) {
	var asked []string
	exists := func(p string) bool {
		asked = append(asked, p)
		return p == "/usr/local/Caskroom/zenvik-gui"
	}
	ch := Detect(GUI, "/Applications/Zenvik.app/Contents/MacOS/zenvik-gui", exists)
	if ch.Name != "Homebrew" {
		t.Errorf("Detect = %+v, want the cask under /usr/local", ch)
	}
	if len(asked) != 2 || asked[0] != "/opt/homebrew/Caskroom/zenvik-gui" || asked[1] != "/usr/local/Caskroom/zenvik-gui" {
		t.Errorf("asked %v, want both prefixes in order", asked)
	}
}

func TestDetectSelfIsDirectInTests(t *testing.T) {
	// The test binary lives in a temp build dir: never a managed location.
	if ch := DetectSelf(CLI); ch.Managed() {
		t.Errorf("DetectSelf = %+v, want direct", ch)
	}
	if ch := DetectSelf(GUI); ch.Managed() {
		t.Errorf("DetectSelf(GUI) = %+v, want direct", ch)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/update/ -run 'TestDetect|TestCask'`
Expected: FAIL to compile: `undefined: Detect`, `undefined: Product`, `undefined: CLI`.

- [ ] **Step 3: Implement**

`internal/update/channel.go`:

```go
package update

import (
	"os"
	"path/filepath"
	"strings"
)

// Product says which binary is asking, so detection matches its own
// package names.
type Product int

const (
	CLI Product = iota
	GUI
)

// sourceKind is which feed answers "what is the latest release".
type sourceKind int

const (
	sourceGitHub sourceKind = iota
	sourceWinget
	sourceChocolatey
)

// Channel is where this binary was installed from. The zero value is a
// direct download: GitHub is the source and the notice links the releases
// page.
type Channel struct {
	Name    string // "Homebrew", "Scoop", "winget", "Chocolatey"; "" for direct
	Upgrade string // the command that upgrades; "" for direct
	source  sourceKind
	pkg     string // the package id source is asked about; "" for GitHub
}

// Managed reports whether a package manager owns this install.
func (c Channel) Managed() bool { return c.Name != "" }

// sourceKey names the source in the cache file's "source" field.
func (c Channel) sourceKey() string {
	switch c.source {
	case sourceWinget:
		return "winget:" + c.pkg
	case sourceChocolatey:
		return "chocolatey:" + c.pkg
	}
	return "github"
}

// cacheName is the cache file for this source: one per source, because the
// formula CLI and the cask app share a state directory but may ask
// different feeds.
func (c Channel) cacheName() string {
	switch c.source {
	case sourceWinget:
		return "update-check-winget-" + c.pkg + ".json"
	case sourceChocolatey:
		return "update-check-chocolatey-" + c.pkg + ".json"
	}
	return "update-check.json"
}

// brewPrefixes are where Homebrew keeps its Caskroom on Apple silicon and
// Intel Macs.
var brewPrefixes = []string{"/opt/homebrew", "/usr/local"}

// Detect classifies exe for p by where the published packages unpack to.
// dirExists answers the Caskroom check; tests inject it. Detect never
// touches the network. Each product matches only its own package names;
// anything else is a direct download.
func Detect(p Product, exe string, dirExists func(string) bool) Channel {
	path := strings.ToLower(strings.ReplaceAll(exe, `\`, "/"))
	if path == "" {
		return Channel{}
	}
	switch p {
	case CLI:
		switch {
		case strings.Contains(path, "/cellar/zenvik/"):
			return Channel{Name: "Homebrew", Upgrade: "brew upgrade zenvik"}
		case strings.Contains(path, "/scoop/apps/zenvik/"):
			return Channel{Name: "Scoop", Upgrade: "scoop update zenvik"}
		case strings.Contains(path, "/winget/packages/chad3814.zenvik_"):
			return Channel{Name: "winget", Upgrade: "winget upgrade chad3814.Zenvik", source: sourceWinget, pkg: "chad3814.Zenvik"}
		case strings.Contains(path, "/chocolatey/lib/zenvik/"):
			return Channel{Name: "Chocolatey", Upgrade: "choco upgrade zenvik", source: sourceChocolatey, pkg: "zenvik"}
		}
	case GUI:
		switch {
		case strings.Contains(path, "/zenvik.app/") && caskInstalled(dirExists):
			return Channel{Name: "Homebrew", Upgrade: "brew upgrade --cask zenvik-gui"}
		case strings.Contains(path, "/scoop/apps/zenvik-gui/"):
			return Channel{Name: "Scoop", Upgrade: "scoop update zenvik-gui"}
		case strings.Contains(path, "/winget/packages/chad3814.zenvikgui_"):
			return Channel{Name: "winget", Upgrade: "winget upgrade chad3814.ZenvikGUI", source: sourceWinget, pkg: "chad3814.ZenvikGUI"}
		case strings.Contains(path, "/chocolatey/lib/zenvik-gui/"):
			return Channel{Name: "Chocolatey", Upgrade: "choco upgrade zenvik-gui", source: sourceChocolatey, pkg: "zenvik-gui"}
		}
	}
	return Channel{}
}

// caskInstalled reports whether Homebrew's Caskroom holds zenvik-gui under
// either prefix.
func caskInstalled(dirExists func(string) bool) bool {
	if dirExists == nil {
		return false
	}
	for _, prefix := range brewPrefixes {
		if dirExists(prefix + "/Caskroom/zenvik-gui") {
			return true
		}
	}
	return false
}

// DetectSelf is Detect for the running binary: os.Executable with symlinks
// (and Windows junctions) resolved, and os.Stat for the Caskroom check. A
// binary whose path cannot be found counts as a direct download.
func DetectSelf(p Product) Channel {
	exe, err := os.Executable()
	if err != nil {
		return Channel{}
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return Detect(p, exe, func(dir string) bool {
		fi, err := os.Stat(dir)
		return err == nil && fi.IsDir()
	})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/update/ && go vet ./internal/update/ && golangci-lint run ./internal/update/`
Expected: `ok`, 0 issues. (`sourceKind`, `sourceKey`, `cacheName` are used only by tests until Task 2; the linter's `unused` check does not flag methods or constants referenced from `_test.go` files in the same package. If it does, add `var _ = Channel{}.cacheName` at the bottom of `channel.go` with a comment "used by Task 2's checker" and remove it in Task 2.)

- [ ] **Step 5: Commit**

```bash
git add internal/update/channel.go internal/update/channel_test.go
git commit -m "update: detect the install channel from the binary's own path"
```

---

### Task 2: winget and Chocolatey sources; cache per source

**Files:**
- Create: `internal/update/sources.go`
- Create: `internal/update/sources_test.go`
- Modify: `internal/update/check.go` (`Checker`, `cacheFile`, `fetchAndRecord`, `fetch` → `fetchGitHub`, `url`, `cachePath`, `readCache`, `DefaultCachePath`)
- Modify: `internal/update/check_test.go` (one assertion)

**Interfaces:**
- Consumes: `Channel`, `sourceKind` constants, `sourceKey`, `cacheName` (Task 1).
- Produces: `Checker.Channel Channel`; `Checker.URL` overrides whichever source's endpoint; cache file field `source`; `func defaultCacheDir() (string, error)`. Tasks 3 and 4 consume `Checker.Channel`.

- [ ] **Step 1: Write the failing tests**

`internal/update/sources_test.go`:

```go
package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const wingetListing = `[
  {"name": "1.2.0", "type": "dir"},
  {"name": "1.10.0", "type": "dir"},
  {"name": "1.3.0", "type": "dir"},
  {"name": "README.md", "type": "file"},
  {"name": "notes", "type": "dir"},
  {"name": "2.0.0", "type": "file"}
]`

const chocolateyFeed = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:d="http://schemas.microsoft.com/ado/2007/08/dataservices" xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata">
  <title type="text">Packages</title>
  <entry>
    <id>https://community.chocolatey.org/api/v2/Packages(Id='zenvik',Version='1.1.0')</id>
    <m:properties><d:Version>1.1.0</d:Version><d:IsApproved m:type="Edm.Boolean">true</d:IsApproved><d:PackageStatus>Approved</d:PackageStatus></m:properties>
  </entry>
  <entry>
    <id>https://community.chocolatey.org/api/v2/Packages(Id='zenvik',Version='1.2.0')</id>
    <m:properties><d:Version>1.2.0</d:Version><d:IsApproved m:type="Edm.Boolean">true</d:IsApproved><d:PackageStatus>Approved</d:PackageStatus></m:properties>
  </entry>
  <entry>
    <id>https://community.chocolatey.org/api/v2/Packages(Id='zenvik',Version='1.3.0')</id>
    <m:properties><d:Version>1.3.0</d:Version><d:IsApproved m:type="Edm.Boolean">false</d:IsApproved><d:PackageStatus>Submitted</d:PackageStatus></m:properties>
  </entry>
</feed>`

const emptyFeed = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title type="text">Packages</title></feed>`

// serve answers every request with body (or status when body is numeric),
// counting hits and keeping the last request.
func serve(t *testing.T, body string) (*httptest.Server, *atomic.Int32, *atomic.Pointer[http.Request]) {
	t.Helper()
	var hits atomic.Int32
	var last atomic.Pointer[http.Request]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		last.Store(r.Clone(context.Background()))
		switch body {
		case "404":
			http.NotFound(w, r)
		case "500":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			fmt.Fprint(w, body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits, &last
}

var (
	wingetCLI = Detect(CLI, `C:\Users\chad\AppData\Local\Microsoft\WinGet\Packages\chad3814.Zenvik_Microsoft.Winget.Source_8wekyb3d8bbwe\zenvik_1.2.1_windows_amd64\zenvik.exe`, nil)
	chocoGUI  = Detect(GUI, `C:\ProgramData\chocolatey\lib\zenvik-gui\tools\zenvik-gui_1.2.1_windows_amd64\zenvik-gui.exe`, nil)
)

func checkerFor(t *testing.T, ch Channel, srv *httptest.Server) *Checker {
	t.Helper()
	return &Checker{Client: srv.Client(), URL: srv.URL, Channel: ch,
		CachePath: filepath.Join(t.TempDir(), "c.json"), UserAgent: "zenvik-test/v1.2.1"}
}

func TestWingetSource(t *testing.T) {
	srv, hits, last := serve(t, wingetListing)
	c := checkerFor(t, wingetCLI, srv)
	r, err := c.Check(context.Background(), "v1.2.1")
	if err != nil || r.Latest.String() != "v1.10.0" || !r.Newer || r.URL != "" {
		t.Fatalf("Check = %+v, %v; want latest v1.10.0 from the directory names", r, err)
	}
	req := last.Load()
	if req.Header.Get("Accept") != "application/vnd.github+json" || req.Header.Get("User-Agent") != "zenvik-test/v1.2.1" || req.Header.Get("X-GitHub-Api-Version") == "" {
		t.Errorf("headers = %v", req.Header)
	}
	if r, err := c.Check(context.Background(), "v1.10.0"); err != nil || r.Newer || hits.Load() != 1 {
		t.Errorf("cached Check for the latest = %+v, %v, hits %d", r, err, hits.Load())
	}
	for body, want := range map[string]string{"404": "not on winget yet", "500": "HTTP 500", `[{"name":"x","type":"dir"}]`: "no versions on winget", "<html>": "invalid response"} {
		srv2, _, _ := serve(t, body)
		_, err := checkerFor(t, wingetCLI, srv2).Force(context.Background(), "v1.2.1")
		if !errors.Is(err, ErrFetch) || !strings.Contains(err.Error(), want) {
			t.Errorf("body %q: err = %v, want ErrFetch mentioning %q", body, err, want)
		}
	}
}

func TestChocolateySource(t *testing.T) {
	srv, _, last := serve(t, chocolateyFeed)
	c := checkerFor(t, chocoGUI, srv)
	// 1.3.0 is still in moderation: 1.2.0 is the latest a user can install.
	r, err := c.Check(context.Background(), "v1.1.0")
	if err != nil || r.Latest.String() != "v1.2.0" || !r.Newer || r.URL != "" {
		t.Fatalf("Check = %+v, %v; want the newest approved version", r, err)
	}
	if req := last.Load(); req.Header.Get("Accept") != "application/atom+xml" || req.Header.Get("User-Agent") != "zenvik-test/v1.2.1" {
		t.Errorf("headers = %v", req.Header)
	}
	if r, err := c.Check(context.Background(), "v1.2.0"); err != nil || r.Newer {
		t.Errorf("Check on the approved version = %+v, %v; want not newer", r, err)
	}
	unapprovedOnly := strings.Replace(chocolateyFeed, `>true<`, `>false<`, -1)
	for body, want := range map[string]string{emptyFeed: "not on Chocolatey yet", unapprovedOnly: "no approved version on Chocolatey", "500": "HTTP 500", "{not xml": "invalid response"} {
		srv2, _, _ := serve(t, body)
		_, err := checkerFor(t, chocoGUI, srv2).Force(context.Background(), "v1.1.0")
		if !errors.Is(err, ErrFetch) || !strings.Contains(err.Error(), want) {
			t.Errorf("body %.30q: err = %v, want ErrFetch mentioning %q", body, err, want)
		}
	}
}

func TestDefaultURLs(t *testing.T) {
	if got := (&Checker{Channel: wingetCLI}).url(); got != "https://api.github.com/repos/microsoft/winget-pkgs/contents/manifests/c/chad3814/Zenvik" {
		t.Errorf("winget url = %q", got)
	}
	gui := Detect(GUI, `C:\x\WinGet\Packages\chad3814.ZenvikGUI_abc\zenvik-gui.exe`, nil)
	if got := (&Checker{Channel: gui}).url(); got != "https://api.github.com/repos/microsoft/winget-pkgs/contents/manifests/c/chad3814/ZenvikGUI" {
		t.Errorf("winget gui url = %q", got)
	}
	if got := (&Checker{Channel: chocoGUI}).url(); got != "https://community.chocolatey.org/api/v2/FindPackagesById()?id='zenvik-gui'" {
		t.Errorf("chocolatey url = %q", got)
	}
	if got := (&Checker{}).url(); got != DefaultURL {
		t.Errorf("github url = %q", got)
	}
}

func TestCacheIsPerSource(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := filepath.Join(state, "zenvik")
	gh, ghHits, _ := serve(t, `{"tag_name":"v1.3.0","html_url":"u"}`)
	wg, wgHits, _ := serve(t, wingetListing)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	github := &Checker{Client: gh.Client(), URL: gh.URL, Now: func() time.Time { return now }}
	winget := &Checker{Client: wg.Client(), URL: wg.URL, Channel: wingetCLI, Now: func() time.Time { return now }}

	if _, err := github.Check(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := winget.Check(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	ghFile, wgFile := filepath.Join(dir, "update-check.json"), filepath.Join(dir, "update-check-winget-chad3814.Zenvik.json")
	for _, f := range []string{ghFile, wgFile} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	ghBody, _ := os.ReadFile(ghFile)
	wgBody, _ := os.ReadFile(wgFile)
	if !strings.Contains(string(ghBody), `"source":"github"`) || !strings.Contains(string(wgBody), `"source":"winget:chad3814.Zenvik"`) {
		t.Errorf("sources not recorded:\n%s\n%s", ghBody, wgBody)
	}
	if _, err := github.Check(context.Background(), "v1.2.0"); err != nil || ghHits.Load() != 1 || wgHits.Load() != 1 {
		t.Errorf("second GitHub check: %v, hits %d/%d (want 1/1)", err, ghHits.Load(), wgHits.Load())
	}

	// A GitHub file written by an older zenvik has no "source": still valid.
	legacy := `{"checked_at":"2026-10-05T11:00:00Z","latest":"v1.3.0","url":"u","error":""}`
	if err := os.WriteFile(ghFile, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := github.Check(context.Background(), "v1.2.0"); err != nil || ghHits.Load() != 1 {
		t.Errorf("legacy cache: %v, GitHub hits %d (want still 1)", err, ghHits.Load())
	}

	// A file claiming another source is ignored.
	foreign := `{"checked_at":"2026-10-05T11:00:00Z","latest":"v1.3.0","url":"u","error":"","source":"chocolatey:zenvik"}`
	if err := os.WriteFile(ghFile, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := github.Check(context.Background(), "v1.2.0"); err != nil || ghHits.Load() != 2 {
		t.Errorf("foreign cache: %v, GitHub hits %d (want 2: refetched)", err, ghHits.Load())
	}
}

// TestRealFeeds pins the live response shapes. It runs only with
// ZENVIK_NET_TESTS=1, against packages that exist today.
func TestRealFeeds(t *testing.T) {
	if os.Getenv("ZENVIK_NET_TESTS") == "" {
		t.Skip("set ZENVIK_NET_TESTS=1 to query the real winget and Chocolatey feeds")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for name, ch := range map[string]Channel{
		"winget":     {Name: "winget", source: sourceWinget, pkg: "Git.Git"},
		"chocolatey": {Name: "Chocolatey", source: sourceChocolatey, pkg: "git"},
	} {
		c := &Checker{Channel: ch, CachePath: filepath.Join(t.TempDir(), "c.json"), UserAgent: "zenvik-test"}
		r, err := c.Force(ctx, "v0.0.1")
		if err != nil || !r.Newer {
			t.Errorf("%s: %+v, %v", name, r, err)
		}
	}
}
```

Also change one line in `internal/update/check_test.go`'s `TestCheckFetchesThenUsesCache`: the `readCache` map assertion currently checks `latest`, `error`, `checked_at`; add `|| m["source"] != "github"` to that condition so the new field is pinned.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/update/ -run 'TestWinget|TestChocolatey|TestDefaultURLs|TestCacheIsPerSource|TestCheckFetches'`
Expected: FAIL to compile: `unknown field Channel in struct literal`.

- [ ] **Step 3: Implement**

`internal/update/sources.go`:

```go
package update

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	wingetContents  = "https://api.github.com/repos/microsoft/winget-pkgs/contents/manifests/"
	chocolateyFeedURL = "https://community.chocolatey.org/api/v2/"
)

// wingetDir is the manifests path for a package id: "chad3814.Zenvik" →
// "c/chad3814/Zenvik".
func wingetDir(pkg string) string {
	publisher, name, _ := strings.Cut(pkg, ".")
	return strings.ToLower(publisher[:1]) + "/" + publisher + "/" + name
}

// get issues the request with the user agent and the extra headers.
func (c *Checker) get(ctx context.Context, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.client().Do(req)
}

var githubHeaders = map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}

// fetchWinget lists the package's manifest directory in microsoft/winget-pkgs;
// each published version is a directory named after it.
func (c *Checker) fetchWinget(ctx context.Context) (Version, string, error) {
	resp, err := c.get(ctx, githubHeaders)
	if err != nil {
		return Version{}, "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Version{}, "", errors.New(c.Channel.pkg + " is not on winget yet")
	case resp.StatusCode != http.StatusOK:
		return Version{}, "", errors.New("HTTP " + resp.Status)
	}
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&entries); err != nil {
		return Version{}, "", fmt.Errorf("invalid response: %w", err)
	}
	var best Version
	found := false
	for _, e := range entries {
		if e.Type != "dir" {
			continue
		}
		v, err := Parse("v" + e.Name)
		if err != nil {
			continue
		}
		if !found || best.Less(v) {
			best, found = v, true
		}
	}
	if !found {
		return Version{}, "", errors.New("no versions on winget")
	}
	return best, "", nil
}

// fetchChocolatey reads the package's versions from the community feed and
// returns the highest approved one; a version still in moderation is never
// offered.
func (c *Checker) fetchChocolatey(ctx context.Context) (Version, string, error) {
	resp, err := c.get(ctx, map[string]string{"Accept": "application/atom+xml"})
	if err != nil {
		return Version{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Version{}, "", errors.New("HTTP " + resp.Status)
	}
	var feed struct {
		Entries []struct {
			Props struct {
				Version    string `xml:"Version"`
				IsApproved string `xml:"IsApproved"`
			} `xml:"properties"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&feed); err != nil {
		return Version{}, "", fmt.Errorf("invalid response: %w", err)
	}
	if len(feed.Entries) == 0 {
		return Version{}, "", errors.New(c.Channel.pkg + " is not on Chocolatey yet")
	}
	var best Version
	found := false
	for _, e := range feed.Entries {
		if !strings.EqualFold(strings.TrimSpace(e.Props.IsApproved), "true") {
			continue
		}
		v, err := Parse("v" + strings.TrimSpace(e.Props.Version))
		if err != nil {
			continue
		}
		if !found || best.Less(v) {
			best, found = v, true
		}
	}
	if !found {
		return Version{}, "", errors.New("no approved version on Chocolatey")
	}
	return best, "", nil
}
```

In `internal/update/check.go`:

1. Add to `Checker` (after `UserAgent`): `Channel Channel // where this binary came from; zero: a direct download, asking GitHub`.
2. Add to `cacheFile`: `Source string \`json:"source"\`` (as the last field).
3. In `fetchAndRecord`, change the first line to `cf := cacheFile{CheckedAt: c.now().UTC(), Source: c.Channel.sourceKey()}`.
4. Rename the existing `fetch` to `fetchGitHub` and replace its request-building lines (`http.NewRequestWithContext` through `c.client().Do(req)`) with `resp, err := c.get(ctx, githubHeaders)`; keep the rest. Add a new dispatcher:

```go
// fetch asks this channel's source for the latest release. Its errors are
// the detail only; callers wrap ErrFetch.
func (c *Checker) fetch(ctx context.Context) (Version, string, error) {
	switch c.Channel.source {
	case sourceWinget:
		return c.fetchWinget(ctx)
	case sourceChocolatey:
		return c.fetchChocolatey(ctx)
	}
	return c.fetchGitHub(ctx)
}
```

5. Replace `url()`:

```go
// url is the endpoint for this channel's source; URL overrides it.
func (c *Checker) url() string {
	if c.URL != "" {
		return c.URL
	}
	switch c.Channel.source {
	case sourceWinget:
		return wingetContents + wingetDir(c.Channel.pkg)
	case sourceChocolatey:
		return chocolateyFeedURL + "FindPackagesById()?id='" + c.Channel.pkg + "'"
	}
	return DefaultURL
}
```

6. Replace `cachePath()` and `DefaultCachePath()`:

```go
func (c *Checker) cachePath() (string, error) {
	if c.CachePath != "" {
		return c.CachePath, nil
	}
	dir, err := defaultCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, c.Channel.cacheName()), nil
}

// defaultCacheDir is $XDG_STATE_HOME/zenvik when XDG_STATE_HOME is
// absolute, else the user cache directory's zenvik (the roots the mount
// records and the GUI queue already use).
func defaultCacheDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "zenvik"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "zenvik"), nil
}

// DefaultCachePath is the GitHub source's cache file, update-check.json,
// under defaultCacheDir. Other sources keep their own file beside it.
func DefaultCachePath() (string, error) {
	dir, err := defaultCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, Channel{}.cacheName()), nil
}
```

7. In `readCache`, after the `CheckedAt.IsZero()` check, add:

```go
	src := cf.Source
	if src == "" {
		src = "github" // written before sources existed
	}
	if src != c.Channel.sourceKey() {
		return cacheFile{}, false
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/update/ && go vet ./internal/update/ && golangci-lint run ./internal/update/`
Expected: `ok`, 0 issues; every pre-existing test still passes (zero-value `Channel` is GitHub).

Then once, with the network: `ZENVIK_NET_TESTS=1 go test ./internal/update/ -run TestRealFeeds -v`
Expected: PASS for both feeds. If Chocolatey's live feed uses a different element name for approval, report the exact XML seen instead of guessing.

- [ ] **Step 5: Commit**

```bash
git add internal/update/sources.go internal/update/sources_test.go internal/update/check.go internal/update/check_test.go
git commit -m "update: ask winget and Chocolatey's own feeds for managed installs, with a cache file per source"
```

---

### Task 3: CLI wording per channel

**Files:**
- Modify: `cmd/zenvik/update.go` (`newUpdateChecker`, `updateNotice`, `start`, `print`; new `noticeLine`)
- Modify: `cmd/zenvik/doctor.go` (`updateLine`)
- Modify: `cmd/zenvik/update_test.go` (append)

**Interfaces:**
- Consumes: `update.DetectSelf(update.CLI)`, `update.Channel{Name, Upgrade}`, `Managed()` (Task 1); `Checker.Channel` (Task 2).

- [ ] **Step 1: Write the failing tests**

Append to `cmd/zenvik/update_test.go`:

```go
// installedVia makes the CLI's checker report ch as the install channel.
// Call after releaseBuild.
func installedVia(t *testing.T, ch update.Channel) {
	t.Helper()
	prev := newUpdateChecker
	newUpdateChecker = func() *update.Checker {
		c := prev()
		c.Channel = ch
		return c
	}
	t.Cleanup(func() { newUpdateChecker = prev })
}

var viaHomebrew = update.Detect(update.CLI, "/opt/homebrew/Cellar/zenvik/1.2.0/bin/zenvik", nil)

func TestUpdateNoticeNamesTheUpgradeCommand(t *testing.T) {
	releaseBuild(t, "v1.3.0")
	installedVia(t, viaHomebrew)
	_, _, errOut := runCLI("info", writeDisc(t, testdisc.SampleMovie()))
	if want := "zenvik: v1.3.0 is available (you have v1.2.0); upgrade with: brew upgrade zenvik\n"; errOut != want {
		t.Errorf("stderr %q, want %q", errOut, want)
	}
}

func TestDoctorUpdateLineNamesTheChannel(t *testing.T) {
	cases := map[string]struct{ latest, want string }{
		"newer":   {"v1.3.0", "! zenvik v1.3.0 is available on Homebrew: brew upgrade zenvik"},
		"current": {"v1.2.0", "✓ zenvik v1.2.0 is the latest on Homebrew"},
		"failure": {"500", "! update check on Homebrew: HTTP 500 Internal Server Error"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			releaseBuild(t, tc.latest)
			installedVia(t, viaHomebrew)
			isolateConfig(t)
			t.Setenv("PATH", t.TempDir())
			_, out, _ := runCLI("doctor")
			if !strings.Contains(out, tc.want+"\n") {
				t.Errorf("output lacks %q:\n%s", tc.want, out)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/zenvik/ -run 'TestUpdateNoticeNamesTheUpgradeCommand|TestDoctorUpdateLineNamesTheChannel'`
Expected: FAIL: stderr still ends with the releases URL; doctor lines lack "on Homebrew".

- [ ] **Step 3: Implement**

In `cmd/zenvik/update.go`:

```go
// newUpdateChecker builds the checker the CLI uses, for the channel this
// binary was installed from; tests point it at a local server and a
// temporary cache.
var newUpdateChecker = func() *update.Checker {
	return &update.Checker{UserAgent: "zenvik/" + version, Channel: update.DetectSelf(update.CLI)}
}
```

Add a `checker *update.Checker` field to `updateNotice`; in `start`, change `checker := newUpdateChecker()` to `n.checker = newUpdateChecker()` and use `n.checker.Check(...)` in the goroutine (capture it in a local first: `checker := n.checker`). In `print`, replace the `Fprintf` with `fmt.Fprintln(w, noticeLine(r, n.checker.Channel))` and add:

```go
// noticeLine is the one-line notice: the upgrade command for a managed
// install, the releases page for a direct download.
func noticeLine(r update.Result, ch update.Channel) string {
	head := fmt.Sprintf("zenvik: %s is available (you have %s)", r.Latest, r.Current)
	if ch.Managed() {
		return head + "; upgrade with: " + ch.Upgrade
	}
	return head + ": " + update.LatestPage
}
```

In `cmd/zenvik/doctor.go`, replace `updateLine`:

```go
// updateLine asks this install's source now (ignoring the daily cache)
// whether a newer release exists and describes the answer, naming the
// package manager when one owns the install. It never affects the exit
// code.
func updateLine(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	c := newUpdateChecker()
	r, err := c.Force(ctx, buildVersion())
	ch := c.Channel
	switch {
	case errors.Is(err, update.ErrDevBuild):
		return "- update check: skipped in development builds"
	case err != nil && ch.Managed():
		return fmt.Sprintf("! update check on %s: %s", ch.Name, strings.TrimPrefix(err.Error(), update.ErrFetch.Error()+": "))
	case err != nil:
		return "! update check: " + strings.TrimPrefix(err.Error(), update.ErrFetch.Error()+": ")
	case r.Newer && ch.Managed():
		return fmt.Sprintf("! zenvik %s is available on %s: %s", r.Latest, ch.Name, ch.Upgrade)
	case r.Newer:
		return fmt.Sprintf("! zenvik %s is available: %s", r.Latest, update.LatestPage)
	case ch.Managed():
		return fmt.Sprintf("✓ zenvik %s is the latest on %s", r.Current, ch.Name)
	}
	return fmt.Sprintf("✓ zenvik %s is the latest release", r.Current)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./cmd/zenvik/ && go vet ./cmd/zenvik/ && golangci-lint run ./cmd/zenvik/`
Expected: `ok`; the earlier `TestUpdateNoticeAfterInfo`, `TestDoctorUpdateLine` and friends still pass because `releaseBuild`'s checker has a zero `Channel` (direct) and the test binary is never in a managed location.

- [ ] **Step 5: Commit**

```bash
git add cmd/zenvik/update.go cmd/zenvik/doctor.go cmd/zenvik/update_test.go
git commit -m "zenvik: the update notice and doctor name the package manager's upgrade command"
```

---

### Task 4: GUI channel, banner wording, `UpgradeHint`

**Files:**
- Modify: `gui/app.go` (`Deps`)
- Modify: `gui/settings.go` (`defaultDeps`)
- Modify: `gui/update.go` (`startUpdateCheck`, `updateBanner`, new `UpgradeHint`)
- Modify: `gui/app_test.go` (`testOpts`, `newTestApp`)
- Modify: `gui/update_test.go` (append)

**Interfaces:**
- Consumes: `update.DetectSelf(update.GUI)`, `update.Channel`, `Managed()`, `Checker.Channel`.
- Produces: `Deps.Channel update.Channel`; bound `func (a *App) UpgradeHint() string`; banner shape for managed installs. Task 5 consumes `UpgradeHint`.

- [ ] **Step 1: Write the failing tests**

Append to `gui/update_test.go`:

```go
var viaChocolatey = update.Detect(update.GUI, `C:\ProgramData\chocolatey\lib\zenvik-gui\tools\zenvik-gui_1.2.0_windows_amd64\zenvik-gui.exe`, nil)

func TestUpdateBannerForManagedInstall(t *testing.T) {
	asRelease(t)
	_, sh, _ := newTestApp(t, testOpts{
		channel: viaChocolatey,
		checkUpdate: func(_ context.Context, current string) (update.Result, error) {
			return fakeResult(t, current, "v1.3.0"), nil
		},
	})
	waitFor(t, "update banner", func() bool { _, ok := banners(sh)["update"]; return ok })
	b := banners(sh)["update"]
	if b.Message != "Zenvik v1.3.0 is available. Upgrade with: choco upgrade zenvik-gui" || b.Action != "" || b.URL != "" {
		t.Errorf("banner = %+v", b)
	}
}

func TestUpgradeHint(t *testing.T) {
	a, _, _ := newTestApp(t, testOpts{channel: viaChocolatey})
	if got := a.UpgradeHint(); got != "choco upgrade zenvik-gui" {
		t.Errorf("UpgradeHint = %q", got)
	}
	direct, _, _ := newTestApp(t, testOpts{})
	if got := direct.UpgradeHint(); got != "" {
		t.Errorf("UpgradeHint for a direct install = %q", got)
	}
}
```

In `gui/app_test.go`, add `channel update.Channel // where the app was installed from; zero: direct` to `testOpts`, and `Channel: o.channel,` to the `Deps` literal in `newTestApp`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd gui && go test ./ -run 'TestUpdateBannerForManagedInstall|TestUpgradeHint'`
Expected: FAIL to compile: `unknown field channel in struct literal`, `a.UpgradeHint undefined`.

- [ ] **Step 3: Implement**

`gui/app.go`, add to `Deps` after `ForceUpdate`:

```go
	Channel      update.Channel                                                   // where the app was installed from; zero: a direct download
```

`gui/settings.go`, in `defaultDeps`, replace the `checker :=` line with:

```go
	channel := update.DetectSelf(update.GUI)
	checker := &update.Checker{UserAgent: "zenvik-gui/" + version, Channel: channel}
```

and add `Channel: channel,` to the `Deps` literal.

`gui/update.go`: change the banner call to `a.setBanner(updateBanner(r, a.deps.Channel))`, replace `updateBanner`, and add `UpgradeHint`:

```go
// updateBanner says a newer release exists: with the package manager's
// upgrade command for a managed install, or with a Download button for a
// direct download.
func updateBanner(r update.Result, ch update.Channel) Banner {
	msg := "Zenvik " + r.Latest.String() + " is available."
	if ch.Managed() {
		return Banner{ID: "update", Message: msg + " Upgrade with: " + ch.Upgrade}
	}
	return Banner{ID: "update", Message: msg, Action: "download", URL: update.LatestPage}
}

// UpgradeHint is the command that upgrades this install, or "" for a direct
// download, where the About dialog links the releases page instead.
func (a *App) UpgradeHint() string { return a.deps.Channel.Upgrade }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd gui && go vet $(go list ./... | grep -v /node_modules/) && go test -race $(go list ./... | grep -v /node_modules/)`
Expected: `ok` for all four packages.

- [ ] **Step 5: Commit**

```bash
git add gui/app.go gui/settings.go gui/update.go gui/app_test.go gui/update_test.go
git commit -m "gui: the update banner names the package manager's upgrade command; UpgradeHint for About"
```

---

### Task 5: Frontend: upgrade command in About; bindings

**Files:**
- Regenerate: `gui/frontend/wailsjs/go/main/App.d.ts`, `App.js`
- Modify: `gui/frontend/src/api.ts`
- Modify: `gui/frontend/src/components/About.tsx`
- Modify: `gui/frontend/src/__tests__/About.test.tsx`

**Interfaces:**
- Consumes: bound `UpgradeHint(): Promise<string>` (Task 4).

- [ ] **Step 1: Regenerate and commit the bindings**

From `gui/` (after `npm ci` in `gui/frontend` if `node_modules` is missing): `go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 generate module`. Revert any file-mode-only changes under `wailsjs/runtime/` as Task 7 of the first plan did. Commit:

```bash
git add gui/frontend/wailsjs
git commit -m "gui: regenerate bindings for UpgradeHint"
```

- [ ] **Step 2: Write the failing tests**

In `gui/frontend/src/__tests__/About.test.tsx`, add `upgradeHint: vi.fn(() => Promise.resolve('')),` to the mocked `api`, and add after the "names the newer release" test:

```tsx
  it('names the upgrade command for a managed install instead of a download link', async () => {
    vi.mocked(api.checkForUpdate).mockResolvedValueOnce('v1.3.0');
    vi.mocked(api.upgradeHint).mockResolvedValueOnce('brew upgrade --cask zenvik-gui');
    await open();
    await userEvent.click(screen.getByRole('link', { name: 'Check for updates' }));
    expect(await screen.findByText('v1.3.0 is available')).toBeInTheDocument();
    expect(screen.getByText('brew upgrade --cask zenvik-gui')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Download' })).toBeNull();
  });

  it('does not ask for the upgrade hint when already current', async () => {
    await open();
    await userEvent.click(screen.getByRole('link', { name: 'Check for updates' }));
    await screen.findByText('You have the latest version');
    expect(api.upgradeHint).not.toHaveBeenCalled();
  });
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd gui/frontend && npm test -- About`
Expected: FAIL: the command text is not rendered and the Download link is present.

- [ ] **Step 4: Implement**

`gui/frontend/src/api.ts`, after `checkForUpdate`:

```ts
  /** upgradeHint is the package manager command that upgrades this install, or '' for a direct download. */
  upgradeHint: (): Promise<string> => App.UpgradeHint(),
```

`gui/frontend/src/components/About.tsx`: change the `available` state to `{ kind: 'available'; tag: string; hint: string }`; in `check`:

```ts
      const tag = await api.checkForUpdate();
      if (!tag) {
        setUpdate({ kind: 'current' });
        return;
      }
      const hint = await api.upgradeHint();
      setUpdate({ kind: 'available', tag, hint });
```

and in `updateText`:

```tsx
      case 'available':
        return (
          <>
            {' · '}
            <span>{update.tag} is available</span>
            {update.hint ? (
              <>
                {' · upgrade with: '}
                <code>{update.hint}</code>
              </>
            ) : (
              <> · {link(releasesURL, 'Download')}</>
            )}
          </>
        );
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd gui/frontend && npm test && npm run lint && npm run build && git checkout -- dist/gitkeep` then `cd .. && go test -race $(go list ./... | grep -v /node_modules/)`
Expected: all pass; `git status --short` clean apart from your source edits.

- [ ] **Step 6: Commit**

```bash
git add gui/frontend/src
git commit -m "gui: About shows the package manager's upgrade command for a managed install"
```

---

### Task 6: Documentation

**Files:**
- Modify: `README.md` (Update notifications paragraph)
- Modify: `gui/README.md`
- Modify: `CLAUDE.md`, `gui/CLAUDE.md`

- [ ] **Step 1: README**

Replace the **Update notifications** paragraph with:

```markdown
**Update notifications.** Release builds check for a newer release at most once a day and,
when there is one, print a line to stderr after `info`, `rip` or `repair-udf`. zenvik works out
how it was installed from its own path and names the matching command: `brew upgrade zenvik`,
`scoop update zenvik`, `winget upgrade chad3814.Zenvik` or `choco upgrade zenvik`; a direct
download gets the releases page instead:
`zenvik: v1.3.0 is available (you have v1.2.0): https://github.com/chad3814/zenvik/releases/latest`.
Homebrew, Scoop and direct downloads ask GitHub's releases API; winget and Chocolatey installs ask
that manager's own feed, so the notice appears only once the package is installable there. The
request carries only a `zenvik/<version>` user agent, and the answer is cached per source in
`$XDG_STATE_HOME/zenvik/` (else the user cache directory). The check is skipped when stderr isn't a
terminal, with `--jsonl`, when `CI` is set, in development builds, when the config has
`update_check = false`, or when `ZENVIK_NO_UPDATE_CHECK` is set to anything. `zenvik doctor` always
checks and reports the result on its own line.
```

- [ ] **Step 2: gui/README.md**

Replace the sentence starting "Release builds check GitHub" with:

```markdown
Release builds check for a newer release at most once a day when the app opens and show a banner: with the upgrade command for an app installed by Homebrew, Scoop, winget or Chocolatey (winget and Chocolatey are asked directly, so the banner waits until the package is installable there), or with a Download button for a direct download; **About → Check for updates** checks on demand. `update_check = false` in the config or the `ZENVIK_NO_UPDATE_CHECK` environment variable turns the launch check off.
```

- [ ] **Step 3: CLAUDE.md files**

Root `CLAUDE.md`: in the "Update check" bullet, after "`Force` always asks;" insert: "`Detect`/`DetectSelf` classify the binary by its own path (Homebrew, Scoop, winget, Chocolatey, or direct), `Checker.Channel` picks the source (GitHub, the winget-pkgs directory listing, or Chocolatey's Atom feed, highest approved version) and the cache file (`update-check.json`, `update-check-winget-<id>.json`, `update-check-chocolatey-<id>.json`); real-feed tests run with `ZENVIK_NET_TESTS=1`;".

`gui/CLAUDE.md`: in the "Update check" bullet, append: " `Deps.Channel` (from `update.DetectSelf(update.GUI)`) drives the banner wording and the bound `UpgradeHint`; tests set it directly."

- [ ] **Step 4: Verify and commit**

Run: `go build ./... && go test -race ./internal/update/ ./cmd/zenvik/ && (cd gui && go test -race $(go list ./... | grep -v /node_modules/))`

```bash
git add README.md gui/README.md CLAUDE.md gui/CLAUDE.md
git commit -m "docs: update notices name the package manager and wait for winget and Chocolatey"
```

---

### Task 7: Whole-feature verification

**Files:** none modified.

- [ ] **Step 1: Full verification**

```bash
go build ./... && CGO_ENABLED=0 go build ./... && go vet ./... && golangci-lint run && go test -race ./... && \
  (cd gui && go vet $(go list ./... | grep -v /node_modules/) && go test -race $(go list ./... | grep -v /node_modules/)) && \
  (cd gui/frontend && npm test && npm run lint && npm run build && git checkout -- dist/gitkeep)
```

- [ ] **Step 2: Real feeds and a managed-path smoke test**

```bash
ZENVIK_NET_TESTS=1 go test ./internal/update/ -run TestRealFeeds -v
mkdir -p /tmp/zenvik-smoke/Cellar/zenvik/1.0.0/bin
go build -ldflags "-X main.version=v1.0.0" -o /tmp/zenvik-smoke/Cellar/zenvik/1.0.0/bin/zenvik ./cmd/zenvik
/tmp/zenvik-smoke/Cellar/zenvik/1.0.0/bin/zenvik doctor | grep -E 'update check|zenvik v'
script -q /dev/null /tmp/zenvik-smoke/Cellar/zenvik/1.0.0/bin/zenvik info /nonexistent; echo "exit $?"
rm -rf /tmp/zenvik-smoke
```

Expected: `TestRealFeeds` passes; doctor prints `! zenvik vX.Y.Z is available on Homebrew: brew upgrade zenvik`; `info` under the pty prints its error and then `zenvik: vX.Y.Z is available (you have v1.0.0); upgrade with: brew upgrade zenvik`.

- [ ] **Step 3: Branch state**

`git status --short` is empty; `git log --oneline origin/main..HEAD` lists the earlier commits plus one spec commit, one plan commit, and seven task commits (Task 5 has two).
