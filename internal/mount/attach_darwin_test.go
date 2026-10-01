//go:build darwin

package mount

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

func fakeRunner(t *testing.T, results ...error) *[]call {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var calls []call
	oldRun, oldLook := runner, lookPath
	t.Cleanup(func() { runner, lookPath = oldRun, oldLook })
	lookPath = func(string) (string, error) { return "/usr/bin/hdiutil", nil }
	i := 0
	runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, call{name, args})
		var err error
		if i < len(results) {
			err = results[i]
		}
		i++
		if err != nil {
			return []byte("hdiutil: failed"), err
		}
		return nil, nil
	}
	return &calls
}

func TestAttachDarwin(t *testing.T) {
	calls := fakeRunner(t)
	m, err := Attach(context.Background(), "/images/my disc.iso")
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(m.Dir); err != nil || !st.IsDir() || !strings.Contains(m.Dir, "zenvik-mount-") {
		t.Fatalf("mount dir %q: %v", m.Dir, err)
	}
	attach := (*calls)[0]
	want := []string{"attach", "-readonly", "-nobrowse", "-noautoopen", "-noverify", "-imagekey", "diskimage-class=CRawDiskImage", "-mountpoint", m.Dir, "/images/my disc.iso"}
	if attach.name != "hdiutil" || strings.Join(attach.args, "|") != strings.Join(want, "|") {
		t.Errorf("attach call = %v", attach)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // detaching must still work after cancellation
	if err := m.Detach(ctx); err != nil {
		t.Fatal(err)
	}
	if d := (*calls)[1]; d.name != "hdiutil" || strings.Join(d.args, " ") != "detach "+m.Dir {
		t.Errorf("detach call = %v", d)
	}
	if _, err := os.Stat(m.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("mount dir not removed: %v", err)
	}
	if err := m.Detach(context.Background()); err != nil || len(*calls) != 2 {
		t.Errorf("second Detach: %v, calls %d", err, len(*calls))
	}
}

func TestDetachDarwinForces(t *testing.T) {
	calls := fakeRunner(t, nil, errors.New("busy"))
	m, err := Attach(context.Background(), "/images/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := (*calls)[2]; strings.Join(d.args, " ") != "detach -force "+m.Dir {
		t.Errorf("force detach call = %v", d)
	}
}

func TestAttachDarwinFailure(t *testing.T) {
	fakeRunner(t, errors.New("exit status 1"))
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	_, err := Attach(context.Background(), "/images/x.iso")
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "hdiutil: failed") {
		t.Errorf("err = %v", err)
	}
	if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
		t.Errorf("temp mount dir left behind after a failed attach: %v", ents)
	}
}

func TestAttachDarwinNoTool(t *testing.T) {
	fakeRunner(t)
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Attach(context.Background(), "/images/x.iso"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
}

func TestAttachDarwinRelativePath(t *testing.T) {
	calls := fakeRunner(t)
	m, err := Attach(context.Background(), "rel/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Detach(context.Background()) })
	args := (*calls)[0].args
	img := args[len(args)-1]
	if !filepath.IsAbs(img) || !strings.HasSuffix(img, "/rel/x.iso") {
		t.Errorf("image arg = %q, want an absolute path ending in /rel/x.iso", img)
	}
}

func recordFiles(t *testing.T) []string {
	t.Helper()
	dir, err := stateDir()
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, filepath.Join(dir, e.Name()))
	}
	return names
}

func TestAttachRegistersAndDetachUnregisters(t *testing.T) {
	fakeRunner(t)
	m, err := Attach(context.Background(), "/images/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	files := recordFiles(t)
	if len(files) != 1 {
		t.Fatalf("records = %v", files)
	}
	b, _ := os.ReadFile(files[0])
	if !strings.Contains(string(b), m.Dir) || !strings.Contains(string(b), "/images/x.iso") {
		t.Errorf("record = %s", b)
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	if files := recordFiles(t); len(files) != 0 {
		t.Errorf("record not removed: %v", files)
	}
}

func TestFailedDetachKeepsRecord(t *testing.T) {
	fakeRunner(t, nil, errors.New("busy"), errors.New("still busy"))
	m, err := Attach(context.Background(), "/images/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Detach(context.Background()); err == nil {
		t.Fatal("expected detach error")
	}
	if files := recordFiles(t); len(files) != 1 {
		t.Errorf("record should be kept after a failed detach: %v", files)
	}
	_ = os.RemoveAll(m.Dir)
}

func TestAvailableDarwin(t *testing.T) {
	fakeRunner(t)
	if tool, err := Available(); err != nil || tool != "hdiutil" {
		t.Errorf("Available = %q, %v", tool, err)
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Available(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
}
