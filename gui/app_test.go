package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/gui/internal/discs"
	"github.com/chad3814/zenvik/gui/internal/queue"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/testdisc"
)

type fakeShell struct {
	mu      sync.Mutex
	events  map[string][]any
	confirm bool
	dir     string
	files   []string
}

func (s *fakeShell) Emit(event string, data any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.events == nil {
		s.events = map[string][]any{}
	}
	s.events[event] = append(s.events[event], data)
}

func (s *fakeShell) last(event string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := s.events[event]
	if len(ev) == 0 {
		return nil
	}
	return ev[len(ev)-1]
}

func (s *fakeShell) PickFiles(string, string, string) ([]string, error) { return s.files, nil }
func (s *fakeShell) PickDir(string) (string, error)                     { return s.dir, nil }
func (s *fakeShell) Confirm(string, string, string, string) (bool, error) {
	return s.confirm, nil
}

// blockRipper blocks until ctx is done or release is closed.
type blockRipper struct{ release chan struct{} }

func (r blockRipper) Rip(ctx context.Context, e queue.Entry, _ func(zenvik.Progress)) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.release:
		return os.WriteFile(e.OutputPath, []byte("mkv"), 0o644)
	}
}

type testOpts struct {
	settingsErr error
	mkvmerge    error
	template    string
	ripper      queue.Ripper // default: blockRipper
}

func newTestApp(t *testing.T, o testOpts) (*App, *fakeShell, string) {
	t.Helper()
	home := t.TempDir()
	tmpl := o.template
	if tmpl == "" {
		tmpl = config.DefaultTemplate
	}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	deps := Deps{
		StatePath: filepath.Join(t.TempDir(), "zenvik", "gui-queue.json"),
		LoadSettings: func() (config.Settings, error) {
			if o.settingsErr != nil {
				return config.Settings{}, o.settingsErr
			}
			return config.Settings{OutputDir: ".", Template: tmpl, MinDuration: config.DefaultMinDuration}, nil
		},
		FindMkvmerge: func(context.Context, string) error { return o.mkvmerge },
		Ripper: func(discs.Opener, func() string) queue.Ripper {
			if o.ripper != nil {
				return o.ripper
			}
			return blockRipper{release}
		},
		Home: home,
		GOOS:         "linux",
	}
	sh := &fakeShell{confirm: true}
	a := NewApp(deps)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := a.init(ctx, sh); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.shutdown(ctx) })
	return a, sh, home
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

func banners(sh *fakeShell) map[string]Banner {
	m := map[string]Banner{}
	if bs, ok := sh.last("banners:changed").([]Banner); ok {
		for _, b := range bs {
			m[b.ID] = b
		}
	}
	return m
}

func sampleDisc(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func readyDisc(t *testing.T, a *App, path string) discs.Summary {
	t.Helper()
	var s discs.Summary
	waitFor(t, "disc ready", func() bool {
		var ok bool
		s, ok = a.discs.Summary(path)
		return ok && s.State == "ready"
	})
	return s
}

func TestStartupBanners(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{
		settingsErr: fmt.Errorf("%w: unknown key bogus", config.ErrInvalid),
		mkvmerge:    zenvik.ErrMkvmergeNotFound,
	})
	a.Ready()
	b := banners(sh)
	if b["config"].Message == "" || b["mkvmerge"].Action != "recheck" {
		t.Errorf("banners = %+v", b)
	}
	if snap, ok := sh.last("queue:changed").(queue.Snapshot); !ok || snap.Ready {
		t.Errorf("queue snapshot = %+v", snap)
	}
	if a.settings.Template != config.DefaultTemplate {
		t.Errorf("fallback template = %q", a.settings.Template)
	}
}

func TestRecheckClearsBanner(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{mkvmerge: zenvik.ErrMkvmergeTooOld})
	if _, ok := banners(sh)["mkvmerge"]; !ok {
		t.Fatal("no mkvmerge banner")
	}
	a.deps.FindMkvmerge = func(context.Context, string) error { return nil }
	a.RecheckMkvmerge()
	if _, ok := banners(sh)["mkvmerge"]; ok || !a.queue.Snapshot().Ready {
		t.Errorf("banners = %+v, ready = %v", banners(sh), a.queue.Snapshot().Ready)
	}
}

func TestAddPathsWithUnusualNames(t *testing.T) {
	a, sh, home := newTestApp(t, testOpts{})
	dir := sampleDisc(t, "Swiss Family Robinson (1960) (USA) é")
	a.AddPaths([]string{dir})
	if got := sh.last("discs:select"); got != dir {
		t.Errorf("select = %v, want %q", got, dir)
	}
	s := readyDisc(t, a, dir)
	if s.OutputDir != home || s.Titles[0].DefaultName != "Sample Movie.mkv" {
		t.Errorf("summary = %+v", s)
	}
	a.AddPaths([]string{dir})
	if n := len(a.discs.Summaries()); n != 1 {
		t.Errorf("%d discs after re-adding", n)
	}
}

func TestEnqueueValidatesEveryName(t *testing.T) {
	a, _, home := newTestApp(t, testOpts{})
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	s := readyDisc(t, a, dir)
	ids := []string{s.Titles[0].ID, s.Titles[1].ID, s.Titles[2].ID}
	msgs := a.Enqueue(dir, ids, []string{"", "../x", "Fine"})
	if len(msgs) != 3 || msgs[0] != "the name is empty" || msgs[1] != "the name must stay inside the output folder" || msgs[2] != "" {
		t.Fatalf("msgs = %q", msgs)
	}
	if n := len(a.queue.Snapshot().Entries); n != 0 {
		t.Fatalf("%d entries after a rejected batch", n)
	}
	if msgs := a.Enqueue(dir, ids[:2], []string{"Movie", "Extras/Trailer.mkv"}); msgs != nil {
		t.Fatalf("msgs = %q", msgs)
	}
	got := a.queue.Snapshot().Entries
	if got[0].OutputPath != filepath.Join(home, "Movie.mkv") || got[1].OutputPath != filepath.Join(home, "Extras", "Trailer.mkv") {
		t.Errorf("outputs = %q, %q", got[0].OutputPath, got[1].OutputPath)
	}
	if msgs := a.Enqueue("/not/open", []string{"01"}, []string{"x"}); len(msgs) != 1 || msgs[0] != "the disc is no longer open" {
		t.Errorf("unknown disc = %q", msgs)
	}
}

func TestEnqueueRejectsUnrippableTitle(t *testing.T) {
	a, _, _ := newTestApp(t, testOpts{})
	root := filepath.Join(t.TempDir(), "SAMPLE_DVD")
	if err := testdisc.SampleDVD().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	a.AddPaths([]string{root})
	readyDisc(t, a, root)
	if msgs := a.Enqueue(root, []string{"08"}, []string{"Angles"}); len(msgs) != 1 || msgs[0] != "this title can't be ripped" {
		t.Errorf("msgs = %q", msgs)
	}
}

func TestRenameKeepsFolder(t *testing.T) {
	a, _, home := newTestApp(t, testOpts{})
	a.queue.SetPaused(true)
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	s := readyDisc(t, a, dir)
	a.Enqueue(dir, []string{s.Titles[0].ID}, []string{"Show/Old.mkv"})
	id := a.queue.Snapshot().Entries[0].ID
	if msg := a.Rename(id, "New name"); msg != "" {
		t.Fatalf("rename = %q", msg)
	}
	if got := a.queue.Snapshot().Entries[0].OutputPath; got != filepath.Join(home, "Show", "New name.mkv") {
		t.Errorf("output = %q", got)
	}
	if msg := a.Rename(id, "a/b"); msg != "a file name can't contain a folder" {
		t.Errorf("rename = %q", msg)
	}
}

func TestBeforeCloseDuringRip(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{})
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	s := readyDisc(t, a, dir)
	a.Enqueue(dir, []string{s.Titles[0].ID}, []string{"Movie"})
	waitFor(t, "ripping", a.queue.Running)

	sh.confirm = false
	if prevent := a.beforeClose(context.Background()); !prevent || !a.queue.Running() {
		t.Fatalf("declined quit: prevent=%v running=%v", prevent, a.queue.Running())
	}
	sh.confirm = true
	if prevent := a.beforeClose(context.Background()); prevent {
		t.Fatal("confirmed quit was prevented")
	}
	if e := a.queue.Snapshot().Entries[0]; e.State != queue.Waiting {
		t.Errorf("entry after quit = %+v", e)
	}
}

func TestReloadConfigUpdatesDefaultNames(t *testing.T) {
	a, _, _ := newTestApp(t, testOpts{})
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	readyDisc(t, a, dir)
	a.deps.LoadSettings = func() (config.Settings, error) {
		return config.Settings{OutputDir: ".", Template: "{name} - {playlist}.mkv"}, nil
	}
	a.ReloadConfig()
	s, _ := a.discs.Summary(dir)
	if s.Titles[0].DefaultName != "Sample Movie - 00800.mkv" {
		t.Errorf("name = %q", s.Titles[0].DefaultName)
	}
}

func TestBoundMethodsWaitForInit(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	a := NewApp(Deps{
		StatePath: filepath.Join(t.TempDir(), "zenvik", "gui-queue.json"),
		LoadSettings: func() (config.Settings, error) {
			return config.Settings{OutputDir: ".", Template: config.DefaultTemplate}, nil
		},
		FindMkvmerge: func(context.Context, string) error { return nil },
		Ripper:       func(discs.Opener, func() string) queue.Ripper { return blockRipper{release} },
		Home:         t.TempDir(),
		GOOS:         "linux",
	})
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	done := make(chan string, 2)
	go func() { a.Ready(); done <- "ready" }()
	go func() { a.AddPaths([]string{dir}); done <- "add" }()
	select {
	case m := <-done:
		t.Fatalf("%s returned before init", m)
	case <-time.After(100 * time.Millisecond):
	}
	sh := &fakeShell{confirm: true}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := a.init(ctx, sh); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.shutdown(ctx) })
	for range 2 {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("bound methods still blocked after init")
		}
	}
	if sh.last("discs:changed") == nil || sh.last("queue:changed") == nil {
		t.Errorf("no snapshots emitted: discs=%v queue=%v", sh.last("discs:changed"), sh.last("queue:changed"))
	}
	if sh.last("discs:select") != dir {
		t.Errorf("select = %v", sh.last("discs:select"))
	}
}

func TestFailedInitReleasesCallers(t *testing.T) {
	a := NewApp(Deps{
		StatePath: t.TempDir(), // a directory, so the queue can't open
		LoadSettings: func() (config.Settings, error) {
			return config.Settings{OutputDir: ".", Template: config.DefaultTemplate}, nil
		},
		FindMkvmerge: func(context.Context, string) error { return nil },
		Ripper:       func(discs.Opener, func() string) queue.Ripper { return nil },
	})
	if err := a.init(context.Background(), &fakeShell{}); err == nil {
		t.Skip("queue.Open accepted a directory as its state path")
	}
	done := make(chan struct{})
	go func() {
		a.Ready()
		a.AddPaths([]string{"/x"})
		a.Enqueue("/x", nil, nil)
		a.SetPaused(true)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bound methods block after a failed init")
	}
}

// mkvmergeAt fakes FindMkvmerge: only good finds mkvmerge.
func mkvmergeAt(good string) func(context.Context, string) error {
	return func(_ context.Context, path string) error {
		if path == good {
			return nil
		}
		return fmt.Errorf("%w: %s", zenvik.ErrMkvmergeNotFound, path)
	}
}

func settingsWithMkvmerge(path string) func() (config.Settings, error) {
	return func() (config.Settings, error) {
		return config.Settings{OutputDir: ".", Template: config.DefaultTemplate, MkvmergePath: path}, nil
	}
}

func TestReloadConfigRechecksMkvmerge(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{})
	a.deps.FindMkvmerge = mkvmergeAt("")
	if _, ok := banners(sh)["mkvmerge"]; ok || !a.queue.Snapshot().Ready {
		t.Fatalf("mkvmerge not fine at start: %+v", banners(sh))
	}
	gone := filepath.Join(t.TempDir(), "nonexistent")
	a.deps.LoadSettings = settingsWithMkvmerge(gone)
	a.ReloadConfig()
	if b := banners(sh)["mkvmerge"]; b.Action != "recheck" || a.queue.Snapshot().Ready {
		t.Errorf("after mkvmerge_path = %s: banner %+v, ready %v", gone, b, a.queue.Snapshot().Ready)
	}
	a.deps.LoadSettings = settingsWithMkvmerge("")
	a.ReloadConfig()
	if _, ok := banners(sh)["mkvmerge"]; ok || !a.queue.Snapshot().Ready {
		t.Errorf("after the path was fixed: banners %+v, ready %v", banners(sh), a.queue.Snapshot().Ready)
	}
}

func TestRecheckRereadsConfig(t *testing.T) {
	good := filepath.Join(t.TempDir(), "mkvmerge")
	a, sh, _ := newTestApp(t, testOpts{mkvmerge: zenvik.ErrMkvmergeNotFound})
	if _, ok := banners(sh)["mkvmerge"]; !ok {
		t.Fatal("no mkvmerge banner")
	}
	a.deps.FindMkvmerge = mkvmergeAt(good)
	a.deps.LoadSettings = settingsWithMkvmerge(good) // edited, but no ReloadConfig yet
	a.RecheckMkvmerge()
	if _, ok := banners(sh)["mkvmerge"]; ok || !a.queue.Snapshot().Ready {
		t.Errorf("Recheck tested a stale path: banners %+v, ready %v", banners(sh), a.queue.Snapshot().Ready)
	}
}

// missingRipper fails every rip as if mkvmerge had been uninstalled.
type missingRipper struct{}

func (missingRipper) Rip(context.Context, queue.Entry, func(zenvik.Progress)) error {
	return fmt.Errorf("muxing: %w: /usr/local/bin/mkvmerge", zenvik.ErrMkvmergeNotFound)
}

func TestRipWithoutMkvmergeRaisesBanner(t *testing.T) {
	a, sh, _ := newTestApp(t, testOpts{ripper: missingRipper{}})
	dir := sampleDisc(t, "SAMPLE_MOVIE")
	a.AddPaths([]string{dir})
	s := readyDisc(t, a, dir)
	if msgs := a.Enqueue(dir, []string{s.Titles[0].ID, s.Titles[1].ID}, []string{"One", "Two"}); msgs != nil {
		t.Fatal(msgs)
	}
	waitFor(t, "mkvmerge banner", func() bool { return banners(sh)["mkvmerge"].Action == "recheck" })
	snap := a.queue.Snapshot()
	if snap.Ready || snap.Entries[0].State != queue.Waiting || snap.Entries[1].State != queue.Waiting {
		t.Errorf("ready %v, entries %+v; want both waiting and the queue stopped", snap.Ready, snap.Entries)
	}
}
