package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDoctorCode(t *testing.T) {
	tests := []struct {
		mkOK, cfgOK bool
		leftovers   int
		want        int
	}{
		{true, true, 0, 0},
		{true, true, 2, 1},
		{true, false, 2, 2},
		{false, false, 2, 4},
		{false, true, 0, 4},
	}
	for _, tt := range tests {
		if got := doctorCode(tt.mkOK, tt.cfgOK, tt.leftovers); got != tt.want {
			t.Errorf("doctorCode(%v, %v, %d) = %d, want %d", tt.mkOK, tt.cfgOK, tt.leftovers, got, tt.want)
		}
	}
}

func TestDoctorMissingMkvmerge(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	code, out, errOut := runCLI("doctor")
	if code != 4 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"✓ config:", "not found; using defaults", "✗ mkvmerge:", "ISO mounting", "✓ no leftover mounts"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDoctorBadConfig(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	writeUserConfig(t, `outptu_dir = "x"`)
	_, out, _ := runCLI("doctor")
	if line := lineWith(out, "config:"); !strings.HasPrefix(line, "✗") || !strings.Contains(line, "outptu_dir") {
		t.Errorf("config line = %q", line)
	}
}

// stubMkvmerge writes a shell script that reports a supported mkvmerge
// version and returns its path.
func stubMkvmerge(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the mkvmerge stub is a shell script")
	}
	p := filepath.Join(t.TempDir(), "mkvmerge")
	script := "#!/bin/sh\necho \"mkvmerge v90.0 ('Stub') 64-bit\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeDeadRecord writes a mount record for dir owned by an exited process
// and returns the record's path.
func writeDeadRecord(t *testing.T, dir string) string {
	t.Helper()
	recDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "zenvik", "mounts")
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dead := exec.Command(os.Args[0], "-test.run=^$")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{"image": "/i/x.iso", "dir": dir, "device": "/dev/loop9",
		"pid": dead.Process.Pid, "created": time.Unix(1700000000, 0).UTC()}
	b, _ := json.Marshal(rec)
	p := filepath.Join(recDir, "leftover.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(p) })
	return p
}

func TestDoctorReportsStaleRecord(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	stub := stubMkvmerge(t)
	writeUserConfig(t, "mkvmerge_path = "+tomlPath(stub))
	p := writeDeadRecord(t, filepath.Join(t.TempDir(), "zenvik-mount-gone"))
	code, out, errOut := runCLI("doctor")
	if code != 0 {
		t.Errorf("exit %d, want 0 (a stale record is only a warning); stderr %q\n%s", code, errOut, out)
	}
	line := lineWith(out, "stale mount record:")
	want := "! stale mount record: /i/x.iso (mount is gone); remove with: rm -f '" + p + "'"
	if line != want {
		t.Errorf("stale line = %q\nwant         %q", line, want)
	}
	if l := lineWith(out, "leftover mount:"); l != "" {
		t.Errorf("stale record reported as a live leftover: %q", l)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("doctor must not delete the record: %v", err)
	}
}

func TestDoctorStaleRecordWithProblems(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	p := writeDeadRecord(t, "/nonexistent/zenvik-mount-x")
	code, out, _ := runCLI("doctor")
	if code != 4 {
		t.Errorf("exit %d, want 4 for missing mkvmerge", code)
	}
	if line := lineWith(out, "stale mount record:"); !strings.HasPrefix(line, "! ") || !strings.Contains(line, "/i/x.iso") {
		t.Errorf("stale line = %q\n%s", line, out)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("doctor must not delete the record: %v", err)
	}
}

func TestDoctorArgs(t *testing.T) {
	if code, _, _ := runCLI("doctor", "extra"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}
