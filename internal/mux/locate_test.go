package mux

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func hooks(t *testing.T, os_ string, exe string, onPath string) {
	t.Helper()
	g, e, l := goos, executable, lookPath
	t.Cleanup(func() { goos, executable, lookPath = g, e, l })
	goos = os_
	executable = func() (string, error) { return exe, nil }
	lookPath = func(string) (string, error) {
		if onPath == "" {
			return "", errors.New("not on PATH")
		}
		return onPath, nil
	}
}

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLocateBundledBeatsPathOnWindows(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "zenvik.exe"))
	touch(t, filepath.Join(dir, "mkvmerge.exe"))
	hooks(t, "windows", filepath.Join(dir, "zenvik.exe"), `C:\Tools\mkvmerge.exe`)
	// The lookup resolves links (macOS's temp dir is behind /var → /private/var).
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, "mkvmerge.exe"))
	p, src, err := locate()
	if err != nil || src != "bundled" || p != want {
		t.Fatalf("locate = %q, %q, %v; want the bundled %q", p, src, err, want)
	}
}

func TestLocateFallsBackToPath(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "zenvik.exe")) // no mkvmerge.exe beside it
	hooks(t, "windows", filepath.Join(dir, "zenvik.exe"), `C:\Tools\mkvmerge.exe`)
	if p, src, err := locate(); err != nil || src != "PATH" || p != `C:\Tools\mkvmerge.exe` {
		t.Fatalf("no bundled copy: locate = %q, %q, %v; want PATH", p, src, err)
	}
}

func TestLocateIgnoresBundledOffWindows(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "zenvik"))
	touch(t, filepath.Join(dir, "mkvmerge.exe"))
	hooks(t, "linux", filepath.Join(dir, "zenvik"), "/usr/bin/mkvmerge")
	if p, src, err := locate(); err != nil || src != "PATH" || p != "/usr/bin/mkvmerge" {
		t.Fatalf("linux: locate = %q, %q, %v; want PATH", p, src, err)
	}
}

// winget's portable installs start zenvik.exe through a symlink in
// WinGet\Links; the bundled copy is beside the link's target.
func TestLocateFollowsTheExecutablesSymlink(t *testing.T) {
	pkg := t.TempDir()
	touch(t, filepath.Join(pkg, "zenvik.exe"))
	touch(t, filepath.Join(pkg, "mkvmerge.exe"))
	links := t.TempDir()
	link := filepath.Join(links, "zenvik.exe")
	if err := os.Symlink(filepath.Join(pkg, "zenvik.exe"), link); err != nil {
		t.Skipf("symlinks not permitted here: %v", err)
	}
	hooks(t, "windows", link, "")
	want, _ := filepath.EvalSymlinks(filepath.Join(pkg, "mkvmerge.exe"))
	p, src, err := locate()
	if err != nil || src != "bundled" {
		t.Fatalf("symlinked exe: locate = %q, %q, %v; want bundled", p, src, err)
	}
	if got, _ := filepath.EvalSymlinks(p); got != want {
		t.Fatalf("symlinked exe: got %q, want %q", got, want)
	}
}

func TestLocateNotFound(t *testing.T) {
	hooks(t, "windows", filepath.Join(t.TempDir(), "zenvik.exe"), "")
	if _, _, err := locate(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
