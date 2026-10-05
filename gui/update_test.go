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
	t.Setenv("ZENVIK_NO_UPDATE_CHECK", "")
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
