package discs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/testdisc"
)

type recorder struct {
	mu     sync.Mutex
	events [][]Summary
}

func (r *recorder) changed(s []Summary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *recorder) all() [][]Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]Summary(nil), r.events...)
}

func (r *recorder) snapshot() []Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return nil
	}
	return r.events[len(r.events)-1]
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

// absPath is an absolute path under the temp folder's volume: a path like
// "/out" has no drive letter, so it isn't absolute on Windows.
func absPath(elem ...string) string {
	root := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
	return filepath.Join(append([]string{root}, elem...)...)
}

func opener(ctx context.Context, path string) (*zenvik.Disc, error) {
	return zenvik.Open(ctx, path)
}

func newList(t *testing.T, open Opener) (*List, *recorder) {
	t.Helper()
	r := &recorder{}
	l := New(context.Background(), open, Config{Template: config.DefaultTemplate, OutputDir: absPath("out")}, r.changed)
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
	if ev := r.all(); len(ev) == 0 || len(ev[0]) != 1 || ev[0][0].State != "opening" {
		t.Fatalf("first event = %+v", ev)
	}
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	s, _ := l.Summary(dir)
	if s.State != "ready" || s.Format != "Blu-ray" || s.Name != "Sample Movie" || s.OutputDir != absPath("out") || len(s.Titles) == 0 {
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

// trackClose makes l count Close calls per disc instead of closing, since a
// *zenvik.Disc can't be observed from outside its package.
func trackClose(l *List) func(*zenvik.Disc) int {
	var mu sync.Mutex
	closes := map[*zenvik.Disc]int{}
	l.closeDisc = func(d *zenvik.Disc) {
		mu.Lock()
		closes[d]++
		mu.Unlock()
		d.Close()
	}
	return func(d *zenvik.Disc) int {
		mu.Lock()
		defer mu.Unlock()
		return closes[d]
	}
}

func TestListRemoveWhileOpeningClosesLater(t *testing.T) {
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	release := make(chan struct{})
	opened := make(chan *zenvik.Disc, 1)
	slow := func(ctx context.Context, path string) (*zenvik.Disc, error) {
		<-release
		d, err := zenvik.Open(ctx, path)
		opened <- d
		return d, err
	}
	l, _ := newList(t, slow)
	closeCount := trackClose(l)
	closed := make(chan struct{})
	inner := l.closeDisc
	l.closeDisc = func(d *zenvik.Disc) { inner(d); close(closed) }
	l.Remove(l.Add([]string{dir})[0])
	close(release)
	d := <-opened
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("disc opened after Remove was never closed")
	}
	if n := closeCount(d); n != 1 {
		t.Errorf("closed %d times, want 1", n)
	}
	if n := len(l.Summaries()); n != 0 {
		t.Errorf("%d discs after remove, want 0", n)
	}
}

func TestListReAddWhileOpeningDoesNotLeak(t *testing.T) {
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	var mu sync.Mutex
	var discs []*zenvik.Disc
	gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
	calls := 0
	open := func(ctx context.Context, path string) (*zenvik.Disc, error) {
		mu.Lock()
		g := gates[calls]
		calls++
		mu.Unlock()
		<-g
		d, err := zenvik.Open(ctx, path)
		if err == nil {
			mu.Lock()
			discs = append(discs, d)
			mu.Unlock()
		}
		return d, err
	}
	l, r := newList(t, open)
	closeCount := trackClose(l)
	l.Add([]string{dir})
	waitFor(t, "first open started", func() bool { mu.Lock(); defer mu.Unlock(); return calls == 1 })
	l.Remove(dir)
	l.Add([]string{dir})
	// The second open finishes first and fills the new item; the first,
	// stale open then finishes and must be closed, not stored.
	close(gates[1])
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	close(gates[0])
	waitFor(t, "both opened", func() bool { mu.Lock(); defer mu.Unlock(); return len(discs) == 2 })
	waitFor(t, "stale disc closed", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return closeCount(discs[0])+closeCount(discs[1]) == 1
	})
	if s, _ := l.Summary(dir); s.State != "ready" {
		t.Errorf("summary = %+v", s)
	}
	mu.Lock()
	defer mu.Unlock()
	if closeCount(discs[0]) != 0 || closeCount(discs[1]) != 1 { // discs are in finish order: live, then stale
		t.Error("the live disc was closed or the stale one was not")
	}
}

// A slow first callback must not let a later snapshot's event overtake it.
func TestListSlowCallbackKeepsEventOrder(t *testing.T) {
	r := &recorder{}
	gate := make(chan struct{})
	var calls atomic.Int32
	l := New(context.Background(), func(context.Context, string) (*zenvik.Disc, error) {
		return nil, errors.New("nope")
	}, Config{Template: config.DefaultTemplate, OutputDir: absPath("out")}, func(s []Summary) {
		if calls.Add(1) == 1 {
			<-gate // the "opening" event stalls in its callback
		}
		r.changed(s)
	})
	t.Cleanup(l.Close)
	done := make(chan struct{})
	go func() { l.Add([]string{absPath("nowhere", "DISC")}); close(done) }()
	time.Sleep(50 * time.Millisecond) // let the open fail while the callback is stalled
	close(gate)
	<-done
	waitFor(t, "error state", func() bool { return len(l.Summaries()) == 1 && l.Summaries()[0].State == "error" })
	waitFor(t, "two events", func() bool { return len(r.all()) == 2 })
	if got := r.snapshot()[0].State; got != "error" {
		t.Errorf("last event shows %q, want error", got)
	}
}

func TestListEventsArriveInSnapshotOrder(t *testing.T) {
	fail := func(ctx context.Context, path string) (*zenvik.Disc, error) {
		return nil, errors.New("nope")
	}
	l, r := newList(t, fail)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			l.Add([]string{absPath("nowhere", fmt.Sprintf("DISC%d", i))})
		}()
		go func() {
			defer wg.Done()
			l.Configure(Config{Template: config.DefaultTemplate, OutputDir: absPath(fmt.Sprintf("out%d", i))})
		}()
	}
	wg.Wait()
	waitFor(t, "all settled", func() bool {
		s := l.Summaries()
		for _, x := range s {
			if x.State == "opening" {
				return false
			}
		}
		return len(s) == 40
	})
	waitFor(t, "last event is final state", func() bool { return reflect.DeepEqual(r.snapshot(), l.Summaries()) })
	for _, s := range r.snapshot() {
		if s.State != "error" {
			t.Errorf("last event shows %+v", s)
		}
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

	l.SetOutputDir(dir, absPath("picked"))
	l.Configure(Config{Template: "{name} - {playlist}.mkv", OutputDir: absPath("new")})
	a, _ := l.Summary(dir)
	b, _ := l.Summary(other)
	if a.OutputDir != absPath("picked") || b.OutputDir != absPath("new") {
		t.Errorf("dirs = %q, %q", a.OutputDir, b.OutputDir)
	}
	if a.Titles[0].DefaultName != "Sample Movie - 00800.mkv" {
		t.Errorf("name after Configure = %q", a.Titles[0].DefaultName)
	}
}

func TestListEmitResendsCurrentSnapshot(t *testing.T) {
	l, r := newList(t, opener)
	dir := writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE")
	l.Add([]string{dir})
	waitFor(t, "ready", func() bool { return readyState(r, dir) })
	before := len(r.all())
	l.Emit()
	ev := r.all()
	if len(ev) != before+1 {
		t.Fatalf("%d events after Emit, want %d", len(ev), before+1)
	}
	if got := ev[len(ev)-1]; len(got) != 1 || got[0].Path != dir || got[0].State != "ready" {
		t.Errorf("emitted %+v", got)
	}
}
