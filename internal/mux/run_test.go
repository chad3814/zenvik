package mux

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type event struct {
	Phase    Phase
	Fraction float64
}

func job(t *testing.T, out string) Job {
	t.Helper()
	return Job{Input: "/disc/BDMV/PLAYLIST/00800.mpls", Output: out, Tracks: []Track{{ID: 0, Type: "video", Default: true}}}
}

func TestMuxSuccess(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "movie.mkv.partial")
	argsFile := filepath.Join(dir, "args.json")
	var events []event
	res, err := fake("mux=0", "ZENVIK_FAKE_ARGS_OUT="+argsFile).Mux(context.Background(), job(t, out),
		func(p Phase, f float64) { events = append(events, event{p, f}) })
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %q", res.Warnings)
	}
	want := []event{{PhaseScanning, 0}, {PhaseScanning, 0.5}, {PhaseMuxing, 0}, {PhaseMuxing, 0.25}, {PhaseMuxing, 1}}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output missing: %v", err)
	}
	var got []string
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if wantArgs := append([]string{"--gui-mode"}, Args(job(t, out))...); !reflect.DeepEqual(got, wantArgs) || !reflect.DeepEqual(res.Args, wantArgs) {
		t.Errorf("mkvmerge received %q (Result.Args %q), want %q", got, res.Args, wantArgs)
	}
}

func TestMuxWarnings(t *testing.T) {
	out := filepath.Join(t.TempDir(), "movie.mkv.partial")
	res, err := fake("mux=1").Mux(context.Background(), job(t, out), nil)
	if err != nil {
		t.Fatalf("exit 1 should succeed: %v", err)
	}
	if want := []string{"the playlist has a gap", "A warning:with escape"}; !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("warnings = %q, want %q", res.Warnings, want)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output should be kept on warnings: %v", err)
	}
}

func TestMuxFailure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "movie.mkv.partial")
	_, err := fake("mux=2").Mux(context.Background(), job(t, out), nil)
	if !errors.Is(err, ErrFailed) || !strings.Contains(err.Error(), "cannot open the playlist") || !strings.Contains(err.Error(), "exit status 2") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("output should be removed on failure: %v", err)
	}
}

func TestMuxCanceled(t *testing.T) {
	out := filepath.Join(t.TempDir(), "movie.mkv.partial")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	_, err := fake("hang").Mux(ctx, job(t, out), func(p Phase, f float64) {
		if p == PhaseMuxing && f > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("cancellation took %v", time.Since(start))
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("output should be removed on cancel: %v", err)
	}
}

func TestMuxOptionsFilePreservesPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out dir", "ü")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, `Réal "Movie" 'x'.mkv.partial`)
	argsFile := filepath.Join(t.TempDir(), "args.json")
	if _, err := fake("mux=0", "ZENVIK_FAKE_ARGS_OUT="+argsFile).Mux(context.Background(), job(t, out), nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 || got[1] != "-o" || got[2] != out {
		t.Errorf("args = %q, want -o %q", got, out)
	}
}

func TestMuxRemovesOptionsFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	out := filepath.Join(t.TempDir(), "movie.mkv.partial")
	if _, err := fake("mux=0").Mux(context.Background(), job(t, out), nil); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
		t.Errorf("temp dir not cleaned: %v", ents)
	}
}

func TestUnescapeGUI(t *testing.T) {
	if got := unescapeGUI(`a\sb\2c\2\cd\he\bf\Bg\\s`); got != `a b"c":d#e[f]g\s` {
		t.Errorf("unescapeGUI = %q", got)
	}
}
