package mux

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Phase is the stage mkvmerge reports progress for.
type Phase int

// Phases reported by Mux.
const (
	PhaseScanning Phase = 1 // scanning the playlist's files
	PhaseMuxing   Phase = 2 // writing the output
)

// Result is the outcome of a successful mux.
type Result struct {
	Warnings []string
	Args     []string // the arguments mkvmerge received
}

var guiUnescaper = strings.NewReplacer(`\s`, " ", `\2`, `"`, `\c`, ":", `\h`, "#", `\b`, "[", `\B`, "]", `\\`, `\`)

// unescapeGUI decodes the escaping mkvmerge applies to --gui-mode messages.
func unescapeGUI(s string) string { return guiUnescaper.Replace(s) }

// Mux runs mkvmerge for job. onProgress, if non-nil, receives each progress
// update. Exit status 1 (warnings) succeeds with the warnings in the
// Result. On failure or cancellation, job.Output is removed.
func (m *Mkvmerge) Mux(ctx context.Context, job Job, onProgress func(Phase, float64)) (*Result, error) {
	args := append([]string{"--gui-mode"}, Args(job)...)
	opts, err := writeOptions(args)
	if err != nil {
		return nil, err
	}
	defer os.Remove(opts)

	cmd := m.command(ctx, "@"+opts)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: starting %s: %w", ErrFailed, m.Path, err)
	}

	phase := PhaseMuxing
	report := func(f float64) {
		if onProgress != nil {
			onProgress(phase, f)
		}
	}
	var warnings, errs []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case strings.HasPrefix(line, "#GUI#begin_scanning_playlists"):
			phase = PhaseScanning
			report(0)
		case strings.HasPrefix(line, "#GUI#end_scanning_playlists"):
			phase = PhaseMuxing
			report(0)
		case strings.HasPrefix(line, "#GUI#progress "):
			if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "#GUI#progress "), "%")); err == nil {
				report(float64(n) / 100)
			}
		case strings.HasPrefix(line, "#GUI#warning "):
			warnings = append(warnings, unescapeGUI(strings.TrimPrefix(line, "#GUI#warning ")))
		case strings.HasPrefix(line, "Warning: "):
			warnings = append(warnings, strings.TrimPrefix(line, "Warning: "))
		case strings.HasPrefix(line, "#GUI#error "):
			errs = append(errs, unescapeGUI(strings.TrimPrefix(line, "#GUI#error ")))
		case strings.HasPrefix(line, "Error: "):
			errs = append(errs, strings.TrimPrefix(line, "Error: "))
		}
	}
	waitErr := cmd.Wait()

	if ctx.Err() != nil {
		os.Remove(job.Output)
		return nil, ctx.Err()
	}
	code := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if !errors.As(waitErr, &ee) {
			os.Remove(job.Output)
			return nil, fmt.Errorf("%w: %w", ErrFailed, waitErr)
		}
		code = ee.ExitCode()
	}
	if code == 0 || code == 1 {
		return &Result{Warnings: warnings, Args: args}, nil
	}
	os.Remove(job.Output)
	detail := strings.Join(errs, "; ")
	if detail == "" {
		detail = strings.TrimSpace(stderr.String())
	}
	return nil, fmt.Errorf("%w (exit status %d): %s", ErrFailed, code, detail)
}

// writeOptions writes args to a temporary mkvmerge options file.
func writeOptions(args []string) (string, error) {
	f, err := os.CreateTemp("", "zenvik-mkvmerge-*.json")
	if err != nil {
		return "", err
	}
	if err := json.NewEncoder(f).Encode(args); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
