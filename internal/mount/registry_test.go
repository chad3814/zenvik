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
	fakeLive(t, true)
	got, err := Leftovers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Image != dead.Image || got[0].Dir != dead.Dir || got[0].Device != dead.Device || !got[0].Created.Equal(dead.Created) {
		t.Fatalf("Leftovers = %+v, want only %+v", got, dead)
	}
	if got[0].Stale {
		t.Errorf("record with a live mount is Stale")
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
		if !strings.Contains(cleanup, "udisksctl unmount --no-user-interaction -b /dev/loop9") ||
			!strings.Contains(cleanup, "udisksctl loop-delete --no-user-interaction -b /dev/loop9") {
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

// writeDeadRecord registers a record for dir owned by an exited process.
func writeDeadRecord(t *testing.T, dir string) {
	t.Helper()
	if _, err := register(Record{Image: "/i/x.iso", Dir: dir, Device: "/dev/loop9", PID: deadPID(t), Created: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
}

func onlyLeftover(t *testing.T) Record {
	t.Helper()
	got, err := Leftovers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Leftovers = %+v, want one record", got)
	}
	return got[0]
}

func TestLeftoversStaleWhenDirMissing(t *testing.T) {
	isolateState(t)
	writeDeadRecord(t, filepath.Join(t.TempDir(), "gone"))
	if r := onlyLeftover(t); !r.Stale {
		t.Errorf("record for a missing Dir is not Stale: %+v", r)
	}
}

func TestLeftoversStaleWhenDirNotMountPoint(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" || runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		t.Skip("mount points are detected by device number on unix only")
	}
	isolateState(t)
	writeDeadRecord(t, t.TempDir()) // exists, but nothing is mounted on it
	if r := onlyLeftover(t); !r.Stale {
		t.Errorf("record for a plain directory is not Stale: %+v", r)
	}
}

func TestStaleCleanupRemovesOnlyRecord(t *testing.T) {
	isolateState(t)
	writeDeadRecord(t, filepath.Join(t.TempDir(), "gone"))
	r := onlyLeftover(t)
	got := r.Cleanup()
	var want string
	switch runtime.GOOS {
	case "windows":
		want = "Remove-Item -LiteralPath " + psQuote(r.Path)
	default:
		want = "rm -f '" + r.Path + "'"
	}
	if got != want {
		t.Errorf("Cleanup() = %q, want %q", got, want)
	}
}
