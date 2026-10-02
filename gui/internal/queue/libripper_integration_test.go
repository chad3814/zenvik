//go:build integration

package queue

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/testdisc"
)

// TestLibRipperRipsRealDisc runs the real queue and ripper against a short
// Blu-ray folder authored with ffmpeg; it needs mkvmerge and ffmpeg.
func TestLibRipperRipsRealDisc(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	disc, err := testdisc.RealMovieDisc(context.Background(), t.TempDir(), 7)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "REAL MOVIE (2001) é")
	if err := disc.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	d, err := libOpen(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	main := d.Main().ID
	d.Close()

	var mu sync.Mutex
	var phases []string
	r := LibRipper{Open: libOpen, MkvmergePath: func() string { return "" }}
	q, dir := runQueue(t, r, Hooks{Progress: func(p Progress) { mu.Lock(); phases = append(phases, p.Phase); mu.Unlock() }})
	out := filepath.Join(dir, "Real Movie.mkv")
	if msgs := q.Add([]NewEntry{{root, main, out}}); msgs != nil {
		t.Fatal(msgs)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		e := q.Snapshot().Entries[0]
		if e.State == Done {
			break
		}
		if e.State == Failed || time.Now().After(deadline) {
			t.Fatalf("entry = %+v", e)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Fatalf("output: %v", err)
	}
	if _, err := os.Stat(out + ".partial"); err == nil {
		t.Error(".partial left behind")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(phases) == 0 {
		t.Error("no progress reported")
	}
}
