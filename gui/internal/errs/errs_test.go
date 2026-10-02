package errs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chad3814/zenvik"
)

func TestLabel(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: title 00800", zenvik.ErrEncrypted), "encrypted"},
		{zenvik.ErrNoSpace, "not enough space"},
		{fmt.Errorf("x: %w", zenvik.ErrUnsupportedTitle), "unsupported"},
		{zenvik.ErrUnsupportedSource, "unsupported"},
		{zenvik.ErrOutputExists, "exists"},
		{zenvik.ErrMkvmergeNotFound, "mkvmerge not found"},
		{zenvik.ErrMkvmergeTooOld, "mkvmerge too old"},
		{zenvik.ErrMuxFailed, "mkvmerge failed"},
		{context.Canceled, "canceled"},
		{errors.New("boom"), "failed"},
	}
	for _, c := range cases {
		if got := Label(c.err); got != c.want {
			t.Errorf("Label(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestMessageDropsLibraryPrefix(t *testing.T) {
	if got := Message(zenvik.ErrNoSpace); got != "not enough free space" {
		t.Errorf("Message = %q", got)
	}
	if got := Message(errors.New("plain")); got != "plain" {
		t.Errorf("Message = %q", got)
	}
}
