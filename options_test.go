package zenvik_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestWithMinDuration(t *testing.T) {
	root := writeDisc(t, testdisc.SampleMovie())
	if menu := mustTitle(t, openDisc(t, root), "00099"); !menu.Rank.Filtered {
		t.Fatal("default: the 20 s menu title should be filtered")
	}
	d, err := zenvik.Open(context.Background(), root, zenvik.WithMinDuration(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if menu := mustTitle(t, d, "00099"); menu.Rank.Filtered {
		t.Errorf("min 10s: 00099 rank = %+v", menu.Rank)
	}
}

func TestRipMkvmergePath(t *testing.T) {
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	missing := filepath.Join(t.TempDir(), "no-such-mkvmerge")
	_, err := d.Rip(context.Background(), d.Main(), zenvik.RipOptions{
		OutputPath:   filepath.Join(t.TempDir(), "x.mkv"),
		MkvmergePath: missing,
	})
	if !errors.Is(err, zenvik.ErrMkvmergeNotFound) {
		t.Errorf("err = %v, want ErrMkvmergeNotFound for %s", err, missing)
	}
}
