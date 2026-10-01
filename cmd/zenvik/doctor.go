package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/mount"
	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/naming"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check mkvmerge, ISO mounting, the config file and leftover mounts",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("doctor takes no arguments")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// doctorError carries doctor's exit code after the report is printed.
type doctorError struct{ code int }

func (e doctorError) Error() string { return "doctor found problems (see above)" }

// doctorCode picks the exit code: 4 for mkvmerge problems, else 2 for an
// invalid config, else 1 for live leftover mounts, else 0. Stale records
// (whose mount is already gone) are only warnings.
func doctorCode(mkvmergeOK, configOK bool, leftovers int) int {
	switch {
	case !mkvmergeOK:
		return 4
	case !configOK:
		return 2
	case leftovers > 0:
		return 1
	}
	return 0
}

func runDoctor(ctx context.Context, out io.Writer) error {
	cfgMsg, mkvmergePath, cfgOK := checkConfig()
	mark := "✓"
	if !cfgOK {
		mark = "✗"
	}
	fmt.Fprintf(out, "%s %s\n", mark, cfgMsg)

	mk, err := mux.Find(ctx, mkvmergePath)
	mkOK := err == nil
	if mkOK {
		fmt.Fprintf(out, "✓ mkvmerge: %s (v%s)\n", mk.Path, mk.Version)
	} else {
		fmt.Fprintf(out, "✗ mkvmerge: %v\n", err)
	}

	if tool, err := mount.Available(); err != nil {
		fmt.Fprintf(out, "! ISO mounting: %v (rip from a folder instead)\n", err)
	} else {
		fmt.Fprintf(out, "✓ ISO mounting: %s\n", tool)
	}

	recs, err := mount.Leftovers()
	live := 0
	if err != nil {
		fmt.Fprintf(out, "! leftover mounts: could not check: %v\n", err)
	}
	for _, r := range recs {
		if r.Stale {
			fmt.Fprintf(out, "! stale mount record: %s (mount is gone)%s\n", r.Image, removeWith(r))
			continue
		}
		live++
		fmt.Fprintf(out, "✗ leftover mount: %s at %s (since %s)%s\n",
			r.Image, r.Dir, r.Created.Format(time.RFC3339), removeWith(r))
	}
	if err == nil && live == 0 {
		fmt.Fprintln(out, "✓ no leftover mounts")
	}

	if code := doctorCode(mkOK, cfgOK, live); code != 0 {
		return doctorError{code: code}
	}
	return nil
}

// removeWith returns "; remove with: <command>" for r, or "" when there is
// no command for this OS.
func removeWith(r mount.Record) string {
	if c := r.Cleanup(); c != "" {
		return "; remove with: " + c
	}
	return ""
}

// checkConfig describes the config file and returns the configured mkvmerge
// path; ok is false when the file is invalid.
func checkConfig() (msg, mkvmergePath string, ok bool) {
	path, err := config.DefaultPath()
	if err != nil {
		return fmt.Sprintf("config: %v", err), "", false
	}
	f, err := config.Load(path)
	if err != nil {
		return fmt.Sprintf("config: %v", err), "", false
	}
	s, err := config.Resolve(f, config.Flags{})
	if err == nil {
		if _, terr := naming.Parse(s.Template); terr != nil {
			err = fmt.Errorf("template %q: %w", s.Template, terr)
		}
	}
	if err != nil {
		return fmt.Sprintf("config: %s: %v", path, err), "", false
	}
	if !f.Found {
		return fmt.Sprintf("config: %s (not found; using defaults)", path), s.MkvmergePath, true
	}
	msg = "config: " + path
	if s.Preset != "" {
		msg += fmt.Sprintf(" (default preset %q)", s.Preset)
	}
	return msg, s.MkvmergePath, true
}
