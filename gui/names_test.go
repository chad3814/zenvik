package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputPath(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "out")
	const outside = "the name must stay inside the output folder"
	cases := []struct {
		name, want, msg string // want is relative to dir
	}{
		{"Movie.mkv", "Movie.mkv", ""},
		{"  Movie  ", "Movie.mkv", ""},
		{"Movie.avi", "Movie.avi.mkv", ""},
		{"Movie.MKV", "Movie.MKV", ""},
		{"Show/S01/e1.mkv", "Show/S01/e1.mkv", ""},
		{"Movie: Part 2?.mkv", "Movie: Part 2?.mkv", ""}, // fine outside Windows
		{"a..b.mkv", "a..b.mkv", ""},
		{"", "", "the name is empty"},
		{".", "", "the name is empty"},
		{"a/../b.mkv", "", outside},
		{"a/..", "", outside},
		{"../escape.mkv", "", outside},
		{"/abs/file.mkv", "", "use a name, not a full path"},
	}
	for _, c := range cases {
		got, msg := outputPath("linux", dir, c.name)
		want := ""
		if c.want != "" {
			want = filepath.Join(dir, filepath.FromSlash(c.want))
		}
		if got != want || msg != c.msg {
			t.Errorf("outputPath(%q) = %q, %q; want %q, %q", c.name, got, msg, want, c.msg)
		}
	}
}

func TestOutputPathWindowsCharacters(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "out")
	for _, name := range []string{"Movie: Part 2", "a*b", "why?", `say "hi"`, "a<b", "a>b", "a|b", "Show/S01: Pilot/e1.mkv"} {
		if got, msg := outputPath("windows", dir, name); got != "" || msg != msgWindowsChars {
			t.Errorf("outputPath(windows, %q) = %q, %q; want %q", name, got, msg, msgWindowsChars)
		}
	}
	if _, msg := outputPath("windows", dir, "Fine name (2001)"); msg != "" {
		t.Errorf("a plain name was rejected on Windows: %q", msg)
	}
}

func TestFileNameError(t *testing.T) {
	for name, want := range map[string]string{
		"New name": "",
		"a?b":      "",
		"":         "the name is empty",
		"  ":       "the name is empty",
		"..":       "the name is empty",
		"a/b.mkv":  "a file name can't contain a folder",
		`a\b.mkv`:  "a file name can't contain a folder",
	} {
		if got := fileNameError("linux", name); got != want {
			t.Errorf("fileNameError(%q) = %q, want %q", name, got, want)
		}
	}
	if got := fileNameError("windows", "a?b"); got != msgWindowsChars {
		t.Errorf("fileNameError(windows, a?b) = %q, want %q", got, msgWindowsChars)
	}
}
