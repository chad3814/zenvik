package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMkvmerge(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Zen vik.app", "Contents")
	exe := filepath.Join(app, "MacOS", "zenvik-gui")
	bundled := filepath.Join(app, "Helpers", "mkvmerge")
	winExe := filepath.Join(dir, "zen vik", "zenvik-gui.exe")
	winBundled := filepath.Join(dir, "zen vik", "mkvmerge.exe")
	for _, f := range []string{bundled, winBundled} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, configured, exe, goos, wantPath, wantSource string
	}{
		{"config wins", "/opt/mkv/mkvmerge", exe, "darwin", "/opt/mkv/mkvmerge", "config"},
		{"bundled on darwin", "", exe, "darwin", bundled, "bundled"},
		{"bundled on windows", "", winExe, "windows", winBundled, "bundled"},
		{"nothing bundled on linux", "", filepath.Join(dir, "zenvik-gui"), "linux", "", "path"},
		{"no bundled copy beside a dev build", "", filepath.Join(dir, "bin", "zenvik-gui"), "darwin", "", "path"},
		{"unknown executable", "", "", "darwin", "", "path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, source := resolveMkvmerge(c.configured, c.exe, c.goos, fileExists)
			if path != c.wantPath || source != c.wantSource {
				t.Errorf("got %q, %q; want %q, %q", path, source, c.wantPath, c.wantSource)
			}
		})
	}
}

func TestMkvmergeSourceURL(t *testing.T) {
	want := "https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-102.0/mkvtoolnix-102.0.tar.xz"
	if got := mkvmergeSourceURL("102.0"); got != want {
		t.Errorf("got %q", got)
	}
}

func TestMkvmergeInfo(t *testing.T) {
	cases := []struct {
		s       mkvmergeState
		bundled string
		want    string
	}{
		{mkvmergeState{source: "bundled", path: "/A/mkvmerge", version: "102.0.0"}, "102.0", "mkvmerge 102.0 (bundled, from MKVToolNix — GPLv2)"},
		{mkvmergeState{source: "config", path: "/opt/mkvmerge", version: "101.0.0"}, "102.0", "mkvmerge 101.0.0 (/opt/mkvmerge)"},
		{mkvmergeState{source: "path", version: "100.0.0"}, "", "mkvmerge 100.0.0 (from PATH)"},
		{mkvmergeState{source: "path", err: os.ErrNotExist}, "", "mkvmerge not available — see the message at the top of the window"},
	}
	for _, c := range cases {
		if got := mkvmergeInfo(c.s, c.bundled); got != c.want {
			t.Errorf("mkvmergeInfo(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
}
