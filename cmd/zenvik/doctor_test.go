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

func TestDoctorReportsLeftovers(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "zenvik", "mounts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dead := exec.Command(os.Args[0], "-test.run=^$")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{"image": "/i/x.iso", "dir": "/tmp/zenvik-mount-x", "device": "/dev/loop9",
		"pid": dead.Process.Pid, "created": time.Unix(1700000000, 0).UTC()}
	b, _ := json.Marshal(rec)
	p := filepath.Join(dir, "leftover.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(p) })
	_, out, _ := runCLI("doctor")
	line := lineWith(out, "leftover mount:")
	if !strings.HasPrefix(line, "✗") || !strings.Contains(line, "/i/x.iso at /tmp/zenvik-mount-x") {
		t.Errorf("leftover line = %q\n%s", line, out)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		if !strings.Contains(line, "remove with: ") {
			t.Errorf("no cleanup command in %q", line)
		}
	}
}

func TestDoctorArgs(t *testing.T) {
	if code, _, _ := runCLI("doctor", "extra"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}
