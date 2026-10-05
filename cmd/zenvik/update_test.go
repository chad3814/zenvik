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
