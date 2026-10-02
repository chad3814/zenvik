package main

import (
	"runtime/debug"
	"strings"
)

// version is set at release build time with
// -ldflags "-X main.version=<tag>"; it is empty in development builds.
var version string

// versionString returns the release version, or "dev" plus the commit
// recorded by the Go toolchain (if any) for development builds.
func versionString(info *debug.BuildInfo) string {
	if version != "" {
		return version
	}
	if info == nil {
		return "dev"
	}
	var rev string
	modified := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	parts := []string{rev}
	if modified {
		parts = append(parts, "modified")
	}
	return "dev (" + strings.Join(parts, ", ") + ")"
}

// buildVersion is versionString with this binary's build information.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = nil
	}
	return versionString(info)
}
