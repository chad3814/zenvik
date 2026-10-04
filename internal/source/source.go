// Package source resolves a user-supplied path (a disc image, a folder containing BDMV or VIDEO_TS, a BDMV or VIDEO_TS folder, or a flattened BDMV folder) to a file tree whose root holds BDMV or VIDEO_TS.
package source

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/chad3814/zenvik/udf"
)

// ErrUnsupported reports a path that is not a Blu-ray or DVD image or folder.
var ErrUnsupported = errors.New("zenvik: unsupported source")

// Kind is the type of source.
type Kind int

// Source kinds.
const (
	ISO     Kind = 1
	BDMVDir Kind = 2
)

// VideoTSDir is a VIDEO_TS folder (or a folder that contains one).
const VideoTSDir Kind = 3

// FlatBDMVDir is a folder whose BDMV tree was flattened: the files of
// PLAYLIST, CLIPINF, STREAM and META/DL sit at its top level.
const FlatBDMVDir Kind = 4

// Format is the disc format.
type Format int

// Disc formats.
const (
	Bluray Format = 1
	DVD    Format = 2
)

func (f Format) String() string {
	switch f {
	case Bluray:
		return "Blu-ray"
	case DVD:
		return "DVD"
	}
	return "unknown"
}

func (k Kind) String() string {
	switch k {
	case ISO:
		return "ISO image"
	case BDMVDir:
		return "BDMV folder"
	case VideoTSDir:
		return "VIDEO_TS folder"
	case FlatBDMVDir:
		return "BDMV folder (flattened)"
	}
	return "unknown"
}

// Source is an opened disc file tree.
type Source struct {
	Kind    Kind
	Format  Format
	Path    string            // the ISO file, or the folder containing BDMV or VIDEO_TS
	Label   string            // UDF volume identifier, or the folder's name
	FS      fs.FS             // root contains BDMV/index.bdmv or <VideoTS>/VIDEO_TS.IFO
	Image   *udf.Image        // the opened UDF image (ISO only)
	VideoTS string            // name of the VIDEO_TS directory in FS (DVD only)
	files   map[string]string // FlatBDMVDir: standard path → absolute real path
	close   func() error
}

// Files returns a FlatBDMVDir source's files: standard tree paths such as
// "BDMV/PLAYLIST/00001.mpls" mapped to the absolute paths of the real
// files. It returns nil for other kinds.
func (s *Source) Files() map[string]string {
	return maps.Clone(s.files)
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
	switch {
	case filepath.Base(dir) == "BDMV" && isFile(filepath.Join(dir, "index.bdmv")):
		root = filepath.Dir(dir)
	case strings.EqualFold(filepath.Base(dir), "VIDEO_TS") && FindName(os.DirFS(dir), ".", "VIDEO_TS.IFO", false) != "":
		root = filepath.Dir(dir)
	}
	fsys := os.DirFS(root)
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	hasIndex := isFile(filepath.Join(root, "BDMV", "index.bdmv"))
	if hasIndex && isDir(filepath.Join(root, "BDMV", "PLAYLIST")) {
		return &Source{Kind: BDMVDir, Format: Bluray, Path: root, Label: filepath.Base(abs), FS: fsys}, nil
	}
	flat, err := flatFiles(root)
	if err != nil {
		return nil, err
	}
	if flat != nil {
		files := make(map[string]string, len(flat))
		for std, rel := range flat {
			files[std] = filepath.Join(abs, filepath.FromSlash(rel))
		}
		return &Source{Kind: FlatBDMVDir, Format: Bluray, Path: root, Label: filepath.Base(abs), FS: newFlatFS(root, flat), files: files}, nil
	}
	if hasIndex {
		return &Source{Kind: BDMVDir, Format: Bluray, Path: root, Label: filepath.Base(abs), FS: fsys}, nil
	}
	if vts := videoTSDir(fsys); vts != "" {
		return &Source{Kind: VideoTSDir, Format: DVD, Path: root, Label: filepath.Base(abs), FS: fsys, VideoTS: vts}, nil
	}
	return nil, explain(root)
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
	_, err = fs.Stat(img, "BDMV/index.bdmv")
	if err == nil {
		return &Source{Kind: ISO, Format: Bluray, Path: name, Label: img.Label(), FS: img, Image: img, close: img.Close}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		img.Close()
		return nil, err
	}
	if vts := videoTSDir(img); vts != "" {
		return &Source{Kind: ISO, Format: DVD, Path: name, Label: img.Label(), FS: img, Image: img, VideoTS: vts, close: img.Close}, nil
	}
	img.Close()
	return nil, explain(name)
}

// explain builds the ErrUnsupported error for a tree that is neither a
// Blu-ray nor a DVD.
func explain(where string) error {
	return fmt.Errorf("%w: %s has no BDMV/index.bdmv or VIDEO_TS/VIDEO_TS.IFO", ErrUnsupported, where)
}

// videoTSDir returns the name of fsys's VIDEO_TS directory if it holds
// VIDEO_TS.IFO (both matched ignoring case), or "".
func videoTSDir(fsys fs.FS) string {
	d := FindName(fsys, ".", "VIDEO_TS", true)
	if d == "" || FindName(fsys, d, "VIDEO_TS.IFO", false) == "" {
		return ""
	}
	return d
}

// FindName returns the name of the entry of directory dir in fsys that
// equals name ignoring case, preferring an exact match, or "" if there is
// none or dir can't be read. wantDir selects directories (true) or
// non-directories (false). AppleDouble "._" entries never match.
func FindName(fsys fs.FS, dir, name string, wantDir bool) string {
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return ""
	}
	found := ""
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "._") || e.IsDir() != wantDir || !strings.EqualFold(n, name) {
			continue
		}
		if n == name {
			return n
		}
		if found == "" {
			found = n
		}
	}
	return found
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
