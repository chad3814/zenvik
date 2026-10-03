// Package discs keeps the GUI's list of open discs and turns each into the
// plain summary the frontend renders.
package discs

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
)

// TitleSummary is one title as the titles list shows it.
type TitleSummary struct {
	ID              string   `json:"id"`
	DurationSeconds float64  `json:"durationSeconds"`
	Chapters        int      `json:"chapters"`
	SizeBytes       int64    `json:"sizeBytes"`
	Main            bool     `json:"main"`
	Rippable        bool     `json:"rippable"`
	Reason          string   `json:"reason,omitempty"` // why it can't be ripped
	SkippedCells    []string `json:"skippedCells"`
	DefaultName     string   `json:"defaultName"` // relative path from the template
}

// Summary is one disc in the list.
type Summary struct {
	Path       string         `json:"path"`
	State      string         `json:"state"` // opening | ready | error
	Error      string         `json:"error,omitempty"`
	ErrorLabel string         `json:"errorLabel,omitempty"`
	Format     string         `json:"format,omitempty"`
	Label      string         `json:"label,omitempty"`
	Name       string         `json:"name,omitempty"`
	Ambiguous  bool           `json:"ambiguous"`
	OutputDir  string         `json:"outputDir"`
	Titles     []TitleSummary `json:"titles"`
}

// Titles summarizes d's titles in rank order. Each gets a default name from
// template; names that collide on this disc get " (2)", " (3)"… before the
// extension.
func Titles(d *zenvik.Disc, template string) []TitleSummary {
	used := map[string]bool{}
	out := make([]TitleSummary, 0, len(d.Titles))
	for _, t := range d.Titles {
		ts := TitleSummary{
			ID:              t.ID,
			DurationSeconds: t.Duration.Seconds(),
			Chapters:        len(t.Chapters),
			SizeBytes:       t.Size,
			Main:            t.Rank.IsMain,
			Rippable:        !t.Encrypted && t.Unsupported == "",
			SkippedCells:    make([]string, 0, len(t.SkippedCells)),
			DefaultName:     unique(defaultName(template, d, t), used),
		}
		switch {
		case t.Encrypted:
			ts.Reason = "encrypted"
		case t.Unsupported != "":
			ts.Reason = t.Unsupported
		}
		for _, c := range t.SkippedCells {
			ts.SkippedCells = append(ts.SkippedCells, fmt.Sprintf("skipped cell %d (%.1f s at sectors %d–%d)",
				c.Cell, c.Duration.Seconds(), c.FirstSector, c.LastSector))
		}
		out = append(out, ts)
	}
	return out
}

func defaultName(template string, d *zenvik.Disc, t *zenvik.Title) string {
	if name, err := zenvik.FormatName(template, d, t, zenvik.NameVars{}); err == nil {
		return name
	}
	if name, err := zenvik.FormatName(config.DefaultTemplate, d, t, zenvik.NameVars{}); err == nil {
		return name
	}
	return t.ID + ".mkv"
}

func unique(name string, used map[string]bool) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	cand := name
	for n := 2; used[cand]; n++ {
		cand = fmt.Sprintf("%s (%d)%s", stem, n, ext)
	}
	used[cand] = true
	return cand
}

// DefaultOutputDir is where a disc's MKVs go until the user picks a folder:
// the config's output_dir, except that the CLI's "." default (the current
// directory, meaningless for an app) becomes ~/Movies on macOS when it exists,
// else the home folder, and a relative output_dir is under the home folder.
func DefaultOutputDir(configured, goos, home string, exists func(string) bool) string {
	if configured != "" && configured != "." {
		if filepath.IsAbs(configured) {
			return configured
		}
		return filepath.Join(home, configured)
	}
	if goos == "darwin" {
		if m := filepath.Join(home, "Movies"); exists(m) {
			return m
		}
	}
	return filepath.FromSlash(home)
}
