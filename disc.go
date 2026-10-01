package zenvik

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/rank"
	"github.com/chad3814/zenvik/internal/source"
)

// SourceKind says what kind of path a disc was opened from.
type SourceKind = source.Kind

// Source kinds.
const (
	ISO     = source.ISO
	BDMVDir = source.BDMVDir
)

// OpenOption customizes Open.
type OpenOption func(*openConfig)

type openConfig struct {
	minDuration time.Duration
}

// WithMinDuration sets the shortest title that can be the main feature
// (default 2 minutes).
func WithMinDuration(d time.Duration) OpenOption {
	return func(c *openConfig) { c.minDuration = d }
}

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
// BDMV folder), ranks its titles and detects encryption. Options customize the
// scanning and ranking behavior; the default minimum duration is 2 minutes.
func Open(ctx context.Context, path string, opts ...OpenOption) (*Disc, error) {
	cfg := openConfig{minDuration: defaultMinDuration}
	for _, opt := range opts {
		opt(&cfg)
	}
	src, err := source.Open(path)
	if err != nil {
		return nil, err
	}
	titles, meta, err := scanTitles(ctx, src.FS, cfg.minDuration, rank.DefaultWeights)
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
