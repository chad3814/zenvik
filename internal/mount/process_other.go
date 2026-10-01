//go:build !unix && !windows

package mount

func processAlive(pid int) bool { return false }
