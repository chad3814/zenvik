package main

import (
	"os/exec"
	"path/filepath"
)

// revealArgs is the command that shows path in the OS file manager: selected
// in Finder or Explorer, or its folder opened on Linux.
func revealArgs(goos, path string) []string {
	switch goos {
	case "darwin":
		return []string{"open", "-R", path}
	case "windows":
		return []string{"explorer", "/select," + path}
	}
	return []string{"xdg-open", filepath.Dir(path)}
}

func reveal(goos, path string) error {
	args := revealArgs(goos, path)
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // explorer exits 1 even on success
	return nil
}
