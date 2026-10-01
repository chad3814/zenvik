package udf_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func sampleNames() []string {
	var names []string
	for n := range sample {
		names = append(names, n)
	}
	return names
}

func TestFSConformance(t *testing.T) {
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			fsys := openImage(t, buildImage(t, sample, l.opt))
			if err := fstest.TestFS(fsys, sampleNames()...); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFSContents(t *testing.T) {
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			fsys := openImage(t, buildImage(t, sample, l.opt))
			for name, f := range sample {
				got, err := fs.ReadFile(fsys, name)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				if !bytes.Equal(got, f.Data) {
					t.Errorf("%s: contents differ (%d bytes, want %d)", name, len(got), len(f.Data))
				}
			}
			ents, err := fs.ReadDir(fsys, "BDMV")
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, e := range ents {
				names = append(names, e.Name())
			}
			want := []string{"BACKUP", "META", "MovieObject.bdmv", "PLAYLIST", "STREAM", "index.bdmv"}
			if !reflect.DeepEqual(names, want) {
				t.Errorf("ReadDir(BDMV) = %v, want %v", names, want)
			}
			if _, err := fsys.Open("BDMV/missing.bin"); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("missing file err = %v", err)
			}
			if _, err := fsys.Open("BDMV/index.bdmv/child"); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("path through file err = %v", err)
			}
			if _, err := fsys.Open("/BDMV"); !errors.Is(err, fs.ErrInvalid) {
				t.Errorf("invalid path err = %v", err)
			}
		})
	}
}

func TestReadAtAndSeek(t *testing.T) {
	fsys := openImage(t, buildImage(t, sample, layouts[2].opt))
	f, err := fsys.Open("BDMV/STREAM/00001.m2ts")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := sample["BDMV/STREAM/00001.m2ts"].Data
	ra := f.(io.ReaderAt)
	buf := make([]byte, 100)
	if n, err := ra.ReadAt(buf, 2000); n != 100 || err != nil || !bytes.Equal(buf, want[2000:2100]) {
		t.Errorf("ReadAt across block boundary = %d, %v", n, err)
	}
	if n, err := ra.ReadAt(buf, int64(len(want))-10); n != 10 || !errors.Is(err, io.EOF) {
		t.Errorf("ReadAt at end = %d, %v; want 10, EOF", n, err)
	}
	sk := f.(io.Seeker)
	if pos, err := sk.Seek(-17, io.SeekEnd); err != nil || pos != int64(len(want))-17 {
		t.Fatalf("Seek = %d, %v", pos, err)
	}
	rest, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(rest, want[len(want)-17:]) {
		t.Errorf("read after seek = %d bytes, %v", len(rest), err)
	}
}

func TestSparseHugeFileWithAllocationExtent(t *testing.T) {
	const size = 5 << 30 // 5 GiB: more than 4 GiB and five extents
	files := map[string]udfimage.File{
		"BDMV/STREAM/00005.m2ts": {SparseSize: size},
		"BDMV/index.bdmv":        {Data: []byte("INDX0200")},
	}
	for _, rev := range []uint16{0x0102, 0x0250} {
		img := buildImage(t, files, udfimage.Options{Revision: rev, MaxInlineADs: 2})
		fsys := openImage(t, img)
		info, err := fs.Stat(fsys, "BDMV/STREAM/00005.m2ts")
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != size {
			t.Errorf("rev %#x: size = %d, want %d", rev, info.Size(), size)
		}
		f, err := fsys.Open("BDMV/STREAM/00005.m2ts")
		if err != nil {
			t.Fatal(err)
		}
		buf := bytes.Repeat([]byte{0xAA}, 64)
		if n, err := f.(io.ReaderAt).ReadAt(buf, size/2+size/4); n != 64 || err != nil || !bytes.Equal(buf, make([]byte, 64)) {
			t.Errorf("rev %#x: sparse ReadAt = %d, %v, %x", rev, n, err, buf[:8])
		}
		f.Close()
		if got, err := fs.ReadFile(fsys, "BDMV/index.bdmv"); err != nil || string(got) != "INDX0200" {
			t.Errorf("rev %#x: index.bdmv = %q, %v", rev, got, err)
		}
	}
}

func TestTruncatedImageNeverReturnsWrongData(t *testing.T) {
	for _, l := range layouts {
		img := buildImage(t, sample, l.opt)
		for _, keep := range []int{len(img) - 2*2048, len(img) - 4*2048, len(img) / 2, 300 * 2048} {
			if keep <= 0 || keep >= len(img) {
				continue
			}
			cut := img[:keep]
			fsys, err := udf.Open(bytes.NewReader(cut), int64(len(cut)))
			if err != nil {
				continue // refusing to open a damaged image is acceptable
			}
			for name, f := range sample {
				got, err := fs.ReadFile(fsys, name)
				if err == nil && !bytes.Equal(got, f.Data) {
					t.Errorf("%s keep=%d: %s returned wrong data without an error", l.name, keep, name)
				}
			}
		}
	}
	// In the non-embedded 2.50 layout, index.bdmv's data block is the last
	// block before the trailing anchor, so cutting two sectors removes it.
	img := buildImage(t, sample, layouts[2].opt)
	cut := img[:len(img)-2*2048]
	fsys := openImage(t, cut)
	if _, err := fs.ReadFile(fsys, "BDMV/index.bdmv"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated data read err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestOpenImage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "disc.iso")
	if err := os.WriteFile(p, buildImage(t, sample, layouts[2].opt), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := udf.OpenImage(p)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if got, err := fs.ReadFile(img, "BDMV/index.bdmv"); err != nil || string(got) != "INDX0200 index" {
		t.Errorf("ReadFile = %q, %v", got, err)
	}
	if _, err := udf.OpenImage(filepath.Join(t.TempDir(), "missing.iso")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing image err = %v", err)
	}
}

func FuzzOpen(f *testing.F) {
	f.Add(buildImage(f, map[string]udfimage.File{"A/b.txt": {Data: []byte("hi")}}, udfimage.Options{Revision: 0x0102}))
	f.Add(buildImage(f, map[string]udfimage.File{"A/b.txt": {Data: []byte("hi")}}, udfimage.Options{Revision: 0x0250, Embed: true}))
	f.Fuzz(func(t *testing.T, img []byte) {
		fsys, err := udf.Open(bytes.NewReader(img), int64(len(img)))
		if err != nil {
			return
		}
		walk(fsys, ".", 0)
	})
}

// walk visits a bounded part of the tree, reading up to 1 MiB per file.
// The depth limit keeps hostile directory cycles from recursing forever.
func walk(fsys fs.FS, dir string, depth int) {
	if depth > 6 {
		return
	}
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return
	}
	for i, e := range ents {
		if i > 64 {
			return
		}
		p := e.Name()
		if dir != "." {
			p = dir + "/" + p
		}
		if !fs.ValidPath(p) {
			continue
		}
		if e.IsDir() {
			walk(fsys, p, depth+1)
			continue
		}
		if fl, err := fsys.Open(p); err == nil {
			_, _ = io.CopyN(io.Discard, fl, 1<<20)
			fl.Close()
		}
	}
}
