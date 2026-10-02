package zenvik_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func hasPair(cmd []string, flag, val string) bool {
	for i := 0; i+1 < len(cmd); i++ {
		if cmd[i] == flag && cmd[i+1] == val {
			return true
		}
	}
	return false
}

func TestRipDVDDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mkvmerge stub is a shell script")
	}
	t.Setenv("PATH", "")
	stub := filepath.Join(t.TempDir(), "mkvmerge")
	script := `#!/bin/sh
case "$1" in
--version) echo "mkvmerge v90.0 ('Stub') 64-bit" ;;
-J) printf '%s\n' '{"tracks":[
{"id":0,"type":"video","codec":"MPEG-1/2","properties":{"stream_id":224}},
{"id":1,"type":"audio","codec":"AC-3","properties":{"stream_id":189,"sub_stream_id":128,"audio_channels":6}},
{"id":2,"type":"audio","codec":"AC-3","properties":{"stream_id":189,"sub_stream_id":129,"audio_channels":2}},
{"id":3,"type":"audio","codec":"AC-3","properties":{"stream_id":189,"sub_stream_id":130,"audio_channels":2}}]}'
;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	out := filepath.Join(t.TempDir(), "movie.mkv")
	res, err := d.Rip(context.Background(), d.Main(), zenvik.RipOptions{OutputPath: out, DryRun: true, MkvmergePath: stub})
	if err != nil {
		t.Fatal(err)
	}
	cmd := res.Command
	if !hasPair(cmd, "--chapters", "<chapters.txt>") {
		t.Errorf("no chapter placeholder in %q", cmd)
	}
	for _, p := range [][2]string{
		{"--language", "1:eng"}, {"--language", "2:fre"}, {"--language", "3:eng"},
		{"--track-name", "1:AC-3 5.1"}, {"--track-name", "3:AC-3 Stereo (Director's Commentary)"},
		{"--default-track-flag", "0:yes"}, {"--default-track-flag", "1:yes"}, {"--default-track-flag", "2:no"},
	} {
		if !hasPair(cmd, p[0], p[1]) {
			t.Errorf("missing %s %s in %q", p[0], p[1], cmd)
		}
	}
	open := slices.Index(cmd, "(")
	if open < 1 || open+3 >= len(cmd) || cmd[open-1] != "--no-chapters" || !strings.HasSuffix(cmd[open+1], filepath.Join("VIDEO_TS", "VTS_01_1.VOB")) ||
		!strings.HasSuffix(cmd[open+2], filepath.Join("VIDEO_TS", "VTS_01_2.VOB")) || cmd[open+3] != ")" {
		t.Errorf("inputs = %q", cmd)
	}
	tail := cmd[slices.Index(cmd, ")")+1:]
	if len(tail) == 0 || tail[len(tail)-1] != "<subtitles.idx>" || !hasPair(tail, "--language", "0:eng") || !hasPair(tail, "--language", "1:fre") ||
		!hasPair(tail, "--track-name", "2:Forced") || !hasPair(tail, "--default-track-flag", "0:no") || !hasPair(tail, "--subtitle-tracks", "0,1,2") {
		t.Errorf("subtitle input = %q", tail)
	}
	if !hasPair(cmd, "--track-order", "0:0,0:1,0:2,0:3,1:0,1:1,1:2") {
		t.Errorf("track order missing in %q", cmd)
	}
	if _, err := os.Stat(out + ".partial"); err == nil {
		t.Error("dry run wrote a partial file")
	}
}

// layoutStub answers --version, -J (one video and one AC-3 track), and
// fails anything else (a mux), so non-dry-run rips stop after the job is built.
func layoutStub(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the mkvmerge stub is a shell script")
	}
	stub := filepath.Join(t.TempDir(), "mkvmerge")
	script := `#!/bin/sh
case "$1" in
--version) echo "mkvmerge v90.0 ('Stub') 64-bit" ;;
-J) printf '%s\n' '{"tracks":[{"id":0,"type":"video","codec":"MPEG-1/2","properties":{"stream_id":224}},{"id":1,"type":"audio","codec":"AC-3","properties":{"stream_id":189,"sub_stream_id":128,"audio_channels":2}}]}' ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func TestDVDCutDryRun(t *testing.T) {
	t.Setenv("PATH", "")
	stub := layoutStub(t)
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	res, err := d.Rip(context.Background(), mustTitle(t, d, "05"), zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "ep.mkv"), DryRun: true, MkvmergePath: stub})
	if err != nil {
		t.Fatal(err)
	}
	cmd := res.Command
	// 20-minute cells are 35964 NTSC frames, 1199.9988 s. VTS 2 has one VOB
	// ID, so each margin is one frame (33.366666 ms): 2399.9976 s and
	// 3599.9964 s, each less a frame.
	if !hasPair(cmd, "--split", "parts:00:39:59.964233334-00:59:59.963033334") {
		t.Errorf("no split in %q", cmd)
	}
	i := slices.Index(cmd, "(")
	if i < 0 || len(cmd) < i+4 || !strings.HasSuffix(cmd[i+1], "VTS_02_1.VOB") || !strings.HasSuffix(cmd[i+2], "VTS_02_2.VOB") || cmd[i+3] != ")" {
		t.Errorf("group = %q", cmd[max(0, i):])
	}
}

func TestDVDCopyDryRun(t *testing.T) {
	t.Setenv("PATH", "")
	stub := layoutStub(t)
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	out := filepath.Join(t.TempDir(), "angle.mkv")
	res, err := d.Rip(context.Background(), mustTitle(t, d, "08"), zenvik.RipOptions{OutputPath: out, DryRun: true, MkvmergePath: stub})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(res.Command, "(")
	if i < 0 || res.Command[i+1] != "<title.vob>" || res.Command[i+2] != ")" {
		t.Errorf("command = %q", res.Command)
	}
	if ents, _ := os.ReadDir(filepath.Dir(out)); len(ents) != 0 {
		t.Errorf("dry run wrote %v", ents)
	}
}

func TestDVDCopyRemovesTempOnMuxFailure(t *testing.T) {
	t.Setenv("PATH", "")
	stub := layoutStub(t)
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	out := filepath.Join(t.TempDir(), "angle.mkv")
	if _, err := d.Rip(context.Background(), mustTitle(t, d, "08"), zenvik.RipOptions{OutputPath: out, MkvmergePath: stub}); err == nil {
		t.Fatal("the stub fails the mux; Rip must fail")
	}
	if ents, _ := os.ReadDir(filepath.Dir(out)); len(ents) != 0 {
		t.Errorf("failed copy rip left %v", ents)
	}
}

func TestDVDSkipWarning(t *testing.T) {
	t.Setenv("PATH", "")
	stub := layoutStub(t)
	d := openDisc(t, writeDVD(t, testdisc.StrayCellDVD()))
	res, err := d.Rip(context.Background(), mustTitle(t, d, "01"), zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "x.mkv"), DryRun: true, MkvmergePath: stub})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Warnings, "title 01: skipped cell 3 (1.0 s at sectors 0–1, out of order)") {
		t.Errorf("warnings = %q", res.Warnings)
	}
}
