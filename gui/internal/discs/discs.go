package discs

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/gui/internal/errs"
)

// Opener opens the disc at path.
type Opener func(ctx context.Context, path string) (*zenvik.Disc, error)

// Config is what summaries depend on: the name template and the default
// output folder for discs whose folder the user hasn't picked.
type Config struct {
	Template  string
	OutputDir string
}

// List is the window's discs, in the order added. Each disc opens in its own
// goroutine; every change is reported to changed with a full snapshot.
type List struct {
	ctx     context.Context
	open    Opener
	changed func([]Summary)
	// closeDisc closes a disc; tests replace it to observe closes.
	closeDisc func(*zenvik.Disc)

	// emitMu serializes snapshot-and-emit so events reach changed in the
	// order their snapshots were taken. It is always taken before mu, and
	// changed must not call back into the List.
	emitMu sync.Mutex
	mu     sync.Mutex
	cfg    Config
	order  []string
	byPath map[string]*item
}

type item struct {
	disc      *zenvik.Disc
	summary   Summary
	customDir bool
}

// New returns an empty list. changed is called without the list's lock held.
func New(ctx context.Context, open Opener, cfg Config, changed func([]Summary)) *List {
	return &List{ctx: ctx, open: open, changed: changed, closeDisc: func(d *zenvik.Disc) { d.Close() }, cfg: cfg, byPath: map[string]*item{}}
}

// Add starts opening each path that isn't listed yet and returns every
// path's absolute form, in order, so the caller can select the last one.
func (l *List) Add(paths []string) []string {
	var abs []string
	l.emitMu.Lock()
	defer l.emitMu.Unlock()
	l.mu.Lock()
	for _, p := range paths {
		a, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		abs = append(abs, a)
		if _, ok := l.byPath[a]; ok {
			continue
		}
		it := &item{summary: Summary{
			Path: a, State: "opening", Name: filepath.Base(a),
			OutputDir: l.cfg.OutputDir, Titles: []TitleSummary{},
		}}
		l.byPath[a] = it
		l.order = append(l.order, a)
		go l.load(a, it)
	}
	s := l.snapshotLocked()
	l.mu.Unlock()
	l.changed(s)
	return abs
}

func (l *List) load(path string, it *item) {
	d, err := l.open(l.ctx, path)
	l.emitMu.Lock()
	defer l.emitMu.Unlock()
	l.mu.Lock()
	if l.byPath[path] != it { // removed (and maybe re-added) while opening
		l.mu.Unlock()
		if d != nil {
			l.closeDisc(d)
		}
		return
	}
	if err != nil {
		it.summary.State = "error"
		it.summary.Error = errs.Message(err)
		it.summary.ErrorLabel = errs.Label(err)
	} else {
		it.disc = d
		it.fill(l.cfg)
	}
	s := l.snapshotLocked()
	l.mu.Unlock()
	l.changed(s)
}

func (it *item) fill(cfg Config) {
	d := it.disc
	it.summary.State = "ready"
	it.summary.Format = d.Format.String()
	it.summary.Label = d.Label
	it.summary.Name = d.Name()
	m := d.Main()
	it.summary.Ambiguous = m != nil && m.Rank.Ambiguous
	if !it.customDir {
		it.summary.OutputDir = cfg.OutputDir
	}
	it.summary.Titles = Titles(d, cfg.Template)
}

// Remove closes and drops the disc at path; queue entries are unaffected.
func (l *List) Remove(path string) {
	l.emitMu.Lock()
	defer l.emitMu.Unlock()
	l.mu.Lock()
	it, ok := l.byPath[path]
	if !ok {
		l.mu.Unlock()
		return
	}
	delete(l.byPath, path)
	for i, p := range l.order {
		if p == path {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	s := l.snapshotLocked()
	l.mu.Unlock()
	if it.disc != nil {
		l.closeDisc(it.disc)
	}
	l.changed(s)
}

// SetOutputDir sets the folder for path's MKVs; Configure leaves it alone.
func (l *List) SetOutputDir(path, dir string) {
	l.emitMu.Lock()
	defer l.emitMu.Unlock()
	l.mu.Lock()
	it, ok := l.byPath[path]
	if !ok {
		l.mu.Unlock()
		return
	}
	it.summary.OutputDir, it.customDir = dir, true
	s := l.snapshotLocked()
	l.mu.Unlock()
	l.changed(s)
}

// Configure applies a new template and default output folder (after the
// config file changed), re-rendering every open disc's default names.
func (l *List) Configure(cfg Config) {
	l.emitMu.Lock()
	defer l.emitMu.Unlock()
	l.mu.Lock()
	l.cfg = cfg
	for _, it := range l.byPath {
		switch {
		case it.disc != nil:
			it.fill(cfg)
		case !it.customDir:
			it.summary.OutputDir = cfg.OutputDir
		}
	}
	s := l.snapshotLocked()
	l.mu.Unlock()
	l.changed(s)
}

// Summaries returns every disc's summary in list order.
func (l *List) Summaries() []Summary {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshotLocked()
}

// Summary returns the summary of the disc at path.
func (l *List) Summary(path string) (Summary, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	it, ok := l.byPath[path]
	if !ok {
		return Summary{}, false
	}
	return it.summary, true
}

// Close closes every open disc.
func (l *List) Close() {
	l.mu.Lock()
	items := l.byPath
	l.byPath, l.order = map[string]*item{}, nil
	l.mu.Unlock()
	for _, it := range items {
		if it.disc != nil {
			l.closeDisc(it.disc)
		}
	}
}

func (l *List) snapshotLocked() []Summary {
	out := make([]Summary, 0, len(l.order))
	for _, p := range l.order {
		out = append(out, l.byPath[p].summary)
	}
	return out
}
