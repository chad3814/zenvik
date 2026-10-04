package main

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/chad3814/zenvik/gui/internal/discs"
	"github.com/chad3814/zenvik/gui/internal/queue"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/naming"
)

// loadSettings reads zenvik's config file the way the CLI does, without
// flags, and checks the template.
func loadSettings() (config.Settings, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return config.Settings{}, err
	}
	f, err := config.Load(path)
	if err != nil {
		return config.Settings{}, err
	}
	s, err := config.Resolve(f, config.Flags{})
	if err != nil {
		return config.Settings{}, err
	}
	if _, err := naming.Parse(s.Template); err != nil {
		return config.Settings{}, f.Invalid("template %q: %w", s.Template, err)
	}
	return s, nil
}

// stateFile is where the queue is saved: under $XDG_STATE_HOME when it is
// absolute, else the user cache folder (the same roots zenvik uses for its
// mount records).
func stateFile() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "zenvik", "gui-queue.json"), nil
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "zenvik", "gui-queue.json"), nil
}

func findMkvmerge(ctx context.Context, path string) (string, error) {
	m, err := mux.Find(ctx, path)
	if err != nil {
		return "", err
	}
	return m.Version.String(), nil
}

func defaultDeps() (Deps, error) {
	state, err := stateFile()
	if err != nil {
		return Deps{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Deps{}, err
	}
	exe, err := os.Executable()
	if err == nil {
		if real, rerr := filepath.EvalSymlinks(exe); rerr == nil {
			exe = real
		}
	} else {
		exe = ""
	}
	return Deps{
		StatePath:    state,
		LoadSettings: loadSettings,
		FindMkvmerge: findMkvmerge,
		Ripper: func(open discs.Opener, mkvmerge func() string) queue.Ripper {
			return queue.LibRipper{Open: open, MkvmergePath: mkvmerge}
		},
		Home:       home,
		GOOS:       goruntime.GOOS,
		Executable: exe,
	}, nil
}
