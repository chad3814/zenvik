package main

import (
	"path/filepath"
	"testing"
)

func TestOutputPath(t *testing.T) {
	dir := filepath.FromSlash("/out")
	cases := []struct {
		name, want, msg string
	}{
		{"Movie.mkv", "/out/Movie.mkv", ""},
		{"  Movie  ", "/out/Movie.mkv", ""},
		{"Movie.avi", "/out/Movie.avi.mkv", ""},
		{"Movie.MKV", "/out/Movie.MKV", ""},
		{"Show/S01/e1.mkv", "/out/Show/S01/e1.mkv", ""},
		{"a/../b.mkv", "/out/b.mkv", ""},
		{"", "", "the name is empty"},
		{".", "", "the name is empty"},
		{"../escape.mkv", "", "the name must stay inside the output folder"},
		{"/abs/file.mkv", "", "use a name, not a full path"},
	}
	for _, c := range cases {
		got, msg := outputPath(dir, c.name)
		want := ""
		if c.want != "" {
			want = filepath.FromSlash(c.want)
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
