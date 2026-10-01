package mount

import "testing"

// fakeDirMounted makes every recorded mount directory look mounted (or not).
func fakeDirMounted(t *testing.T, mounted bool) {
	t.Helper()
	old := dirMounted
	dirMounted = func(string) bool { return mounted }
	t.Cleanup(func() { dirMounted = old })
}

// fakeLive makes every recorded mount look live (or not), including the
// device check.
func fakeLive(t *testing.T, live bool) {
	t.Helper()
	fakeDirMounted(t, live)
	old := deviceMatches
	deviceMatches = func(Record) bool { return live }
	t.Cleanup(func() { deviceMatches = old })
}
