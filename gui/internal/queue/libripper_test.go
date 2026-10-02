package queue

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func libOpen(ctx context.Context, path string) (*zenvik.Disc, error) { return zenvik.Open(ctx, path) }

func TestLibRipperMissingDisc(t *testing.T) {
	r := LibRipper{Open: libOpen, MkvmergePath: func() string { return "" }}
	err := r.Rip(context.Background(), Entry{DiscPath: filepath.Join(t.TempDir(), "gone.iso"), TitleID: "00800", OutputPath: "/x.mkv"}, nil)
	if err == nil {
		t.Fatal("want an error for a missing disc")
	}
}

func TestLibRipperUnknownTitle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "SAMPLE_MOVIE")
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	r := LibRipper{Open: libOpen, MkvmergePath: func() string { return "" }}
	err := r.Rip(context.Background(), Entry{DiscPath: root, TitleID: "99999", OutputPath: "/x.mkv"}, nil)
	if err == nil || !strings.Contains(err.Error(), "title 99999 not found on SAMPLE_MOVIE") {
		t.Errorf("err = %v", err)
	}
}
