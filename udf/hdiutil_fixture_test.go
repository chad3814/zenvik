package udf_test

import (
	"bytes"
	"io/fs"
	"testing"

	"github.com/chad3814/zenvik/udf"
)

// TestHdiutilFixture reads an image written by macOS hdiutil, an
// independent UDF implementation (see scripts/gen-udf-fixtures.sh).
func TestHdiutilFixture(t *testing.T) {
	img, err := udf.OpenImage("testdata/hdiutil-udf102.iso")
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if img.Label() != "ZENVIK_HDIUTIL" {
		t.Errorf("Label = %q", img.Label())
	}
	want := map[string][]byte{
		"BDMV/PLAYLIST/00800.mpls": []byte("MPLS0200"),
		"BDMV/hello.txt":           []byte("hello from hdiutil\n"),
		"BDMV/STREAM/00001.m2ts":   bytes.Repeat([]byte("z"), 300000),
	}
	for name, data := range want {
		got, err := fs.ReadFile(img, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !bytes.Equal(got, data) {
			t.Errorf("%s: %d bytes, want %d", name, len(got), len(data))
		}
	}
}
