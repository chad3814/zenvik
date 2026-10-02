//go:build darwin || linux || freebsd || dragonfly

package zenvik

import "syscall"

// freeBytes reports the bytes available to unprivileged users in dir.
func freeBytes(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true //nolint:unconvert // Bavail is int64 on freebsd and dragonfly
}
