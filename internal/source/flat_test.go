package source

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/chad3814/zenvik/internal/testdisc"
)

// writeFlat writes the sample movie under a folder named FLAT_MOVIE and
// flattens it.
func writeFlat(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "FLAT_MOVIE")
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	if err := testdisc.Flatten(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestOpenFlat(t *testing.T) {
	root := writeFlat(t)
	for name, path := range map[string]string{
		"folder": root,
		"bdmv":   filepath.Join(root, "BDMV"),
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if s.Kind != FlatBDMVDir || s.Format != Bluray || s.Label != "FLAT_MOVIE" || s.Path != root {
				t.Errorf("got Kind %v, Format %v, Label %q, Path %q", s.Kind, s.Format, s.Label, s.Path)
			}
			assertBDMV(t, s)
			for _, p := range []string{"BDMV/PLAYLIST/00800.mpls", "BDMV/CLIPINF/00001.clpi", "BDMV/STREAM/00001.m2ts", "BDMV/MovieObject.bdmv", "BDMV/META/DL/bdmt_eng.xml"} {
				if _, err := fs.Stat(s.FS, p); err != nil {
					t.Errorf("Stat(%s): %v", p, err)
				}
			}
		})
	}
}

func TestFlatKindString(t *testing.T) {
	if got := FlatBDMVDir.String(); got != "BDMV folder (flattened)" {
		t.Errorf("String() = %q", got)
	}
}

func TestOpenFlatContentsMatch(t *testing.T) {
	root := writeFlat(t)
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for name, want := range testdisc.SampleMovie().Files() {
		got, err := fs.ReadFile(s.FS, name)
		if err != nil {
			t.Errorf("ReadFile(%s): %v", name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: contents differ", name)
		}
	}
}

func TestOpenFlatFS(t *testing.T) {
	s, err := Open(writeFlat(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var want []string
	for name := range testdisc.SampleMovie().Files() {
		want = append(want, name)
	}
	if err := fstest.TestFS(s.FS, want...); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFlatNoBDMVDir(t *testing.T) {
	for _, index := range []string{"BDMV_index.bdmv", "index.bdmv"} {
		t.Run(index, func(t *testing.T) {
			root := writeFlat(t)
			if err := os.RemoveAll(filepath.Join(root, "BDMV")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(root, "BDMV_index.bdmv")); err != nil {
				t.Fatal(err)
			}
			want := testdisc.SampleMovie().Files()["BDMV/index.bdmv"]
			if err := os.WriteFile(filepath.Join(root, index), want, 0o644); err != nil {
				t.Fatal(err)
			}
			s, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if s.Kind != FlatBDMVDir {
				t.Errorf("Kind = %v", s.Kind)
			}
			got, err := fs.ReadFile(s.FS, "BDMV/index.bdmv")
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("index: err %v, equal %v", err, bytes.Equal(got, want))
			}
			// MovieObject.bdmv now exists only as the backup copy.
			if _, err := fs.Stat(s.FS, "BDMV/MovieObject.bdmv"); err != nil {
				t.Errorf("MovieObject.bdmv: %v", err)
			}
		})
	}
}

func TestOpenFlatBackups(t *testing.T) {
	root := writeFlat(t)
	// 00800 survives only as its backup; 00801's primary differs from its
	// backup and must win.
	if err := os.Remove(filepath.Join(root, "00800.mpls")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "00801.mpls"), []byte("primary"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := testdisc.SampleMovie().Files()["BDMV/PLAYLIST/00800.mpls"]
	if got, err := fs.ReadFile(s.FS, "BDMV/PLAYLIST/00800.mpls"); err != nil || !bytes.Equal(got, want) {
		t.Errorf("00800 from backup: err %v", err)
	}
	if got, err := fs.ReadFile(s.FS, "BDMV/PLAYLIST/00801.mpls"); err != nil || string(got) != "primary" {
		t.Errorf("00801 = %q, %v; want the primary copy", got, err)
	}
	files := s.Files()
	if got := files["BDMV/PLAYLIST/00800.mpls"]; got != filepath.Join(root, "00800.1.mpls") {
		t.Errorf("Files()[00800] = %q", got)
	}
	// The stat name is the standard name, not the backup's.
	fi, err := fs.Stat(s.FS, "BDMV/PLAYLIST/00800.mpls")
	if err != nil || fi.Name() != "00800.mpls" {
		t.Errorf("Stat name = %v, %v", fi, err)
	}
}

func TestFlatFiles(t *testing.T) {
	root := writeFlat(t)
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	files := s.Files()
	if len(files) != len(testdisc.SampleMovie().Files()) {
		t.Errorf("Files() has %d entries, want %d", len(files), len(testdisc.SampleMovie().Files()))
	}
	for std, real := range files {
		if !filepath.IsAbs(real) {
			t.Errorf("%s → %s is not absolute", std, real)
		}
		if _, err := os.Stat(real); err != nil {
			t.Errorf("%s → %s: %v", std, real, err)
		}
	}
	if got := files["BDMV/index.bdmv"]; got != filepath.Join(root, "BDMV", "index.bdmv") {
		t.Errorf("index → %q, want the copy under BDMV", got)
	}
	files["BDMV/index.bdmv"] = "changed"
	if s.Files()["BDMV/index.bdmv"] == "changed" {
		t.Error("Files() returned the internal map")
	}

	normal, err := Open(writeSample(t))
	if err != nil {
		t.Fatal(err)
	}
	defer normal.Close()
	if normal.Files() != nil {
		t.Error("Files() of a normal folder is not nil")
	}
}

func TestOpenFlatRejects(t *testing.T) {
	t.Run("playlists without an index", func(t *testing.T) {
		root := t.TempDir()
		writeFiles(t, root, map[string]string{"00001.mpls": "x", "00001.m2ts": "x"})
		if _, err := Open(root); !errors.Is(err, ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("index without playlists", func(t *testing.T) {
		root := t.TempDir()
		writeFiles(t, root, map[string]string{"BDMV_index.bdmv": "x", "00001.m2ts": "x", "1234.mpls": "x", "00001.MPLS": "x"})
		if _, err := Open(root); !errors.Is(err, ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("PLAYLIST directory present", func(t *testing.T) {
		root := writeSample(t)
		writeFiles(t, root, map[string]string{"00009.mpls": "x"})
		s, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if s.Kind != BDMVDir {
			t.Errorf("Kind = %v, want BDMVDir", s.Kind)
		}
	})
	t.Run("empty BDMV folder stays BDMVDir", func(t *testing.T) {
		root := t.TempDir()
		writeFiles(t, root, map[string]string{"BDMV/index.bdmv": "x"})
		s, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if s.Kind != BDMVDir {
			t.Errorf("Kind = %v, want BDMVDir", s.Kind)
		}
	})
}

func TestFlatFSErrors(t *testing.T) {
	s, err := Open(writeFlat(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for name, want := range map[string]error{
		"BDMV/PLAYLIST/99999.mpls": fs.ErrNotExist,
		"00800.mpls":               fs.ErrNotExist,
		"BDMV/BDJO":                fs.ErrNotExist,
		"../x":                     fs.ErrInvalid,
		"/BDMV":                    fs.ErrInvalid,
	} {
		_, err := s.FS.Open(name)
		var pe *fs.PathError
		if !errors.Is(err, want) || !errors.As(err, &pe) {
			t.Errorf("Open(%q) = %v, want %v as *fs.PathError", name, err, want)
		}
	}
	if _, err := fs.ReadDir(s.FS, "BDMV/PLAYLIST/00800.mpls"); err == nil || !strings.Contains(err.Error(), "00800.mpls") {
		t.Errorf("ReadDir of a file: %v", err)
	}
}
