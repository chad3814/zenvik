package zenvik_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik"
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

func TestFormatName(t *testing.T) {
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	main := mustTitle(t, d, "00800")
	tests := []struct {
		tmpl string
		vars zenvik.NameVars
		want string
	}{
		{"{name}[ ({year})].mkv", zenvik.NameVars{}, "Sample Movie.mkv"},
		{"{name}[ ({year})].mkv", zenvik.NameVars{Year: "2026"}, "Sample Movie (2026).mkv"},
		{"{name}[ ({year})].mkv", zenvik.NameVars{Name: "Override", Year: "1999"}, "Override (1999).mkv"},
		{"{name}[ ({year})]/{name}[ ({year})].mkv", zenvik.NameVars{Year: "2026"}, filepath.Join("Sample Movie (2026)", "Sample Movie (2026).mkv")},
		{"{label}_{playlist}", zenvik.NameVars{}, "SAMPLE_MOVIE_00800.mkv"},
	}
	for _, tt := range tests {
		got, err := zenvik.FormatName(tt.tmpl, d, main, tt.vars)
		if err != nil || got != tt.want {
			t.Errorf("FormatName(%q, %+v) = %q, %v; want %q", tt.tmpl, tt.vars, got, err, tt.want)
		}
	}
	if _, err := zenvik.FormatName("{bogus}", d, main, zenvik.NameVars{}); !errors.Is(err, zenvik.ErrInvalidTemplate) {
		t.Errorf("bad template err = %v", err)
	}
}
