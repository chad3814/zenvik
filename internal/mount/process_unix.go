//go:build unix

package mount

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with this PID exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
