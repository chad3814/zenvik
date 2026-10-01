//go:build darwin

package mount

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func attach(ctx context.Context, image string) (*Mount, error) {
	if _, err := lookPath("hdiutil"); err != nil {
		return nil, fmt.Errorf("%w: hdiutil not found", ErrUnavailable)
	}
	dir, err := os.MkdirTemp("", "zenvik-mount-")
	if err != nil {
		return nil, err
	}
	out, err := runner(ctx, "hdiutil", "attach", "-readonly", "-nobrowse", "-noautoopen", "-noverify",
		"-imagekey", "diskimage-class=CRawDiskImage", "-mountpoint", dir, image)
	if err != nil {
		os.Remove(dir)
		return nil, fmt.Errorf("%w: hdiutil attach %s: %w: %s", ErrUnavailable, image, err, strings.TrimSpace(string(out)))
	}
	return &Mount{Dir: dir, detach: func(ctx context.Context) error {
		out, err := runner(ctx, "hdiutil", "detach", dir)
		if err != nil {
			out, err = runner(ctx, "hdiutil", "detach", "-force", dir)
		}
		if err != nil {
			return fmt.Errorf("zenvik: hdiutil detach %s: %w: %s (run `hdiutil detach -force %s`)", dir, err, strings.TrimSpace(string(out)), dir)
		}
		return os.Remove(dir)
	}}, nil
}

func available() (string, error) {
	if _, err := lookPath("hdiutil"); err != nil {
		return "", fmt.Errorf("%w: hdiutil not found", ErrUnavailable)
	}
	return "hdiutil", nil
}

func cleanupCommand(r Record) string { return fmt.Sprintf("hdiutil detach -force %q", r.Dir) }
