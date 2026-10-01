package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
)

var yearRE = regexp.MustCompile(`^\d{4}$`)

func newRipCmd() *cobra.Command {
	var playlist, outDir, name, year, template, preset string
	var overwrite, dryRun bool
	cmd := &cobra.Command{
		Use:   "rip <path>",
		Short: "Remux a title (the main feature by default) to MKV",
		Long: `Remux one title of a Blu-ray ISO image or BDMV folder to an MKV file with
mkvmerge, keeping every video, audio and subtitle track, chapters and
languages. Without --playlist, the main feature is chosen automatically.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError{fmt.Errorf("rip needs exactly one path, got %d", len(args))}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
			if year != "" && !yearRE.MatchString(year) {
				return usageError{fmt.Errorf("--year must be four digits, got %q", year)}
			}
			var flags config.Flags
			if cmd.Flags().Changed("output-dir") {
				flags.OutputDir = &outDir
			}
			if cmd.Flags().Changed("template") {
				flags.Template = &template
			}
			if cmd.Flags().Changed("preset") {
				flags.Preset = &preset
			}
			s, err := loadSettings(flags)
			if err != nil {
				return err
			}
			d, err := zenvik.Open(cmd.Context(), args[0], zenvik.WithMinDuration(s.MinDuration))
			if err != nil {
				return err
			}
			defer d.Close()
			t, err := pickTitle(d, playlist)
			if err != nil {
				return err
			}
			auto := playlist == ""
			if auto && t.Rank.Ambiguous {
				fmt.Fprintf(stderr, "warning: the main title is a close call (%s) — pass --playlist to choose\n", closeSecond(t))
			}
			rel, err := zenvik.FormatName(s.Template, d, t, zenvik.NameVars{Name: name, Year: year})
			if err != nil {
				return err
			}
			out := filepath.Join(s.OutputDir, rel)
			fmt.Fprintf(stdout, "Ripping %s (%s) → %s\n", t.ID, formatDuration(t.Duration), out)
			if auto && len(t.Rank.Reasons) > 0 {
				fmt.Fprintf(stdout, "  chosen because: %s\n", strings.Join(t.Rank.Reasons, "; "))
			}
			prog := newProgressPrinter(stdout, isTerminal(stdout))
			res, err := d.Rip(cmd.Context(), t, zenvik.RipOptions{OutputPath: out, Overwrite: overwrite, DryRun: dryRun, MkvmergePath: s.MkvmergePath, OnProgress: prog.update})
			prog.done()
			if err != nil {
				return withHint(err)
			}
			for _, w := range res.Warnings {
				fmt.Fprintf(stderr, "warning: %s\n", w)
			}
			if dryRun {
				fmt.Fprintln(stdout, shellQuote(res.Command))
				return nil
			}
			fmt.Fprintf(stdout, "Done: %s\n", res.OutputPath)
			return nil
		},
	}
	cmd.Flags().StringVarP(&playlist, "playlist", "p", "", "rip this playlist (e.g. 00800) instead of the main feature")
	cmd.Flags().StringVarP(&outDir, "output-dir", "o", "", "directory for the MKV file (default: config output_dir, else the current directory)")
	cmd.Flags().StringVar(&name, "name", "", "title for {name} (default: the disc's title or tidied volume label)")
	cmd.Flags().StringVar(&year, "year", "", "year for {year}, e.g. 1999")
	cmd.Flags().StringVar(&template, "template", "", `output name template, e.g. "{name}[ ({year})].mkv" (default: config template)`)
	cmd.Flags().StringVar(&preset, "preset", "", "config preset to apply (empty for none)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing output file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the output path and mkvmerge command without ripping")
	return cmd
}

// pickTitle returns the requested playlist, or the main title.
func pickTitle(d *zenvik.Disc, playlist string) (*zenvik.Title, error) {
	if playlist != "" {
		t, err := d.Title(playlist)
		if err != nil {
			return nil, usageError{err}
		}
		return t, nil
	}
	if m := d.Main(); m != nil {
		return m, nil
	}
	return nil, errors.New("no title qualifies as the main feature; run `zenvik info --all` and pass --playlist")
}

// withHint adds advice for errors a user can fix.
func withHint(err error) error {
	switch {
	case errors.Is(err, zenvik.ErrOutputExists):
		return fmt.Errorf("%w (use --overwrite to replace it)", err)
	case errors.Is(err, zenvik.ErrMkvmergeNotFound):
		return fmt.Errorf("%w (install MKVToolNix: https://mkvtoolnix.download/)", err)
	}
	return err
}
