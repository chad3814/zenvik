package zenvik_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestWithMinDuration(t *testing.T) {
	root := writeDisc(t, testdisc.SampleMovie())
	if menu := mustTitle(t, openDisc(t, root), "00099"); !menu.Rank.Filtered {
		t.Fatal("default: the 20 s menu title should be filtered")
	}
	d, err := zenvik.Open(context.Background(), root, zenvik.WithMinDuration(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if menu := mustTitle(t, d, "00099"); menu.Rank.Filtered {
		t.Errorf("min 10s: 00099 rank = %+v", menu.Rank)
	}
}

func TestRipMkvmergePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mkvmerge stub is a shell script")
	}
	t.Setenv("PATH", "")
	stub := filepath.Join(t.TempDir(), "mkvmerge")
	// The stub answers --version with a supported version and -J with one
	// video track on the sample movie's video PID (0x1011).
	script := `#!/bin/sh
case "$1" in
--version) echo "mkvmerge v90.0 ('Stub') 64-bit" ;;
-J) echo '{"tracks":[{"id":0,"type":"video","codec":"AVC","properties":{"number":4113}}]}' ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	res, err := d.Rip(context.Background(), d.Main(), zenvik.RipOptions{
		OutputPath:   filepath.Join(t.TempDir(), "x.mkv"),
		DryRun:       true,
		MkvmergePath: stub,
	})
	if err != nil {
		t.Fatalf("dry run with MkvmergePath: %v", err)
	}
	if len(res.Command) == 0 || res.Command[0] != stub {
		t.Errorf("Command = %q, want it to start with %q", res.Command, stub)
	}
}

func TestRipMkvmergePathMissing(t *testing.T) {
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	missing := filepath.Join(t.TempDir(), "no-such-mkvmerge")
	_, err := d.Rip(context.Background(), d.Main(), zenvik.RipOptions{
		OutputPath:   filepath.Join(t.TempDir(), "x.mkv"),
		MkvmergePath: missing,
	})
	if !errors.Is(err, zenvik.ErrMkvmergeNotFound) {
		t.Errorf("err = %v, want ErrMkvmergeNotFound for %s", err, missing)
	}
}
