//go:build integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestRipCommand(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "REAL_MOVIE")
	if err := testdisc.RealMovie(context.Background(), disc, 3); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	code, stdout, errOut := runCLI("rip", "-p", "00800", "-d", out, disc)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	want := filepath.Join(out, "Real Movie.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("output missing: %v\nstdout:\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Done: "+want) || !strings.Contains(stdout, "muxing 100%") {
		t.Errorf("stdout = %q", stdout)
	}

	code, stdout, _ = runCLI("rip", "-p", "00800", "-d", t.TempDir(), "--dry-run", disc)
	if code != 0 || !strings.Contains(stdout, "mkvmerge") || !strings.Contains(stdout, "--language 1:jpn") {
		t.Errorf("dry run: exit %d, stdout %q", code, stdout)
	}
}

func TestRipCommandJSONL(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "REAL_MOVIE")
	if err := testdisc.RealMovie(context.Background(), disc, 3); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(t.TempDir(), "movie.mkv")
	code, stdout, errOut := runCLI("rip", "--jsonl", "-p", "00800", "-o", want, disc)
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", code, errOut, stdout)
	}
	evs := events(t, stdout)
	ks := kinds(evs)
	if ks[0] != "start" || ks[len(ks)-1] != "done" || evs[len(evs)-1]["output"] != want {
		t.Fatalf("events = %v\n%s", ks, stdout)
	}
	sawMux100 := false
	for _, ev := range evs {
		if ev["event"] == "progress" && ev["phase"] == "muxing" && ev["fraction"] == float64(1) {
			sawMux100 = true
		}
	}
	if !sawMux100 {
		t.Errorf("no muxing progress at 100%%:\n%s", stdout)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("output missing: %v", err)
	}
}
