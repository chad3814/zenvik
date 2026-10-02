package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestRevealArgs(t *testing.T) {
	p := filepath.FromSlash("/out/a.mkv")
	cases := map[string][]string{
		"darwin":  {"open", "-R", p},
		"windows": {"explorer", "/select," + p},
		"linux":   {"xdg-open", filepath.Dir(p)},
	}
	for goos, want := range cases {
		if got := revealArgs(goos, p); !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
}
