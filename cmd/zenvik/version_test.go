package main

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	code, out, errOut := runCLI("--version")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if !strings.HasPrefix(out, "zenvik dev") || !strings.HasSuffix(out, "\n") {
		t.Errorf("--version = %q, want \"zenvik dev…\\n\"", out)
	}
}

func TestVersionString(t *testing.T) {
	defer func(v string) { version = v }(version)
	version = "v1.2.3"
	if got := versionString(nil); got != "v1.2.3" {
		t.Errorf("release build = %q", got)
	}
	version = ""
	if got := versionString(nil); got != "dev" {
		t.Errorf("no build info = %q", got)
	}
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}
	if got := versionString(info); got != "dev (0123456789ab, modified)" {
		t.Errorf("vcs build = %q", got)
	}
	info.Settings[1].Value = "false"
	if got := versionString(info); got != "dev (0123456789ab)" {
		t.Errorf("clean vcs build = %q", got)
	}
}
