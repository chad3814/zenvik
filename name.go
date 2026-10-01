package zenvik

import (
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
