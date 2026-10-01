//go:build integration && darwin

package zenvik_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func realISO(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	disc, err := testdisc.RealMovieDisc(context.Background(), t.TempDir(), 7)
	if err != nil {
		t.Fatal(err)
	}
	img, err := disc.ISO(udfimage.Options{Revision: 0x0250, Label: "REAL_MOVIE"})
	if err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(t.TempDir(), "real movie.iso")
	if err := os.WriteFile(iso, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return iso
}

func assertDetached(t *testing.T, iso string) {
	t.Helper()
	out, err := exec.Command("hdiutil", "info").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), iso) {
		t.Errorf("image still attached:\n%s", out)
	}
}

func TestRipISO(t *testing.T) {
	iso := realISO(t)
	d := openDisc(t, iso)
	out := filepath.Join(t.TempDir(), "movie.mkv")
	var phases []zenvik.Phase
	if _, err := d.Rip(context.Background(), mustTitle(t, d, "00800"), zenvik.RipOptions{
		OutputPath: out,
		OnProgress: func(p zenvik.Progress) { phases = append(phases, p.Phase) },
	}); err != nil {
		t.Fatal(err)
	}
	assertRipped(t, out)
	if !slices.Contains(phases, zenvik.PhaseMounting) {
		t.Errorf("phases = %v, want mounting", phases)
	}
	assertDetached(t, iso)
}

func TestRipISOCanceled(t *testing.T) {
	iso := realISO(t)
	d := openDisc(t, iso)
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
	if _, err := os.Stat(out + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Error("partial file left behind")
	}
	assertDetached(t, iso)
}
