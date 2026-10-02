package zenvik

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
)

// openFlat writes the sample movie flattened and opens it. TMPDIR (TMP and
// TEMP on Windows) points at a fresh directory so tests can check what is
// left in it.
func openFlat(t *testing.T) (*Disc, string) {
	t.Helper()
	tmp := t.TempDir()
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	root := filepath.Join(t.TempDir(), "FLAT_MOVIE")
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	if err := testdisc.Flatten(root); err != nil {
		t.Fatal(err)
	}
	d, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, tmp
}

func noReport(Phase, float64) {}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Errorf("%s still holds %v", dir, ents)
	}
}

func TestMountRootFlat(t *testing.T) {
	if runtime.GOOS == "windows" {
		probe := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(os.Args[0], probe); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	d, tmp := openFlat(t)
	if d.Kind != FlatBDMVDir {
		t.Fatalf("Kind = %v", d.Kind)
	}
	root, release, err := d.mountRoot(context.Background(), noReport)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(root) != tmp || !strings.HasPrefix(filepath.Base(root), "zenvik-bdmv-") {
		t.Errorf("root = %s, want zenvik-bdmv-* in %s", root, tmp)
	}
	if !hasDiscMarker(root, Bluray) {
		t.Error("tree has no BDMV/index.bdmv")
	}
	files := d.src.Files()
	for _, std := range []string{"BDMV/PLAYLIST/00800.mpls", "BDMV/CLIPINF/00001.clpi", "BDMV/STREAM/00001.m2ts", "BDMV/MovieObject.bdmv"} {
		p := filepath.Join(root, filepath.FromSlash(std))
		target, err := os.Readlink(p)
		if err != nil || target != files[std] {
			t.Errorf("%s → %q, %v; want %q", std, target, err, files[std])
		}
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, testdisc.SampleMovie().Files()[std]) {
			t.Errorf("%s: read through the link failed: %v", std, err)
		}
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	assertEmptyDir(t, tmp)
	for std, real := range files {
		if _, err := os.Stat(real); err != nil {
			t.Errorf("release removed %s's target: %v", std, err)
		}
	}
}

func TestMountRootFlatLinkFailure(t *testing.T) {
	d, tmp := openFlat(t)
	calls := 0
	orig := symlink
	t.Cleanup(func() { symlink = orig })
	symlink = func(oldname, newname string) error {
		calls++
		if calls == 3 {
			return &os.LinkError{Op: "symlink", Old: oldname, New: newname, Err: errors.New("not permitted")}
		}
		return orig(oldname, newname)
	}
	_, _, err := d.mountRoot(context.Background(), noReport)
	if err == nil || !strings.Contains(err.Error(), "rebuild the BDMV folder layout") || !strings.Contains(err.Error(), "not permitted") {
		t.Errorf("err = %v", err)
	}
	assertEmptyDir(t, tmp)
}
