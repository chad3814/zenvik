// Package mount attaches disc images read-only so external programs (such
// as mkvmerge) can read their files.
package mount

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ErrUnavailable reports that images cannot be mounted here: the OS tool
// is missing, not permitted, or failed.
var ErrUnavailable = errors.New("zenvik: cannot mount disc images on this system")

const detachTimeout = time.Minute

// Mount is an attached image.
type Mount struct {
	Dir    string // directory where the image's root is visible
	detach func(ctx context.Context) error
	device string // block device (Linux), for cleanup instructions
	record string // path of this mount's record file
}

// Attach mounts image read-only. A relative image path is resolved against
// the current directory first, because some mount tools (Windows' CIM
// provider) do not resolve it themselves.
func Attach(ctx context.Context, image string) (*Mount, error) {
	abs, err := filepath.Abs(image)
	if err != nil {
		return nil, fmt.Errorf("zenvik: resolving image path %q: %w", image, err)
	}
	m, err := attach(ctx, abs)
	if err != nil {
		return nil, err
	}
	// Best-effort: a missing record only means doctor can't report this
	// mount if it is left behind.
	if p, err := register(Record{Image: abs, Dir: m.Dir, Device: m.device, PID: os.Getpid(), Created: time.Now()}); err == nil {
		m.record = p
	}
	return m, nil
}

// Available reports which tool mounts images on this system, or an error
// wrapping ErrUnavailable when none can be used.
func Available() (string, error) { return available() }

// Detach unmounts the image. It ignores cancellation of ctx so cleanup
// still runs after Ctrl-C, gives up after one minute, and is safe to call
// more than once.
func (m *Mount) Detach(ctx context.Context) error {
	if m.detach == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachTimeout)
	defer cancel()
	err := m.detach(ctx)
	if err == nil && m.record != "" {
		_ = os.Remove(m.record)
		m.record = ""
	}
	m.detach = nil
	return err
}

// runner runs a command and returns its combined output; tests replace it.
var runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8") // stable English output; UTF-8 so paths survive GLib
	return cmd.CombinedOutput()
}

// lookPath finds a tool on PATH; tests replace it.
var lookPath = exec.LookPath
