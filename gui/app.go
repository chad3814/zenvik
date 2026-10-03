package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/gui/internal/discs"
	"github.com/chad3814/zenvik/gui/internal/errs"
	"github.com/chad3814/zenvik/gui/internal/queue"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const msgNotStarted = "Zenvik didn't start"

var errNotStarted = errors.New(msgNotStarted)

// Deps are App's outside dependencies, replaced in tests.
type Deps struct {
	StatePath    string
	LoadSettings func() (config.Settings, error)
	FindMkvmerge func(ctx context.Context, path string) error
	Ripper       func(open discs.Opener, mkvmergePath func() string) queue.Ripper
	Home         string
	GOOS         string
}

// Banner is a message across the top of the window.
type Banner struct {
	ID      string `json:"id"`
	Message string `json:"message"`
	Action  string `json:"action,omitempty"` // "recheck": show a Recheck button
}

// App is the object bound to the frontend. Its exported methods are the
// frontend's API; data flows back as events (see emit calls).
type App struct {
	deps  Deps
	ctx   context.Context
	shell Shell
	discs *discs.List
	queue *queue.Queue

	// ready is closed when init finishes, successfully or not. Wails runs
	// OnStartup in its own goroutine while the page loads, so bound methods
	// may be called first; they wait here.
	ready chan struct{}

	mu       sync.Mutex
	settings config.Settings
	banners  []Banner
}

// NewApp returns an App; Wails calls startup once the window exists.
func NewApp(deps Deps) *App { return &App{deps: deps, ready: make(chan struct{})} }

// wait blocks until init has finished and reports whether it succeeded;
// bound methods do nothing when it didn't.
func (a *App) wait() bool {
	<-a.ready
	return a.queue != nil
}

func (a *App) startup(ctx context.Context) {
	if err := a.init(ctx, wailsShell{ctx}); err != nil {
		runtime.MessageDialog(ctx, runtime.MessageDialogOptions{Type: runtime.ErrorDialog, Title: "Zenvik can't start", Message: err.Error()})
		runtime.Quit(ctx)
	}
}

func (a *App) init(ctx context.Context, sh Shell) error {
	defer close(a.ready)
	a.ctx, a.shell = ctx, sh
	a.reloadSettings()
	// The discs and queue callbacks run with those packages' emit locks held,
	// so they must never call back into List or Queue methods: they may only
	// emit events and set or clear banners (App's own mutex).
	a.discs = discs.New(ctx, a.open, a.discConfig(), func(s []discs.Summary) { a.shell.Emit("discs:changed", s) })
	q, err := queue.Open(a.deps.StatePath, a.deps.Ripper(a.open, a.mkvmergePath), queue.Hooks{
		Changed: func(s queue.Snapshot) {
			a.shell.Emit("queue:changed", s)
			if s.SaveError != "" {
				a.setBanner(Banner{ID: "save", Message: s.SaveError})
			} else {
				a.clearBanner("save")
			}
		},
		Progress: func(p queue.Progress) { a.shell.Emit("queue:progress", p) },
		// A rip found mkvmerge missing: the queue has already stopped itself
		// (not ready), so only the banner is left to show.
		MkvmergeMissing: func(err error) { a.setBanner(mkvmergeBanner(err)) },
	})
	if err != nil {
		return err
	}
	a.queue = q
	if w := q.Warning(); w != "" {
		a.setBanner(Banner{ID: "queue", Message: w})
	}
	a.recheckMkvmerge()
	q.Start(ctx)
	return nil
}

// open opens a disc with the config's min_duration, as the CLI does; every
// title is still listed.
func (a *App) open(ctx context.Context, path string) (*zenvik.Disc, error) {
	a.mu.Lock()
	minDur := a.settings.MinDuration
	a.mu.Unlock()
	return zenvik.Open(ctx, path, zenvik.WithMinDuration(minDur))
}

func (a *App) mkvmergePath() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings.MkvmergePath
}

func (a *App) reloadSettings() {
	s, err := a.deps.LoadSettings()
	if err != nil {
		s, _ = config.Resolve(&config.File{}, config.Flags{})
		a.setBanner(Banner{ID: "config", Message: errs.Message(err) + " Using the built-in defaults."})
	} else {
		a.clearBanner("config")
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
}

func (a *App) discConfig() discs.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return discs.Config{
		Template:  a.settings.Template,
		OutputDir: discs.DefaultOutputDir(a.settings.OutputDir, a.deps.GOOS, a.deps.Home, dirExists),
	}
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func (a *App) setBanner(b Banner) {
	a.mu.Lock()
	i := slices.IndexFunc(a.banners, func(x Banner) bool { return x.ID == b.ID })
	if i >= 0 {
		a.banners[i] = b
	} else {
		a.banners = append(a.banners, b)
	}
	a.mu.Unlock()
	a.emitBanners()
}

func (a *App) clearBanner(id string) {
	a.mu.Lock()
	n := len(a.banners)
	a.banners = slices.DeleteFunc(a.banners, func(x Banner) bool { return x.ID == id })
	changed := len(a.banners) != n
	a.mu.Unlock()
	if changed {
		a.emitBanners()
	}
}

func (a *App) emitBanners() {
	a.mu.Lock()
	bs := append([]Banner{}, a.banners...)
	sh := a.shell
	a.mu.Unlock()
	if sh != nil {
		sh.Emit("banners:changed", bs)
	}
}

// Ready re-sends every snapshot; the frontend calls it once it is listening.
func (a *App) Ready() {
	if !a.wait() {
		return
	}
	a.discs.Emit()
	a.queue.Emit()
	a.emitBanners()
}

// Version is the build's version string.
func (a *App) Version() string { return version }

// AddPaths adds discs (dropped or picked) and selects the last one.
func (a *App) AddPaths(paths []string) {
	if !a.wait() {
		return
	}
	if abs := a.discs.Add(paths); len(abs) > 0 {
		a.shell.Emit("discs:select", abs[len(abs)-1])
	}
}

// PickISOs lets the user choose disc images to add.
func (a *App) PickISOs() error {
	if !a.wait() {
		return errNotStarted
	}
	paths, err := a.shell.PickFiles("Add disc images", "Disc images (*.iso)", "*.iso")
	if err != nil {
		return err
	}
	a.AddPaths(paths)
	return nil
}

// PickFolder lets the user choose a disc folder to add.
func (a *App) PickFolder() error {
	if !a.wait() {
		return errNotStarted
	}
	dir, err := a.shell.PickDir("Add a disc folder")
	if err != nil || dir == "" {
		return err
	}
	a.AddPaths([]string{dir})
	return nil
}

// RemoveDisc closes a disc; its queue entries stay.
func (a *App) RemoveDisc(path string) {
	if a.wait() {
		a.discs.Remove(path)
	}
}

// PickOutputDir lets the user choose where a disc's MKVs go.
func (a *App) PickOutputDir(path string) error {
	if !a.wait() {
		return errNotStarted
	}
	dir, err := a.shell.PickDir("Choose the output folder")
	if err != nil || dir == "" {
		return err
	}
	a.discs.SetOutputDir(path, dir)
	return nil
}

// Enqueue queues titles of the disc at path, each with its name from the
// titles list. It returns nil on success, or a message per title ("" where it
// was fine) and queues nothing.
func (a *App) Enqueue(path string, titleIDs []string, names []string) []string {
	if !a.wait() {
		return slices.Repeat([]string{msgNotStarted}, len(titleIDs))
	}
	msgs := make([]string, len(titleIDs))
	s, ok := a.discs.Summary(path)
	if !ok || s.State != "ready" || len(titleIDs) != len(names) {
		for i := range msgs {
			msgs[i] = "the disc is no longer open"
		}
		return msgs
	}
	items := make([]queue.NewEntry, len(titleIDs))
	bad := false
	for i, id := range titleIDs {
		out, msg := outputPath(s.OutputDir, names[i])
		if msg == "" && !rippable(s, id) {
			msg = "this title can't be ripped"
		}
		msgs[i], items[i] = msg, queue.NewEntry{DiscPath: path, TitleID: id, OutputPath: out}
		bad = bad || msg != ""
	}
	if bad {
		return msgs
	}
	return a.queue.Add(items)
}

func rippable(s discs.Summary, id string) bool {
	for _, t := range s.Titles {
		if t.ID == id {
			return t.Rippable
		}
	}
	return false
}

// Rename gives a waiting entry a new file name in the same folder.
func (a *App) Rename(id, name string) string {
	if !a.wait() {
		return msgNotStarted
	}
	if msg := fileNameError(name); msg != "" {
		return msg
	}
	for _, e := range a.queue.Snapshot().Entries {
		if e.ID == id {
			return a.queue.Rename(id, filepath.Join(filepath.Dir(e.OutputPath), withMKV(strings.TrimSpace(name))))
		}
	}
	return "no such queue entry"
}

// Move puts a queue entry at index.
func (a *App) Move(id string, index int) {
	if a.wait() {
		a.queue.Move(id, index)
	}
}

// Remove drops a queue entry that isn't ripping.
func (a *App) Remove(id string) {
	if a.wait() {
		a.queue.Remove(id)
	}
}

// Cancel stops the running rip.
func (a *App) Cancel(id string) {
	if a.wait() {
		a.queue.Cancel(id)
	}
}

// Retry queues a failed or canceled entry again.
func (a *App) Retry(id string) {
	if a.wait() {
		a.queue.Retry(id)
	}
}

// ClearFinished drops done, failed and canceled entries.
func (a *App) ClearFinished() {
	if a.wait() {
		a.queue.ClearFinished()
	}
}

// SetPaused pauses or resumes the queue.
func (a *App) SetPaused(paused bool) {
	if a.wait() {
		a.queue.SetPaused(paused)
	}
}

// RecheckMkvmerge re-reads the config (mkvmerge_path may have changed) and
// looks for mkvmerge again; the queue only runs once found.
func (a *App) RecheckMkvmerge() {
	a.ReloadConfig()
}

func (a *App) recheckMkvmerge() {
	err := a.deps.FindMkvmerge(a.ctx, a.mkvmergePath())
	if err == nil {
		a.clearBanner("mkvmerge")
	} else {
		a.setBanner(mkvmergeBanner(err))
	}
	a.queue.SetReady(err == nil)
}

// mkvmergeBanner says what's wrong with mkvmerge and offers a Recheck.
func mkvmergeBanner(err error) Banner {
	b := Banner{ID: "mkvmerge", Action: "recheck", Message: errs.Message(err)}
	switch {
	case errors.Is(err, zenvik.ErrMkvmergeNotFound):
		b.Message = "mkvmerge wasn't found. Install MKVToolNix (https://mkvtoolnix.download) or set mkvmerge_path in zenvik's config, then press Recheck."
	case errors.Is(err, zenvik.ErrMkvmergeTooOld):
		b.Message += " Update MKVToolNix, then press Recheck."
	}
	return b
}

// ReloadConfig re-reads zenvik's config (the frontend calls it when the
// window regains focus or becomes visible), re-renders default names and
// folders, and checks mkvmerge again, since mkvmerge_path may have changed or
// MKVToolNix may have been installed or removed meanwhile.
func (a *App) ReloadConfig() {
	if !a.wait() {
		return
	}
	a.reloadSettings()
	a.discs.Configure(a.discConfig())
	a.recheckMkvmerge()
}

// Reveal shows a finished entry's file in the file manager; it returns "" or
// what went wrong.
func (a *App) Reveal(id string) string {
	if !a.wait() {
		return msgNotStarted
	}
	for _, e := range a.queue.Snapshot().Entries {
		if e.ID == id {
			if err := reveal(a.deps.GOOS, e.OutputPath); err != nil {
				return err.Error()
			}
			return ""
		}
	}
	return "no such queue entry"
}

// beforeClose asks before quitting during a rip; a confirmed quit cancels the
// rip and returns its entry to waiting.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.queue == nil || !a.queue.Running() {
		return false
	}
	ok, err := a.shell.Confirm("Quit Zenvik?", "A rip is running. Cancel it and quit? It starts again the next time you open Zenvik.", "Cancel rip and quit", "Keep ripping")
	if err != nil || !ok {
		return true
	}
	a.queue.Stop(30 * time.Second)
	return false
}

func (a *App) shutdown(ctx context.Context) {
	if a.queue != nil {
		a.queue.Stop(30 * time.Second)
	}
	if a.discs != nil {
		a.discs.Close()
	}
}
