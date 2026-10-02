package discs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/testdisc"
)

type recorder struct {
	mu   sync.Mutex
	last []Summary
	n    int
}

func (r *recorder) changed(s []Summary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last, r.n = s, r.n+1
}

func (r *recorder) snapshot() []Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func opener(ctx context.Context, path string) (*zenvik.Disc, error) {
	return zenvik.Open(ctx, path)
}

func newList(t *testing.T, open Opener) (*List, *recorder) {
	t.Helper()
	r := &recorder{}
	l := New(context.Background(), open, Config{Template: config.DefaultTemplate, OutputDir: "/out"}, r.changed)
	t.Cleanup(l.Close)
	return l, r
}

func readyState(r *recorder, path string) bool {
	for _, s := range r.snapshot() {
		if s.Path == path && s.State != "opening" {
			return true
		}
	}
	return false
}

func TestListAddOpensInBackground(t *testing.T) {
	l, r := newList(t, opener)
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	abs := l.Add([]string{dir + string(os.PathSeparator)})
	if len(abs) != 1 || abs[0] != dir {
		t.Fatalf("Add = %q, want [%q]", abs, dir)
	}
	if s := r.snapshot(); len(s) != 1 || s[0].State != "opening" {
		t.Fatalf("first event = %+v", s)
	}
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	s, _ := l.Summary(dir)
	if s.State != "ready" || s.Format != "Blu-ray" || s.Name != "Sample Movie" || s.OutputDir != "/out" || len(s.Titles) == 0 {
		t.Errorf("summary = %+v", s)
	}
}

func TestListAddExistingPathOnlyReturnsIt(t *testing.T) {
	l, r := newList(t, opener)
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	l.Add([]string{dir})
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	if abs := l.Add([]string{dir}); len(abs) != 1 || abs[0] != dir {
		t.Errorf("Add again = %q", abs)
	}
	if n := len(l.Summaries()); n != 1 {
		t.Errorf("%d discs, want 1", n)
	}
}

func TestListOpenError(t *testing.T) {
	l, r := newList(t, opener)
	enc := testdisc.SampleMovie()
	for id := range enc.Clips {
		enc.ClipData[id] = testdisc.ScrambledM2TS(1)
	}
	dir := writeBluray(t, enc, "ENCRYPTED")
	l.Add([]string{dir})
	waitFor(t, "error", func() bool { return readyState(r, dir) })
	s, _ := l.Summary(dir)
	if s.State != "error" || s.ErrorLabel != "encrypted" || s.Error == "" {
		t.Errorf("summary = %+v", s)
	}
}

func TestListAmbiguous(t *testing.T) {
	l, r := newList(t, opener)
	d := testdisc.SampleMovie()
	d.Titles, d.MovieObjects = nil, nil // no title-1 signal
	var segs []testdisc.Segment
	for i := 201; i <= 220; i++ {
		segs = append(segs, testdisc.Segment{Clip: fmt.Sprintf("%05d", i), Length: 297 * time.Second})
	}
	d.Playlists["00802"] = testdisc.SimplePlaylist(segs...)
	d.AddClipsFor()
	dir := writeBluray(t, d, "AMBIGUOUS")
	l.Add([]string{dir})
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	if s, _ := l.Summary(dir); !s.Ambiguous {
		t.Errorf("summary = %+v, want ambiguous", s)
	}
}

func TestListRemoveWhileOpeningClosesLater(t *testing.T) {
	release := make(chan struct{})
	var closed sync.WaitGroup
	closed.Add(1)
	slow := func(ctx context.Context, path string) (*zenvik.Disc, error) {
		<-release
		defer closed.Done()
		return nil, errors.New("too late")
	}
	l, _ := newList(t, slow)
	abs := l.Add([]string{filepath.Join(t.TempDir(), "DISC")})
	l.Remove(abs[0])
	close(release)
	closed.Wait()
	time.Sleep(20 * time.Millisecond)
	if n := len(l.Summaries()); n != 0 {
		t.Errorf("%d discs after remove, want 0", n)
	}
}

func TestListOutputDirAndConfigure(t *testing.T) {
	l, r := newList(t, opener)
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	l.Add([]string{dir})
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	other := writeBluray(t, testdisc.SampleMovie(), "OTHER")
	l.Add([]string{other})
	waitFor(t, "ready", func() bool { return readyState(r, other) })

	l.SetOutputDir(dir, "/picked")
	l.Configure(Config{Template: "{name} - {playlist}.mkv", OutputDir: "/new"})
	a, _ := l.Summary(dir)
	b, _ := l.Summary(other)
	if a.OutputDir != "/picked" || b.OutputDir != "/new" {
		t.Errorf("dirs = %q, %q", a.OutputDir, b.OutputDir)
	}
	if a.Titles[0].DefaultName != "Sample Movie - 00800.mkv" {
		t.Errorf("name after Configure = %q", a.Titles[0].DefaultName)
	}
}
