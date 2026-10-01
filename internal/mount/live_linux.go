//go:build linux

package mount

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// readSysFile reads a file under /sys; tests replace it.
var readSysFile = os.ReadFile

var loopDeviceRE = regexp.MustCompile(`^/dev/(loop[0-9]+)$`)

func deviceLive(r Record) bool { return loopLive(r) }

// loopLive reports whether r's loop device is still backed by r's image, so
// a reused /dev/loopN is not mistaken for zenvik's. Other devices are not
// checked.
func loopLive(r Record) bool {
	m := loopDeviceRE.FindStringSubmatch(r.Device)
	if m == nil {
		return true
	}
	b, err := readSysFile(filepath.Join("/sys/block", m[1], "loop", "backing_file"))
	if err != nil {
		return false
	}
	// The kernel appends " (deleted)" when the image file was removed while
	// the device is still attached; the mount is still live then.
	backing := filepath.Clean(strings.TrimSuffix(strings.TrimSpace(string(b)), " (deleted)"))
	if backing == filepath.Clean(r.Image) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(r.Image)
	return err == nil && backing == filepath.Clean(resolved)
}
