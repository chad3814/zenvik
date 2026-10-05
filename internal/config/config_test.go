package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func ptr(s string) *string { return &s }

func TestDefaultPath(t *testing.T) {
	x := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", x)
	if p, err := DefaultPath(); err != nil || p != filepath.Join(x, "zenvik", "config.toml") {
		t.Errorf("with XDG_CONFIG_HOME: %q, %v", p, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	if p, err := DefaultPath(); err != nil || p != filepath.Join(home, ".config", "zenvik", "config.toml") {
		t.Errorf("fallback: %q, %v", p, err)
	}
}

func TestDefaultPathIgnoresRelativeXDG(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fallback is %AppData% on Windows")
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "relative/config")
	t.Setenv("HOME", home)
	if p, err := DefaultPath(); err != nil || p != filepath.Join(home, ".config", "zenvik", "config.toml") {
		t.Errorf("relative XDG_CONFIG_HOME: %q, %v; want the ~/.config fallback", p, err)
	}
}

func TestDefaultPathFailureIsInvalid(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("home lookup differs")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := DefaultPath(); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestLoadReadErrorIsInvalid(t *testing.T) {
	dir := t.TempDir() // a directory, not a file
	_, err := Load(dir)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if n := strings.Count(err.Error(), dir); n != 1 {
		t.Errorf("err %q names the path %d times, want once", err, n)
	}
}

func TestLoadMissing(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil || f.Found || f.OutputDir != nil {
		t.Errorf("Load(missing) = %+v, %v", f, err)
	}
}

const sample = `
output_dir    = "~/Movies"
template      = "{name}[ ({year})].mkv"
min_duration  = "2m"
mkvmerge_path = ""
preset        = ""

[presets.plex]
output_dir = "/Volumes/Media/Movies"
template   = "{name}[ ({year})]/{name}[ ({year})].mkv"
`

func TestLoadSample(t *testing.T) {
	f, err := Load(writeConfig(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if !f.Found || *f.OutputDir != "~/Movies" || *f.MinDuration != "2m" || *f.Preset != "" {
		t.Errorf("top level = %+v", f)
	}
	p, ok := f.Presets["plex"]
	if !ok || *p.OutputDir != "/Volumes/Media/Movies" || p.MinDuration != nil {
		t.Errorf("plex preset = %+v", p)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := map[string]struct {
		body string
		want string
	}{
		"unknown key":         {`outptu_dir = "x"`, "outptu_dir"},
		"unknown preset key":  {"[presets.plex]\nmkvmerge_path = \"x\"", "mkvmerge_path"},
		"syntax":              {`output_dir = `, "line"},
		"bad duration":        {`min_duration = "two minutes"`, "min_duration"},
		"negative duration":   {`min_duration = "-5s"`, "min_duration"},
		"bad preset duration": {"[presets.plex]\nmin_duration = \"soon\"", "presets.plex.min_duration"},
		"wrong type":          {`output_dir = 5`, "output_dir"},
	}
	for name, tt := range tests {
		_, err := Load(writeConfig(t, tt.body))
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want ErrInvalid mentioning %q", name, err, tt.want)
		}
	}
}

func TestResolvePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	file := &File{
		OutputDir:    ptr("~/Movies"),
		MinDuration:  ptr("90s"),
		MkvmergePath: ptr("~/bin/mkvmerge"),
		Preset:       ptr("plex"),
		Presets: map[string]Preset{
			"plex": {OutputDir: ptr("/media/plex"), Template: ptr("{name}/{name}.mkv")},
			"kodi": {MinDuration: ptr("10m")},
		},
	}
	tests := []struct {
		name  string
		file  *File
		flags Flags
		want  Settings
	}{
		{"defaults", &File{}, Flags{}, Settings{OutputDir: ".", Template: DefaultTemplate, MinDuration: 2 * time.Minute, UpdateCheck: true}},
		{"file default preset", file, Flags{}, Settings{OutputDir: "/media/plex", Template: "{name}/{name}.mkv", MinDuration: 90 * time.Second,
			MkvmergePath: filepath.Join(home, "bin", "mkvmerge"), Preset: "plex", UpdateCheck: true}},
		{"flag preset wins", file, Flags{Preset: ptr("kodi")}, Settings{OutputDir: filepath.Join(home, "Movies"), Template: DefaultTemplate,
			MinDuration: 10 * time.Minute, MkvmergePath: filepath.Join(home, "bin", "mkvmerge"), Preset: "kodi", UpdateCheck: true}},
		{"flags win over preset", file, Flags{OutputDir: ptr("/tmp/out"), Template: ptr("{label}.mkv")}, Settings{OutputDir: "/tmp/out",
			Template: "{label}.mkv", MinDuration: 90 * time.Second, MkvmergePath: filepath.Join(home, "bin", "mkvmerge"), Preset: "plex", UpdateCheck: true}},
		{"empty flag preset disables default preset", file, Flags{Preset: ptr("")}, Settings{OutputDir: filepath.Join(home, "Movies"),
			Template: DefaultTemplate, MinDuration: 90 * time.Second, MkvmergePath: filepath.Join(home, "bin", "mkvmerge"), UpdateCheck: true}},
	}
	for _, tt := range tests {
		got, err := Resolve(tt.file, tt.flags)
		if err != nil || got != tt.want {
			t.Errorf("%s: Resolve = %+v, %v\nwant %+v", tt.name, got, err, tt.want)
		}
	}
}

func TestResolveUnknownPreset(t *testing.T) {
	file := &File{Presets: map[string]Preset{"plex": {}, "kodi": {}}}
	_, err := Resolve(file, Flags{Preset: ptr("jellyfin")})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), `"jellyfin"`) || !strings.Contains(err.Error(), "kodi, plex") {
		t.Errorf("err = %v", err)
	}
	if _, err := Resolve(&File{}, Flags{Preset: ptr("x")}); err == nil || !strings.Contains(err.Error(), "no presets") {
		t.Errorf("no presets: err = %v", err)
	}
	if _, err := Resolve(&File{}, Flags{Template: ptr("  ")}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "template") {
		t.Errorf("empty template: err = %v", err)
	}
}

func TestResolveErrorsNameThePath(t *testing.T) {
	f := &File{Path: "/cfg/config.toml", Found: true, Presets: map[string]Preset{"plex": {}}}
	_, err := Resolve(f, Flags{Preset: ptr("nope")})
	if !errors.Is(err, ErrInvalid) || strings.Count(err.Error(), "/cfg/config.toml") != 1 {
		t.Errorf("err = %v, want ErrInvalid naming the path once", err)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for in, want := range map[string]string{
		"~":          home,
		"~/Movies":   filepath.Join(home, "Movies"),
		"/abs/~/x":   "/abs/~/x",
		"rel":        "rel",
		"":           "",
		"~someone/x": "~someone/x",
	} {
		if got, err := ExpandHome(in); err != nil || got != want {
			t.Errorf("ExpandHome(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestUpdateCheck(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"absent": {"", true},
		"true":   {"update_check = true", true},
		"false":  {"update_check = false", false},
	} {
		t.Run(name, func(t *testing.T) {
			f, err := Load(writeConfig(t, tc.body))
			if err != nil {
				t.Fatal(err)
			}
			s, err := Resolve(f, Flags{})
			if err != nil || s.UpdateCheck != tc.want {
				t.Errorf("UpdateCheck = %v, %v; want %v", s.UpdateCheck, err, tc.want)
			}
		})
	}
	_, err := Load(writeConfig(t, `update_check = "yes"`))
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "update_check") {
		t.Errorf("non-boolean update_check: %v, want ErrInvalid naming the key", err)
	}
	_, err = Load(writeConfig(t, "[presets.plex]\nupdate_check = false"))
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "update_check") {
		t.Errorf("update_check in a preset: %v, want ErrInvalid naming the key", err)
	}
}
