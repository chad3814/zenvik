//go:build windows

package mount

import (
	"context"
	"fmt"
	"strings"
)

func attach(ctx context.Context, image string) (*Mount, error) {
	ps, err := lookPath("powershell.exe")
	if err != nil {
		return nil, fmt.Errorf("%w: powershell.exe not found", ErrUnavailable)
	}
	run := func(ctx context.Context, script string) ([]byte, error) {
		return runner(ctx, ps, "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; "+script)
	}
	q := psQuote(image)
	// If the volume lookup fails after the image attached, dismount inside
	// PowerShell so a failed Attach never leaves the image mounted.
	out, err := run(ctx, fmt.Sprintf("$i = Mount-DiskImage -ImagePath %s -Access ReadOnly -PassThru; "+
		"try { ($i | Get-Volume).DriveLetter } catch { Dismount-DiskImage -ImagePath %s | Out-Null; throw }", q, q))
	dismount := func(ctx context.Context) ([]byte, error) {
		return run(ctx, fmt.Sprintf("Dismount-DiskImage -ImagePath %s | Out-Null", q))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: Mount-DiskImage %s: %w: %s", ErrUnavailable, image, err, strings.TrimSpace(string(out)))
	}
	dir, err := parseDriveLetter(string(out))
	if err != nil {
		_, _ = dismount(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return &Mount{Dir: dir, detach: func(ctx context.Context) error {
		if out, err := dismount(ctx); err != nil {
			return fmt.Errorf("zenvik: Dismount-DiskImage %s: %w: %s", image, err, strings.TrimSpace(string(out)))
		}
		return nil
	}}, nil
}

func available() (string, error) {
	if _, err := lookPath("powershell.exe"); err != nil {
		return "", fmt.Errorf("%w: powershell.exe not found", ErrUnavailable)
	}
	return "PowerShell Mount-DiskImage", nil
}

func cleanupCommand(r Record) string { return "Dismount-DiskImage -ImagePath " + psQuote(r.Image) }
