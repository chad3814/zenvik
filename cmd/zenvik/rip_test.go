package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestRipExitCodes(t *testing.T) {
	good := writeDisc(t, testdisc.SampleMovie())
	enc := testdisc.SampleMovie()
	for id := range enc.Clips {
		enc.ClipData[id] = testdisc.ScrambledM2TS(1)
	}
	encrypted := writeDisc(t, enc)
	short := testdisc.SampleMovie()
	short.Titles, short.MovieObjects = nil, nil
	delete(short.Playlists, "00800")
	delete(short.Playlists, "00801")
	delete(short.Playlists, "00010")
	noMain := writeDisc(t, short)

	existsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(existsDir, "Sample Movie.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want int
		msg  string
	}{
		{"missing path", []string{"rip", filepath.Join(good, "nope")}, 1, ""},
		{"no args", []string{"rip"}, 2, ""},
		{"unknown playlist", []string{"rip", "-p", "12345", good}, 2, "12345"},
		{"encrypted", []string{"rip", encrypted}, 3, "encrypted"},
		{"no main title", []string{"rip", "-d", t.TempDir(), noMain}, 1, "--title"},
		{"output exists", []string{"rip", "-d", existsDir, good}, 1, "--overwrite"},
	}
	for _, tt := range tests {
		code, _, errOut := runCLI(tt.args...)
		if code != tt.want || !strings.Contains(errOut, tt.msg) {
			t.Errorf("%s: exit %d (want %d), stderr %q (want containing %q)", tt.name, code, tt.want, errOut, tt.msg)
		}
	}
}

func TestRipMissingMkvmerge(t *testing.T) {
	good := writeDisc(t, testdisc.SampleMovie())
	t.Setenv("PATH", t.TempDir())
	code, _, errOut := runCLI("rip", "-d", t.TempDir(), good)
	if code != 4 || !strings.Contains(errOut, "mkvmerge not found") || !strings.Contains(errOut, "mkvtoolnix.download") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestRipAmbiguousWarning(t *testing.T) {
	d := testdisc.SampleMovie()
	d.Titles, d.MovieObjects = nil, nil
	var segs []testdisc.Segment
	for i := 201; i <= 220; i++ {
		segs = append(segs, testdisc.Segment{Clip: fmt.Sprintf("%05d", i), Length: 297 * time.Second})
	}
	d.Playlists["00802"] = testdisc.SimplePlaylist(segs...)
	d.AddClipsFor()
	t.Setenv("PATH", t.TempDir()) // stop before muxing; the warning comes first
	_, out, errOut := runCLI("rip", "-d", t.TempDir(), writeDisc(t, d))
	if !strings.Contains(errOut, "close call") || !strings.Contains(errOut, "--title") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(out, "Ripping 00800") || !strings.Contains(out, "chosen because:") {
		t.Errorf("stdout = %q", out)
	}
}
