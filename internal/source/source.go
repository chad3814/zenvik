// Package source resolves a user-supplied path (an ISO image, a folder
// containing BDMV, or a BDMV folder) to a file tree whose root holds BDMV.
package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/chad3814/zenvik/udf"
)

// ErrUnsupported reports a path that is not a Blu-ray image or folder.
var ErrUnsupported = errors.New("zenvik: unsupported source")

// Kind is the type of source.
type Kind int

// Source kinds.
const (
	ISO     Kind = 1
	BDMVDir Kind = 2
)

func (k Kind) String() string {
	switch k {
	case ISO:
		return "ISO image"
	case BDMVDir:
		return "BDMV folder"
	}
	return "unknown"
}

// Source is an opened Blu-ray file tree.
type Source struct {
	Kind  Kind
	Path  string // the ISO file, or the folder containing BDMV
	Label string // UDF volume identifier, or the folder's name
	FS    fs.FS  // root contains BDMV/index.bdmv
	close func() error
}

// Close releases the source.
func (s *Source) Close() error {
	if s.close == nil {
		return nil
	}
	err := s.close()
	s.close = nil
	return err
}

// Open resolves path to a Blu-ray source.
func Open(path string) (*Source, error) {
	clean := filepath.Clean(path)
	st, err := os.Stat(clean)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return openDir(clean)
	}
	return openImage(clean)
}

func openDir(dir string) (*Source, error) {
	root := dir
	if filepath.Base(dir) == "BDMV" && isFile(filepath.Join(dir, "index.bdmv")) {
		root = filepath.Dir(dir)
	}
	fsys := os.DirFS(root)
	if !isFile(filepath.Join(root, "BDMV", "index.bdmv")) {
		return nil, explain(fsys, root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Source{Kind: BDMVDir, Path: root, Label: filepath.Base(abs), FS: fsys}, nil
}

func openImage(name string) (*Source, error) {
	img, err := udf.OpenImage(name)
	switch {
	case errors.Is(err, udf.ErrNotUDF):
		return nil, fmt.Errorf("%w: %s is not a UDF disc image", ErrUnsupported, name)
	case errors.Is(err, udf.ErrUnsupported):
		return nil, fmt.Errorf("%w: %s: %w", ErrUnsupported, name, err)
	case err != nil:
		return nil, err
	}
	if _, err := fs.Stat(img, "BDMV/index.bdmv"); err != nil {
		img.Close()
		if errors.Is(err, fs.ErrNotExist) {
			return nil, explain(img, name)
		}
		return nil, err
	}
	return &Source{Kind: ISO, Path: name, Label: img.Label(), FS: img, close: img.Close}, nil
}

// explain builds the ErrUnsupported error for a tree without BDMV.
func explain(fsys fs.FS, where string) error {
	if st, err := fs.Stat(fsys, "VIDEO_TS"); err == nil && st.IsDir() {
		return fmt.Errorf("%w: %s is a DVD (VIDEO_TS); DVD support is planned but not available yet", ErrUnsupported, where)
	}
	return fmt.Errorf("%w: %s has no BDMV/index.bdmv", ErrUnsupported, where)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
