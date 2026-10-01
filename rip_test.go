package zenvik_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestRipErrorsBeforeMuxing(t *testing.T) {
	ctx := context.Background()
	disc := testdisc.SampleMovie()
	disc.ClipData["00030"] = testdisc.ScrambledM2TS(1)
	d := openDisc(t, writeDisc(t, disc))
	main := d.Main()
	out := filepath.Join(t.TempDir(), "movie.mkv")

	if _, err := d.Rip(ctx, mustTitle(t, d, "00010"), zenvik.RipOptions{OutputPath: out}); !errors.Is(err, zenvik.ErrEncrypted) {
		t.Errorf("encrypted title: err = %v", err)
	}
	if _, err := d.Rip(ctx, main, zenvik.RipOptions{}); err == nil || !strings.Contains(err.Error(), "OutputPath") {
		t.Errorf("missing output path: err = %v", err)
	}
	other := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	if _, err := d.Rip(ctx, other.Main(), zenvik.RipOptions{OutputPath: out}); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Errorf("foreign title: err = %v", err)
	}
	if err := os.WriteFile(out, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Rip(ctx, main, zenvik.RipOptions{OutputPath: out}); !errors.Is(err, zenvik.ErrOutputExists) {
		t.Errorf("existing output: err = %v", err)
	}
	if b, _ := os.ReadFile(out); string(b) != "keep me" {
		t.Error("existing output was modified")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Rip(ctx, main, zenvik.RipOptions{OutputPath: out + "2"}); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("closed disc: err = %v", err)
	}
}

func TestRipMissingMkvmerge(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	_, err := d.Rip(context.Background(), d.Main(), zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "x.mkv")})
	if !errors.Is(err, zenvik.ErrMkvmergeNotFound) {
		t.Errorf("err = %v, want ErrMkvmergeNotFound", err)
	}
}
