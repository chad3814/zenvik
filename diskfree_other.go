//go:build !(darwin || linux || freebsd || dragonfly || windows)

package zenvik

// freeBytes can't measure free space here; the copy path skips the check.
func freeBytes(string) (uint64, bool) { return 0, false }
