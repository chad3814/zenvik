package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
)

// scriptRipper rips by title ID: block (until ctx is done or release is
// closed), fail, or emit progress; it writes the output on success.
type scriptRipper struct {
	mu       sync.Mutex
	order    []string
	active   atomic.Int32
	maxSeen  atomic.Int32
	release  chan struct{}
	failWith map[string]error
	emit     func(progress func(zenvik.Progress))
}

func (r *scriptRipper) Rip(ctx context.Context, e Entry, progress func(zenvik.Progress)) error {
	n := r.active.Add(1)
	defer r.active.Add(-1)
	for {
		m := r.maxSeen.Load()
		if n <= m || r.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	r.mu.Lock()
	r.order = append(r.order, e.TitleID)
	r.mu.Unlock()
	if r.emit != nil {
		r.emit(progress)
	}
	if err := r.failWith[e.TitleID]; err != nil {
		return err
	}
	if r.release != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.release:
		}
	}
	return os.WriteFile(e.OutputPath, []byte("mkv"), 0o644)
}

func runQueue(t *testing.T, r Ripper, h Hooks) (*Queue, string) {
	t.Helper()
	dir := t.TempDir()
	q, err := Open(filepath.Join(dir, "state", "gui-queue.json"), r, h)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q.SetReady(true)
	q.Start(ctx)
	return q, dir
}

func waitState(t *testing.T, q *Queue, id string, want State) Entry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, e := range q.Snapshot().Entries {
			if e.ID == id && e.State == want {
				return e
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("entry %s never reached %s: %+v", id, want, q.Snapshot().Entries)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func entryIDs(q *Queue) []string {
	var out []string
	for _, e := range q.Snapshot().Entries {
		out = append(out, e.ID)
	}
	return out
}

func TestRunnerRipsInOrderOneAtATime(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	q.Add([]NewEntry{{discA, "01", filepath.Join(dir, "a.mkv")}, {discA, "02", filepath.Join(dir, "b.mkv")}, {discA, "03", filepath.Join(dir, "c.mkv")}})
	for _, id := range entryIDs(q) {
		e := waitState(t, q, id, Done)
		if e.StartedAt == nil || e.EndedAt == nil {
			t.Errorf("times not set: %+v", e)
		}
	}
	if got := r.order; len(got) != 3 || got[0] != "01" || got[2] != "03" {
		t.Errorf("order = %v", got)
	}
	if m := r.maxSeen.Load(); m != 1 {
		t.Errorf("%d rips ran at once", m)
	}
}

func TestRunnerWaitsForReadyAndUnpause(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	q.SetReady(false)
	q.SetPaused(true)
	q.Add([]NewEntry{{discA, "01", filepath.Join(dir, "a.mkv")}})
	time.Sleep(50 * time.Millisecond)
	if q.Snapshot().Entries[0].State != Waiting {
		t.Fatal("ripped while paused and not ready")
	}
	q.SetReady(true)
	time.Sleep(50 * time.Millisecond)
	if q.Snapshot().Entries[0].State != Waiting {
		t.Fatal("ripped while paused")
	}
	q.SetPaused(false)
	waitState(t, q, entryIDs(q)[0], Done)
}

func TestRunnerFailureContinues(t *testing.T) {
	r := &scriptRipper{failWith: map[string]error{"01": fmt.Errorf("title 01: %w", zenvik.ErrEncrypted)}}
	q, dir := runQueue(t, r, Hooks{})
	q.Add([]NewEntry{{discA, "01", filepath.Join(dir, "a.mkv")}, {discA, "02", filepath.Join(dir, "b.mkv")}})
	ids := entryIDs(q)
	e := waitState(t, q, ids[0], Failed)
	if e.Label != "encrypted" || e.Message == "" {
		t.Errorf("failed entry = %+v", e)
	}
	waitState(t, q, ids[1], Done)
}

func TestRunnerOutputExists(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	out := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(out, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	q.Add([]NewEntry{{discA, "01", out}})
	e := waitState(t, q, entryIDs(q)[0], Failed)
	if e.Message != "a.mkv exists" || e.Label != "exists" || len(r.order) != 0 {
		t.Errorf("entry = %+v, ripper calls %v", e, r.order)
	}
}

func TestRunnerIgnoresPartialFile(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	out := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(out+".partial", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	q.Add([]NewEntry{{discA, "01", out}})
	waitState(t, q, entryIDs(q)[0], Done)
}

func TestRunnerCreatesOutputFolder(t *testing.T) {
	r := &scriptRipper{}
	q, dir := runQueue(t, r, Hooks{})
	out := filepath.Join(dir, "Show", "Season 1", "e1.mkv")
	q.Add([]NewEntry{{discA, "01", out}})
	waitState(t, q, entryIDs(q)[0], Done)
	if _, err := os.Stat(out); err != nil {
		t.Error(err)
	}
}

func TestRunnerCancel(t *testing.T) {
	r := &scriptRipper{release: make(chan struct{})}
	q, dir := runQueue(t, r, Hooks{})
	q.Add([]NewEntry{{discA, "01", filepath.Join(dir, "a.mkv")}, {discA, "02", filepath.Join(dir, "b.mkv")}})
	ids := entryIDs(q)
	waitState(t, q, ids[0], Ripping)
	if !q.Running() {
		t.Error("Running() = false during a rip")
	}
	q.Cancel(ids[0])
	e := waitState(t, q, ids[0], Canceled)
	if e.Message != "" || e.EndedAt == nil {
		t.Errorf("canceled entry = %+v", e)
	}
	waitState(t, q, ids[1], Ripping)
	close(r.release)
	waitState(t, q, ids[1], Done)
}

func TestRunnerStopReturnsEntryToWaiting(t *testing.T) {
	r := &scriptRipper{release: make(chan struct{})}
	q, dir := runQueue(t, r, Hooks{})
	q.Add([]NewEntry{{discA, "01", filepath.Join(dir, "a.mkv")}, {discA, "02", filepath.Join(dir, "b.mkv")}})
	ids := entryIDs(q)
	waitState(t, q, ids[0], Ripping)
	q.Stop(5 * time.Second)
	s := q.Snapshot()
	if s.Entries[0].State != Waiting || s.Entries[0].StartedAt != nil || s.Entries[1].State != Waiting {
		t.Errorf("after Stop = %+v", s.Entries)
	}
	time.Sleep(50 * time.Millisecond)
	if q.Running() {
		t.Error("a rip started after Stop")
	}
	reopened, err := Open(q.path, r, Hooks{})
	if err != nil || reopened.Snapshot().Entries[0].State != Waiting {
		t.Errorf("saved state = %+v, %v", reopened.Snapshot().Entries, err)
	}
}

func TestProgressThrottle(t *testing.T) {
	var mu sync.Mutex
	var got []Progress
	r := &scriptRipper{emit: func(progress func(zenvik.Progress)) {
		for i := 0; i < 200; i++ {
			progress(zenvik.Progress{Phase: zenvik.PhaseMounting, Fraction: float64(i) / 1000})
		}
		progress(zenvik.Progress{Phase: zenvik.PhaseMounting + 1, Fraction: 0.5})
		progress(zenvik.Progress{Phase: zenvik.PhaseMounting + 1, Fraction: 1})
	}}
	q, dir := runQueue(t, r, Hooks{Progress: func(p Progress) { mu.Lock(); got = append(got, p); mu.Unlock() }})
	q.Add([]NewEntry{{discA, "01", filepath.Join(dir, "a.mkv")}})
	waitState(t, q, entryIDs(q)[0], Done)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("%d progress events, want 3 (first, phase change, 100%%): %+v", len(got), got)
	}
	if got[0].Phase != zenvik.PhaseMounting.String() || got[2].Fraction != 1 || got[0].ID == "" {
		t.Errorf("events = %+v", got)
	}
}

var _ = errors.New

// stopThenSucceed writes the output and calls Stop before returning nil, so
// finish sees both r.stopped and a nil error.
type stopThenSucceed struct{ q *Queue }

func (r *stopThenSucceed) Rip(ctx context.Context, e Entry, progress func(zenvik.Progress)) error {
	if err := os.WriteFile(e.OutputPath, []byte("mkv"), 0o644); err != nil {
		return err
	}
	r.q.Stop(0)
	return nil
}

func TestStopAfterSuccessfulRipKeepsDone(t *testing.T) {
	r := &stopThenSucceed{}
	q, dir := runQueue(t, r, Hooks{})
	r.q = q
	q.Add([]NewEntry{{discA, "01", filepath.Join(dir, "a.mkv")}})
	e := waitState(t, q, entryIDs(q)[0], Done)
	if e.EndedAt == nil {
		t.Errorf("EndedAt not set: %+v", e)
	}
	reopened, err := Open(q.path, r, Hooks{})
	if err != nil || reopened.Snapshot().Entries[0].State != Done {
		t.Errorf("saved state = %+v, %v", reopened.Snapshot().Entries, err)
	}
}

func TestRunnerMkvmergeMissingWaitsAndStops(t *testing.T) {
	for name, cause := range map[string]error{"not found": zenvik.ErrMkvmergeNotFound, "too old": zenvik.ErrMkvmergeTooOld} {
		t.Run(name, func(t *testing.T) {
			r := &scriptRipper{failWith: map[string]error{"01": fmt.Errorf("muxing: %w", cause)}}
			missing := make(chan error, 4)
			q, dir := runQueue(t, r, Hooks{MkvmergeMissing: func(err error) { missing <- err }})
			q.Add([]NewEntry{{discA, "01", filepath.Join(dir, "a.mkv")}, {discA, "02", filepath.Join(dir, "b.mkv")}})
			select {
			case err := <-missing:
				if !errors.Is(err, cause) {
					t.Errorf("hook got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("MkvmergeMissing was never called")
			}
			waitState(t, q, entryIDs(q)[0], Waiting)
			time.Sleep(50 * time.Millisecond) // give the runner a chance to start the next entry
			s := q.Snapshot()
			if s.Ready || q.Running() {
				t.Errorf("ready = %v, running = %v after mkvmerge went missing", s.Ready, q.Running())
			}
			if e := s.Entries[0]; e.Message != "" || e.StartedAt != nil || e.EndedAt != nil {
				t.Errorf("first entry = %+v, want plain waiting", e)
			}
			r.mu.Lock()
			order := append([]string{}, r.order...)
			r.mu.Unlock()
			if len(order) != 1 || s.Entries[1].State != Waiting {
				t.Errorf("rips = %v, second entry %s; the queue should have stopped", order, s.Entries[1].State)
			}
		})
	}
}
