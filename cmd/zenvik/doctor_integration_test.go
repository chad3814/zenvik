//go:build integration

package main

import (
	"strings"
	"testing"
)

func TestDoctorHealthy(t *testing.T) {
	isolateConfig(t)
	code, out, errOut := runCLI("doctor")
	if code != 0 || !strings.Contains(out, "✓ mkvmerge:") || !strings.Contains(out, "✓ no leftover mounts") {
		t.Errorf("exit %d, stderr %q, out:\n%s", code, errOut, out)
	}
}
