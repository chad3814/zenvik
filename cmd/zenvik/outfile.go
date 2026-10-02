package main

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chad3814/zenvik/internal/config"
)

// isAbsPath reports whether an --output-file path is absolute under goos's
// rules: on Windows, a drive letter ("C:\x", "C:x"), or a leading \ or /
// (which also covers UNC paths); elsewhere, a leading /.
func isAbsPath(p, goos string) bool {
	if goos == "windows" {
		if len(p) >= 2 && p[1] == ':' && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z') {
			return true
		}
		return strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "/")
	}
	return strings.HasPrefix(p, "/")
}

// outputFilePath resolves an --output-file argument. It is used as given,
// with no templating or name cleaning: a leading "~" is the home
// directory, an absolute path stands alone, and a relative path is joined
// to the output directory (which it may leave with "..").
func outputFilePath(p, outputDir string) (string, error) {
	if p == "" {
		return "", usageError{errors.New("--output-file needs a path")}
	}
	if strings.HasSuffix(p, "/") || strings.HasSuffix(p, string(filepath.Separator)) {
		return "", usageError{errors.New("--output-file must name a file, not a directory: " + p)}
	}
	p, err := config.ExpandHome(p)
	if err != nil {
		return "", err
	}
	if isAbsPath(p, runtime.GOOS) {
		return filepath.Clean(p), nil
	}
	return filepath.Join(outputDir, p), nil
}
