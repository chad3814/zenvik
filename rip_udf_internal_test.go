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

// fakeMount makes mounting "succeed" at dir (which may lack BDMV, as when
// Linux rejects the image's directories) and fakes the host OS.
func fakeMount(t *testing.T, goos, dir string) *int {
	t.Helper()
	oldA, oldG := mountAttach, mountGOOS
	t.Cleanup(func() { mountAttach, mountGOOS = oldA, oldG })
	calls := 0
	mountAttach = func(context.Context, string) (*mount.Mount, error) {
		calls++
		return &mount.Mount{Dir: dir}, nil
	}
	mountGOOS = goos
	return &calls
}

// mountedTree is a directory holding BDMV/index.bdmv, as a kernel that
// accepts the image would show it.
func mountedTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "BDMV"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BDMV", "index.bdmv"), []byte("INDX0200"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func openDisc(t *testing.T, p string) *Disc {
	t.Helper()
	d, err := Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestLinuxExplainsUnpaddedFIDCRCWhenTheMountShowsNoBDMV(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Atomic Blonde (2017) - Director's Cut")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := writeISO(t, dir, udfimage.Options{Revision: 0x0250, Label: "X", UnpaddedFIDCRC: true})
	d := openDisc(t, p)
	ctx := context.Background()

	calls := fakeMount(t, "linux", t.TempDir())
	_, _, err := d.mountRoot(ctx, func(Phase, float64) {})
	if !errors.Is(err, ErrNeedsUDFRepair) || *calls != 1 {
		t.Fatalf("linux, empty mount: err = %v, mounts = %d; want ErrNeedsUDFRepair after one mount", err, *calls)
	}
	if want := "zenvik repair-udf " + shellArg(p); !strings.Contains(err.Error(), want) {
		t.Errorf("linux: error %q doesn't contain %q", err, want)
	}
	if !strings.Contains(err.Error(), "found no BDMV/index.bdmv") {
		t.Errorf("linux: error %q doesn't keep the mount check's own message", err)
	}

	fakeMount(t, "darwin", t.TempDir())
	if _, _, err := d.mountRoot(ctx, func(Phase, float64) {}); err == nil || errors.Is(err, ErrNeedsUDFRepair) {
		t.Fatalf("darwin, empty mount: err = %v, want the plain mount-check error", err)
	}
}

// An older kernel (without udf_verify_fi's strict check) mounts these images
// fine: rip must not refuse them.
func TestLinuxRipsAFlawedImageThatMounts(t *testing.T) {
	p := writeISO(t, t.TempDir(), udfimage.Options{Revision: 0x0250, Label: "X", UnpaddedFIDCRC: true})
	d := openDisc(t, p)
	tree := mountedTree(t)
	fakeMount(t, "linux", tree)
	root, release, err := d.mountRoot(context.Background(), func(Phase, float64) {})
	if err != nil || root != tree {
		t.Fatalf("flawed image that mounts: root %q, err = %v; want %q, nil", root, err, tree)
	}
	_ = release()
}

func TestLinuxCleanImageWithoutBDMVIsNotBlamedOnUDF(t *testing.T) {
	p := writeISO(t, t.TempDir(), udfimage.Options{Revision: 0x0102, Label: "X"})
	d := openDisc(t, p)
	fakeMount(t, "linux", t.TempDir())
	if _, _, err := d.mountRoot(context.Background(), func(Phase, float64) {}); err == nil || errors.Is(err, ErrNeedsUDFRepair) {
		t.Fatalf("clean image, empty mount: err = %v, want the plain mount-check error", err)
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
