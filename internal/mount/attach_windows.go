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
	out, err := run(ctx, fmt.Sprintf("(Mount-DiskImage -ImagePath %s -Access ReadOnly -PassThru | Get-Volume).DriveLetter", psQuote(image)))
	dismount := func(ctx context.Context) ([]byte, error) {
		return run(ctx, fmt.Sprintf("Dismount-DiskImage -ImagePath %s | Out-Null", psQuote(image)))
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
