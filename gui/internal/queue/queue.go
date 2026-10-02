// Package queue holds the GUI's rip queue: entries that rip one at a time,
// saved to disk after every change so the queue survives restarts.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/chad3814/zenvik"
)

// State is where an entry is in its life.
type State string

const (
	Waiting  State = "waiting"
	Ripping  State = "ripping"
	Done     State = "done"
	Failed   State = "failed"
	Canceled State = "canceled"
)

// Entry is one title to rip to one file.
type Entry struct {
	ID         string     `json:"id"`
	DiscPath   string     `json:"discPath"`
	TitleID    string     `json:"titleId"`
	OutputPath string     `json:"outputPath"` // absolute
	State      State      `json:"state"`
	Message    string     `json:"message,omitempty"` // why it failed
	Label      string     `json:"label,omitempty"`   // short form of Message
	AddedAt    time.Time  `json:"addedAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	EndedAt    *time.Time `json:"endedAt,omitempty"`
}

// NewEntry is what Add needs to queue a title.
type NewEntry struct {
	DiscPath, TitleID, OutputPath string
}

// Snapshot is the whole queue as the frontend renders it.
type Snapshot struct {
	Entries   []Entry `json:"entries"`
	Paused    bool    `json:"paused"`
	Ready     bool    `json:"ready"` // mkvmerge was found
	SaveError string  `json:"saveError,omitempty"`
}

// Progress is a running entry's progress, as reported to Hooks.Progress.
type Progress struct {
	ID         string  `json:"id"`
	Phase      string  `json:"phase"`
	Fraction   float64 `json:"fraction"`
	BytesDone  int64   `json:"bytesDone"`
	BytesTotal int64   `json:"bytesTotal"`
}

// Ripper rips one entry, reporting progress; it must return promptly with
// ctx's error once ctx is canceled.
type Ripper interface {
	Rip(ctx context.Context, e Entry, progress func(zenvik.Progress)) error
}

// Hooks are called without the queue's lock held.
type Hooks struct {
	Changed  func(Snapshot)
	Progress func(Progress)
}

// Queue is the rip queue. Its methods are safe for concurrent use.
type Queue struct {
	path   string
	ripper Ripper
	hooks  Hooks
	now    func() time.Time

	emitMu   sync.Mutex // orders Hooks.Changed calls; taken before mu
	mu       sync.Mutex
	entries  []Entry
	paused   bool
	ready    bool
	warning  string
	saveErr  string
	run      *running // the entry being ripped; nil when idle
	stopping bool     // Stop was called: start nothing new

	wake chan struct{}
}

// Open loads the queue saved at path (a missing file is an empty queue). An
// unreadable file is moved aside to path.bad-<unix time> and reported by
// Warning; entries that were ripping when the app stopped are waiting again.
func Open(path string, r Ripper, h Hooks) (*Queue, error) {
	q := &Queue{path: path, ripper: r, hooks: h, now: time.Now, wake: make(chan struct{}, 1)}
	if q.hooks.Changed == nil {
		q.hooks.Changed = func(Snapshot) {}
	}
	if q.hooks.Progress == nil {
		q.hooks.Progress = func(Progress) {}
	}
	st, err := load(path)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
	case errors.Is(err, errCorrupt):
		bad := fmt.Sprintf("%s.bad-%d", path, q.now().Unix())
		if rerr := os.Rename(path, bad); rerr != nil {
			return nil, rerr
		}
		q.warning = fmt.Sprintf("The saved queue couldn't be read and was moved to %s.", filepath.Base(bad))
	default:
		return nil, err
	}
	for _, e := range st.Entries {
		if e.State == Ripping {
			e.State, e.StartedAt = Waiting, nil
		}
		q.entries = append(q.entries, e)
	}
	q.paused = st.Paused
	return q, nil
}

// Warning describes a problem found when the queue was opened, or "".
func (q *Queue) Warning() string { return q.warning }

const msgTaken = "another queue entry already writes this file"

// Add queues items at the end. If any item is invalid (a relative output
// path, or one another waiting or ripping entry already writes) nothing is
// added and the result has a message for each item ("" where it was fine);
// on success it is nil.
func (q *Queue) Add(items []NewEntry) []string {
	var msgs []string
	q.update(func() bool {
		msgs = make([]string, len(items))
		taken := q.activeOutputsLocked("")
		bad := false
		for i, it := range items {
			switch {
			case !filepath.IsAbs(it.OutputPath):
				msgs[i] = "the output path must be absolute"
			case taken[it.OutputPath]:
				msgs[i] = msgTaken
			default:
				taken[it.OutputPath] = true
				continue
			}
			bad = true
		}
		if bad {
			return false
		}
		for _, it := range items {
			q.entries = append(q.entries, Entry{
				ID: newID(), DiscPath: it.DiscPath, TitleID: it.TitleID,
				OutputPath: it.OutputPath, State: Waiting, AddedAt: q.now(),
			})
		}
		msgs = nil
		return true
	})
	return msgs
}

// Rename changes a waiting entry's output path; it returns "" or why not.
func (q *Queue) Rename(id, outputPath string) string {
	var msg string
	q.update(func() bool {
		i := q.indexLocked(id)
		switch {
		case i < 0:
			msg = "no such queue entry"
		case q.entries[i].State != Waiting:
			msg = "only waiting entries can be renamed"
		case !filepath.IsAbs(outputPath):
			msg = "the output path must be absolute"
		case q.activeOutputsLocked(id)[outputPath]:
			msg = msgTaken
		}
		if msg != "" || q.entries[i].OutputPath == outputPath {
			return false
		}
		q.entries[i].OutputPath = outputPath
		return true
	})
	return msg
}

// Move puts the entry at index (clamped to the list).
func (q *Queue) Move(id string, index int) {
	q.update(func() bool {
		i := q.indexLocked(id)
		if i < 0 {
			return false
		}
		index = max(0, min(index, len(q.entries)-1))
		if i == index {
			return false
		}
		e := q.entries[i]
		q.entries = slices.Delete(q.entries, i, i+1)
		q.entries = slices.Insert(q.entries, index, e)
		return true
	})
}

// Remove drops an entry that isn't ripping (cancel it first).
func (q *Queue) Remove(id string) {
	q.update(func() bool {
		i := q.indexLocked(id)
		if i < 0 || q.entries[i].State == Ripping {
			return false
		}
		q.entries = slices.Delete(q.entries, i, i+1)
		return true
	})
}

// Retry moves a failed or canceled entry to the end of the queue, waiting.
func (q *Queue) Retry(id string) {
	q.update(func() bool {
		i := q.indexLocked(id)
		if i < 0 || (q.entries[i].State != Failed && q.entries[i].State != Canceled) {
			return false
		}
		e := q.entries[i]
		e.State, e.Message, e.Label, e.StartedAt, e.EndedAt = Waiting, "", "", nil, nil
		q.entries = append(slices.Delete(q.entries, i, i+1), e)
		return true
	})
}

// ClearFinished drops done, failed and canceled entries.
func (q *Queue) ClearFinished() {
	q.update(func() bool {
		n := len(q.entries)
		q.entries = slices.DeleteFunc(q.entries, func(e Entry) bool {
			return e.State == Done || e.State == Failed || e.State == Canceled
		})
		return len(q.entries) != n
	})
}

// SetPaused stops (or resumes) starting new rips; a running rip finishes.
func (q *Queue) SetPaused(p bool) {
	q.update(func() bool {
		changed := q.paused != p
		q.paused = p
		return changed
	})
}

// SetReady records whether mkvmerge was found; nothing starts until it is.
func (q *Queue) SetReady(r bool) {
	q.update(func() bool {
		changed := q.ready != r
		q.ready = r
		return changed
	})
}

// Snapshot returns the whole queue.
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.snapshotLocked()
}

// update runs f under the lock. When f reports a change, the queue is saved,
// Hooks.Changed gets a snapshot, and the runner is woken.
//
// emitMu is taken before mu and held through the Changed call, so snapshots
// reach Hooks.Changed in the order the changes were made. Hooks run without mu
// held: code that Hooks.Changed may wait on must take only mu, never emitMu
// (Task 4's Cancel and Stop do), or it would deadlock.
func (q *Queue) update(f func() bool) {
	q.emitMu.Lock()
	q.mu.Lock()
	if !f() {
		q.mu.Unlock()
		q.emitMu.Unlock()
		return
	}
	q.persistLocked()
	s := q.snapshotLocked()
	q.mu.Unlock()
	q.hooks.Changed(s)
	q.emitMu.Unlock()
	q.poke()
}

func (q *Queue) persistLocked() {
	if err := save(q.path, stored{Version: 1, Paused: q.paused, Entries: q.entries}); err != nil {
		q.saveErr = "The queue couldn't be saved: " + err.Error()
	} else {
		q.saveErr = ""
	}
}

func (q *Queue) snapshotLocked() Snapshot {
	return Snapshot{
		Entries: append([]Entry{}, q.entries...),
		Paused:  q.paused, Ready: q.ready, SaveError: q.saveErr,
	}
}

func (q *Queue) indexLocked(id string) int {
	return slices.IndexFunc(q.entries, func(e Entry) bool { return e.ID == id })
}

func (q *Queue) activeOutputsLocked(except string) map[string]bool {
	m := map[string]bool{}
	for _, e := range q.entries {
		if e.ID != except && (e.State == Waiting || e.State == Ripping) {
			m[e.OutputPath] = true
		}
	}
	return m
}

func (q *Queue) poke() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// running is the entry being ripped; its flags are guarded by Queue.mu.
type running struct {
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	canceled bool // Cancel: the entry ends canceled
	stopped  bool // Stop: the entry goes back to waiting
	done     chan struct{}
}
