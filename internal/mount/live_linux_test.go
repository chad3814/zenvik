//go:build linux

package mount

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fakeSys serves /sys reads from files.
func fakeSys(t *testing.T, files map[string]string) {
	t.Helper()
	old := readSysFile
	readSysFile = func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, fs.ErrNotExist
	}
	t.Cleanup(func() { readSysFile = old })
}

func TestLeftoversLoopBackingFile(t *testing.T) {
	const backing = "/sys/block/loop9/loop/backing_file"
	tests := []struct {
		name  string
		files map[string]string
		stale bool
	}{
		{"matches", map[string]string{backing: "/i/x.iso\n"}, false},
		{"unclean match", map[string]string{backing: "/i/./x.iso\n"}, false},
		{"deleted image", map[string]string{backing: "/i/x.iso (deleted)\n"}, false},
		{"other image", map[string]string{backing: "/i/other.iso\n"}, true},
		{"loop device gone", map[string]string{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateState(t)
			fakeDirMounted(t, true)
			fakeSys(t, tt.files)
			writeDeadRecord(t, "/m/x")
			if r := onlyLeftover(t); r.Stale != tt.stale {
				t.Errorf("Stale = %v, want %v", r.Stale, tt.stale)
			}
		})
	}
}

func TestLoopLiveIgnoresNonLoopDevice(t *testing.T) {
	fakeSys(t, nil)
	if !loopLive(Record{Image: "/i/x.iso", Device: "/dev/sr0"}) {
		t.Error("a non-loop device should not be checked against /sys")
	}
	if loopLive(Record{Image: "/i/x.iso", Device: "/dev/loop3"}) {
		t.Error("a loop device without backing_file should not be live")
	}
}

func TestLoopLiveResolvesImageSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.iso")
	link := filepath.Join(dir, "link.iso")
	if err := os.WriteFile(real, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	fakeSys(t, map[string]string{"/sys/block/loop2/loop/backing_file": resolved + "\n"})
	if !loopLive(Record{Image: link, Device: "/dev/loop2"}) {
		t.Error("a symlinked image path should match its resolved backing file")
	}
}
