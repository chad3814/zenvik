package queue

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/gui/internal/errs"
)

// Start rips in the background until ctx is done: whenever the queue is
// ready, not paused and idle, it rips the first waiting entry.
func (q *Queue) Start(ctx context.Context) { go q.loop(ctx) }

func (q *Queue) loop(ctx context.Context) {
	for {
		e, r, ok := q.next(ctx)
		if !ok {
			select {
			case <-q.wake:
				continue
			case <-ctx.Done():
				return
			}
		}
		err := prerun(e)
		if err == nil {
			err = q.ripper.Rip(r.ctx, e, q.progress(e.ID))
		}
		q.finish(r, err)
	}
}

func (q *Queue) next(ctx context.Context) (Entry, *running, bool) {
	var e Entry
	var r *running
	q.update(func() bool {
		if q.paused || !q.ready || q.stopping || q.run != nil || ctx.Err() != nil {
			return false
		}
		i := slices.IndexFunc(q.entries, func(e Entry) bool { return e.State == Waiting })
		if i < 0 {
			return false
		}
		now := q.now()
		q.entries[i].State, q.entries[i].StartedAt, q.entries[i].EndedAt = Ripping, &now, nil
		q.entries[i].Message, q.entries[i].Label = "", ""
		rctx, cancel := context.WithCancel(ctx)
		r = &running{id: q.entries[i].ID, ctx: rctx, cancel: cancel, done: make(chan struct{})}
		q.run = r
		e = q.entries[i]
		return true
	})
	return e, r, r != nil
}

// existsError reports a finished output already in the way; zenvik's own
// "<output>.partial" doesn't count.
type existsError struct{ name string }

func (e existsError) Error() string { return e.name + " exists" }
func (e existsError) Unwrap() error { return zenvik.ErrOutputExists }

func prerun(e Entry) error {
	if _, err := os.Stat(e.OutputPath); err == nil {
		return existsError{filepath.Base(e.OutputPath)}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.MkdirAll(filepath.Dir(e.OutputPath), 0o755)
}

func (q *Queue) finish(r *running, err error) {
	r.cancel()
	q.update(func() bool {
		q.run = nil
		i := q.indexLocked(r.id)
		if i < 0 {
			return true
		}
		e := &q.entries[i]
		now := q.now()
		switch {
		case r.stopped:
			e.State, e.StartedAt = Waiting, nil
		case err == nil:
			e.State, e.EndedAt = Done, &now
		case r.canceled:
			e.State, e.EndedAt = Canceled, &now
		default:
			e.State, e.EndedAt = Failed, &now
			e.Message, e.Label = errs.Message(err), errs.Label(err)
		}
		return true
	})
	close(r.done)
}

// Cancel stops the entry's rip if it is the one running; zenvik removes the
// partial file and the entry ends canceled.
func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.run != nil && q.run.id == id {
		q.run.canceled = true
		q.run.cancel()
	}
}

// Stop is for quitting: it starts nothing new, cancels the running rip (which
// goes back to waiting, so it restarts next launch) and waits up to timeout
// for it to wind down.
func (q *Queue) Stop(timeout time.Duration) {
	q.mu.Lock()
	q.stopping = true
	r := q.run
	if r != nil {
		r.stopped = true
		r.cancel()
	}
	q.mu.Unlock()
	if r != nil {
		select {
		case <-r.done:
		case <-time.After(timeout):
		}
	}
}

// Running reports whether a rip is in progress.
func (q *Queue) Running() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.run != nil
}

// progress forwards a rip's progress to Hooks.Progress, at most once per
// 100 ms within a phase; a new phase and completion always get through.
func (q *Queue) progress(id string) func(zenvik.Progress) {
	var last time.Time
	var lastPhase zenvik.Phase
	return func(p zenvik.Progress) {
		now := q.now()
		if p.Phase == lastPhase && p.Fraction < 1 && now.Sub(last) < 100*time.Millisecond {
			return
		}
		last, lastPhase = now, p.Phase
		q.hooks.Progress(Progress{ID: id, Phase: p.Phase.String(), Fraction: p.Fraction, BytesDone: p.BytesDone, BytesTotal: p.BytesTotal})
	}
}
