package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// mkvtoolnixVersion is the MKVToolNix release whose mkvmerge this build
// bundles, set with -ldflags "-X main.mkvtoolnixVersion=<ver>" by
// scripts/release-gui.sh on macOS and Windows; empty when nothing is bundled.
var mkvtoolnixVersion = ""

// resolveMkvmerge picks the mkvmerge to run: the config's mkvmerge_path when
// set (even if it's broken — no silent fallback), else the copy bundled beside
// the app's executable exe, else "" for a PATH lookup.
func resolveMkvmerge(configured, exe, goos string, exists func(string) bool) (path, source string) {
	if configured != "" {
		return configured, "config"
	}
	if exe != "" {
		var bundled string
		switch goos {
		case "darwin":
			bundled = filepath.Join(filepath.Dir(exe), "..", "Helpers", "mkvmerge")
		case "windows":
			bundled = filepath.Join(filepath.Dir(exe), "mkvmerge.exe")
		}
		if bundled != "" && exists(bundled) {
			return filepath.Clean(bundled), "bundled"
		}
	}
	return "", "path"
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// mkvmergeSourceURL is where zenvik hosts the source of the MKVToolNix
// release it bundles (GPLv2).
func mkvmergeSourceURL(version string) string {
	return fmt.Sprintf("https://github.com/chad3814/zenvik/releases/download/mkvtoolnix-src-%s/mkvtoolnix-%s.tar.xz", version, version)
}

// mkvmergeState is the result of the last mkvmerge check.
type mkvmergeState struct {
	source, path, version string
	err                   error
}

// mkvmergeInfo is the About box's mkvmerge line.
func mkvmergeInfo(s mkvmergeState, bundledVersion string) string {
	switch {
	case s.err != nil:
		return "mkvmerge not available — see the message at the top of the window"
	case s.source == "bundled":
		v := bundledVersion
		if v == "" {
			v = s.version
		}
		return fmt.Sprintf("mkvmerge %s (bundled, from MKVToolNix — GPLv2)", v)
	case s.source == "config":
		return fmt.Sprintf("mkvmerge %s (%s)", s.version, s.path)
	}
	return fmt.Sprintf("mkvmerge %s (from PATH)", s.version)
}
