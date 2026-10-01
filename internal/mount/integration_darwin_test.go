//go:build integration && darwin

package mount

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func TestAttachRealImage(t *testing.T) {
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0250, Label: "SAMPLE_MOVIE"})
	if err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(t.TempDir(), "sample disc.iso")
	if err := os.WriteFile(iso, img, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Attach(context.Background(), iso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "BDMV", "index.bdmv")); err != nil {
		t.Errorf("index.bdmv not visible at %s: %v", m.Dir, err)
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("hdiutil", "info").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), iso) {
		t.Errorf("image still attached:\n%s", out)
	}
}
