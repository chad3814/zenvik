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

func TestRipDVDISO(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	img, err := testdisc.ISOFromDir(authoredDVD(t), udfimage.Options{Revision: 0x0102, Label: "AUTHORED_DVD"})
	if err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(t.TempDir(), "authored dvd.iso")
	if err := os.WriteFile(iso, img, 0o644); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, iso)
	if d.Kind != zenvik.ISO || d.Format != zenvik.DVD {
		t.Fatalf("kind %v, format %v", d.Kind, d.Format)
	}
	m := mustTitle(t, d, "01")
	out := filepath.Join(t.TempDir(), "dvd.mkv")
	if _, err := d.Rip(context.Background(), m, zenvik.RipOptions{OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	assertDVDRipped(t, out, m)
	assertDetached(t, iso)
}
