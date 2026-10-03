package main

import (
	"path/filepath"
	"slices"
	"strings"
)

// windowsChars can't appear in a Windows file or folder name.
const windowsChars = `:*?"<>|`

const msgWindowsChars = `names can't contain : * ? " < > | on Windows`

// outputPath joins a disc's output folder and a name from the titles list.
// The name is a relative path (the template may add subfolders) that must
// stay inside the folder, so a ".." part is refused even where it would
// clean away; ".mkv" is added unless the name already ends in it. On Windows
// the characters Windows forbids in names are refused too. It returns the
// path, or "" and why the name can't be used.
func outputPath(goos, dir, name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "the name is empty"
	}
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.HasPrefix(name, "/") {
		return "", "use a name, not a full path"
	}
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == filepath.Separator })
	if slices.Contains(parts, "..") {
		return "", "the name must stay inside the output folder"
	}
	if goos == "windows" && strings.ContainsAny(name, windowsChars) {
		return "", msgWindowsChars
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." {
		return "", "the name is empty"
	}
	return filepath.Join(dir, withMKV(clean)), ""
}

func withMKV(name string) string {
	if strings.EqualFold(filepath.Ext(name), ".mkv") {
		return name
	}
	return name + ".mkv"
}

// fileNameError checks a queue entry's new file name (the folder stays).
func fileNameError(goos, name string) string {
	n := strings.TrimSpace(name)
	switch {
	case n == "" || n == "." || n == "..":
		return "the name is empty"
	case strings.ContainsAny(n, `/\`):
		return "a file name can't contain a folder"
	case goos == "windows" && strings.ContainsAny(n, windowsChars):
		return msgWindowsChars
	}
	return ""
}
