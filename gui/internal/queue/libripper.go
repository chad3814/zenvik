package queue

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/chad3814/zenvik"
)

// LibRipper rips with the zenvik library. It opens the disc afresh for each
// entry, so a queue restored after a restart doesn't depend on what the
// window has open, and finds the title by ID.
type LibRipper struct {
	Open         func(ctx context.Context, path string) (*zenvik.Disc, error)
	MkvmergePath func() string // the config's mkvmerge_path, read when the rip starts
}

// Rip implements Ripper.
func (r LibRipper) Rip(ctx context.Context, e Entry, progress func(zenvik.Progress)) error {
	d, err := r.Open(ctx, e.DiscPath)
	if err != nil {
		return err
	}
	defer d.Close()
	t, err := d.Title(e.TitleID)
	if err != nil {
		return fmt.Errorf("title %s not found on %s: %w", e.TitleID, filepath.Base(e.DiscPath), err)
	}
	_, err = d.Rip(ctx, t, zenvik.RipOptions{OutputPath: e.OutputPath, MkvmergePath: r.MkvmergePath(), OnProgress: progress})
	return err
}
