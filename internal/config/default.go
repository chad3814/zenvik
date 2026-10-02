package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// DefaultFileContent is the config file `zenvik doctor` writes when none
// exists: every top-level key set to its built-in default, with comments,
// and a commented-out example preset.
func DefaultFileContent() string {
	return fmt.Sprintf(`# zenvik configuration — written by `+"`zenvik doctor`"+`.
# Every key is optional; delete one to use zenvik's built-in default.

# Where MKV files go ("~" is your home directory).
output_dir = %s

# Output name; [ ... ] is dropped when a variable in it is empty.
template = %s

# Shorter titles are never the main feature.
min_duration = %s

# mkvmerge executable; empty searches PATH.
mkvmerge_path = ""

# preset = "plex"
#
# [presets.plex]
# output_dir = "~/Movies"
# template = "{name} ({year})/{name} ({year}).mkv"
`, strconv.Quote("."), strconv.Quote(DefaultTemplate), strconv.Quote(formatDuration(DefaultMinDuration)))
}

// formatDuration renders d the way a person would write it in the config
// file: whole minutes as "2m", anything else as time.Duration prints it.
func formatDuration(d time.Duration) string {
	if d > 0 && d%time.Minute == 0 {
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	}
	return d.String()
}

// WriteDefault creates the config file at path with DefaultFileContent,
// creating its directory if needed. It never replaces an existing file:
// if path exists, the error satisfies errors.Is(err, fs.ErrExist).
func WriteDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(DefaultFileContent())
	if err := errors.Join(werr, f.Close()); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
