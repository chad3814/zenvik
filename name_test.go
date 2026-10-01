package zenvik_test

import (
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestDiscName(t *testing.T) {
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	if got := d.Name(); got != "Sample Movie" {
		t.Errorf("Name with meta = %q", got)
	}

	noMeta := testdisc.SampleMovie()
	noMeta.MetaTitle = ""
	root := filepath.Join(t.TempDir(), "THE_MATRIX_1999")
	if err := noMeta.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	if got := openDisc(t, root).Name(); got != "The Matrix 1999" {
		t.Errorf("Name from label = %q", got)
	}

	blank := filepath.Join(t.TempDir(), "___")
	if err := noMeta.WriteDir(blank); err != nil {
		t.Fatal(err)
	}
	if got := openDisc(t, blank).Name(); got != "untitled" {
		t.Errorf("Name with blank label = %q", got)
	}
}
