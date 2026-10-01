//go:build !linux

package mount

// deviceLive is true: only Linux loop devices are checked.
func deviceLive(Record) bool { return true }
