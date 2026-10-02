package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

// events decodes rip --jsonl output, failing on any line that isn't a JSON object.
func events(t *testing.T, out string) []map[string]any {
	t.Helper()
	var evs []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("stdout line is not JSON: %q (%v)\nall output:\n%s", line, err, out)
		}
		evs = append(evs, ev)
	}
	return evs
}

func kinds(evs []map[string]any) []string {
	var out []string
	for _, ev := range evs {
		out = append(out, ev["event"].(string))
	}
	return out
}

// dryRunMkvmerge answers --version and -J for the sample movie's playlist.
func dryRunMkvmerge(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the mkvmerge stub is a shell script")
	}
	p := filepath.Join(t.TempDir(), "mkvmerge")
	script := `#!/bin/sh
case "$1" in
--version) echo "mkvmerge v90.0 ('Stub') 64-bit" ;;
-J) printf '%s\n' '{"tracks":[{"id":0,"type":"video","codec":"AVC","properties":{"number":4113}},{"id":1,"type":"audio","codec":"TrueHD","properties":{"number":4352,"audio_channels":6}},{"id":2,"type":"audio","codec":"AC-3","properties":{"number":4353,"audio_channels":2}},{"id":3,"type":"subtitles","codec":"PGS","properties":{"number":4608}}]}' ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRipJSONLDryRun(t *testing.T) {
	stub := dryRunMkvmerge(t)
	writeUserConfig(t, "mkvmerge_path = "+tomlPath(stub))
	disc := writeDisc(t, testdisc.SampleMovie())
	code, out, errOut := runCLI("rip", "--jsonl", "--dry-run", "-o", "rel.mkv", disc)
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", code, errOut, out)
	}
	evs := events(t, out)
	if got := strings.Join(kinds(evs), ","); got != "start,dry_run" {
		t.Fatalf("events = %s\n%s", got, out)
	}
	start := evs[0]
	wd, _ := os.Getwd()
	wantOut := filepath.Join(wd, "rel.mkv")
	if start["version"] != float64(1) || start["title"] != "00800" || start["output"] != wantOut || start["auto"] != true ||
		start["kind"] != "bdmv" || start["format"] != "bluray" || !filepath.IsAbs(start["source"].(string)) {
		t.Errorf("start = %v", start)
	}
	if d, _ := start["duration_seconds"].(float64); d != 6000 {
		t.Errorf("duration_seconds = %v", start["duration_seconds"])
	}
	if r, _ := start["reasons"].([]any); len(r) == 0 {
		t.Errorf("start has no reasons: %v", start)
	}
	dry := evs[1]
	cmd, _ := dry["command"].([]any)
	if dry["output"] != wantOut || len(cmd) == 0 || cmd[0] != stub {
		t.Errorf("dry_run = %v", dry)
	}
}

func TestRipJSONLErrors(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"bad flag", []string{"rip", "--jsonl", "--bogus", disc}, 2, "unknown flag"},
		{"bad year", []string{"rip", "--jsonl", "--year", "99", disc}, 2, "--year"},
		{"not a disc", []string{"rip", "--jsonl", t.TempDir()}, 3, "no BDMV"},
		{"no mkvmerge", []string{"rip", "--jsonl", "-d", t.TempDir(), disc}, 4, "mkvmerge"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			code, out, errOut := runCLI(tt.args...)
			if code != tt.code || errOut != "" {
				t.Fatalf("exit %d (want %d), stderr %q", code, tt.code, errOut)
			}
			evs := events(t, out)
			last := evs[len(evs)-1]
			if last["event"] != "error" || last["exit_code"] != float64(tt.code) || !strings.Contains(last["message"].(string), tt.want) {
				t.Errorf("last event = %v", last)
			}
			if _, ok := last["canceled"]; ok {
				t.Errorf("canceled set on a non-cancellation error: %v", last)
			}
		})
	}
}

func TestRipJSONLCanceled(t *testing.T) {
	disc := writeDisc(t, testdisc.SampleMovie())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errb bytes.Buffer
	code := run(ctx, []string{"rip", "--jsonl", disc}, &out, &errb)
	evs := events(t, out.String())
	last := evs[len(evs)-1]
	if code != 1 || errb.Len() != 0 || last["event"] != "error" || last["canceled"] != true {
		t.Errorf("exit %d, stderr %q, last = %v", code, errb.String(), last)
	}
}

func TestEventWriterProgressThrottle(t *testing.T) {
	var b bytes.Buffer
	ew := newEventWriter(&b)
	for _, f := range []float64{0, 0.001, 0.004, 0.01, 0.015, 0.02, 0.5, 0.5, 1} {
		ew.progress(zenvik.Progress{Phase: zenvik.PhaseMuxing, Fraction: f, BytesTotal: 1000, BytesDone: int64(f * 1000)})
	}
	ew.progress(zenvik.Progress{Phase: zenvik.PhaseFinalizing, Fraction: 0})
	evs := events(t, b.String())
	var got []string
	for _, ev := range evs {
		got = append(got, fmt.Sprint(ev["phase"], ":", ev["fraction"]))
	}
	if want := "muxing:0,muxing:0.01,muxing:0.02,muxing:0.5,muxing:1,finalizing:0"; strings.Join(got, ",") != want {
		t.Errorf("progress events = %s, want %s", strings.Join(got, ","), want)
	}
	if evs[1]["bytes_done"] != float64(10) || evs[1]["bytes_total"] != float64(1000) {
		t.Errorf("bytes = %v", evs[1])
	}
}

func TestRipJSONLRipMethod(t *testing.T) {
	root := filepath.Join(t.TempDir(), "STRAY")
	if err := testdisc.StrayCellDVD().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	_, out, _ := runCLI("rip", "--jsonl", "-t", "2", "-d", t.TempDir(), root)
	start := events(t, out)[0]
	if start["event"] != "start" || start["rip_method"] != "copy" || start["temp_bytes"] != float64(50*2048) {
		t.Errorf("start = %v", start)
	}
}
