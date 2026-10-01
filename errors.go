package zenvik

import (
	"errors"

	"github.com/chad3814/zenvik/internal/source"
)

// Errors returned by Open; check them with errors.Is.
var (
	// ErrUnsupportedSource: the path is not a Blu-ray ISO image or BDMV folder.
	ErrUnsupportedSource = source.ErrUnsupported
	// ErrEncrypted: every usable title on the disc is AACS-encrypted.
	ErrEncrypted = errors.New("zenvik: disc is encrypted; zenvik only reads unencrypted discs")
	// ErrNoTitles: the disc has no playlists.
	ErrNoTitles = errors.New("zenvik: no playlists found")
)
