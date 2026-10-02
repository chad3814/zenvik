package source

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// flatFiles maps the standard tree paths of a flattened BDMV folder at root
// (one whose PLAYLIST, CLIPINF, STREAM and META/DL files sit at its top
// level, with BACKUP copies named "<name>.1.<ext>") to paths relative to
// root. It returns nil if root has a BDMV/PLAYLIST directory, no top-level
// playlist, or no index.
func flatFiles(root string) (map[string]string, error) {
	if isDir(filepath.Join(root, "BDMV", "PLAYLIST")) {
		return nil, nil
	}
	have := map[string]bool{} // relative, slash-separated
	top, err := fileNames(root)
	if err != nil {
		return nil, err
	}
	for _, n := range top {
		have[n] = true
	}
	inBDMV, err := fileNames(filepath.Join(root, "BDMV"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, n := range inBDMV {
		have["BDMV/"+n] = true
	}

	files := map[string]string{}
	playlists := false
	for _, n := range top {
		id, backup, ext, ok := splitClipName(n)
		switch {
		case !ok:
			if strings.HasPrefix(n, "bdmt_") && strings.HasSuffix(n, ".xml") {
				files["BDMV/META/DL/"+n] = n
			}
		case ext == "mpls" && (!backup || !have[id+".mpls"]):
			files["BDMV/PLAYLIST/"+id+".mpls"] = n
			playlists = true
		case ext == "clpi" && (!backup || !have[id+".clpi"]):
			files["BDMV/CLIPINF/"+id+".clpi"] = n
		case ext == "m2ts" && !backup:
			files["BDMV/STREAM/"+id+".m2ts"] = n
		}
	}
	if !playlists {
		return nil, nil
	}
	pick := func(std string, candidates ...string) {
		for _, c := range candidates {
			if have[c] {
				files[std] = c
				return
			}
		}
	}
	pick("BDMV/index.bdmv", "BDMV/index.bdmv", "BDMV_index.bdmv", "index.bdmv", "index.1.bdmv")
	if files["BDMV/index.bdmv"] == "" {
		return nil, nil
	}
	pick("BDMV/MovieObject.bdmv", "BDMV/MovieObject.bdmv", "BDMV_MovieObject.bdmv", "MovieObject.bdmv", "MovieObject.1.bdmv")
	return files, nil
}

// fileNames lists the non-directory entries of dir, skipping AppleDouble
// "._" files.
func fileNames(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), "._") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// splitClipName splits "00001.mpls" or the backup form "00001.1.mpls" into
// its five-digit ID, whether it is a backup, and its extension ("mpls",
// "clpi" or "m2ts", lower case only).
func splitClipName(n string) (id string, backup bool, ext string, ok bool) {
	if len(n) < 10 || n[5] != '.' {
		return "", false, "", false
	}
	id, rest := n[:5], n[6:]
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", false, "", false
		}
	}
	rest, backup = strings.CutPrefix(rest, "1.")
	switch rest {
	case "mpls", "clpi", "m2ts":
		return id, backup, rest, true
	}
	return "", false, "", false
}

// flatFS serves a flattened folder's files under their standard tree paths.
type flatFS struct {
	root  string            // the folder, in OS form
	files map[string]string // standard path → path relative to root
	dirs  map[string][]string
}

func newFlatFS(root string, files map[string]string) *flatFS {
	dirs := map[string][]string{".": nil}
	for std := range files {
		for p := std; p != "."; p = path.Dir(p) {
			parent, base := path.Dir(p), path.Base(p)
			if !slices.Contains(dirs[parent], base) {
				dirs[parent] = append(dirs[parent], base)
			}
		}
	}
	for d := range dirs {
		slices.Sort(dirs[d])
	}
	return &flatFS{root: root, files: files, dirs: dirs}
}

func (f *flatFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if rel, ok := f.files[name]; ok {
		file, err := os.Open(filepath.Join(f.root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, &fs.PathError{Op: "open", Path: name, Err: underlying(err)}
		}
		return &flatFile{File: file, name: path.Base(name)}, nil
	}
	if _, ok := f.dirs[name]; ok {
		ents, err := f.ReadDir(name)
		if err != nil {
			return nil, err
		}
		return &flatDir{info: dirInfo(path.Base(name)), ents: ents}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (f *flatFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if rel, ok := f.files[name]; ok {
		fi, err := os.Stat(filepath.Join(f.root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, &fs.PathError{Op: "stat", Path: name, Err: underlying(err)}
		}
		return renamedInfo{FileInfo: fi, name: path.Base(name)}, nil
	}
	if _, ok := f.dirs[name]; ok {
		return dirInfo(path.Base(name)), nil
	}
	return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
}

func (f *flatFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	children, ok := f.dirs[name]
	if !ok {
		err := fs.ErrNotExist
		if _, isFile := f.files[name]; isFile {
			err = errors.New("not a directory")
		}
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
	}
	ents := make([]fs.DirEntry, 0, len(children))
	for _, c := range children {
		fi, err := f.Stat(path.Join(name, c))
		if err != nil {
			return nil, err
		}
		ents = append(ents, fs.FileInfoToDirEntry(fi))
	}
	return ents, nil
}

// underlying unwraps an *fs.PathError so the flat FS can report its own
// path instead of the real one.
func underlying(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// flatFile is a real file reported under its standard name.
type flatFile struct {
	*os.File
	name string
}

func (f *flatFile) Stat() (fs.FileInfo, error) {
	fi, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return renamedInfo{FileInfo: fi, name: f.name}, nil
}

type renamedInfo struct {
	fs.FileInfo
	name string
}

func (r renamedInfo) Name() string { return r.name }

// dirInfo describes a synthesized directory.
type dirInfo string

func (d dirInfo) Name() string       { return string(d) }
func (d dirInfo) Size() int64        { return 0 }
func (d dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (d dirInfo) ModTime() time.Time { return time.Time{} }
func (d dirInfo) IsDir() bool        { return true }
func (d dirInfo) Sys() any           { return nil }

// flatDir is an open synthesized directory.
type flatDir struct {
	info dirInfo
	ents []fs.DirEntry
	off  int
}

func (d *flatDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *flatDir) Close() error               { return nil }

func (d *flatDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: string(d.info), Err: errors.New("is a directory")}
}

func (d *flatDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.ents[d.off:]
	if n <= 0 {
		d.off = len(d.ents)
		return slices.Clone(rest), nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	n = min(n, len(rest))
	d.off += n
	return slices.Clone(rest[:n]), nil
}
