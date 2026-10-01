package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
)

func TestProgressPlain(t *testing.T) {
	var b bytes.Buffer
	p := newProgressPrinter(&b, false)
	for _, f := range []float64{0, 0.03, 0.12, 0.15, 0.5, 1} {
		p.update(zenvik.Progress{Phase: zenvik.PhaseMuxing, Fraction: f, BytesTotal: 1000})
	}
	p.update(zenvik.Progress{Phase: zenvik.PhaseFinalizing, Fraction: 1, BytesTotal: 1000})
	p.done()
	want := "muxing 0%\nmuxing 12%\nmuxing 50%\nmuxing 100%\nfinalizing 100%\n"
	if b.String() != want {
		t.Errorf("plain progress =\n%q\nwant\n%q", b.String(), want)
	}
}

func TestProgressTTY(t *testing.T) {
	var b bytes.Buffer
	p := newProgressPrinter(&b, true)
	p.update(zenvik.Progress{Phase: zenvik.PhaseMuxing, Fraction: 0.42, BytesDone: 420 << 20, BytesTotal: 1000 << 20})
	p.done()
	out := b.String()
	if !strings.HasPrefix(out, "\r[########............]  42% muxing") || !strings.Contains(out, "420.0 MiB / 1000.0 MiB") || !strings.HasSuffix(out, "\n") {
		t.Errorf("tty progress = %q", out)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote([]string{"/usr/bin/mkvmerge", "-o", "/out dir/Bob's.mkv", "--language", "1:jpn"})
	want := `/usr/bin/mkvmerge -o '/out dir/Bob'\''s.mkv' --language 1:jpn`
	if got != want {
		t.Errorf("shellQuote = %s\nwant          %s", got, want)
	}
}
