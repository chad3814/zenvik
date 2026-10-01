package zenvik

import (
	"errors"

	"github.com/chad3814/zenvik/internal/mount"
	"github.com/chad3814/zenvik/internal/mux"
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
	// ErrMkvmergeNotFound: mkvmerge (MKVToolNix) is not installed or not on PATH.
	ErrMkvmergeNotFound = mux.ErrNotFound
	// ErrMkvmergeTooOld: the installed mkvmerge is older than the supported minimum.
	ErrMkvmergeTooOld = mux.ErrTooOld
	// ErrMuxFailed: mkvmerge reported an error.
	ErrMuxFailed = mux.ErrFailed
	// ErrMountUnavailable: the ISO image could not be mounted on this system.
	ErrMountUnavailable = mount.ErrUnavailable
	// ErrOutputExists: the output file exists and RipOptions.Overwrite is false.
	ErrOutputExists = errors.New("zenvik: output file already exists")
)
