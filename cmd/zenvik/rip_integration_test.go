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
	code, stdout, errOut := runCLI("rip", "-p", "00800", "-o", out, disc)
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

	code, stdout, _ = runCLI("rip", "-p", "00800", "-o", t.TempDir(), "--dry-run", disc)
	if code != 0 || !strings.Contains(stdout, "mkvmerge") || !strings.Contains(stdout, "--language 1:jpn") {
		t.Errorf("dry run: exit %d, stdout %q", code, stdout)
	}
}
