package zenvik

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/mount"
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func writeISO(t *testing.T, dir string, opt udfimage.Options) string {
	t.Helper()
	img, err := testdisc.SampleMovie().ISO(opt)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "disc.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeMount(t *testing.T, goos string) *int {
	t.Helper()
	oldA, oldG := mountAttach, mountGOOS
	t.Cleanup(func() { mountAttach, mountGOOS = oldA, oldG })
	calls := 0
	mountAttach = func(context.Context, string) (*mount.Mount, error) {
		calls++
		return nil, errors.New("fake mount: not attached")
	}
	mountGOOS = goos
	return &calls
}

func TestLinuxRefusesUnpaddedFIDCRCBeforeMounting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Atomic Blonde (2017) - Director's Cut")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := writeISO(t, dir, udfimage.Options{Revision: 0x0250, Label: "X", UnpaddedFIDCRC: true})
	ctx := context.Background()
	d, err := Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	calls := fakeMount(t, "linux")
	_, _, err = d.mountRoot(ctx, func(Phase, float64) {})
	if !errors.Is(err, ErrNeedsUDFRepair) {
		t.Fatalf("linux: err = %v, want ErrNeedsUDFRepair", err)
	}
	if *calls != 0 {
		t.Fatalf("linux: mount attempted %d times, want 0", *calls)
	}
	if want := "zenvik repair-udf " + shellArg(p); !strings.Contains(err.Error(), want) {
		t.Errorf("linux: error %q doesn't contain %q", err, want)
	}

	calls = fakeMount(t, "darwin")
	_, _, err = d.mountRoot(ctx, func(Phase, float64) {})
	if errors.Is(err, ErrNeedsUDFRepair) || *calls != 1 {
		t.Fatalf("darwin: err = %v, mounts = %d; want a mount attempt", err, *calls)
	}
}

func TestLinuxMountsCleanImages(t *testing.T) {
	p := writeISO(t, t.TempDir(), udfimage.Options{Revision: 0x0102, Label: "X"})
	ctx := context.Background()
	d, err := Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	calls := fakeMount(t, "linux")
	if _, _, err := d.mountRoot(ctx, func(Phase, float64) {}); errors.Is(err, ErrNeedsUDFRepair) || *calls != 1 {
		t.Fatalf("clean image on linux: err = %v, mounts = %d; want a mount attempt", err, *calls)
	}
}

func TestShellArg(t *testing.T) {
	for in, want := range map[string]string{
		"plain.iso":                  "plain.iso",
		"Atomic Blonde (2017)/x.iso": `'Atomic Blonde (2017)/x.iso'`,
		"Director's Cut.iso":         `'Director'\''s Cut.iso'`,
	} {
		if got := shellArg(in); got != want {
			t.Errorf("shellArg(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDiscMarkerErr(t *testing.T) {
	root := t.TempDir()
	if err := discMarkerErr(root, Bluray); err == nil || !strings.Contains(err.Error(), "found no BDMV/index.bdmv") {
		t.Errorf("missing marker: err = %v, want found no BDMV/index.bdmv", err)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission errors need a non-root Unix user")
	}
	bdmv := filepath.Join(root, "BDMV")
	if err := os.Mkdir(bdmv, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bdmv, "index.bdmv"), []byte("INDX0200"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := discMarkerErr(root, Bluray); err != nil {
		t.Errorf("present marker: err = %v, want nil", err)
	}
	if err := os.Chmod(bdmv, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bdmv, 0o755) })
	err := discMarkerErr(root, Bluray)
	if err == nil || !strings.Contains(err.Error(), "can't read BDMV/index.bdmv") || !errors.Is(err, os.ErrPermission) {
		t.Errorf("unreadable marker: err = %v, want can't read … permission denied", err)
	}
}
