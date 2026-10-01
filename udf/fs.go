package udf

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

var errIsDir = errors.New("is a directory")

// Open implements fs.FS.
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	e, err := f.lookup(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return &file{fs: f, e: e, name: path.Base(name)}, nil
}

// lookup resolves a valid path. It tracks the entries resolved along the
// path so a directory cycle is reported as corruption rather than walked
// forever.
func (f *FS) lookup(name string) (*entry, error) {
	e := f.root
	if name == "." {
		return e, nil
	}
	seen := map[entryAddr]bool{e.addr: true}
	for _, part := range strings.Split(name, "/") {
		if e.fileType != fileTypeDirectory {
			return nil, fs.ErrNotExist
		}
		ents, err := e.readDir()
		if err != nil {
			return nil, err
		}
		var next *entry
		for _, de := range ents {
			if de.name == part {
				if next, err = f.readEntry(de.icb.ref, de.icb.block); err != nil {
					return nil, err
				}
				break
			}
		}
		if next == nil {
			return nil, fs.ErrNotExist
		}
		if seen[next.addr] {
			return nil, fmt.Errorf("%w: directory cycle at %q", ErrCorrupt, part)
		}
		seen[next.addr] = true
		e = next
	}
	return e, nil
}

type file struct {
	fs     *FS
	e      *entry
	name   string
	off    int64
	ents   []fs.DirEntry
	loaded bool
	dirPos int
}

func (fl *file) Stat() (fs.FileInfo, error) { return &fileInfo{name: fl.name, e: fl.e}, nil }

func (fl *file) Close() error { return nil }

func (fl *file) Read(p []byte) (int, error) {
	if fl.e.fileType == fileTypeDirectory {
		return 0, &fs.PathError{Op: "read", Path: fl.name, Err: errIsDir}
	}
	n, err := fl.e.ReadAt(p, fl.off)
	fl.off += int64(n)
	return n, err
}

func (fl *file) ReadAt(p []byte, off int64) (int, error) {
	if fl.e.fileType == fileTypeDirectory {
		return 0, &fs.PathError{Op: "read", Path: fl.name, Err: errIsDir}
	}
	return fl.e.ReadAt(p, off)
}

func (fl *file) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += fl.off
	case io.SeekEnd:
		offset += fl.e.size
	default:
		return 0, &fs.PathError{Op: "seek", Path: fl.name, Err: fs.ErrInvalid}
	}
	if offset < 0 {
		return 0, &fs.PathError{Op: "seek", Path: fl.name, Err: fs.ErrInvalid}
	}
	fl.off = offset
	return offset, nil
}

// ReadDir implements fs.ReadDirFile; entries are sorted by name.
func (fl *file) ReadDir(n int) ([]fs.DirEntry, error) {
	if !fl.loaded {
		ents, err := fl.e.readDir()
		if err != nil {
			return nil, &fs.PathError{Op: "readdir", Path: fl.name, Err: err}
		}
		for _, de := range ents {
			fl.ents = append(fl.ents, &dirEntry{fs: fl.fs, d: de})
		}
		sort.Slice(fl.ents, func(i, j int) bool { return fl.ents[i].Name() < fl.ents[j].Name() })
		fl.loaded = true
	}
	rest := fl.ents[fl.dirPos:]
	if n <= 0 {
		fl.dirPos = len(fl.ents)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	rest = rest[:min(n, len(rest))]
	fl.dirPos += len(rest)
	return rest, nil
}

type dirEntry struct {
	fs *FS
	d  dirent
}

func (de *dirEntry) Name() string { return de.d.name }
func (de *dirEntry) IsDir() bool  { return de.d.dir }

func (de *dirEntry) Type() fs.FileMode {
	if de.d.dir {
		return fs.ModeDir
	}
	return 0
}

func (de *dirEntry) Info() (fs.FileInfo, error) {
	e, err := de.fs.readEntry(de.d.icb.ref, de.d.icb.block)
	if err != nil {
		return nil, err
	}
	return &fileInfo{name: de.d.name, e: e}, nil
}

func (de *dirEntry) String() string { return fs.FormatDirEntry(de) }

type fileInfo struct {
	name string
	e    *entry
}

func (fi *fileInfo) Name() string       { return fi.name }
func (fi *fileInfo) Size() int64        { return fi.e.size }
func (fi *fileInfo) ModTime() time.Time { return fi.e.modTime }
func (fi *fileInfo) IsDir() bool        { return fi.e.fileType == fileTypeDirectory }
func (fi *fileInfo) Sys() any           { return nil }
func (fi *fileInfo) String() string     { return fs.FormatFileInfo(fi) }

func (fi *fileInfo) Mode() fs.FileMode {
	if fi.IsDir() {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
