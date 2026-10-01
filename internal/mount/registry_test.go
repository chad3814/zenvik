package mount

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// isolateState points the record directory at a temp dir.
func isolateState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return filepath.Join(dir, "zenvik", "mounts")
}

// deadPID returns the PID of a process that has already exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestLeftovers(t *testing.T) {
	dir := isolateState(t)
	live := Record{Image: "/i/live.iso", Dir: "/m/live", PID: os.Getpid(), Created: time.Unix(100, 0)}
	dead := Record{Image: "/i/dead.iso", Dir: "/m/dead", Device: "/dev/loop9", PID: deadPID(t), Created: time.Unix(200, 0)}
	for _, r := range []Record{live, dead} {
		if _, err := register(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Leftovers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Image != dead.Image || got[0].Dir != dead.Dir || got[0].Device != dead.Device || !got[0].Created.Equal(dead.Created) {
		t.Fatalf("Leftovers = %+v, want only %+v", got, dead)
	}
	cleanup := got[0].Cleanup()
	if got[0].Path == "" || !strings.Contains(cleanup, got[0].Path) {
		t.Errorf("cleanup %q should remove record %q", cleanup, got[0].Path)
	}
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(cleanup, "hdiutil detach -force") || !strings.Contains(cleanup, dead.Dir) {
			t.Errorf("cleanup = %q", cleanup)
		}
	case "linux":
		if !strings.Contains(cleanup, "udisksctl unmount -b /dev/loop9") {
			t.Errorf("cleanup = %q", cleanup)
		}
	case "windows":
		if !strings.Contains(cleanup, "Dismount-DiskImage -ImagePath '/i/dead.iso'") {
			t.Errorf("cleanup = %q", cleanup)
		}
	}
}

func TestLeftoversNoDirectory(t *testing.T) {
	isolateState(t)
	if got, err := Leftovers(); err != nil || got != nil {
		t.Errorf("Leftovers = %v, %v", got, err)
	}
}

func TestLeftoversIgnoresTempFiles(t *testing.T) {
	dir := isolateState(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	partial := `{"image":"/i/x.iso","dir":"/m/x","pid":0,"created":"2020-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "1-abc.tmp"), []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Leftovers(); err != nil || len(got) != 0 {
		t.Errorf("Leftovers = %v, %v", got, err)
	}
}

func TestRegisterLeavesNoTempFiles(t *testing.T) {
	dir := isolateState(t)
	p, err := register(Record{Image: "/i/x.iso", Dir: "/m/x", PID: os.Getpid(), Created: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(p) != ".json" {
		t.Errorf("path = %q", p)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || filepath.Join(dir, ents[0].Name()) != p {
		t.Errorf("entries = %v", ents)
	}
}

func TestStateDirIgnoresRelativeXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative/state")
	got, err := stateDir()
	if err != nil {
		t.Skip("no user cache dir")
	}
	if !filepath.IsAbs(got) || strings.Contains(got, "relative") {
		t.Errorf("stateDir = %q", got)
	}
}
