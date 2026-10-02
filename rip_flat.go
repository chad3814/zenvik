package zenvik

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

// symlink is os.Symlink; tests replace it to simulate failures.
var symlink = os.Symlink

// linkTree builds a standard BDMV tree for mkvmerge from a flattened
// folder's files (standard path → absolute real path): a temporary
// directory of symlinks to the real files. It returns the directory and a
// function that removes it, which never touches the link targets.
func linkTree(files map[string]string) (string, func() error, error) {
	dir, err := os.MkdirTemp("", "zenvik-bdmv-*")
	if err != nil {
		return "", nil, err
	}
	release := func() error { return os.RemoveAll(dir) }
	for _, std := range slices.Sorted(maps.Keys(files)) {
		p := filepath.Join(dir, filepath.FromSlash(std))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", nil, errors.Join(err, release())
		}
		if err := symlink(files[std], p); err != nil {
			err = fmt.Errorf("zenvik: can't link %s into a temporary BDMV tree (rebuild the BDMV folder layout and pass that instead): %w", files[std], err)
			return "", nil, errors.Join(err, release())
		}
	}
	return dir, release, nil
}
