//go:build unix

package mount

import (
	"os"
	"path/filepath"
	"syscall"
)

// isMountPoint reports whether dir exists and is on a different device than
// its parent directory. If either cannot be examined, it is not.
func isMountPoint(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil {
		return false
	}
	pi, err := os.Stat(filepath.Dir(filepath.Clean(dir)))
	if err != nil {
		return false
	}
	st, ok1 := fi.Sys().(*syscall.Stat_t)
	pt, ok2 := pi.Sys().(*syscall.Stat_t)
	return ok1 && ok2 && st.Dev != pt.Dev
}
