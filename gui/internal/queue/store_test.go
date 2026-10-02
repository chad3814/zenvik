package queue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndReopen(t *testing.T) {
	q, _, path := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	q.SetPaused(true)
	q2, err := Open(path, nopRipper{}, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	s := q2.Snapshot()
	if len(s.Entries) != 1 || s.Entries[0].OutputPath != abs("a.mkv") || !s.Paused || s.Ready {
		t.Errorf("reopened = %+v", s)
	}
	if q2.Warning() != "" {
		t.Errorf("warning = %q", q2.Warning())
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".gui-queue-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left: %v", leftovers)
	}
}

func TestOpenRestoresRippingAsWaiting(t *testing.T) {
	q, _, path := newQueue(t)
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	q.mu.Lock()
	q.entries[0].State = Ripping
	now := q.now()
	q.entries[0].StartedAt = &now
	q.persistLocked()
	q.mu.Unlock()
	q2, err := Open(path, nopRipper{}, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	e := q2.Snapshot().Entries[0]
	if e.State != Waiting || e.StartedAt != nil {
		t.Errorf("restored = %+v", e)
	}
}

func TestOpenMissingFileIsEmpty(t *testing.T) {
	q, err := Open(filepath.Join(t.TempDir(), "none.json"), nopRipper{}, Hooks{})
	if err != nil || len(q.Snapshot().Entries) != 0 {
		t.Errorf("q = %+v, err = %v", q, err)
	}
}

func TestOpenCorruptFileIsMovedAside(t *testing.T) {
	for name, content := range map[string]string{
		"not json":      "{nope",
		"wrong version": `{"version":7,"entries":[]}`,
		"bad entry":     `{"version":1,"entries":[{"id":"","state":"waiting","outputPath":"/a.mkv"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gui-queue.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			q, err := Open(path, nopRipper{}, Hooks{})
			if err != nil {
				t.Fatal(err)
			}
			if len(q.Snapshot().Entries) != 0 || !strings.Contains(q.Warning(), "gui-queue.json.bad-") {
				t.Errorf("entries %d, warning %q", len(q.Snapshot().Entries), q.Warning())
			}
			bad, _ := filepath.Glob(path + ".bad-*")
			if len(bad) != 1 {
				t.Errorf("moved-aside files = %v", bad)
			}
		})
	}
}

func TestSaveErrorIsReported(t *testing.T) {
	q, _, _ := newQueue(t)
	blocker := filepath.Join(t.TempDir(), "zenvik")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil { // a file where the folder should be
		t.Fatal(err)
	}
	q.path = filepath.Join(blocker, "gui-queue.json")
	q.Add([]NewEntry{{"/d", "01", abs("a.mkv")}})
	if q.Snapshot().SaveError == "" {
		t.Error("SaveError empty after a failed save")
	}
}
