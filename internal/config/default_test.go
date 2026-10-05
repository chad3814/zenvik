package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWriteDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new dir", "zenvik", "config.toml")
	if err := WriteDefault(path); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("written file does not load: %v", err)
	}
	if !f.Found {
		t.Fatal("written file not found by Load")
	}
	got, err := Resolve(f, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := Resolve(&File{Path: path}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("written file resolves to %+v, built-in defaults are %+v", got, want)
	}
	for _, key := range []string{"output_dir", "template", "min_duration", "mkvmerge_path", "update_check"} {
		if f2 := mustRead(t, path); !strings.Contains(f2, "\n"+key+" = ") {
			t.Errorf("file does not set %s:\n%s", key, f2)
		}
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o644 && st.Mode().Perm() != 0o666 {
		t.Errorf("file mode = %v, %v", st.Mode(), err)
	}
}

func TestWriteDefaultNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("mine = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(path); !errors.Is(err, fs.ErrExist) {
		t.Errorf("err = %v, want fs.ErrExist", err)
	}
	if got := mustRead(t, path); got != "mine = true\n" {
		t.Errorf("existing file changed to %q", got)
	}
}

// TestDefaultFileExamplePreset uncomments the example preset in the
// written file and checks that it is valid configuration.
func TestDefaultFileExamplePreset(t *testing.T) {
	var lines []string
	for _, l := range strings.Split(DefaultFileContent(), "\n") {
		if rest, ok := strings.CutPrefix(l, "# "); ok && (strings.HasPrefix(rest, "preset") || strings.HasPrefix(rest, "[presets.") ||
			strings.HasPrefix(rest, "output_dir") || strings.HasPrefix(rest, "template")) {
			l = rest
		}
		lines = append(lines, l)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("example preset does not load: %v\n%s", err, strings.Join(lines, "\n"))
	}
	s, err := Resolve(f, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Preset != "plex" || !strings.HasSuffix(s.OutputDir, "Movies") {
		t.Errorf("settings with the example preset = %+v", s)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
