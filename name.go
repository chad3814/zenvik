package zenvik

import (
	"path/filepath"
	"strings"

	"github.com/chad3814/zenvik/internal/naming"
)

// Name returns a human-readable disc name: the disc library title if
// present, otherwise the volume label tidied up ("THE_MATRIX" → "The
// Matrix"), otherwise "untitled".
func (d *Disc) Name() string {
	if d.Meta != nil {
		if t := strings.TrimSpace(d.Meta.Title); t != "" {
			return t
		}
	}
	if n := naming.CleanLabel(d.Label); n != "" {
		return n
	}
	return "untitled"
}

// NameVars are caller-supplied template variables.
type NameVars struct {
	Name string // overrides the disc name for {name}
	Year string // {year}
}

// FormatName renders a name template for title t of disc d and returns a
// relative path (OS separators) to place under the output directory.
// Variables: {name} (vars.Name, else d.Name()), {year} (vars.Year),
// {label} (the volume label) and {playlist} (t.ID).
func FormatName(tmpl string, d *Disc, t *Title, vars NameVars) (string, error) {
	tp, err := naming.Parse(tmpl)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(vars.Name)
	if name == "" {
		name = d.Name()
	}
	id := ""
	if t != nil {
		id = t.ID
	}
	rel, err := tp.Render(map[string]string{"name": name, "year": vars.Year, "label": d.Label, "playlist": id})
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(rel), nil
}
