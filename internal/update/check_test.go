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
		"404":      {func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", http.StatusNotFound) }, "HTTP 404 Not Found"},
		"500":      {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }, "HTTP 500"},
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
	for name, body := range map[string]string{"garbage": "{not json", "empty": "", "no time": `{"latest":"v9.0.0"}`,
		"bad latest":   `{"checked_at":"2026-10-05T11:00:00Z","latest":"nightly"}`,
		"empty latest": `{"checked_at":"2026-10-05T11:00:00Z","latest":"","error":""}`} {
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

func TestCancelWithDeadlineCauseIsCached(t *testing.T) {
	var hits atomic.Int32
	started := make(chan struct{}, 1)
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		started <- struct{}{}
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancelCause(context.Background())
	go func() { <-started; cancel(fmt.Errorf("%w: gave up", context.DeadlineExceeded)) }()
	if _, err := f.c.Check(ctx, "v1.2.0"); !errors.Is(err, ErrFetch) {
		t.Fatalf("Check = %v, want ErrFetch", err)
	}
	if m := f.readCache(t); m["error"] == "" {
		t.Errorf("cache = %v, want the failure recorded", m)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.c.Check(context.Background(), "v1.2.0"); !errors.Is(err, ErrFetch) || hits.Load() != 1 {
		t.Errorf("second Check = %v, requests = %d; want the cached failure and 1 request", err, hits.Load())
	}
}

func TestDeadlineExceededIsCached(t *testing.T) {
	var hits atomic.Int32
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-r.Context().Done()
	}))
	f.c.Client = &http.Client{Timeout: 50 * time.Millisecond}
	if _, err := f.c.Check(context.Background(), "v1.2.0"); !errors.Is(err, ErrFetch) {
		t.Fatalf("Check = %v, want ErrFetch", err)
	}
	if m := f.readCache(t); m["error"] == "" {
		t.Errorf("cache = %v, want the failure recorded", m)
	}
	f.now = f.now.Add(time.Hour)
	if _, err := f.c.Check(context.Background(), "v1.2.0"); !errors.Is(err, ErrFetch) || hits.Load() != 1 {
		t.Errorf("second Check = %v, requests = %d; want the cached failure and 1 request", err, hits.Load())
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

func TestFailedRenameLeavesNoTempFile(t *testing.T) {
	f := newFixture(t, nil)
	// A directory at the cache path makes the final rename fail.
	if err := os.MkdirAll(filepath.Join(f.cache, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := f.c.Check(context.Background(), "v1.2.0")
	if err != nil || !r.Newer {
		t.Errorf("Check = %+v, %v", r, err)
	}
	if st, err := os.Stat(filepath.Join(f.cache, "keep")); err != nil || !st.IsDir() {
		t.Errorf("existing directory disturbed: %v", err)
	}
	ents, err := os.ReadDir(filepath.Dir(f.cache))
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 {
		t.Errorf("stray files next to the cache: %v", ents)
	}
}
