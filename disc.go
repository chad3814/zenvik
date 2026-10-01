package zenvik

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/source"
)

// SourceKind says what kind of path a disc was opened from.
type SourceKind = source.Kind

// Source kinds.
const (
	ISO     = source.ISO
	BDMVDir = source.BDMVDir
)

// Disc is an opened Blu-ray disc image or BDMV folder.
type Disc struct {
	Path   string           // the path passed to Open
	Kind   SourceKind       //
	Label  string           // UDF volume identifier, or the folder's name
	Meta   *bluray.DiscMeta // disc library metadata (title, language); nil if absent
	Titles []*Title         // ranked: the main title, if any, is Titles[0]
	src    *source.Source
}

// Open reads the disc at path (an ISO image, a folder containing BDMV, or a
// BDMV folder), ranks its titles and detects encryption.
func Open(ctx context.Context, path string) (*Disc, error) {
	src, err := source.Open(path)
	if err != nil {
		return nil, err
	}
	titles, meta, err := scanTitles(ctx, src.FS)
	if err != nil {
		src.Close()
		return nil, err
	}
	return &Disc{Path: path, Kind: src.Kind, Label: src.Label, Meta: meta, Titles: titles, src: src}, nil
}

// Main returns the main title, or nil if no title qualifies.
func (d *Disc) Main() *Title {
	for _, t := range d.Titles {
		if t.Rank.IsMain {
			return t
		}
	}
	return nil
}

// Title returns the title with playlist ID id, e.g. "00800".
func (d *Disc) Title(id string) (*Title, error) {
	for _, t := range d.Titles {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("zenvik: no title %q on this disc: %w", id, fs.ErrNotExist)
}

// Close releases the disc. It is safe to call more than once.
func (d *Disc) Close() error {
	if d.src == nil {
		return nil
	}
	err := d.src.Close()
	d.src = nil
	return err
}
