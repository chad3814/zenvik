package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRevealArgs(t *testing.T) {
	p := filepath.Join(os.TempDir(), "out", "a.mkv")
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

// Explorer only selects the file when the quotes surround the path alone, as
// in /select,"C:\x y.mkv"; Go's own quoting would wrap the whole argument.
func TestExplorerCmdLine(t *testing.T) {
	got := explorerCmdLine(`C:\Users\me\Movies\Sample Movie (2).mkv`)
	if want := `explorer /select,"C:\Users\me\Movies\Sample Movie (2).mkv"`; got != want {
		t.Errorf("cmdline = %s, want %s", got, want)
	}
}
