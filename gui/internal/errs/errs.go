// Package errs turns zenvik errors into the short labels and messages the
// GUI shows.
package errs

import (
	"context"
	"errors"
	"strings"

	"github.com/chad3814/zenvik"
)

// Message is err's text without the "zenvik: " prefix the library adds.
func Message(err error) string {
	return strings.TrimPrefix(err.Error(), "zenvik: ")
}

// Label is a one- or two-word summary of err for a list row; the full
// Message goes in its tooltip.
func Label(err error) string {
	switch {
	case errors.Is(err, zenvik.ErrEncrypted):
		return "encrypted"
	case errors.Is(err, zenvik.ErrNoSpace):
		return "not enough space"
	case errors.Is(err, zenvik.ErrUnsupportedTitle), errors.Is(err, zenvik.ErrUnsupportedSource):
		return "unsupported"
	case errors.Is(err, zenvik.ErrOutputExists):
		return "exists"
	case errors.Is(err, zenvik.ErrMkvmergeNotFound):
		return "mkvmerge not found"
	case errors.Is(err, zenvik.ErrMkvmergeTooOld):
		return "mkvmerge too old"
	case errors.Is(err, zenvik.ErrMuxFailed):
		return "mkvmerge failed"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "failed"
}
