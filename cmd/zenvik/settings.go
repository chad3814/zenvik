package main

import (
	"strings"

	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/naming"
)

// loadSettings reads the config file and resolves the effective settings,
// validating the template so mistakes fail before any disc work.
func loadSettings(flags config.Flags) (config.Settings, error) {
	s, _, err := readConfig(flags)
	return s, err
}

// readConfig is loadSettings that also returns the loaded file (nil when
// the config path could not be determined or the file could not be read).
// A bad --template is reported as an invalid name template; a bad template
// from the file is an invalid configuration that names the file.
func readConfig(flags config.Flags) (config.Settings, *config.File, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Settings{}, nil, err
	}
	f, err := config.Load(path)
	if err != nil {
		return config.Settings{}, nil, err
	}
	s, err := config.Resolve(f, flags)
	if err != nil {
		return config.Settings{}, f, err
	}
	if _, err := naming.Parse(s.Template); err != nil {
		if flags.Template != nil {
			return config.Settings{}, f, err
		}
		return config.Settings{}, f, f.Invalid("template %q: %w", s.Template, unprefixed{err})
	}
	return s, f, nil
}

// unprefixed drops the leading "zenvik: " from a wrapped error's message, so
// a message that already starts with it does not repeat it.
type unprefixed struct{ err error }

func (e unprefixed) Error() string { return strings.TrimPrefix(e.err.Error(), "zenvik: ") }
func (e unprefixed) Unwrap() error { return e.err }
