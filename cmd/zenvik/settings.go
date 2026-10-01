package main

import (
	"fmt"

	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/naming"
)

// loadSettings reads the config file and resolves the effective settings,
// validating the template so mistakes fail before any disc work.
func loadSettings(flags config.Flags) (config.Settings, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Settings{}, err
	}
	f, err := config.Load(path)
	if err != nil {
		return config.Settings{}, err
	}
	s, err := config.Resolve(f, flags)
	if err != nil {
		return config.Settings{}, err
	}
	if _, err := naming.Parse(s.Template); err != nil {
		return config.Settings{}, fmt.Errorf("%w: template %q: %w", config.ErrInvalid, s.Template, err)
	}
	return s, nil
}
