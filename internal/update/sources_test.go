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
	unapprovedOnly := strings.ReplaceAll(chocolateyFeed, `>true<`, `>false<`)
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
