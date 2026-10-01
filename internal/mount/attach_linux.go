//go:build linux

package mount

import (
	"context"
	"fmt"
	"strings"
)

func attach(ctx context.Context, image string) (*Mount, error) {
	if _, err := lookPath("udisksctl"); err != nil {
		return nil, fmt.Errorf("%w: udisksctl not found (install udisks2)", ErrUnavailable)
	}
	out, err := runner(ctx, "udisksctl", "loop-setup", "--no-user-interaction", "-r", "-f", image)
	if err != nil {
		return nil, fmt.Errorf("%w: udisksctl loop-setup: %w: %s", ErrUnavailable, err, strings.TrimSpace(string(out)))
	}
	dev, err := parseUdisksLoop(string(out))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	cleanup := func() {
		_, _ = runner(context.WithoutCancel(ctx), "udisksctl", "loop-delete", "--no-user-interaction", "-b", dev)
	}
	dir, err := mountLoop(ctx, dev)
	if err != nil {
		cleanup()
		return nil, err
	}
	return &Mount{Dir: dir, device: dev, detach: func(ctx context.Context) error {
		if out, err := runner(ctx, "udisksctl", "unmount", "--no-user-interaction", "-b", dev); err != nil {
			return fmt.Errorf("zenvik: udisksctl unmount %s: %w: %s (run `udisksctl unmount -b %s && udisksctl loop-delete -b %s`, then run `zenvik doctor` to clear its record)",
				dev, err, strings.TrimSpace(string(out)), dev, dev)
		}
		if out, err := runner(ctx, "udisksctl", "loop-delete", "--no-user-interaction", "-b", dev); err != nil {
			return fmt.Errorf("zenvik: udisksctl loop-delete %s: %w: %s", dev, err, strings.TrimSpace(string(out)))
		}
		return nil
	}}, nil
}

// mountLoop mounts dev, or finds where the desktop already auto-mounted it.
func mountLoop(ctx context.Context, dev string) (string, error) {
	out, err := runner(ctx, "udisksctl", "mount", "--no-user-interaction", "-b", dev)
	if err == nil {
		dir, perr := parseUdisksMount(string(out))
		if perr != nil {
			return "", fmt.Errorf("%w: %w", ErrUnavailable, perr)
		}
		return dir, nil
	}
	if strings.Contains(string(out), "AlreadyMounted") {
		info, ierr := runner(ctx, "udisksctl", "info", "-b", dev)
		if ierr == nil {
			if dir, perr := parseUdisksInfoMountPoint(string(info)); perr == nil {
				return dir, nil
			}
		}
	}
	return "", fmt.Errorf("%w: udisksctl mount %s: %w: %s", ErrUnavailable, dev, err, strings.TrimSpace(string(out)))
}

func available() (string, error) {
	if _, err := lookPath("udisksctl"); err != nil {
		return "", fmt.Errorf("%w: udisksctl not found (install udisks2)", ErrUnavailable)
	}
	return "udisksctl", nil
}

func cleanupCommand(r Record) string {
	cmd := fmt.Sprintf("udisksctl unmount --no-user-interaction -b %s && udisksctl loop-delete --no-user-interaction -b %s",
		r.Device, r.Device)
	if r.Path != "" {
		cmd += " && rm -f " + shQuote(r.Path)
	}
	return cmd
}
