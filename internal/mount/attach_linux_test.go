//go:build linux

package mount

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type step struct {
	out string
	err error
}

func fakeRunner(t *testing.T, steps ...step) *[]string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var calls []string
	oldRun, oldLook := runner, lookPath
	t.Cleanup(func() { runner, lookPath = oldRun, oldLook })
	lookPath = func(string) (string, error) { return "/usr/bin/udisksctl", nil }
	i := 0
	runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		s := step{}
		if i < len(steps) {
			s = steps[i]
		}
		i++
		return []byte(s.out), s.err
	}
	return &calls
}

func TestAttachLinux(t *testing.T) {
	calls := fakeRunner(t,
		step{out: "Mapped file /i/x.iso as /dev/loop9.\n"},
		step{out: "Mounted /dev/loop9 at /media/u/DISC\n"},
		step{}, step{})
	m, err := Attach(context.Background(), "/i/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	if m.Dir != "/media/u/DISC" {
		t.Errorf("Dir = %q", m.Dir)
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"udisksctl loop-setup --no-user-interaction -r -f /i/x.iso",
		"udisksctl mount --no-user-interaction -b /dev/loop9",
		"udisksctl unmount --no-user-interaction -b /dev/loop9",
		"udisksctl loop-delete --no-user-interaction -b /dev/loop9",
	}
	if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls =\n%s", strings.Join(*calls, "\n"))
	}
}

func TestAttachLinuxAlreadyMounted(t *testing.T) {
	fakeRunner(t,
		step{out: "Mapped file /i/x.iso as /dev/loop9.\n"},
		step{out: "Error mounting /dev/loop9: GDBus.Error:org.freedesktop.UDisks2.Error.AlreadyMounted: Device /dev/loop9 is already mounted\n", err: errors.New("exit status 1")},
		step{out: "    MountPoints:        /media/u/AUTO\n"})
	m, err := Attach(context.Background(), "/i/x.iso")
	if err != nil || m.Dir != "/media/u/AUTO" {
		t.Errorf("Attach = %+v, %v", m, err)
	}
}

func TestAttachLinuxFailures(t *testing.T) {
	calls := fakeRunner(t,
		step{out: "Mapped file /i/x.iso as /dev/loop9.\n"},
		step{out: "Not authorized to perform operation", err: errors.New("exit status 1")},
		step{})
	if _, err := Attach(context.Background(), "/i/x.iso"); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "Not authorized") {
		t.Errorf("err = %v", err)
	}
	if last := (*calls)[len(*calls)-1]; last != "udisksctl loop-delete --no-user-interaction -b /dev/loop9" {
		t.Errorf("loop device not cleaned up; last call %q", last)
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Attach(context.Background(), "/i/x.iso"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no udisksctl: err = %v", err)
	}
}

func TestAttachLinuxRecordsDevice(t *testing.T) {
	fakeRunner(t,
		step{out: "Mapped file /i/x.iso as /dev/loop9.\n"},
		step{out: "Mounted /dev/loop9 at /media/u/DISC\n"})
	m, err := Attach(context.Background(), "/i/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	if m.device != "/dev/loop9" {
		t.Errorf("device = %q", m.device)
	}
	if tool, err := Available(); err != nil || tool != "udisksctl" {
		t.Errorf("Available = %q, %v", tool, err)
	}
}
