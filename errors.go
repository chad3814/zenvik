package zenvik

import (
	"errors"

	"github.com/chad3814/zenvik/internal/mount"
	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/naming"
	"github.com/chad3814/zenvik/internal/source"
)

// Errors returned by Open; check them with errors.Is.
var (
	// ErrUnsupportedSource: the path is not a Blu-ray or DVD ISO image or folder.
	ErrUnsupportedSource = source.ErrUnsupported
	// ErrEncrypted: every usable title on the disc is AACS- or CSS-encrypted.
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
	// ErrInvalidTemplate: a name template is malformed or renders an unsafe path.
	ErrInvalidTemplate = naming.ErrTemplate
	// ErrUnsupportedTitle: the title can't be ripped yet; Title.Unsupported says why.
	ErrUnsupportedTitle = errors.New("zenvik: title is not supported")
	// ErrNoSpace: the output drive lacks the free space a DVD title's temporary copy needs.
	ErrNoSpace = errors.New("zenvik: not enough free space")
	// ErrNeedsUDFRepair: on Linux, the image's directory entries have tag CRC
	// lengths that leave out padding, which Linux's UDF driver rejects;
	// `zenvik repair-udf` fixes them.
	ErrNeedsUDFRepair = errors.New("zenvik: Linux can't read this image's directories: their entries' CRC lengths leave out padding, which Linux's UDF driver rejects as corrupt")
)
