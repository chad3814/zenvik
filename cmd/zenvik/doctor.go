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
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check mkvmerge, ISO mounting, the config file and leftover mounts",
		Long: `Check that mkvmerge is installed, whether ISO images can be mounted, that the
config file is valid, and whether a crashed rip left mounts behind. If there is no config
file, doctor creates one with the defaults; it never changes an existing file.`,
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
	mark, cfgMsg, mkvmergePath, cfgOK := checkConfig()
	fmt.Fprintf(out, "%s %s\n", mark, cfgMsg)

	mk, err := mux.Find(ctx, mkvmergePath)
	mkOK := err == nil
	if mkOK {
		fmt.Fprintf(out, "✓ mkvmerge: %s (v%s, %s)\n", mk.Path, mk.Version, mk.Source)
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
// path; ok is false when the file is invalid. Every failure reads
// "config: <err>", where err names the file once.
// checkConfig loads and validates the config file and returns the doctor
// line's mark and message, the configured mkvmerge path, and whether the
// configuration is usable. A missing config file is created with the
// defaults; failing to create it is only a warning.
func checkConfig() (mark, msg, mkvmergePath string, ok bool) {
	s, f, err := readConfig(config.Flags{})
	if err != nil {
		return "✗", fmt.Sprintf("config: %v", err), "", false
	}
	if !f.Found {
		if err := config.WriteDefault(f.Path); err != nil {
			return "!", fmt.Sprintf("config: %s not found, and could not create it: %v", f.Path, err), s.MkvmergePath, true
		}
		return "✓", fmt.Sprintf("config: created %s with the defaults", f.Path), s.MkvmergePath, true
	}
	msg = "config: " + f.Path
	if s.Preset != "" {
		msg += fmt.Sprintf(" (default preset %q)", s.Preset)
	}
	return "✓", msg, s.MkvmergePath, true
}
