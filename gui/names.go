package main

import (
	"path/filepath"
	"strings"
)

// outputPath joins a disc's output folder and a name from the titles list.
// The name is a relative path (the template may add subfolders) that must
// stay inside the folder; ".mkv" is added unless the name already ends in it.
// It returns the path, or "" and why the name can't be used.
func outputPath(dir, name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "the name is empty"
	}
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" || strings.HasPrefix(name, "/") {
		return "", "use a name, not a full path"
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	switch {
	case clean == ".":
		return "", "the name is empty"
	case clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)):
		return "", "the name must stay inside the output folder"
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
func fileNameError(name string) string {
	n := strings.TrimSpace(name)
	switch {
	case n == "" || n == "." || n == "..":
		return "the name is empty"
	case strings.ContainsAny(n, `/\`):
		return "a file name can't contain a folder"
	}
	return ""
}
