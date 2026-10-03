package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputPath(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "out")
	cases := []struct {
		name, want, msg string // want is relative to dir
	}{
		{"Movie.mkv", "Movie.mkv", ""},
		{"  Movie  ", "Movie.mkv", ""},
		{"Movie.avi", "Movie.avi.mkv", ""},
		{"Movie.MKV", "Movie.MKV", ""},
		{"Show/S01/e1.mkv", "Show/S01/e1.mkv", ""},
		{"a/../b.mkv", "b.mkv", ""},
		{"", "", "the name is empty"},
		{".", "", "the name is empty"},
		{"../escape.mkv", "", "the name must stay inside the output folder"},
		{"/abs/file.mkv", "", "use a name, not a full path"},
	}
	for _, c := range cases {
		got, msg := outputPath(dir, c.name)
		want := ""
		if c.want != "" {
			want = filepath.Join(dir, filepath.FromSlash(c.want))
		}
		if got != want || msg != c.msg {
			t.Errorf("outputPath(%q) = %q, %q; want %q, %q", c.name, got, msg, want, c.msg)
		}
	}
}

func TestFileNameError(t *testing.T) {
	for name, want := range map[string]string{
		"New name": "",
		"":         "the name is empty",
		"  ":       "the name is empty",
		"..":       "the name is empty",
		"a/b.mkv":  "a file name can't contain a folder",
		`a\b.mkv`:  "a file name can't contain a folder",
	} {
		if got := fileNameError(name); got != want {
			t.Errorf("fileNameError(%q) = %q, want %q", name, got, want)
		}
	}
}
