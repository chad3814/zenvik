package zenvik_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
	n := len(cmd)
	if n < 5 || cmd[n-5] != "--no-chapters" || cmd[n-4] != "(" || !strings.HasSuffix(cmd[n-3], filepath.Join("VIDEO_TS", "VTS_01_1.VOB")) ||
		!strings.HasSuffix(cmd[n-2], filepath.Join("VIDEO_TS", "VTS_01_2.VOB")) || cmd[n-1] != ")" {
		t.Errorf("inputs = %q", cmd[max(0, n-5):])
	}
	if _, err := os.Stat(out + ".partial"); err == nil {
		t.Error("dry run wrote a partial file")
	}
}
