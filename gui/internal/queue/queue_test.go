package queue

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
)

type nopRipper struct{}

func (nopRipper) Rip(context.Context, Entry, func(zenvik.Progress)) error { return nil }

type hookLog struct {
	mu    sync.Mutex
	snaps []Snapshot
}

func (h *hookLog) changed(s Snapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snaps = append(h.snaps, s)
}

func (h *hookLog) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.snaps)
}

func newQueue(t *testing.T) (*Queue, *hookLog, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "zenvik", "gui-queue.json")
	h := &hookLog{}
	q, err := Open(path, nopRipper{}, Hooks{Changed: h.changed})
	if err != nil {
		t.Fatal(err)
	}
	return q, h, path
}

func abs(name string) string { return filepath.Join(string(filepath.Separator)+"out", name) }

func ids(s Snapshot) []string {
	var out []string
	for _, e := range s.Entries {
		out = append(out, e.TitleID)
	}
	return out
}

func TestAddAppendsWaitingEntries(t *testing.T) {
	q, h, _ := newQueue(t)
	if msgs := q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}, {"/d", "02", abs("b.mkv")}}); msgs != nil {
		t.Fatalf("Add = %q", msgs)
	}
	s := q.Snapshot()
	if len(s.Entries) != 2 || s.Entries[0].State != Waiting || s.Entries[0].ID == "" || s.Entries[0].ID == s.Entries[1].ID {
		t.Fatalf("entries = %+v", s.Entries)
	}
	if s.Entries[0].AddedAt.IsZero() || h.count() != 1 {
		t.Errorf("addedAt %v, %d change events", s.Entries[0].AddedAt, h.count())
	}
}

func TestAddRejectsDuplicateOutputs(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	msgs := q.Add([]NewEntry{{"/d", "02", abs("b.mkv")}, {"/d", "03", abs("a.mkv")}, {"/e", "01", abs("b.mkv")}, {"/e", "02", "relative.mkv"}})
	want := []string{"", "another queue entry already writes this file", "another queue entry already writes this file", "the output path must be absolute"}
	if len(msgs) != len(want) {
		t.Fatalf("msgs = %q", msgs)
	}
	for i := range want {
		if msgs[i] != want[i] {
			t.Errorf("msgs[%d] = %q, want %q", i, msgs[i], want[i])
		}
	}
	if n := len(q.Snapshot().Entries); n != 1 {
		t.Errorf("%d entries after a rejected batch, want 1", n)
	}
}

func TestFinishedEntriesDontBlockTheirOutput(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	q.mu.Lock()
	q.entries[0].State = Done
	q.mu.Unlock()
	if msgs := q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}}); msgs != nil {
		t.Errorf("Add after done = %q", msgs)
	}
}

func TestRename(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}, {"/d", "02", abs("b.mkv")}})
	s := q.Snapshot()
	a, b := s.Entries[0].ID, s.Entries[1].ID
	if msg := q.Rename(a, abs("b.mkv")); msg != "another queue entry already writes this file" {
		t.Errorf("rename onto b = %q", msg)
	}
	if msg := q.Rename(a, abs("a.mkv")); msg != "" {
		t.Errorf("rename to itself = %q", msg)
	}
	if msg := q.Rename(b, abs("c.mkv")); msg != "" || q.Snapshot().Entries[1].OutputPath != abs("c.mkv") {
		t.Errorf("rename = %q, %+v", msg, q.Snapshot().Entries[1])
	}
	q.mu.Lock()
	q.entries[0].State = Done
	q.mu.Unlock()
	if msg := q.Rename(a, abs("z.mkv")); msg != "only waiting entries can be renamed" {
		t.Errorf("rename done = %q", msg)
	}
	if msg := q.Rename("nope", abs("z.mkv")); msg != "no such queue entry" {
		t.Errorf("rename missing = %q", msg)
	}
}

func TestMoveRemoveRetryClear(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("1.mkv")}, {"/d", "02", abs("2.mkv")}, {"/d", "03", abs("3.mkv")}})
	s := q.Snapshot()
	q.Move(s.Entries[2].ID, 0)
	if got := ids(q.Snapshot()); got[0] != "03" || got[1] != "01" || got[2] != "02" {
		t.Errorf("after move = %v", got)
	}
	q.Move(s.Entries[2].ID, 99)
	if got := ids(q.Snapshot()); got[2] != "03" {
		t.Errorf("after move to end = %v", got)
	}

	q.mu.Lock()
	q.entries[0].State, q.entries[0].Message = Failed, "boom"
	q.entries[1].State = Done
	q.mu.Unlock()
	q.Retry(s.Entries[0].ID) // "01", failed: back to waiting at the end
	got := q.Snapshot()
	last := got.Entries[len(got.Entries)-1]
	if last.TitleID != "01" || last.State != Waiting || last.Message != "" {
		t.Errorf("after retry = %+v", got.Entries)
	}
	q.ClearFinished()
	if got := ids(q.Snapshot()); len(got) != 2 {
		t.Errorf("after clear = %v", got)
	}
	q.Remove(q.Snapshot().Entries[0].ID)
	if n := len(q.Snapshot().Entries); n != 1 {
		t.Errorf("after remove: %d entries", n)
	}
}

func TestRemoveIgnoresRippingEntry(t *testing.T) {
	q, _, _ := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("1.mkv")}})
	q.mu.Lock()
	q.entries[0].State = Ripping
	q.mu.Unlock()
	q.Remove(q.Snapshot().Entries[0].ID)
	if n := len(q.Snapshot().Entries); n != 1 {
		t.Errorf("ripping entry removed")
	}
}

func TestPausedAndReadyAreReported(t *testing.T) {
	q, _, _ := newQueue(t)
	q.SetPaused(true)
	q.SetReady(true)
	if s := q.Snapshot(); !s.Paused || !s.Ready {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestSnapshotEntriesNeverNil(t *testing.T) {
	q, _, _ := newQueue(t)
	if q.Snapshot().Entries == nil {
		t.Error("Entries is nil; the frontend expects []")
	}
}

var _ = time.Second

func TestChangedEventsArriveInOrder(t *testing.T) {
	q, h, _ := newQueue(t)
	var calls atomic.Int32
	q.hooks.Changed = func(s Snapshot) {
		if calls.Add(1) == 1 {
			time.Sleep(50 * time.Millisecond) // a slow first delivery
		}
		h.changed(s)
	}
	started := make(chan struct{})
	go func() {
		close(started)
		q.SetPaused(true)
	}()
	<-started
	time.Sleep(10 * time.Millisecond) // the first update is now stuck delivering
	q.SetPaused(false)
	time.Sleep(100 * time.Millisecond)

	h.mu.Lock()
	last := h.snaps[len(h.snaps)-1]
	h.mu.Unlock()
	if want := q.Snapshot(); !reflect.DeepEqual(last, want) {
		t.Errorf("last event has paused=%v; queue has paused=%v", last.Paused, want.Paused)
	}
}

func TestConcurrentUpdatesEndOnTheFinalState(t *testing.T) {
	q, h, _ := newQueue(t)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				q.SetPaused(i%2 == 0)
				q.Add([]NewEntry{{"/d", "01", abs(fmt.Sprintf("%d-%d.mkv", g, i))}})
			}
		}()
	}
	wg.Wait()
	h.mu.Lock()
	last := h.snaps[len(h.snaps)-1]
	h.mu.Unlock()
	if want := q.Snapshot(); !reflect.DeepEqual(last, want) {
		t.Errorf("last event has %d entries, queue has %d", len(last.Entries), len(want.Entries))
	}
}
