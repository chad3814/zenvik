//go:build !darwin && !linux && !windows

package mount

import (
	"context"
	"fmt"
	"runtime"
)

func attach(ctx context.Context, image string) (*Mount, error) {
	return nil, fmt.Errorf("%w: no image mounting support on %s", ErrUnavailable, runtime.GOOS)
}

func available() (string, error) {
	return "", fmt.Errorf("%w: no image mounting support on %s", ErrUnavailable, runtime.GOOS)
}

func cleanupCommand(Record) string { return "" }
