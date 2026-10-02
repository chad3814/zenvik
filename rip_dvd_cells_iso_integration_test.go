//go:build integration && darwin

package zenvik_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func TestRipDVDCutISO(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	img, err := testdisc.ISOFromDir(episodesDisc(t), udfimage.Options{Revision: 0x0102, Label: "EPISODES"})
	if err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(t.TempDir(), "episodes.iso")
	if err := os.WriteFile(iso, img, 0o644); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, iso)
	out := filepath.Join(t.TempDir(), "blue.mkv")
	if _, err := d.Rip(context.Background(), mustTitle(t, d, "04"), zenvik.RipOptions{OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	if a, z := colours(t, out); a != "blue" || z != "blue" {
		t.Errorf("colours %s → %s, want blue", a, z)
	}
	assertDetached(t, iso)
}
