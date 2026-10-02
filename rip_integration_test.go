//go:build integration

package zenvik_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func realMovie(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "REAL_MOVIE")
	if err := testdisc.RealMovie(context.Background(), dir, 7); err != nil {
		t.Fatal(err)
	}
	return dir
}

func identify(t *testing.T, path string) *mux.Identification {
	t.Helper()
	mk, err := mux.Find(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := mk.Identify(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertRipped(t *testing.T, out string) {
	t.Helper()
	if _, err := os.Stat(out + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("partial file left behind: %v", err)
	}
	id := identify(t, out)
	if len(id.Tracks) != 2 || id.Tracks[0].Type != "video" || id.Tracks[1].Type != "audio" {
		t.Fatalf("tracks = %+v", id.Tracks)
	}
	if id.Tracks[1].Language != "jpn" {
		t.Errorf("audio language = %q, want jpn (from the disc, not the stream's eng)", id.Tracks[1].Language)
	}
	if id.Chapters != 3 {
		t.Errorf("chapters = %d, want 3", id.Chapters)
	}
}

func TestRipDirectory(t *testing.T) {
	d := openDisc(t, realMovie(t))
	title := mustTitle(t, d, "00800")
	out := filepath.Join(t.TempDir(), "out dir", `Réal "Movie".mkv`)
	var events []zenvik.Progress
	res, err := d.Rip(context.Background(), title, zenvik.RipOptions{
		OutputPath: out,
		OnProgress: func(p zenvik.Progress) { events = append(events, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OutputPath != out || res.Duration != title.Duration || len(res.Command) == 0 {
		t.Errorf("result = %+v", res)
	}
	assertRipped(t, out)
	if len(events) == 0 || events[len(events)-1] != (zenvik.Progress{Phase: zenvik.PhaseFinalizing, Fraction: 1, BytesDone: title.Size, BytesTotal: title.Size}) {
		t.Errorf("last event = %+v", events[len(events)-1])
	}
	last := zenvik.Phase(0)
	for _, e := range events {
		if e.Phase < last || e.Fraction < 0 || e.Fraction > 1 || e.BytesTotal != title.Size {
			t.Fatalf("bad progress sequence: %+v", events)
		}
		last = e.Phase
	}
	if !slices.ContainsFunc(events, func(e zenvik.Progress) bool { return e.Phase == zenvik.PhaseMuxing }) {
		t.Error("no muxing progress reported")
	}
}

func TestRipFlattened(t *testing.T) {
	root := realMovie(t)
	if err := testdisc.Flatten(root); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, root)
	if d.Kind != zenvik.FlatBDMVDir {
		t.Fatalf("Kind = %v", d.Kind)
	}
	title := mustTitle(t, d, "00800")
	out := filepath.Join(t.TempDir(), "flat.mkv")
	res, err := d.Rip(context.Background(), title, zenvik.RipOptions{OutputPath: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %q", res.Warnings)
	}
	assertRipped(t, out)
	i := slices.IndexFunc(res.Command, func(a string) bool { return strings.HasSuffix(a, "00800.mpls") })
	if i < 0 || !strings.Contains(res.Command[i], "zenvik-bdmv-") {
		t.Fatalf("command reads no playlist from a temporary tree: %q", res.Command)
	}
	if _, err := os.Stat(res.Command[i]); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary tree input %s still exists: %v", res.Command[i], err)
	}
}

func TestRipOverwrite(t *testing.T) {
	d := openDisc(t, realMovie(t))
	title := mustTitle(t, d, "00800")
	out := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Rip(context.Background(), title, zenvik.RipOptions{OutputPath: out, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	assertRipped(t, out)
}

func TestRipDryRun(t *testing.T) {
	d := openDisc(t, realMovie(t))
	out := filepath.Join(t.TempDir(), "movie.mkv")
	res, err := d.Rip(context.Background(), mustTitle(t, d, "00800"), zenvik.RipOptions{OutputPath: out, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(res.Command, "--language")
	if i < 0 || res.Command[i+1] != "1:jpn" || res.Command[len(res.Command)-1] != filepath.Join(d.Path, "BDMV", "PLAYLIST", "00800.mpls") {
		t.Errorf("command = %q", res.Command)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("dry run created the output")
	}
}

func TestRipCanceled(t *testing.T) {
	d := openDisc(t, realMovie(t))
	out := filepath.Join(t.TempDir(), "movie.mkv")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := d.Rip(ctx, mustTitle(t, d, "00800"), zenvik.RipOptions{
		OutputPath: out,
		OnProgress: func(p zenvik.Progress) {
			if p.Phase == zenvik.PhaseMuxing {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	for _, p := range []string{out, out + ".partial"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left behind: %v", p, err)
		}
	}
}
