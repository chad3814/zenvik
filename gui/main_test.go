package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points XDG config and state at temp dirs so no test touches the
// user's real zenvik config or saved queue.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zenvik-gui-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
