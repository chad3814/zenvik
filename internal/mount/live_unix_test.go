//go:build darwin || linux

package mount

import (
	"runtime"
	"testing"
)

func TestIsMountPoint(t *testing.T) {
	mounted := "/dev" // devfs on macOS
	if runtime.GOOS == "linux" {
		mounted = "/proc"
	}
	if !isMountPoint(mounted) {
		t.Errorf("isMountPoint(%q) = false", mounted)
	}
	if isMountPoint(t.TempDir()) {
		t.Error("a plain directory is not a mount point")
	}
	if isMountPoint("/nonexistent/zenvik") {
		t.Error("a missing directory is not a mount point")
	}
}
