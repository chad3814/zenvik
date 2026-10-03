package main

import (
	"os/exec"
	"path/filepath"
)

// revealArgs is the command that shows path in the OS file manager: selected
// in Finder or Explorer, or its folder opened on Linux. On Windows the
// command line actually run is explorerCmdLine's (see reveal_windows.go).
func revealArgs(goos, path string) []string {
	switch goos {
	case "darwin":
		return []string{"open", "-R", path}
	case "windows":
		return []string{"explorer", "/select," + path}
	}
	return []string{"xdg-open", filepath.Dir(path)}
}

// explorerCmdLine is Explorer's select command for path. Explorer needs the
// quotes around the path alone (/select,"C:\a b.mkv"); Go's argument quoting
// would quote all of "/select,C:\a b.mkv", and Explorer then ignores the
// selection and opens a default folder.
func explorerCmdLine(path string) string {
	return `explorer /select,"` + path + `"`
}

func reveal(goos, path string) error {
	args := revealArgs(goos, path)
	cmd := exec.Command(args[0], args[1:]...)
	setCmdLine(cmd, goos, path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck // explorer exits 1 even on success
	return nil
}
