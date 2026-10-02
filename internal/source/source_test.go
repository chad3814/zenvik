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

	t.Run("empty folder", func(t *testing.T) {
		_, err := Open(t.TempDir())
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "no BDMV/index.bdmv or VIDEO_TS/VIDEO_TS.IFO") {
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

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenVideoTSFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "My Movie")
	writeFiles(t, root, map[string]string{"VIDEO_TS/VIDEO_TS.IFO": "DVDVIDEO-VMG"})
	for _, path := range []string{root, filepath.Join(root, "VIDEO_TS")} {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open(%s): %v", path, err)
		}
		if s.Kind != VideoTSDir || s.Format != DVD || s.VideoTS != "VIDEO_TS" || s.Path != root || s.Label != "My Movie" {
			t.Errorf("Open(%s) = %+v", path, s)
		}
		if _, err := fs.Stat(s.FS, "VIDEO_TS/VIDEO_TS.IFO"); err != nil {
			t.Error(err)
		}
	}
}

func TestOpenVideoTSLowerCase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disc")
	writeFiles(t, root, map[string]string{"video_ts/video_ts.ifo": "DVDVIDEO-VMG", "._VIDEO_TS": "x"})
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.VideoTS != "video_ts" || s.Format != DVD {
		t.Errorf("Source = %+v", s)
	}
}

func TestOpenVideoTSWithoutIFO(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"VIDEO_TS/VTS_01_1.VOB": "x"})
	_, err := Open(root)
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "VIDEO_TS/VIDEO_TS.IFO") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenHybridPrefersBluray(t *testing.T) {
	root := t.TempDir()
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, root, map[string]string{"VIDEO_TS/VIDEO_TS.IFO": "DVDVIDEO-VMG"})
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Format != Bluray || s.Kind != BDMVDir || s.VideoTS != "" {
		t.Errorf("Source = %+v", s)
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
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Kind != ISO || s.Format != DVD || s.VideoTS != "VIDEO_TS" || s.Label != "SOME_DVD" {
		t.Errorf("Source = %+v", s)
	}
}

func TestFindName(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"Video_TS/vts_01_1.vob": "x", "Video_TS/._VTS_01_1.VOB": "x"})
	fsys := os.DirFS(root)
	if got := FindName(fsys, ".", "VIDEO_TS", true); got != "Video_TS" {
		t.Errorf("dir = %q", got)
	}
	if got := FindName(fsys, "Video_TS", "VTS_01_1.VOB", false); got != "vts_01_1.vob" {
		t.Errorf("file = %q", got)
	}
	if got := FindName(fsys, "Video_TS", "VTS_01_1.VOB", true); got != "" {
		t.Errorf("file found as dir: %q", got)
	}
	if got := FindName(fsys, "missing", "x", false); got != "" {
		t.Errorf("missing dir: %q", got)
	}
}

func TestKindString(t *testing.T) {
	if ISO.String() != "ISO image" || BDMVDir.String() != "BDMV folder" || VideoTSDir.String() != "VIDEO_TS folder" || Kind(0).String() != "unknown" {
		t.Error("Kind.String mismatch")
	}
	if Bluray.String() != "Blu-ray" || DVD.String() != "DVD" {
		t.Error("Format.String mismatch")
	}
}
