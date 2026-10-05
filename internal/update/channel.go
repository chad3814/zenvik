package update

import (
	"os"
	"path/filepath"
	"strings"
)

// Product says which binary is asking, so detection matches its own
// package names.
type Product int

const (
	CLI Product = iota
	GUI
)

// sourceKind is which feed answers "what is the latest release".
type sourceKind int

const (
	sourceGitHub sourceKind = iota
	sourceWinget
	sourceChocolatey
)

// Channel is where this binary was installed from. The zero value is a
// direct download: GitHub is the source and the notice links the releases
// page.
type Channel struct {
	Name    string // "Homebrew", "Scoop", "winget", "Chocolatey"; "" for direct
	Upgrade string // the command that upgrades; "" for direct
	source  sourceKind
	pkg     string // the package id source is asked about; "" for GitHub
}

// Managed reports whether a package manager owns this install.
func (c Channel) Managed() bool { return c.Name != "" }

// sourceKey names the source in the cache file's "source" field.
func (c Channel) sourceKey() string {
	switch c.source {
	case sourceWinget:
		return "winget:" + c.pkg
	case sourceChocolatey:
		return "chocolatey:" + c.pkg
	}
	return "github"
}

// cacheName is the cache file for this source: one per source, because the
// formula CLI and the cask app share a state directory but may ask
// different feeds.
func (c Channel) cacheName() string {
	switch c.source {
	case sourceWinget:
		return "update-check-winget-" + c.pkg + ".json"
	case sourceChocolatey:
		return "update-check-chocolatey-" + c.pkg + ".json"
	}
	return "update-check.json"
}

// brewPrefixes are where Homebrew keeps its Caskroom on Apple silicon and
// Intel Macs.
var brewPrefixes = []string{"/opt/homebrew", "/usr/local"}

// Detect classifies exe for p by where the published packages unpack to.
// dirExists answers the Caskroom check; tests inject it. Detect never
// touches the network. Each product matches only its own package names;
// anything else is a direct download.
func Detect(p Product, exe string, dirExists func(string) bool) Channel {
	path := strings.ToLower(strings.ReplaceAll(exe, `\`, "/"))
	if path == "" {
		return Channel{}
	}
	switch p {
	case CLI:
		switch {
		case strings.Contains(path, "/cellar/zenvik/"):
			return Channel{Name: "Homebrew", Upgrade: "brew upgrade zenvik"}
		case strings.Contains(path, "/scoop/apps/zenvik/"):
			return Channel{Name: "Scoop", Upgrade: "scoop update zenvik"}
		case strings.Contains(path, "/winget/packages/chad3814.zenvik_"):
			return Channel{Name: "winget", Upgrade: "winget upgrade chad3814.Zenvik", source: sourceWinget, pkg: "chad3814.Zenvik"}
		case strings.Contains(path, "/chocolatey/lib/zenvik/"):
			return Channel{Name: "Chocolatey", Upgrade: "choco upgrade zenvik", source: sourceChocolatey, pkg: "zenvik"}
		}
	case GUI:
		switch {
		case strings.Contains(path, "/zenvik.app/") && caskInstalled(dirExists):
			return Channel{Name: "Homebrew", Upgrade: "brew upgrade --cask zenvik-gui"}
		case strings.Contains(path, "/scoop/apps/zenvik-gui/"):
			return Channel{Name: "Scoop", Upgrade: "scoop update zenvik-gui"}
		case strings.Contains(path, "/winget/packages/chad3814.zenvikgui_"):
			return Channel{Name: "winget", Upgrade: "winget upgrade chad3814.ZenvikGUI", source: sourceWinget, pkg: "chad3814.ZenvikGUI"}
		case strings.Contains(path, "/chocolatey/lib/zenvik-gui/"):
			return Channel{Name: "Chocolatey", Upgrade: "choco upgrade zenvik-gui", source: sourceChocolatey, pkg: "zenvik-gui"}
		}
	}
	return Channel{}
}

// caskInstalled reports whether Homebrew's Caskroom holds zenvik-gui under
// either prefix.
func caskInstalled(dirExists func(string) bool) bool {
	if dirExists == nil {
		return false
	}
	for _, prefix := range brewPrefixes {
		if dirExists(prefix + "/Caskroom/zenvik-gui") {
			return true
		}
	}
	return false
}

// DetectSelf is Detect for the running binary: os.Executable with symlinks
// resolved, and os.Stat for the Caskroom check. Windows junctions are left as
// they are (Go 1.23 and later do not resolve mount points in EvalSymlinks);
// Scoop's current still contains scoop/apps/<name>/. A symlink that cannot be
// resolved falls back to the raw path, which usually classifies as direct.
func DetectSelf(p Product) Channel {
	exe, err := os.Executable()
	if err != nil {
		return Channel{}
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return Detect(p, exe, func(dir string) bool {
		fi, err := os.Stat(dir)
		return err == nil && fi.IsDir()
	})
}
