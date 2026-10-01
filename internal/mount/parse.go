package mount

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	udisksLoopRE  = regexp.MustCompile(`(?m)Mapped file .* as (/dev/\S+?)\.?\s*$`)
	udisksMountRE = regexp.MustCompile(`(?m)Mounted /dev/\S+ at (.+?)\.?\s*$`)
	udisksInfoRE  = regexp.MustCompile(`(?m)^\s*MountPoints:[ \t]*(\S.*?)\s*$`)
)

func parseUdisksLoop(out string) (string, error) {
	m := udisksLoopRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("unrecognized udisksctl loop-setup output: %q", strings.TrimSpace(out))
	}
	return m[1], nil
}

func parseUdisksMount(out string) (string, error) {
	m := udisksMountRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("unrecognized udisksctl mount output: %q", strings.TrimSpace(out))
	}
	return m[1], nil
}

func parseUdisksInfoMountPoint(out string) (string, error) {
	m := udisksInfoRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("udisksctl info shows no mount point")
	}
	return m[1], nil
}

// parseDriveLetter turns PowerShell's DriveLetter output into a root path.
func parseDriveLetter(out string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(out))
	if len(s) != 1 || s[0] < 'A' || s[0] > 'Z' {
		return "", fmt.Errorf("unexpected drive letter %q", strings.TrimSpace(out))
	}
	return s + `:\`, nil
}

// psQuote quotes s as a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
