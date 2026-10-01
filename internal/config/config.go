// Package config loads zenvik's TOML configuration file and resolves the
// effective settings from built-in defaults, the file, a preset and
// command-line flags.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ErrInvalid reports a configuration problem; the message names the key.
var ErrInvalid = errors.New("zenvik: invalid configuration")

// Built-in defaults.
const (
	DefaultTemplate    = "{name}[ ({year})].mkv"
	DefaultMinDuration = 2 * time.Minute
)

// Preset overrides a subset of settings.
type Preset struct {
	OutputDir   *string `toml:"output_dir"`
	Template    *string `toml:"template"`
	MinDuration *string `toml:"min_duration"`
}

// File is a parsed config file. Nil fields were not set.
type File struct {
	Path         string            `toml:"-"` // where it was loaded from
	Found        bool              `toml:"-"` // false when the file does not exist
	OutputDir    *string           `toml:"output_dir"`
	Template     *string           `toml:"template"`
	MinDuration  *string           `toml:"min_duration"`
	MkvmergePath *string           `toml:"mkvmerge_path"`
	Preset       *string           `toml:"preset"`
	Presets      map[string]Preset `toml:"presets"`
}

// DefaultPath returns where the config file lives:
// $XDG_CONFIG_HOME/zenvik/config.toml when XDG_CONFIG_HOME is set (on any OS),
// otherwise %AppData%\zenvik\config.toml on Windows and
// ~/.config/zenvik/config.toml elsewhere.
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "zenvik", "config.toml"), nil
	}
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "zenvik", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "zenvik", "config.toml"), nil
}

// Load reads the config file at path. A missing file gives an empty File
// with Found false. Syntax errors, unknown keys and invalid values wrap
// ErrInvalid and name the key or line.
func Load(path string) (*File, error) {
	f := &File{Path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("zenvik: reading config %s: %w", path, err)
	}
	f.Found = true
	if err := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(f); err != nil {
		return nil, fmt.Errorf("%w: %s: %s", ErrInvalid, path, describe(err))
	}
	if err := checkDuration("min_duration", f.MinDuration); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, path, err)
	}
	for name, p := range f.Presets {
		if err := checkDuration("presets."+name+".min_duration", p.MinDuration); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrInvalid, path, err)
		}
	}
	return f, nil
}

func describe(err error) string {
	var sme *toml.StrictMissingError
	if errors.As(err, &sme) {
		var keys []string
		for _, e := range sme.Errors {
			keys = append(keys, strings.Join(e.Key(), "."))
		}
		return "unknown key(s): " + strings.Join(keys, ", ")
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, col := de.Position()
		msg := fmt.Sprintf("line %d, column %d: %s", row, col, de.Error())
		if k := de.Key(); len(k) > 0 {
			msg += " (key " + strings.Join(k, ".") + ")"
		}
		return msg
	}
	return err.Error()
}

func checkDuration(key string, s *string) error {
	if s == nil {
		return nil
	}
	if _, err := parseDuration(*s); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use values like \"90s\" or \"2m\")", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("negative duration %q", s)
	}
	return d, nil
}

// Settings are the effective values after Resolve.
type Settings struct {
	OutputDir    string
	Template     string
	MinDuration  time.Duration
	MkvmergePath string // empty: search PATH
	Preset       string // preset applied, "" if none
}

// Flags holds command-line values; nil means the flag was not given.
type Flags struct {
	Preset    *string
	OutputDir *string
	Template  *string
}

// Resolve applies built-in defaults, then the file's top-level values, then
// the selected preset (flags.Preset if given, else the file's preset; an
// empty name selects none), then flags. "~" is expanded in paths.
func Resolve(f *File, flags Flags) (Settings, error) {
	s := Settings{OutputDir: ".", Template: DefaultTemplate, MinDuration: DefaultMinDuration}
	apply := func(prefix string, out, tmpl, minDur *string) error {
		if out != nil {
			s.OutputDir = *out
		}
		if tmpl != nil {
			s.Template = *tmpl
		}
		if minDur != nil {
			d, err := parseDuration(*minDur)
			if err != nil {
				return fmt.Errorf("%w: %smin_duration: %w", ErrInvalid, prefix, err)
			}
			s.MinDuration = d
		}
		return nil
	}
	if err := apply("", f.OutputDir, f.Template, f.MinDuration); err != nil {
		return Settings{}, err
	}
	if f.MkvmergePath != nil {
		s.MkvmergePath = *f.MkvmergePath
	}
	name := ""
	if f.Preset != nil {
		name = *f.Preset
	}
	if flags.Preset != nil {
		name = *flags.Preset
	}
	if name != "" {
		p, ok := f.Presets[name]
		if !ok {
			return Settings{}, fmt.Errorf("%w: unknown preset %q%s", ErrInvalid, name, presetList(f))
		}
		if err := apply("presets."+name+".", p.OutputDir, p.Template, p.MinDuration); err != nil {
			return Settings{}, err
		}
		s.Preset = name
	}
	if flags.OutputDir != nil {
		s.OutputDir = *flags.OutputDir
	}
	if flags.Template != nil {
		s.Template = *flags.Template
	}
	if strings.TrimSpace(s.Template) == "" {
		return Settings{}, fmt.Errorf("%w: template is empty", ErrInvalid)
	}
	var err error
	if s.OutputDir, err = ExpandHome(s.OutputDir); err != nil {
		return Settings{}, err
	}
	if s.MkvmergePath, err = ExpandHome(s.MkvmergePath); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func presetList(f *File) string {
	if len(f.Presets) == 0 {
		return " (no presets are defined)"
	}
	names := make([]string, 0, len(f.Presets))
	for n := range f.Presets {
		names = append(names, n)
	}
	sort.Strings(names)
	return " (available: " + strings.Join(names, ", ") + ")"
}

// ExpandHome replaces a leading "~" or "~/" (or "~\") with the user's home
// directory. Other paths, including "~user/...", are returned unchanged.
func ExpandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("zenvik: expanding %q: %w", p, err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}
