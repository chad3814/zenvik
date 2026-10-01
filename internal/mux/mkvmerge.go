// Package mux runs MKVToolNix's mkvmerge to remux Blu-ray playlists into
// Matroska files.
package mux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

// Errors returned by Find, Identify and Mux.
var (
	ErrNotFound = errors.New("zenvik: mkvmerge not found (install MKVToolNix)")
	ErrTooOld   = errors.New("zenvik: mkvmerge is too old")
	ErrFailed   = errors.New("zenvik: mkvmerge failed")
)

// MinVersion is the oldest supported MKVToolNix release.
var MinVersion = Version{Major: 80}

// Version is an MKVToolNix version number.
type Version struct{ Major, Minor, Patch int }

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less reports whether v is older than o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

var versionRE = regexp.MustCompile(`mkvmerge v(\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion extracts the version from `mkvmerge --version` output, such
// as "mkvmerge v102.0 ('Little Houses') 64-bit".
func ParseVersion(out string) (Version, error) {
	m := versionRE.FindStringSubmatch(out)
	if m == nil {
		return Version{}, fmt.Errorf("zenvik: unrecognized mkvmerge version output %q", out)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		v.Patch, _ = strconv.Atoi(m[3])
	}
	return v, nil
}

// Mkvmerge is a located, version-checked mkvmerge executable.
type Mkvmerge struct {
	Path    string
	Version Version
	pre     []string // leading arguments (tests run the test binary as a fake)
	env     []string // extra environment (tests)
}

// Find locates mkvmerge at path, or on PATH when path is empty, and checks
// that it is at least MinVersion.
func Find(ctx context.Context, path string) (*Mkvmerge, error) {
	if path == "" {
		p, err := exec.LookPath("mkvmerge")
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, err.Error())
		}
		path = p
	}
	m := &Mkvmerge{Path: path}
	if err := m.checkVersion(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Mkvmerge) checkVersion(ctx context.Context) error {
	out, err := m.command(ctx, "--version").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNotFound, m.Path)
		}
		return fmt.Errorf("zenvik: running %s --version: %w", m.Path, err)
	}
	v, err := ParseVersion(string(out))
	if err != nil {
		return err
	}
	m.Version = v
	if v.Less(MinVersion) {
		return fmt.Errorf("%w: found v%s, need v%s or newer", ErrTooOld, v, MinVersion)
	}
	return nil
}

func (m *Mkvmerge) command(ctx context.Context, args ...string) *exec.Cmd {
	all := append(append([]string{}, m.pre...), args...)
	cmd := exec.CommandContext(ctx, m.Path, all...)
	if len(m.env) > 0 {
		cmd.Env = append(os.Environ(), m.env...)
	}
	return cmd
}
