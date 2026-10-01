package source

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func writeSample(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_MOVIE")
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertBDMV(t *testing.T, s *Source) {
	t.Helper()
	if _, err := fs.Stat(s.FS, "BDMV/index.bdmv"); err != nil {
		t.Errorf("FS has no BDMV/index.bdmv: %v", err)
	}
}

func TestOpenPathVariants(t *testing.T) {
	root := writeSample(t)
	for name, path := range map[string]string{
		"parent":         root,
		"bdmv":           filepath.Join(root, "BDMV"),
		"trailing slash": root + string(filepath.Separator),
		"bdmv slash":     filepath.Join(root, "BDMV") + string(filepath.Separator),
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if s.Kind != BDMVDir || s.Label != "SAMPLE_MOVIE" || filepath.Clean(s.Path) != root {
				t.Errorf("got Kind %v, Label %q, Path %q", s.Kind, s.Label, s.Path)
			}
			assertBDMV(t, s)
		})
	}

	t.Run("relative dot", func(t *testing.T) {
		t.Chdir(root)
		s, err := Open(".")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if s.Label != "SAMPLE_MOVIE" {
			t.Errorf("Label = %q", s.Label)
		}
		assertBDMV(t, s)
	})

	t.Run("file inside BDMV", func(t *testing.T) {
		_, err := Open(filepath.Join(root, "BDMV", "index.bdmv"))
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "not a UDF disc image") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, err := Open(filepath.Join(root, "nope"))
		if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("dvd folder", func(t *testing.T) {
		dvd := filepath.Join(t.TempDir(), "DVD")
		if err := os.MkdirAll(filepath.Join(dvd, "VIDEO_TS"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Open(dvd)
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "DVD") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("empty folder", func(t *testing.T) {
		_, err := Open(t.TempDir())
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "no BDMV/index.bdmv") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("random file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "junk.iso")
		if err := os.WriteFile(p, make([]byte, 1<<20), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Open(p)
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})
}

func TestOpenISO(t *testing.T) {
	for _, rev := range []uint16{0x0102, 0x0250} {
		img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: rev, Label: "SAMPLE_MOVIE"})
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "disc.iso")
		if err := os.WriteFile(p, img, 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Open(p)
		if err != nil {
			t.Fatalf("rev %#x: %v", rev, err)
		}
		if s.Kind != ISO || s.Label != "SAMPLE_MOVIE" || s.Path != p {
			t.Errorf("rev %#x: got Kind %v, Label %q, Path %q", rev, s.Kind, s.Label, s.Path)
		}
		assertBDMV(t, s)
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}
}

func TestOpenDVDImage(t *testing.T) {
	img, err := udfimage.Build(map[string]udfimage.File{"VIDEO_TS/VIDEO_TS.IFO": {Data: []byte("DVDVIDEO-VMG")}},
		udfimage.Options{Revision: 0x0102, Label: "SOME_DVD"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "dvd.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Open(p)
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "DVD") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenCorruptImage(t *testing.T) {
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0250, Label: "X"})
	if err != nil {
		t.Fatal(err)
	}
	img = img[:300*2048] // keeps the volume structure, cuts off the partition
	p := filepath.Join(t.TempDir(), "cut.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Open(p)
	if err == nil || errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want a non-unsupported error", err)
	}
	if err != nil && !errors.Is(err, udf.ErrCorrupt) && !errors.Is(err, udf.ErrNotUDF) {
		t.Errorf("err = %v, want udf.ErrCorrupt or udf.ErrNotUDF", err)
	}
}

func TestKindString(t *testing.T) {
	if ISO.String() != "ISO image" || BDMVDir.String() != "BDMV folder" || Kind(0).String() != "unknown" {
		t.Error("Kind.String mismatch")
	}
}
