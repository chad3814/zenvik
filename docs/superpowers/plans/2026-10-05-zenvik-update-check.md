# zenvik update check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tell people running a release build of zenvik when a newer release exists: one stderr line after a CLI command, a line in `zenvik doctor`, a banner in the desktop app, and a "Check for updates" link in its About dialog.

**Architecture:** One standard-library package, `internal/update`, parses release tags, asks the GitHub Releases API for the latest final release, and keeps a 24-hour cache file shared by the CLI and the app. `cmd/zenvik` starts the check in the background from the root command's `PersistentPreRun` and prints the notice after the command; `doctor` forces a fresh check. The Wails app runs the cached check at launch for a banner and exposes a bound `CheckForUpdate` for the About dialog. A config key and an environment variable turn the passive checks off; development builds never check.

**Tech Stack:** Go 1.27 (`net/http`, `encoding/json`, `httptest`), cobra, go-toml v2, Wails v2.16 + React 19 / TypeScript / Vitest.

**Spec:** `docs/superpowers/specs/2026-10-05-zenvik-update-check-design.md`

## Global Constraints

- Root module `github.com/chad3814/zenvik`: no cgo; third-party deps limited to `spf13/cobra` and `pelletier/go-toml/v2`; `internal/update` imports only the standard library.
- `gui/` may use Wails; the root module never imports `gui/`.
- Bound App methods take and return only primitives, string slices and `error`.
- TypeScript: strict, never `any`; never edit `gui/frontend/wailsjs/` by hand (run `wails generate module`).
- Tests never touch real config/state: `cmd/zenvik` and `gui` have a `TestMain` that sets `XDG_CONFIG_HOME`/`XDG_STATE_HOME`; tests in this plan also point `Checker.CachePath` at a temp dir.
- No test reaches the network. Development builds (`version` empty in the CLI, `"dev"` in the GUI) never check.
- Sentinel errors are wrapped with `%w`.
- Verify before each commit: `go build ./...` (also with `CGO_ENABLED=0`), `go vet ./...`, `golangci-lint run`, `go test -race ./...`; for `gui/`: `cd gui && go vet $(go list ./... | grep -v /node_modules/) && go test -race $(go list ./... | grep -v /node_modules/)`; for the frontend: `cd gui/frontend && npm test && npm run lint && npm run build`.
- Copy that users see, verbatim from the spec:
  - CLI notice: `zenvik: v1.3.0 is available (you have v1.2.0): https://github.com/chad3814/zenvik/releases/latest`
  - doctor: `✓ zenvik v1.2.0 is the latest release`, `! zenvik v1.3.0 is available: https://github.com/chad3814/zenvik/releases/latest`, `! update check: <detail>`, `- update check: skipped in development builds`
  - GUI banner: `Zenvik v1.3.0 is available.` with a **Download** button
  - About: **Check for updates**, `Checking…`, `You have the latest version`, `v1.3.0 is available`

## Review Focus

Inputs the spec implies but did not spell out. Each has its test in the task that owns the code.

1. **A `tag_name` with a pre-release suffix from the API** (someone marks an rc as "latest" by mistake): the running final build must not be told to "upgrade" to an rc. `Less` treats `v1.3.0-rc1 < v1.3.0` but `v1.2.0 < v1.3.0-rc1`, so the notice would appear; this is correct semver and the spec accepts it. Pinned in Task 1's `TestLess`.
2. **The cache file says `checked_at` in the future** (clock moved back): the check must not stay silent forever. Task 2 treats a future `checked_at` as stale and fetches.
3. **Ctrl-C during a rip while the check is in flight**: the cancelled attempt must not be cached as a 24-hour failure. Task 2's `TestCanceledContextIsNotCached`.
4. **A cached result written by an older binary before an upgrade**: the new binary must not be told it is out of date by its own stale cache. `Newer` is recomputed from the cached tag; Task 2's `TestCacheIsSharedAcrossVersions`.
5. **`--jsonl` given as `--jsonl=true` or after the path**: rip's JSON stream must stay clean. Task 4 reads the parsed flag value rather than scanning `os.Args`, and tests `--jsonl` placed last.

---

### Task 1: `internal/update` versions

**Files:**
- Create: `internal/update/version.go`
- Create: `internal/update/version_test.go`

**Interfaces:**
- Produces: `type Version struct{ Major, Minor, Patch int; Pre string }`, `var ErrBadVersion error`, `func Parse(tag string) (Version, error)`, `func (v Version) String() string`, `func (v Version) Less(o Version) bool`. Tasks 2, 4, 5 and 6 use these.

- [ ] **Step 1: Write the failing tests**

`internal/update/version_test.go`:

```go
package update

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	good := map[string]Version{
		"v1.2.0":      {1, 2, 0, ""},
		"v0.0.1":      {0, 0, 1, ""},
		"v10.20.30":   {10, 20, 30, ""},
		"v1.1.0-rc3":  {1, 1, 0, "rc3"},
		"v2.0.0-beta.1": {2, 0, 0, "beta.1"},
	}
	for tag, want := range good {
		got, err := Parse(tag)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", tag, got, err, want)
		}
		if got.String() != tag {
			t.Errorf("Parse(%q).String() = %q", tag, got.String())
		}
	}
	bad := []string{"", "dev", "dev (0123456789ab, modified)", "1.2.0", "v1.2", "v1.2.0.1", "v01.2.0",
		"v1.2.0+1", "v1.2.0-", "v1.2.0-rc..1", "v1.2.0-rc_1", "V1.2.0", "v1.2.0 ", "v-1.2.0", "v1.a.0"}
	for _, tag := range bad {
		if _, err := Parse(tag); !errors.Is(err, ErrBadVersion) {
			t.Errorf("Parse(%q) error = %v, want ErrBadVersion", tag, err)
		}
	}
}

func TestLess(t *testing.T) {
	// Each pair is (lower, higher).
	ordered := [][2]string{
		{"v1.2.0", "v1.3.0"},
		{"v1.2.0", "v2.0.0"},
		{"v1.2.0", "v1.2.1"},
		{"v1.9.9", "v1.10.0"},
		{"v1.1.0-rc3", "v1.1.0"},
		{"v1.1.0-rc.2", "v1.1.0-rc.10"},
		{"v1.1.0-alpha", "v1.1.0-alpha.1"},
		{"v1.1.0-alpha.1", "v1.1.0-alpha.beta"},
		{"v1.1.0-alpha.beta", "v1.1.0-beta"},
		{"v1.1.0-beta.2", "v1.1.0-beta.11"},
		{"v1.1.0-beta.11", "v1.1.0-rc.1"},
		{"v1.1.0-1", "v1.1.0-a"},
		{"v1.2.0", "v1.3.0-rc1"}, // a final is older than the next minor's rc
	}
	for _, p := range ordered {
		lo, hi := mustParse(t, p[0]), mustParse(t, p[1])
		if !lo.Less(hi) {
			t.Errorf("%s should be less than %s", p[0], p[1])
		}
		if hi.Less(lo) {
			t.Errorf("%s should not be less than %s", p[1], p[0])
		}
		if lo.Less(lo) || hi.Less(hi) {
			t.Errorf("Less must be irreflexive for %s / %s", p[0], p[1])
		}
	}
}

func mustParse(t *testing.T, tag string) Version {
	t.Helper()
	v, err := Parse(tag)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
```

Note: semver compares `rc2` and `rc10` as ASCII strings, so `rc10 < rc2`. The table deliberately uses `rc.2`/`rc.10` for the numeric case. The repo's own tags (`rc1`, `rc2`, `rc3`) never pass 9, so ASCII order is also release order for them.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/update/`
Expected: FAIL to compile: `undefined: Parse`, `undefined: Version`, `undefined: ErrBadVersion`.

- [ ] **Step 3: Implement versions**

`internal/update/version.go`:

```go
// Package update finds out whether a newer zenvik release exists. It talks
// to the GitHub Releases API, caches the answer for a day, and compares
// release tags by semantic-version precedence.
package update

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrBadVersion reports a string that is not a zenvik release tag.
var ErrBadVersion = errors.New("zenvik: not a release version")

// Version is a release tag: vMAJOR.MINOR.PATCH with an optional -PRERELEASE.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // "" for a final release; "rc2" for v1.1.0-rc2
}

// Parse reads a tag such as "v1.2.0" or "v1.1.0-rc3". Anything else,
// including development version strings and build metadata ("+..."), wraps
// ErrBadVersion.
func Parse(tag string) (Version, error) {
	bad := func() (Version, error) { return Version{}, fmt.Errorf("%w: %q", ErrBadVersion, tag) }
	rest, ok := strings.CutPrefix(tag, "v")
	if !ok {
		return bad()
	}
	core, pre, hasPre := strings.Cut(rest, "-")
	if hasPre && !validPre(pre) {
		return bad()
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return bad()
	}
	var nums [3]int
	for i, p := range parts {
		n, ok := number(p)
		if !ok {
			return bad()
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2], Pre: pre}, nil
}

// number parses a non-negative decimal with no leading zero.
func number(s string) (int, bool) {
	if !isNumeric(s) || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// isNumeric reports whether s is one or more ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// validPre accepts dot-separated identifiers of ASCII letters, digits and
// hyphens, none empty.
func validPre(pre string) bool {
	for _, id := range strings.Split(pre, ".") {
		if id == "" {
			return false
		}
		for _, c := range id {
			alnum := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
			if !alnum && c != '-' {
				return false
			}
		}
	}
	return true
}

// String renders v as its tag: "v1.2.0", "v1.1.0-rc3".
func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Less reports whether v precedes o in semantic-version order: by major,
// minor and patch; a pre-release before its final; pre-releases identifier
// by identifier, numeric ones numerically and before alphanumeric ones,
// and a shorter list of equal identifiers first.
func (v Version) Less(o Version) bool {
	switch {
	case v.Major != o.Major:
		return v.Major < o.Major
	case v.Minor != o.Minor:
		return v.Minor < o.Minor
	case v.Patch != o.Patch:
		return v.Patch < o.Patch
	case v.Pre == o.Pre:
		return false
	case v.Pre == "":
		return false
	case o.Pre == "":
		return true
	}
	return preLess(v.Pre, o.Pre)
}

func preLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		an, bn := isNumeric(as[i]), isNumeric(bs[i])
		switch {
		case an && bn:
			x, _ := strconv.Atoi(as[i])
			y, _ := strconv.Atoi(bs[i])
			return x < y
		case an:
			return true
		case bn:
			return false
		}
		return as[i] < bs[i]
	}
	return len(as) < len(bs)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/update/ && go vet ./internal/update/ && golangci-lint run ./internal/update/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/update/version.go internal/update/version_test.go
git commit -m "update: parse and order release tags by semver precedence"
```

---

### Task 2: `internal/update` checker and cache

**Files:**
- Create: `internal/update/check.go`
- Create: `internal/update/check_test.go`

**Interfaces:**
- Consumes: `Parse`, `Version`, `Less` from Task 1.
- Produces: `type Checker struct{ Client *http.Client; URL, CachePath string; Now func() time.Time; UserAgent string }`, `type Result struct{ Current, Latest Version; URL string; Newer bool; CheckedAt time.Time }`, `var ErrDevBuild, ErrFetch error`, `const DefaultURL, LatestPage string`, `func (c *Checker) Check(ctx context.Context, current string) (Result, error)`, `func (c *Checker) Force(ctx context.Context, current string) (Result, error)`, `func DefaultCachePath() (string, error)`. Tasks 4, 5 and 6 use these.

- [ ] **Step 1: Write the failing tests**

`internal/update/check_test.go`:

```go
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// release is an httptest handler answering like GitHub's releases/latest
// endpoint for tag, counting requests in hits.
func release(tag string, hits *atomic.Int32, lastReq *atomic.Pointer[http.Request]) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if lastReq != nil {
			lastReq.Store(r.Clone(context.Background()))
		}
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://github.com/chad3814/zenvik/releases/tag/%s","prerelease":false}`, tag, tag)
	})
}

type fixture struct {
	srv   *httptest.Server
	hits  atomic.Int32
	req   atomic.Pointer[http.Request]
	now   time.Time
	cache string
	c     *Checker
}

func newFixture(t *testing.T, h http.Handler) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	if h == nil {
		h = release("v1.3.0", &f.hits, &f.req)
	}
	f.srv = httptest.NewServer(h)
	t.Cleanup(f.srv.Close)
	f.cache = filepath.Join(t.TempDir(), "state", "zenvik", "update-check.json")
	f.c = &Checker{Client: f.srv.Client(), URL: f.srv.URL, CachePath: f.cache,
		Now: func() time.Time { return f.now }, UserAgent: "zenvik-test/v1.2.0"}
	return f
}

func (f *fixture) readCache(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(f.cache)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("cache %s: %v", b, err)
	}
	return m
}

func TestCheckFetchesThenUsesCache(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	r, err := f.c.Check(ctx, "v1.2.0")
	if err != nil || !r.Newer || r.Latest.String() != "v1.3.0" || r.Current.String() != "v1.2.0" ||
		r.URL != "https://github.com/chad3814/zenvik/releases/tag/v1.3.0" || !r.CheckedAt.Equal(f.now) {
		t.Fatalf("first Check = %+v, %v", r, err)
	}
	m := f.readCache(t)
	if m["latest"] != "v1.3.0" || m["error"] != "" || m["checked_at"] != "2026-10-05T12:00:00Z" {
		t.Errorf("cache = %v", m)
	}
	if st, err := os.Stat(f.cache); err == nil && runtime.GOOS != "windows" && st.Mode().Perm() != 0o644 {
		t.Errorf("cache mode = %v, want 0644", st.Mode().Perm())
	}

	f.now = f.now.Add(23 * time.Hour)
	if r, err := f.c.Check(ctx, "v1.2.0"); err != nil || !r.Newer || f.hits.Load() != 1 {
		t.Errorf("cached Check = %+v, %v, requests = %d (want 1)", r, err, f.hits.Load())
	}

	f.now = f.now.Add(2 * time.Hour) // 25 h after the first
	if r, err := f.c.Check(ctx, "v1.2.0"); err != nil || !r.Newer || f.hits.Load() != 2 {
		t.Errorf("stale Check = %+v, %v, requests = %d (want 2)", r, err, f.hits.Load())
	}
}

func TestForceAlwaysFetches(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		if r, err := f.c.Force(ctx, "v1.2.0"); err != nil || !r.Newer || f.hits.Load() != int32(i) {
			t.Errorf("Force #%d = %+v, %v, requests = %d", i, r, err, f.hits.Load())
		}
	}
}

func TestNewer(t *testing.T) {
	for current, want := range map[string]bool{"v1.2.0": true, "v1.3.0": false, "v1.4.0": false, "v1.3.0-rc1": true} {
		f := newFixture(t, nil)
		r, err := f.c.Check(context.Background(), current)
		if err != nil || r.Newer != want {
			t.Errorf("current %s: Newer = %v, %v; want %v", current, r.Newer, err, want)
		}
	}
}

func TestDevBuildNeverChecks(t *testing.T) {
	f := newFixture(t, nil)
	for _, cur := range []string{"dev", "", "dev (0123456789ab, modified)"} {
		if _, err := f.c.Check(context.Background(), cur); !errors.Is(err, ErrDevBuild) {
			t.Errorf("Check(%q) error = %v, want ErrDevBuild", cur, err)
		}
		if _, err := f.c.Force(context.Background(), cur); !errors.Is(err, ErrDevBuild) {
			t.Errorf("Force(%q) error = %v, want ErrDevBuild", cur, err)
		}
	}
	if f.hits.Load() != 0 {
		t.Errorf("%d requests for dev builds", f.hits.Load())
	}
	if _, err := os.Stat(f.cache); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cache written for a dev build: %v", err)
	}
}

func TestFetchFailuresAreCached(t *testing.T) {
	cases := map[string]struct {
		h    http.HandlerFunc
		want string // substring of the error detail
	}{
		"404": {func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", http.StatusNotFound) }, "HTTP 404 Not Found"},
		"500": {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }, "HTTP 500"},
		"bad json": {func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>") }, "invalid response"},
		"bad tag":  {func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"tag_name":"nightly"}`) }, `unexpected release tag "nightly"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); tc.h(w, r) }))
			r, err := f.c.Check(context.Background(), "v1.2.0")
			if !errors.Is(err, ErrFetch) || !strings.Contains(err.Error(), tc.want) || r.Newer {
				t.Fatalf("Check = %+v, %v; want ErrFetch mentioning %q", r, err, tc.want)
			}
			if m := f.readCache(t); m["error"] == "" || m["latest"] != "" {
				t.Errorf("cache = %v, want the failure recorded", m)
			}
			f.now = f.now.Add(time.Hour)
			if _, err := f.c.Check(context.Background(), "v1.2.0"); !errors.Is(err, ErrFetch) || hits.Load() != 1 {
				t.Errorf("second Check = %v, requests = %d; want the cached failure and 1 request", err, hits.Load())
			}
			f.now = f.now.Add(24 * time.Hour)
			if _, err := f.c.Check(context.Background(), "v1.2.0"); !errors.Is(err, ErrFetch) || hits.Load() != 2 {
				t.Errorf("Check after a day = %v, requests = %d; want a retry", err, hits.Load())
			}
		})
	}
}

func TestMalformedCacheIsIgnored(t *testing.T) {
	for name, body := range map[string]string{"garbage": "{not json", "empty": "", "no time": `{"latest":"v9.0.0"}`} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			if err := os.MkdirAll(filepath.Dir(f.cache), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.cache, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			r, err := f.c.Check(context.Background(), "v1.2.0")
			if err != nil || !r.Newer || r.Latest.String() != "v1.3.0" || f.hits.Load() != 1 {
				t.Errorf("Check = %+v, %v, requests = %d", r, err, f.hits.Load())
			}
		})
	}
}

func TestFutureCacheIsStale(t *testing.T) {
	f := newFixture(t, nil)
	if _, err := f.c.Check(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(-2 * time.Hour) // the clock went backwards
	if _, err := f.c.Check(context.Background(), "v1.2.0"); err != nil || f.hits.Load() != 2 {
		t.Errorf("Check with a future cache: %v, requests = %d (want 2)", err, f.hits.Load())
	}
}

func TestCacheIsSharedAcrossVersions(t *testing.T) {
	f := newFixture(t, nil)
	if r, err := f.c.Check(context.Background(), "v1.2.0"); err != nil || !r.Newer {
		t.Fatalf("Check = %+v, %v", r, err)
	}
	// The user upgraded; the same cache must now say "current".
	if r, err := f.c.Check(context.Background(), "v1.3.0"); err != nil || r.Newer || f.hits.Load() != 1 {
		t.Errorf("Check after upgrade = %+v, %v, requests = %d", r, err, f.hits.Load())
	}
}

func TestRequestHeaders(t *testing.T) {
	f := newFixture(t, nil)
	if _, err := f.c.Force(context.Background(), "v1.2.0"); err != nil {
		t.Fatal(err)
	}
	req := f.req.Load()
	if req == nil {
		t.Fatal("no request seen")
	}
	if req.Method != http.MethodGet || req.Header.Get("Accept") != "application/vnd.github+json" ||
		req.Header.Get("X-GitHub-Api-Version") != "2022-11-28" || req.Header.Get("User-Agent") != "zenvik-test/v1.2.0" {
		t.Errorf("request = %s %s %v", req.Method, req.URL, req.Header)
	}
}

func TestDefaultUserAgentAndURL(t *testing.T) {
	c := &Checker{}
	if c.userAgent() != "zenvik" || c.url() != DefaultURL || DefaultURL != "https://api.github.com/repos/chad3814/zenvik/releases/latest" {
		t.Errorf("defaults = %q %q", c.userAgent(), c.url())
	}
}

func TestUnwritableCacheStillAnswers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions work differently on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	f := newFixture(t, nil)
	dir := filepath.Dir(f.cache)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	r, err := f.c.Check(context.Background(), "v1.2.0")
	if err != nil || !r.Newer {
		t.Errorf("Check = %+v, %v", r, err)
	}
	if _, err := os.Stat(f.cache); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cache stat = %v, want not exist", err)
	}
}

func TestCanceledContextIsNotCached(t *testing.T) {
	started := make(chan struct{})
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	_, err := f.c.Check(ctx, "v1.2.0")
	if !errors.Is(err, ErrFetch) || !errors.Is(err, context.Canceled) {
		t.Errorf("Check = %v, want ErrFetch wrapping context.Canceled", err)
	}
	if _, err := os.Stat(f.cache); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a cancelled attempt was cached: %v", err)
	}
}

func TestDefaultCachePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	p, err := DefaultCachePath()
	if err != nil || p != filepath.Join(dir, "zenvik", "update-check.json") {
		t.Errorf("DefaultCachePath = %q, %v", p, err)
	}
	t.Setenv("XDG_STATE_HOME", "relative")
	p, err = DefaultCachePath()
	if err != nil || filepath.Base(p) != "update-check.json" || strings.Contains(p, "relative") {
		t.Errorf("DefaultCachePath with relative XDG = %q, %v", p, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/update/`
Expected: FAIL to compile: `undefined: Checker`, `undefined: ErrFetch`, `undefined: DefaultCachePath` and friends.

- [ ] **Step 3: Implement the checker**

`internal/update/check.go`:

```go
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultURL is GitHub's "latest final release" endpoint for zenvik.
	DefaultURL = "https://api.github.com/repos/chad3814/zenvik/releases/latest"
	// LatestPage is the human-facing page the notices link to.
	LatestPage = "https://github.com/chad3814/zenvik/releases/latest"

	cacheTTL       = 24 * time.Hour
	maxBody        = 1 << 20
	defaultTimeout = 5 * time.Second
)

var (
	// ErrDevBuild says the running version is not a release, so nothing was
	// checked.
	ErrDevBuild = errors.New("zenvik: update check skipped in development builds")
	// ErrFetch says the latest release could not be determined; the message
	// carries the detail.
	ErrFetch = errors.New("zenvik: update check failed")
)

// Checker finds the newest final release of zenvik. The zero value uses
// GitHub, a 5 s timeout, DefaultCachePath and time.Now.
type Checker struct {
	Client    *http.Client     // nil: http.Client with a 5 s timeout
	URL       string           // "": DefaultURL
	CachePath string           // "": DefaultCachePath()
	Now       func() time.Time // nil: time.Now
	UserAgent string           // "": "zenvik"
}

// Result is what a check learned.
type Result struct {
	Current   Version
	Latest    Version
	URL       string    // the release's page (html_url)
	Newer     bool      // Current.Less(Latest)
	CheckedAt time.Time // when Latest was fetched (zero when it never was)
}

// cacheFile is update-check.json. Newer is never stored: it is recomputed
// against whichever version reads the file.
type cacheFile struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url"`
	Error     string    `json:"error"`
}

// Check returns the cached result when it is less than 24 hours old,
// otherwise fetches, records the attempt in the cache and returns.
// A current version that is not a release tag returns ErrDevBuild.
func (c *Checker) Check(ctx context.Context, current string) (Result, error) {
	cur, err := Parse(current)
	if err != nil {
		return Result{}, ErrDevBuild
	}
	now := c.now()
	if cf, ok := c.readCache(); ok && !cf.CheckedAt.After(now) && now.Sub(cf.CheckedAt) < cacheTTL {
		return cf.result(cur)
	}
	return c.fetchAndRecord(ctx, cur)
}

// Force always fetches, then records the attempt in the cache.
func (c *Checker) Force(ctx context.Context, current string) (Result, error) {
	cur, err := Parse(current)
	if err != nil {
		return Result{}, ErrDevBuild
	}
	return c.fetchAndRecord(ctx, cur)
}

func (c *Checker) fetchAndRecord(ctx context.Context, cur Version) (Result, error) {
	cf := cacheFile{CheckedAt: c.now().UTC()}
	latest, url, err := c.fetch(ctx)
	switch {
	case errors.Is(err, context.Canceled):
		// The caller gave up (Ctrl-C); that says nothing about the network.
		return Result{Current: cur}, fmt.Errorf("%w: %w", ErrFetch, err)
	case err != nil:
		cf.Error = err.Error()
	default:
		cf.Latest, cf.URL = latest.String(), url
	}
	c.writeCache(cf)
	return cf.result(cur)
}

// result turns a cache entry into what cur's caller wants to know.
func (cf cacheFile) result(cur Version) (Result, error) {
	r := Result{Current: cur, CheckedAt: cf.CheckedAt}
	if cf.Error != "" {
		return r, fmt.Errorf("%w: %s", ErrFetch, cf.Error)
	}
	latest, err := Parse(cf.Latest)
	if err != nil {
		return r, fmt.Errorf("%w: cached tag %q", ErrFetch, cf.Latest)
	}
	r.Latest, r.URL, r.Newer = latest, cf.URL, cur.Less(latest)
	return r, nil
}

// fetch asks the API for the latest release. Its errors are the detail
// only; callers wrap ErrFetch.
func (c *Checker) fetch(ctx context.Context) (Version, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(), nil)
	if err != nil {
		return Version{}, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent())
	resp, err := c.client().Do(req)
	if err != nil {
		return Version{}, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Version{}, "", errors.New("HTTP " + resp.Status)
	}
	var body struct {
		Tag string `json:"tag_name"`
		URL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return Version{}, "", fmt.Errorf("invalid response: %w", err)
	}
	latest, err := Parse(body.Tag)
	if err != nil {
		return Version{}, "", fmt.Errorf("unexpected release tag %q", body.Tag)
	}
	return latest, body.URL, nil
}

func (c *Checker) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (c *Checker) url() string {
	if c.URL != "" {
		return c.URL
	}
	return DefaultURL
}

func (c *Checker) userAgent() string {
	if c.UserAgent != "" {
		return c.UserAgent
	}
	return "zenvik"
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) cachePath() (string, error) {
	if c.CachePath != "" {
		return c.CachePath, nil
	}
	return DefaultCachePath()
}

// readCache returns the cache entry, or false when there is none usable.
func (c *Checker) readCache() (cacheFile, bool) {
	p, err := c.cachePath()
	if err != nil {
		return cacheFile{}, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return cacheFile{}, false
	}
	var cf cacheFile
	if json.Unmarshal(b, &cf) != nil || cf.CheckedAt.IsZero() {
		return cacheFile{}, false
	}
	return cf, true
}

// writeCache replaces the cache atomically. Failures are ignored: the next
// run simply fetches again.
func (c *Checker) writeCache(cf cacheFile) {
	p, err := c.cachePath()
	if err != nil {
		return
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	b, err := json.Marshal(cf)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	if err := errors.Join(werr, tmp.Close(), os.Chmod(tmp.Name(), 0o644), os.Rename(tmp.Name(), p)); err != nil {
		os.Remove(tmp.Name())
	}
}

// DefaultCachePath is $XDG_STATE_HOME/zenvik/update-check.json when
// XDG_STATE_HOME is absolute, else the user cache directory's
// zenvik/update-check.json (the roots the mount records and the GUI queue
// already use).
func DefaultCachePath() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "zenvik", "update-check.json"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "zenvik", "update-check.json"), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/update/ && go vet ./internal/update/ && golangci-lint run ./internal/update/`
Expected: `ok`. If `TestFetchFailuresAreCached/500` fails on the detail, note `resp.Status` is `"500 Internal Server Error"`, so the detail is `HTTP 500 Internal Server Error`, which contains `HTTP 500`.

- [ ] **Step 5: Commit**

```bash
git add internal/update/check.go internal/update/check_test.go
git commit -m "update: ask GitHub for the latest release, cached for a day and shared by the CLI and the app"
```

---

### Task 3: config key `update_check`

**Files:**
- Modify: `internal/config/config.go` (`File`, `Settings`, `Resolve`)
- Modify: `internal/config/default.go` (`DefaultFileContent`)
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/default_test.go:36`

**Interfaces:**
- Produces: `File.UpdateCheck *bool` (TOML `update_check`), `Settings.UpdateCheck bool` (default true). Tasks 4 and 6 read `Settings.UpdateCheck`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestUpdateCheck(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"absent": {"", true},
		"true":   {"update_check = true", true},
		"false":  {"update_check = false", false},
	} {
		t.Run(name, func(t *testing.T) {
			f, err := Load(writeConfig(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			s, err := Resolve(f, Flags{})
			if err != nil || s.UpdateCheck != tc.want {
				t.Errorf("UpdateCheck = %v, %v; want %v", s.UpdateCheck, err, tc.want)
			}
		})
	}
	_, err := Load(writeConfig(t, `update_check = "yes"`))
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "update_check") {
		t.Errorf("non-boolean update_check: %v, want ErrInvalid naming the key", err)
	}
	_, err = Load(writeConfig(t, "[presets.plex]\nupdate_check = false"))
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "update_check") {
		t.Errorf("update_check in a preset: %v, want ErrInvalid naming the key", err)
	}
}
```

(`writeConfig` already exists in that file; check its imports include `errors` and `strings`, and add them if not.)

In `internal/config/default_test.go`, change the key list on line 36 to:

```go
	for _, key := range []string{"output_dir", "template", "min_duration", "mkvmerge_path", "update_check"} {
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL: `s.UpdateCheck undefined` (compile error).

- [ ] **Step 3: Implement**

In `internal/config/config.go`:

Add to `File` after `MkvmergePath`:

```go
	UpdateCheck  *bool             `toml:"update_check"`
```

Add to `Settings` after `MkvmergePath`:

```go
	UpdateCheck  bool   // tell the user about newer releases (default true)
```

In `Resolve`, change the first line to:

```go
	s := Settings{OutputDir: ".", Template: DefaultTemplate, MinDuration: DefaultMinDuration, UpdateCheck: true}
```

and after the `if f.MkvmergePath != nil { ... }` block add:

```go
	if f.UpdateCheck != nil {
		s.UpdateCheck = *f.UpdateCheck
	}
```

In `internal/config/default.go`, in `DefaultFileContent`, after the `mkvmerge_path = ""` line and its blank line, insert:

```
# Tell me when a newer zenvik is released (checks GitHub at most once a day;
# the ZENVIK_NO_UPDATE_CHECK environment variable also turns this off).
update_check = true

```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/config/ ./cmd/zenvik/ && golangci-lint run ./internal/config/`
Expected: `ok` for both (`cmd/zenvik`'s `TestDoctorWritesDefaultConfig` compares against `DefaultFileContent`, so it keeps passing).

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "config: update_check key, default true"
```

---

### Task 4: CLI passive notice

**Files:**
- Create: `cmd/zenvik/update.go`
- Create: `cmd/zenvik/update_test.go`
- Modify: `cmd/zenvik/main.go:38-58` (`run`) and `cmd/zenvik/main.go:102-132` (`newRootCmd`)

**Interfaces:**
- Consumes: `update.Checker`, `update.Result`, `update.LatestPage` (Task 2); `config.Settings.UpdateCheck` (Task 3); existing `loadSettings(config.Flags)` in `settings.go`, `version` in `version.go`.
- Produces: `var newUpdateChecker func() *update.Checker`, `var isTerminal func(*os.File) bool`, `var noticeWait time.Duration`, `type updateNotice`, `func newUpdateNotice() *updateNotice`, `func (n *updateNotice) start(cmd *cobra.Command)`, `func (n *updateNotice) print(w io.Writer)`, and `newRootCmd(n *updateNotice)`. Task 5 reuses `newUpdateChecker`.

- [ ] **Step 1: Write the failing tests**

`cmd/zenvik/update_test.go`:

```go
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/update"
)

const latestPage = "https://github.com/chad3814/zenvik/releases/latest"

// releaseBuild makes the CLI behave as release v1.2.0 whose update checker
// talks to a server answering with latest (an HTTP status when latest is
// numeric, e.g. "500"), with stderr counted as a terminal. It returns the
// request counter.
func releaseBuild(t *testing.T, latest string) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if latest == "500" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://github.com/chad3814/zenvik/releases/tag/%s"}`, latest, latest)
	}))
	t.Cleanup(srv.Close)
	cache := filepath.Join(t.TempDir(), "update-check.json")
	prevVersion, prevNew, prevTerm := version, newUpdateChecker, isTerminal
	version = "v1.2.0"
	newUpdateChecker = func() *update.Checker {
		return &update.Checker{URL: srv.URL, CachePath: cache, Client: srv.Client()}
	}
	isTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { version, newUpdateChecker, isTerminal = prevVersion, prevNew, prevTerm })
	t.Setenv("CI", "")
	t.Setenv("ZENVIK_NO_UPDATE_CHECK", "")
	return &hits
}

func TestUpdateNoticeAfterInfo(t *testing.T) {
	hits := releaseBuild(t, "v1.3.0")
	disc := writeDisc(t, testdisc.SampleMovie())
	want := "zenvik: v1.3.0 is available (you have v1.2.0): " + latestPage + "\n"
	code, out, errOut := runCLI("info", disc)
	if code != 0 || errOut != want || !strings.HasPrefix(out, "Sample Movie") {
		t.Fatalf("exit %d, stderr %q, want %q", code, errOut, want)
	}
	if hits.Load() != 1 {
		t.Errorf("%d requests, want 1", hits.Load())
	}
	// A second run is answered from the cache and still says so.
	if _, _, errOut := runCLI("info", disc); errOut != want || hits.Load() != 1 {
		t.Errorf("second run: stderr %q, requests %d", errOut, hits.Load())
	}
}

func TestUpdateNoticeFollowsTheError(t *testing.T) {
	releaseBuild(t, "v1.3.0")
	code, _, errOut := runCLI("info", filepath.Join(t.TempDir(), "missing"))
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if code != 1 || len(lines) != 2 || !strings.HasPrefix(lines[0], "zenvik: ") || lines[1] != "zenvik: v1.3.0 is available (you have v1.2.0): "+latestPage {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestUpdateNoticeWhenCurrent(t *testing.T) {
	hits := releaseBuild(t, "v1.2.0")
	if _, _, errOut := runCLI("info", writeDisc(t, testdisc.SampleMovie())); errOut != "" || hits.Load() != 1 {
		t.Errorf("stderr %q, requests %d", errOut, hits.Load())
	}
}

func TestUpdateNoticeSilentOnFailure(t *testing.T) {
	hits := releaseBuild(t, "500")
	if code, _, errOut := runCLI("info", writeDisc(t, testdisc.SampleMovie())); code != 0 || errOut != "" || hits.Load() != 1 {
		t.Errorf("exit %d, stderr %q, requests %d", code, errOut, hits.Load())
	}
}

func TestUpdateNoticeSkipped(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	cases := map[string]struct {
		setup func(t *testing.T)
		args  []string
	}{
		"dev build":        {func(*testing.T) { version = "" }, []string{"info", disc}},
		"CI":               {func(t *testing.T) { t.Setenv("CI", "true") }, []string{"info", disc}},
		"env opt-out":      {func(t *testing.T) { t.Setenv("ZENVIK_NO_UPDATE_CHECK", "1") }, []string{"info", disc}},
		"config opt-out":   {func(t *testing.T) { writeUserConfig(t, "update_check = false\n") }, []string{"info", disc}},
		"not a terminal":   {func(*testing.T) { isTerminal = func(*os.File) bool { return false } }, []string{"info", disc}},
		"--version":        {func(*testing.T) {}, []string{"--version"}},
		"help":             {func(*testing.T) {}, []string{"help"}},
		"rip --jsonl":      {func(t *testing.T) { t.Setenv("PATH", t.TempDir()) }, []string{"rip", "--jsonl", "--dry-run", disc}},
		"rip --jsonl last": {func(t *testing.T) { t.Setenv("PATH", t.TempDir()) }, []string{"rip", "--dry-run", disc, "--jsonl=true"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hits := releaseBuild(t, "v1.3.0")
			tc.setup(t)
			_, _, errOut := runCLI(tc.args...)
			if strings.Contains(errOut, "is available") {
				t.Errorf("notice printed: %q", errOut)
			}
			if hits.Load() != 0 {
				t.Errorf("%d requests, want none", hits.Load())
			}
		})
	}
}

func TestUpdateNoticeDoesNotWaitForASlowServer(t *testing.T) {
	releaseBuild(t, "v1.3.0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	t.Cleanup(srv.Close)
	cache := filepath.Join(t.TempDir(), "update-check.json")
	newUpdateChecker = func() *update.Checker {
		return &update.Checker{URL: srv.URL, CachePath: cache, Client: &http.Client{Timeout: 300 * time.Millisecond}}
	}
	defer func(d time.Duration) { noticeWait = d }(noticeWait)
	noticeWait = 50 * time.Millisecond
	start := time.Now()
	code, _, errOut := runCLI("info", writeDisc(t, testdisc.SampleMovie()))
	if code != 0 || errOut != "" {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("info took %v with a hung update server", took)
	}
}
```

`rip --dry-run` with an empty `PATH` exits 4 (no mkvmerge) after printing its JSONL error event; the test only cares that no request was made and nothing was added to stderr. If `--dry-run` turns out to need mkvmerge before it reads the flag, drop `--dry-run`; the outcome is the same.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/zenvik/ -run 'TestUpdateNotice'`
Expected: FAIL to compile: `undefined: newUpdateChecker`, `undefined: isTerminal`, `undefined: noticeWait`.

- [ ] **Step 3: Implement the notice**

`cmd/zenvik/update.go`:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/update"
)

// newUpdateChecker builds the checker the CLI uses; tests point it at a
// local server and a temporary cache.
var newUpdateChecker = func() *update.Checker {
	return &update.Checker{UserAgent: "zenvik/" + version}
}

// isTerminal reports whether f is a character device (a terminal rather
// than a pipe or file); tests override it.
var isTerminal = func(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// noticeWait bounds how long run waits for the check after the command.
// With a daily cache the wait happens once a day; it lets a fast command
// such as info still get (and cache) an answer.
var noticeWait = 2 * time.Second

// checkTimeout bounds the background request itself.
const checkTimeout = 5 * time.Second

// updateNotice carries the passive update check from the goroutine the
// root command's PersistentPreRun starts to run, which prints it after the
// command has finished.
type updateNotice struct {
	started bool
	result  chan update.Result // one value, Newer false on any failure
}

func newUpdateNotice() *updateNotice {
	return &updateNotice{result: make(chan update.Result, 1)}
}

// start begins the check for cmd when the conditions hold; it never blocks.
func (n *updateNotice) start(cmd *cobra.Command) {
	if !passiveCheckWanted(cmd) {
		return
	}
	n.started = true
	ctx, cancel := context.WithTimeout(cmd.Context(), checkTimeout)
	checker := newUpdateChecker()
	go func() {
		defer cancel()
		r, err := checker.Check(ctx, version)
		if err != nil {
			r = update.Result{}
		}
		n.result <- r
	}()
}

// passiveCheckWanted applies the design's conditions: a release build; one
// of info, rip or repair-udf (doctor runs its own explicit check); not
// --jsonl; no CI or ZENVIK_NO_UPDATE_CHECK in the environment; update_check
// not turned off in the config; and a terminal on stderr.
func passiveCheckWanted(cmd *cobra.Command) bool {
	if version == "" {
		return false
	}
	switch cmd.Name() {
	case "info", "rip", "repair-udf":
	default:
		return false
	}
	if f := cmd.Flags().Lookup("jsonl"); f != nil && f.Value.String() == "true" {
		return false
	}
	if os.Getenv("ZENVIK_NO_UPDATE_CHECK") != "" || os.Getenv("CI") != "" {
		return false
	}
	if !isTerminal(os.Stderr) {
		return false
	}
	s, err := loadSettings(config.Flags{})
	return err == nil && s.UpdateCheck
}

// print writes the notice to w when a newer release was found, waiting at
// most noticeWait for the check. It prints nothing when no check was
// started, the check failed, or this build is current.
func (n *updateNotice) print(w io.Writer) {
	if !n.started {
		return
	}
	select {
	case r := <-n.result:
		if r.Newer {
			fmt.Fprintf(w, "zenvik: %s is available (you have %s): %s\n", r.Latest, r.Current, update.LatestPage)
		}
	case <-time.After(noticeWait):
	}
}
```

In `cmd/zenvik/main.go`, change `run`:

```go
// run executes the CLI and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	notice := newUpdateNotice()
	root := newRootCmd(notice)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	defer notice.print(stderr)
	if err == nil {
		return 0
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "zenvik:") { // library errors already carry the prefix
		msg = "zenvik: " + msg
	}
	code := exitCode(err)
	if jsonlRequested(root, args) {
		newEventWriter(stdout).fail(msg, code, errors.Is(err, context.Canceled))
		return code
	}
	fmt.Fprintln(stderr, msg)
	return code
}
```

(`defer notice.print(stderr)` runs after the error line is written, so the notice is the last line.)

Change `newRootCmd`'s signature and add the hook:

```go
func newRootCmd(notice *updateNotice) *cobra.Command {
	root := &cobra.Command{
		Use:           "zenvik",
		Short:         "Inspect and remux unencrypted Blu-ray and DVD disc images",
		Version:       buildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			notice.start(cmd)
		},
		Args: func(cmd *cobra.Command, args []string) error {
```

(the rest of the literal is unchanged.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./cmd/zenvik/ && go vet ./cmd/zenvik/ && golangci-lint run ./cmd/zenvik/ && CGO_ENABLED=0 go build ./...`
Expected: `ok`; every pre-existing CLI test still passes because `version` is `""` outside `releaseBuild`.

If `TestUpdateNoticeSkipped/rip --jsonl last` fails because cobra's `PersistentPreRun` sees `--jsonl` as unparsed: it does not; flags are parsed before hooks run. If it fails for another reason, print `cmd.Flags().Lookup("jsonl").Value.String()` inside `passiveCheckWanted` to see what the hook sees.

- [ ] **Step 5: Commit**

```bash
git add cmd/zenvik/update.go cmd/zenvik/update_test.go cmd/zenvik/main.go
git commit -m "zenvik: say when a newer release exists, once a day, after info, rip and repair-udf"
```

---

### Task 5: `zenvik doctor` update line

**Files:**
- Modify: `cmd/zenvik/doctor.go` (`runDoctor`, new `updateLine`)
- Modify: `cmd/zenvik/update_test.go` (append)

**Interfaces:**
- Consumes: `newUpdateChecker` (Task 4), `update.Force`, `update.ErrDevBuild`, `update.ErrFetch`, `update.LatestPage` (Task 2), existing `buildVersion()`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/zenvik/update_test.go`:

```go
func TestDoctorUpdateLine(t *testing.T) {
	cases := map[string]struct {
		latest string
		dev    bool
		want   string
	}{
		"newer":   {"v1.3.0", false, "! zenvik v1.3.0 is available: " + latestPage},
		"current": {"v1.2.0", false, "✓ zenvik v1.2.0 is the latest release"},
		"failure": {"500", false, "! update check: HTTP 500 Internal Server Error"},
		"dev":     {"v1.3.0", true, "- update check: skipped in development builds"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hits := releaseBuild(t, tc.latest)
			if tc.dev {
				version = ""
			}
			isolateConfig(t)
			t.Setenv("PATH", t.TempDir())
			code, out, errOut := runCLI("doctor")
			if code != 4 {
				t.Errorf("exit %d, want 4 (mkvmerge missing; the update line must not change it)", code)
			}
			if !strings.Contains(out, tc.want+"\n") {
				t.Errorf("output lacks %q:\n%s", tc.want, out)
			}
			if strings.Contains(errOut, "is available") {
				t.Errorf("doctor also printed the passive notice: %q", errOut)
			}
			want := int32(1)
			if tc.dev {
				want = 0
			}
			if hits.Load() != want {
				t.Errorf("%d requests, want %d", hits.Load(), want)
			}
		})
	}
	t.Run("forces a fresh check", func(t *testing.T) {
		hits := releaseBuild(t, "v1.3.0")
		isolateConfig(t)
		t.Setenv("PATH", t.TempDir())
		runCLI("doctor")
		runCLI("doctor")
		if hits.Load() != 2 {
			t.Errorf("%d requests across two doctor runs, want 2", hits.Load())
		}
	})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/zenvik/ -run TestDoctorUpdateLine`
Expected: FAIL: `update line = "", want "! zenvik v1.3.0 is available: ..."` for each case.

- [ ] **Step 3: Implement**

In `cmd/zenvik/doctor.go`, add imports `"errors"` and `"strings"` and `"github.com/chad3814/zenvik/internal/update"`, then in `runDoctor` replace

```go
	if err == nil && live == 0 {
		fmt.Fprintln(out, "✓ no leftover mounts")
	}

	if code := doctorCode(mkOK, cfgOK, live); code != 0 {
```

with

```go
	if err == nil && live == 0 {
		fmt.Fprintln(out, "✓ no leftover mounts")
	}

	fmt.Fprintln(out, updateLine(ctx))

	if code := doctorCode(mkOK, cfgOK, live); code != 0 {
```

and add at the end of the file:

```go
// updateLine asks GitHub now (ignoring the daily cache) whether a newer
// release exists and describes the answer. It never affects the exit code.
func updateLine(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	r, err := newUpdateChecker().Force(ctx, buildVersion())
	switch {
	case errors.Is(err, update.ErrDevBuild):
		return "- update check: skipped in development builds"
	case err != nil:
		return "! update check: " + strings.TrimPrefix(err.Error(), update.ErrFetch.Error()+": ")
	case r.Newer:
		return fmt.Sprintf("! zenvik %s is available: %s", r.Latest, update.LatestPage)
	}
	return fmt.Sprintf("✓ zenvik %s is the latest release", r.Current)
}
```

Also update the command's `Short` and `Long` in `newDoctorCmd`:

```go
		Short: "Check mkvmerge, ISO mounting, the config file, leftover mounts and for a newer release",
		Long: `Check that mkvmerge is installed, whether ISO images can be mounted, that the
config file is valid, whether a crashed rip left mounts behind, and whether a newer zenvik
has been released. If there is no config file, doctor creates one with the defaults; it
never changes an existing file.`,
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./cmd/zenvik/ && golangci-lint run ./cmd/zenvik/`
Expected: `ok`. The pre-existing doctor tests now also see `- update check: skipped in development builds`; none asserts on the full output.

- [ ] **Step 5: Commit**

```bash
git add cmd/zenvik/doctor.go cmd/zenvik/update_test.go
git commit -m "doctor: report whether a newer zenvik release exists"
```

---

### Task 6: GUI banner and `CheckForUpdate`

**Files:**
- Create: `gui/update.go`
- Create: `gui/update_test.go`
- Modify: `gui/app.go:24-33` (`Deps`), `gui/app.go:36-40` (`Banner`), `gui/app.go:77-108` (`init`)
- Modify: `gui/settings.go:60-84` (`defaultDeps`)
- Modify: `gui/app_test.go:68-115` (`testOpts`, `newTestApp`)

**Interfaces:**
- Consumes: `update.Checker`, `update.Result`, `update.Parse`, `update.LatestPage` (Tasks 1–2); `config.Settings.UpdateCheck` (Task 3); existing `errs.Message`, `errNotStarted`, `setBanner`, `wait`.
- Produces: `Deps.CheckUpdate`, `Deps.ForceUpdate func(ctx context.Context, current string) (update.Result, error)`; `Banner.URL string` (JSON `url`); bound `func (a *App) CheckForUpdate() (string, error)`. Task 7 binds and renders these.

- [ ] **Step 1: Write the failing tests**

`gui/update_test.go`:

```go
package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/update"
)

func fakeResult(t *testing.T, current, latest string) update.Result {
	t.Helper()
	cur, err := update.Parse(current)
	if err != nil {
		t.Fatal(err)
	}
	lat, err := update.Parse(latest)
	if err != nil {
		t.Fatal(err)
	}
	return update.Result{Current: cur, Latest: lat, Newer: cur.Less(lat), URL: "https://github.com/chad3814/zenvik/releases/tag/" + latest}
}

func asRelease(t *testing.T) {
	t.Helper()
	prev := version
	version = "v1.2.0"
	t.Cleanup(func() { version = prev })
}

func TestUpdateBannerAtLaunch(t *testing.T) {
	asRelease(t)
	cases := map[string]struct {
		latest string
		err    error
		want   bool
	}{
		"newer": {"v1.3.0", nil, true},
		"same":  {"v1.2.0", nil, false},
		"error": {"", errors.New("boom"), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			a, sh, _ := newTestApp(t, testOpts{checkUpdate: func(_ context.Context, current string) (update.Result, error) {
				calls.Add(1)
				if tc.err != nil {
					return update.Result{}, tc.err
				}
				return fakeResult(t, current, tc.latest), nil
			}})
			waitFor(t, "update check", func() bool { return calls.Load() == 1 })
			if tc.want {
				waitFor(t, "update banner", func() bool { _, ok := banners(sh)["update"]; return ok })
				b := banners(sh)["update"]
				if b.Message != "Zenvik v1.3.0 is available." || b.Action != "download" || b.URL != update.LatestPage {
					t.Errorf("banner = %+v", b)
				}
				return
			}
			time.Sleep(50 * time.Millisecond)
			a.Ready()
			if b, ok := banners(sh)["update"]; ok {
				t.Errorf("unexpected banner %+v", b)
			}
		})
	}
}

func TestUpdateCheckSkipped(t *testing.T) {
	cases := map[string]func(t *testing.T) testOpts{
		"config off": func(t *testing.T) testOpts { asRelease(t); return testOpts{noUpdateCheck: true} },
		"env":        func(t *testing.T) testOpts { asRelease(t); t.Setenv("ZENVIK_NO_UPDATE_CHECK", "1"); return testOpts{} },
		"dev build":  func(*testing.T) testOpts { return testOpts{} },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			o := setup(t)
			var calls atomic.Int32
			o.checkUpdate = func(context.Context, string) (update.Result, error) {
				calls.Add(1)
				return fakeResult(t, "v1.2.0", "v1.3.0"), nil
			}
			a, sh, _ := newTestApp(t, o)
			time.Sleep(50 * time.Millisecond)
			a.Ready()
			if calls.Load() != 0 {
				t.Errorf("checker called %d times", calls.Load())
			}
			if _, ok := banners(sh)["update"]; ok {
				t.Error("update banner shown")
			}
		})
	}
}

func TestCheckForUpdate(t *testing.T) {
	asRelease(t)
	cases := map[string]struct {
		latest  string
		err     error
		want    string
		wantErr string
	}{
		"newer":   {"v1.3.0", nil, "v1.3.0", ""},
		"current": {"v1.2.0", nil, "", ""},
		"failure": {"", errors.New("zenvik: update check failed: HTTP 500"), "", "update check failed: HTTP 500"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var seen string
			a, _, _ := newTestApp(t, testOpts{forceUpdate: func(_ context.Context, current string) (update.Result, error) {
				seen = current
				if tc.err != nil {
					return update.Result{}, tc.err
				}
				return fakeResult(t, current, tc.latest), nil
			}})
			got, err := a.CheckForUpdate()
			if seen != "v1.2.0" {
				t.Errorf("checked %q, want the build version", seen)
			}
			if got != tc.want || (err == nil) != (tc.wantErr == "") || (err != nil && err.Error() != tc.wantErr) {
				t.Errorf("CheckForUpdate = %q, %v; want %q, %q", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestCheckForUpdateInDevBuild(t *testing.T) {
	a, _, _ := newTestApp(t, testOpts{forceUpdate: (&update.Checker{URL: "http://127.0.0.1:0/never"}).Force})
	got, err := a.CheckForUpdate()
	if got != "" || err == nil || err.Error() != "update check skipped in development builds" {
		t.Errorf("CheckForUpdate = %q, %v", got, err)
	}
}
```

Modify `gui/app_test.go`: add to `testOpts`

```go
	noUpdateCheck bool                                                            // config update_check = false
	checkUpdate   func(ctx context.Context, current string) (update.Result, error) // default: nil (never called)
	forceUpdate   func(ctx context.Context, current string) (update.Result, error)
```

in `newTestApp`'s `LoadSettings` return `config.Settings{OutputDir: ".", Template: tmpl, MinDuration: config.DefaultMinDuration, UpdateCheck: !o.noUpdateCheck}`, and add to the `Deps` literal:

```go
		CheckUpdate: o.checkUpdate,
		ForceUpdate: o.forceUpdate,
```

Add `"github.com/chad3814/zenvik/internal/update"` to that file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd gui && go test ./ -run 'TestUpdate|TestCheckForUpdate'`
Expected: FAIL to compile: `unknown field checkUpdate in struct literal`, `a.CheckForUpdate undefined`.

- [ ] **Step 3: Implement**

In `gui/app.go`, add to `Deps`:

```go
	CheckUpdate  func(ctx context.Context, current string) (update.Result, error) // passive, cached; nil: never
	ForceUpdate  func(ctx context.Context, current string) (update.Result, error) // About's explicit check
```

change `Banner` to:

```go
// Banner is a message across the top of the window.
type Banner struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Action  string `json:"action,omitempty"` // "recheck": a Recheck button; "download": a Download button opening URL
	URL     string `json:"url,omitempty"`
}
```

and in `init`, after `a.recheckMkvmerge()` and before `q.Start(ctx)`, add:

```go
	a.startUpdateCheck(ctx)
```

Add the import `"github.com/chad3814/zenvik/internal/update"` to `gui/app.go`.

`gui/update.go`:

```go
package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/chad3814/zenvik/gui/internal/errs"
	"github.com/chad3814/zenvik/internal/update"
)

// updateTimeout bounds each request to GitHub.
const updateTimeout = 10 * time.Second

// startUpdateCheck runs the cached update check in the background and
// raises the "update" banner when a newer release exists. It does nothing
// in development builds, when update_check is false in the config, when
// ZENVIK_NO_UPDATE_CHECK is set, or when no checker was supplied.
func (a *App) startUpdateCheck(ctx context.Context) {
	if _, err := update.Parse(version); err != nil || a.deps.CheckUpdate == nil {
		return
	}
	if os.Getenv("ZENVIK_NO_UPDATE_CHECK") != "" {
		return
	}
	a.mu.Lock()
	want := a.settings.UpdateCheck
	a.mu.Unlock()
	if !want {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, updateTimeout)
		defer cancel()
		r, err := a.deps.CheckUpdate(ctx, version)
		if err != nil || !r.Newer {
			return
		}
		a.setBanner(updateBanner(r))
	}()
}

func updateBanner(r update.Result) Banner {
	return Banner{ID: "update", Message: "Zenvik " + r.Latest.String() + " is available.", Action: "download", URL: update.LatestPage}
}

// CheckForUpdate asks GitHub now and returns the newer release's tag, or ""
// when this build is the latest. Development builds and network problems
// return an error whose message is shown as is.
func (a *App) CheckForUpdate() (string, error) {
	if !a.wait() {
		return "", errNotStarted
	}
	if a.deps.ForceUpdate == nil {
		return "", errors.New(errs.Message(update.ErrDevBuild))
	}
	ctx, cancel := context.WithTimeout(a.ctx, updateTimeout)
	defer cancel()
	r, err := a.deps.ForceUpdate(ctx, version)
	if err != nil {
		return "", errors.New(errs.Message(err))
	}
	if !r.Newer {
		return "", nil
	}
	return r.Latest.String(), nil
}
```

In `gui/settings.go`'s `defaultDeps`, before the `return Deps{` line add:

```go
	checker := &update.Checker{UserAgent: "zenvik-gui/" + version}
```

and add to the `Deps` literal:

```go
		CheckUpdate: checker.Check,
		ForceUpdate: checker.Force,
```

with the import `"github.com/chad3814/zenvik/internal/update"`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd gui && go vet $(go list ./... | grep -v /node_modules/) && go test -race $(go list ./... | grep -v /node_modules/)`
Expected: `ok`. `TestStartupBanners` and friends are unaffected: their `checkUpdate` is nil and `version` is `dev`.

- [ ] **Step 5: Commit**

```bash
git add gui/update.go gui/update_test.go gui/app.go gui/app_test.go gui/settings.go
git commit -m "gui: update banner at launch and a CheckForUpdate binding for About"
```

---

### Task 7: Frontend: Download button, About link, bindings

**Files:**
- Regenerate: `gui/frontend/wailsjs/go/main/App.d.ts`, `gui/frontend/wailsjs/go/main/App.js` (via `wails generate module`)
- Modify: `gui/frontend/src/types.ts:56-60` (`Banner`)
- Modify: `gui/frontend/src/api.ts` (add `checkForUpdate`)
- Modify: `gui/frontend/src/components/Banners.tsx`
- Modify: `gui/frontend/src/components/About.tsx`
- Modify: `gui/frontend/src/__tests__/Banners.test.tsx`
- Create: `gui/frontend/src/__tests__/About.test.tsx`

**Interfaces:**
- Consumes: bound `CheckForUpdate(): Promise<string>` (Task 6; a Go error rejects the promise with the message string), `Banner.url` and `action: 'download'` (Task 6).

- [ ] **Step 1: Regenerate the bindings**

Run from `gui/`:

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 generate module
```

Expected: `gui/frontend/wailsjs/go/main/App.d.ts` gains `export function CheckForUpdate():Promise<string>;` and `App.js` the matching function. On Linux add `-tags webkit2_41`. Commit only the `wailsjs/` changes in this step if the generator touched nothing else:

```bash
git add gui/frontend/wailsjs
git commit -m "gui: regenerate bindings for CheckForUpdate"
```

- [ ] **Step 2: Write the failing frontend tests**

Replace `gui/frontend/src/__tests__/Banners.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Banners } from '../components/Banners';

vi.mock('../api', () => ({ api: { recheckMkvmerge: vi.fn(() => Promise.resolve()), openURL: vi.fn() } }));

describe('Banners', () => {
  it('shows each banner and a Recheck button for mkvmerge', async () => {
    render(<Banners banners={[{ id: 'config', message: 'bad key' }, { id: 'mkvmerge', message: 'mkvmerge missing', action: 'recheck' }]} />);
    expect(screen.getAllByRole('alert')).toHaveLength(2);
    await userEvent.click(screen.getByRole('button', { name: 'Recheck' }));
    const { api } = await import('../api');
    expect(api.recheckMkvmerge).toHaveBeenCalled();
  });

  it('offers a Download button that opens the update URL', async () => {
    const url = 'https://github.com/chad3814/zenvik/releases/latest';
    render(<Banners banners={[{ id: 'update', message: 'Zenvik v1.3.0 is available.', action: 'download', url }]} />);
    expect(screen.getByRole('alert')).toHaveTextContent('Zenvik v1.3.0 is available.');
    await userEvent.click(screen.getByRole('button', { name: 'Download' }));
    const { api } = await import('../api');
    expect(api.openURL).toHaveBeenCalledWith(url);
  });

  it('shows no button for a download banner without a URL', () => {
    render(<Banners banners={[{ id: 'update', message: 'Zenvik v1.3.0 is available.', action: 'download' }]} />);
    expect(screen.queryByRole('button')).toBeNull();
  });
});
```

Create `gui/frontend/src/__tests__/About.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { About } from '../components/About';

vi.mock('../api', () => ({
  api: {
    version: vi.fn(() => Promise.resolve('v1.2.0')),
    mkvmergeInfo: vi.fn(() => Promise.resolve('')),
    mkvmergeSourceURL: vi.fn(() => Promise.resolve('')),
    checkForUpdate: vi.fn(() => Promise.resolve('')),
    openURL: vi.fn(),
  },
}));

describe('About', () => {
  let api: typeof import('../api').api;
  beforeEach(async () => {
    api = (await import('../api')).api;
    vi.clearAllMocks();
  });

  const open = async () => {
    render(<About />);
    await userEvent.click(screen.getByRole('button', { name: 'About' }));
    await screen.findByText('Zenvik v1.2.0');
  };

  it('says when this build is the latest', async () => {
    await open();
    await userEvent.click(screen.getByRole('link', { name: 'Check for updates' }));
    expect(await screen.findByText('You have the latest version')).toBeInTheDocument();
    expect(api.checkForUpdate).toHaveBeenCalledTimes(1);
  });

  it('names the newer release and links to the releases page', async () => {
    vi.mocked(api.checkForUpdate).mockResolvedValueOnce('v1.3.0');
    await open();
    await userEvent.click(screen.getByRole('link', { name: 'Check for updates' }));
    expect(await screen.findByText('v1.3.0 is available')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: 'Download' }));
    expect(api.openURL).toHaveBeenCalledWith('https://github.com/chad3814/zenvik/releases/latest');
  });

  it('shows the error text when the check fails', async () => {
    vi.mocked(api.checkForUpdate).mockRejectedValueOnce('update check failed: HTTP 500');
    await open();
    await userEvent.click(screen.getByRole('link', { name: 'Check for updates' }));
    expect(await screen.findByText('update check failed: HTTP 500')).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd gui/frontend && npm test -- Banners About`
Expected: FAIL: no `Download` button; `Check for updates` link not found; TypeScript complaint that `'download'` is not assignable to `'recheck'`.

- [ ] **Step 4: Implement**

`gui/frontend/src/types.ts`, replace the `Banner` interface:

```ts
export interface Banner {
  id: string;
  message: string;
  action?: 'recheck' | 'download';
  url?: string;
}
```

`gui/frontend/src/api.ts`, after `recheckMkvmerge`:

```ts
  /** checkForUpdate asks GitHub now; it resolves to the newer tag, or '' when current, and rejects with a message. */
  checkForUpdate: (): Promise<string> => App.CheckForUpdate(),
```

`gui/frontend/src/components/Banners.tsx`:

```tsx
import { api } from '../api';
import type { Banner } from '../types';

function action(b: Banner) {
  switch (b.action) {
    case 'recheck':
      return <button onClick={() => void api.recheckMkvmerge()}>Recheck</button>;
    case 'download': {
      const url = b.url;
      return url ? <button onClick={() => api.openURL(url)}>Download</button> : null;
    }
    default:
      return null;
  }
}

export function Banners({ banners }: { banners: Banner[] }) {
  if (banners.length === 0) return null;
  return (
    <div className="banners">
      {banners.map((b) => (
        <div key={b.id} role="alert" className="banner">
          <span>{b.message}</span>
          {action(b)}
        </div>
      ))}
    </div>
  );
}
```

`gui/frontend/src/components/About.tsx`:

```tsx
import { useState } from 'react';
import { api } from '../api';

const repoURL = 'https://github.com/chad3814/zenvik';
const releasesURL = 'https://github.com/chad3814/zenvik/releases/latest';

interface Info {
  version: string;
  mkvmerge: string;
  sourceURL: string;
}

type UpdateState =
  | { kind: 'idle' }
  | { kind: 'checking' }
  | { kind: 'current' }
  | { kind: 'available'; tag: string }
  | { kind: 'error'; message: string };

export function About() {
  const [info, setInfo] = useState<Info | null>(null);
  const [open, setOpen] = useState(false);
  const [update, setUpdate] = useState<UpdateState>({ kind: 'idle' });
  const show = async () => {
    setOpen(true);
    setUpdate({ kind: 'idle' });
    const [version, mkvmerge, sourceURL] = await Promise.all([
      api.version(),
      api.mkvmergeInfo(),
      api.mkvmergeSourceURL(),
    ]);
    setInfo({ version, mkvmerge, sourceURL });
  };
  const check = async () => {
    setUpdate({ kind: 'checking' });
    try {
      const tag = await api.checkForUpdate();
      setUpdate(tag ? { kind: 'available', tag } : { kind: 'current' });
    } catch (e) {
      setUpdate({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    }
  };
  const link = (url: string, text: string) => (
    <a
      href={url}
      onClick={(e) => {
        e.preventDefault();
        api.openURL(url);
      }}
    >
      {text}
    </a>
  );
  const updateText = () => {
    switch (update.kind) {
      case 'idle':
        return null;
      case 'checking':
        return <> · Checking…</>;
      case 'current':
        return <> · <span>You have the latest version</span></>;
      case 'available':
        return <> · <span>{update.tag} is available</span> · {link(releasesURL, 'Download')}</>;
      case 'error':
        return <> · <span>{update.message}</span></>;
    }
  };
  return (
    <>
      <button onClick={() => void show()}>About</button>
      {open && (
        <div role="dialog" aria-label="About Zenvik" className="dialog">
          <p><strong>Zenvik {info?.version ?? ''}</strong></p>
          <p>Rips unencrypted Blu-ray and DVD images to MKV with mkvmerge.</p>
          {info?.mkvmerge && (
            <p>
              <span>{info.mkvmerge}</span>
              {info.sourceURL && <> · {link(info.sourceURL, 'MKVToolNix source')}</>}
            </p>
          )}
          <p>
            <a
              href="#check-for-updates"
              onClick={(e) => {
                e.preventDefault();
                void check();
              }}
            >
              Check for updates
            </a>
            {updateText()}
          </p>
          <p>{link(repoURL, 'github.com/chad3814/zenvik')}</p>
          <button onClick={() => setOpen(false)}>Close</button>
        </div>
      )}
    </>
  );
}
```

Wails rejects a bound call whose Go method returned an error with the error's message as a string, so `String(e)` is the path taken in the app; the `Error` branch covers test and future runtime shapes.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd gui/frontend && npm test && npm run lint && npm run build`
Expected: all pass; `tsc` clean. If `About.test.tsx`'s "latest" assertion fails because `findByText('Zenvik v1.2.0')` matches the `<strong>` with a trailing space, use `await screen.findByText(/Zenvik v1\.2\.0/)`.

Then from `gui/`: `go test -race $(go list ./... | grep -v /node_modules/)` still passes (the embedded `frontend/dist` was rebuilt).

- [ ] **Step 6: Commit**

```bash
git add gui/frontend/src
git commit -m "gui: Download button on the update banner and Check for updates in About"
```

---

### Task 8: Documentation

**Files:**
- Modify: `README.md` (Install, Usage comment, Configuration)
- Modify: `gui/README.md`
- Modify: `CLAUDE.md`
- Modify: `gui/CLAUDE.md`

- [ ] **Step 1: README**

In `README.md`, after the paragraph ending "`zenvik --version` prints the release." in **Install**, add:

```markdown
**Update notifications.** Release builds check GitHub for a newer release at most once a day
and, when there is one, print a line to stderr after `info`, `rip` or `repair-udf`:
`zenvik: v1.3.0 is available (you have v1.2.0): https://github.com/chad3814/zenvik/releases/latest`.
Nothing else is sent, and the answer is cached in `$XDG_STATE_HOME/zenvik/update-check.json`
(else the user cache directory). The check is skipped when stderr isn't a terminal, with
`--jsonl`, when `CI` is set, in development builds, when the config has `update_check = false`,
or when `ZENVIK_NO_UPDATE_CHECK` is set to anything. `zenvik doctor` always checks and reports
the result on its own line.
```

In **Usage**, change the doctor comment line to:

```
zenvik doctor                                            # check mkvmerge, mounting, config, leftovers, updates
```

In **Configuration**, add to the TOML sample after `preset`:

```toml
update_check  = true                     # say when a newer release exists (daily check)
```

- [ ] **Step 2: gui/README.md**

After the paragraph starting "Zenvik reads the same config file", add:

```markdown
Release builds check GitHub for a newer release at most once a day when the app opens and show a banner with a Download button; **About → Check for updates** checks on demand. `update_check = false` in the config or the `ZENVIK_NO_UPDATE_CHECK` environment variable turns the launch check off.
```

- [ ] **Step 3: CLAUDE.md files**

In the root `CLAUDE.md` **Commands** list, add after the UDF repair bullet:

```markdown
- Update check (`internal/update`, standard library only; design `docs/superpowers/specs/2026-10-05-zenvik-update-check-design.md`): `Checker.Check` asks GitHub's `releases/latest` at most every 24 h via `<state>/zenvik/update-check.json`, `Force` always asks; both return `ErrDevBuild` for non-release versions, so tests and dev builds never reach the network. The CLI's root `PersistentPreRun` starts the passive check (release build; `info`/`rip`/`repair-udf`; not `--jsonl`; no `CI`/`ZENVIK_NO_UPDATE_CHECK`; config `update_check`; terminal stderr) and `run` waits ≤2 s to print it; `doctor` forces one. Tests swap `newUpdateChecker`, `isTerminal`, `noticeWait` and `version`.
```

In `gui/CLAUDE.md` **Rules**, add:

```markdown
- Update check: `startUpdateCheck` (launch banner `update`, action `download`) and the bound `CheckForUpdate` go through `Deps.CheckUpdate`/`Deps.ForceUpdate`; tests inject fakes and set `version` to a release tag, since `dev` never checks.
```

- [ ] **Step 4: Verify and commit**

Run: `go build ./... && go test -race ./... && golangci-lint run && cd gui && go test -race $(go list ./... | grep -v /node_modules/) && cd frontend && npm test && npm run lint`
Expected: all pass.

```bash
git add README.md gui/README.md CLAUDE.md gui/CLAUDE.md
git commit -m "docs: update notifications and how to turn them off"
```

---

### Task 9: Whole-feature verification

**Files:** none modified.

- [ ] **Step 1: Full verification, both modules**

Run from the worktree root:

```bash
go build ./... && CGO_ENABLED=0 go build ./... && go vet ./... && golangci-lint run && go test -race ./... && \
  (cd gui && go vet $(go list ./... | grep -v /node_modules/) && go test -race $(go list ./... | grep -v /node_modules/)) && \
  (cd gui/frontend && npm test && npm run lint && npm run build)
```

Expected: every command succeeds.

- [ ] **Step 2: Try a release build by hand against the real API**

This is the only step that touches the network, and it is manual, not a test.

```bash
go build -ldflags "-X main.version=v1.0.0" -o /tmp/zenvik-old ./cmd/zenvik
/tmp/zenvik-old doctor | grep -E 'update check|zenvik v'
cat "${XDG_STATE_HOME:-$HOME/Library/Caches}/zenvik/update-check.json"; echo
/tmp/zenvik-old info /nonexistent; echo "exit $?"
ZENVIK_NO_UPDATE_CHECK=1 /tmp/zenvik-old info /nonexistent 2>&1 | grep -c 'is available'
rm /tmp/zenvik-old
```

Expected: doctor prints `! zenvik vX.Y.Z is available: https://github.com/chad3814/zenvik/releases/latest` with the current latest tag; the cache file holds that tag; `info` prints its error line and then the notice; the opt-out run prints `0`. On Linux the cache is under `~/.cache/zenvik/`.

- [ ] **Step 3: Confirm the branch is clean**

```bash
git status --short
git log --oneline origin/main..HEAD
```

Expected: no uncommitted files; one commit per task above plus the spec commit.
