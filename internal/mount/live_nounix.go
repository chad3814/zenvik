//go:build !unix

package mount

import "os"

// isMountPoint reports whether dir exists; mount points can't be told apart
// from plain directories here.
func isMountPoint(dir string) bool {
	_, err := os.Stat(dir)
	return err == nil
}
