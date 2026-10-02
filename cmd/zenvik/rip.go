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
	var playlist, titleID, outDir, outFile, name, year, template, preset string
	var overwrite, dryRun, jsonl bool
	cmd := &cobra.Command{
		Use:   "rip <path>",
		Short: "Remux a title (the main feature by default) to MKV",
		Long: `Remux one title of a Blu-ray or DVD disc image or folder to an MKV file
with mkvmerge, keeping every video, audio and subtitle track, chapters and
languages. Without --title (or --playlist), the main feature is chosen
automatically.

The file is named from the template (--template, else the config's) inside
the output directory (--output-dir, else the config's output_dir, else the
current directory). --output-file instead gives the path itself, used as is:
an absolute path stands alone, and a relative one is inside the output
directory.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError{fmt.Errorf("rip needs exactly one path, got %d", len(args))}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
			var ev *eventWriter
			if jsonl {
				ev = newEventWriter(stdout)
			}
			if year != "" && !yearRE.MatchString(year) {
				return usageError{fmt.Errorf("--year must be four digits, got %q", year)}
			}
			if playlist != "" && titleID != "" {
				return usageError{errors.New("use --title or --playlist, not both")}
			}
			id := titleID
			if id == "" {
				id = playlist
			}
			useOutFile := cmd.Flags().Changed("output-file")
			if useOutFile {
				for _, f := range []string{"template", "name", "year"} {
					if cmd.Flags().Changed(f) {
						return usageError{fmt.Errorf("--output-file names the file itself; it can't be combined with --%s", f)}
					}
				}
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
			t, err := pickTitle(d, id)
			if err != nil {
				return err
			}
			auto := id == ""
			if auto && t.Rank.Ambiguous {
				if ev != nil {
					ev.warning(fmt.Sprintf("the main title is a close call (%s)", closeSecond(t)))
				} else {
					fmt.Fprintf(stderr, "warning: the main title is a close call (%s) — pass --title to choose\n", closeSecond(t))
				}
			}
			var out string
			if useOutFile {
				if out, err = outputFilePath(outFile, s.OutputDir); err != nil {
					return err
				}
			} else {
				rel, err := zenvik.FormatName(s.Template, d, t, zenvik.NameVars{Name: name, Year: year})
				if err != nil {
					return err
				}
				out = filepath.Join(s.OutputDir, rel)
			}
			opts := zenvik.RipOptions{OutputPath: out, Overwrite: overwrite, DryRun: dryRun, MkvmergePath: s.MkvmergePath}
			if ev != nil {
				if opts.OutputPath, err = filepath.Abs(out); err != nil {
					return err
				}
				source, err := filepath.Abs(args[0])
				if err != nil {
					return err
				}
				ev.start(startEvent{Source: source, Kind: kindName(d.Kind), Format: formatName(d.Format), Title: t.ID,
					DurationSeconds: t.Duration.Seconds(), SizeBytes: t.Size, Output: opts.OutputPath,
					Auto: auto, Ambiguous: t.Rank.Ambiguous, Reasons: t.Rank.Reasons})
				opts.OnProgress = ev.progress
			} else {
				fmt.Fprintf(stdout, "Ripping %s (%s) → %s\n", t.ID, formatDuration(t.Duration), out)
				if auto && len(t.Rank.Reasons) > 0 {
					fmt.Fprintf(stdout, "  chosen because: %s\n", strings.Join(t.Rank.Reasons, "; "))
				}
			}
			var prog *progressPrinter
			if ev == nil {
				prog = newProgressPrinter(stdout, isTerminal(stdout))
				opts.OnProgress = prog.update
			}
			res, err := d.Rip(cmd.Context(), t, opts)
			if prog != nil {
				prog.done()
			}
			if err != nil {
				return withHint(err)
			}
			for _, w := range res.Warnings {
				if ev != nil {
					ev.warning(w)
				} else {
					fmt.Fprintf(stderr, "warning: %s\n", w)
				}
			}
			switch {
			case ev != nil && dryRun:
				ev.dryRun(res.OutputPath, res.Command)
			case ev != nil:
				ev.done(res.OutputPath, res.Duration.Seconds())
			case dryRun:
				fmt.Fprintln(stdout, shellQuote(res.Command))
			default:
				fmt.Fprintf(stdout, "Done: %s\n", res.OutputPath)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&titleID, "title", "t", "", "rip this title instead of the main feature: a DVD title number (e.g. 3) or a Blu-ray playlist (e.g. 00800)")
	cmd.Flags().StringVarP(&playlist, "playlist", "p", "", "same as --title, for Blu-ray playlists (e.g. 00800)")
	cmd.Flags().StringVarP(&outDir, "output-dir", "d", "", "directory for the MKV file (default: config output_dir, else the current directory)")
	cmd.Flags().StringVarP(&outFile, "output-file", "o", "", "write the MKV to this path, as is (no template); a relative path is inside the output directory")
	cmd.Flags().StringVar(&name, "name", "", "title for {name} (default: the disc's title or tidied volume label)")
	cmd.Flags().StringVar(&year, "year", "", "year for {year}, e.g. 1999")
	cmd.Flags().StringVar(&template, "template", "", `output name template, e.g. "{name}[ ({year})].mkv" (default: config template)`)
	cmd.Flags().StringVar(&preset, "preset", "", "config preset to apply (empty for none)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing output file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the output path and mkvmerge command without ripping")
	cmd.Flags().BoolVar(&jsonl, "jsonl", false, "write JSON Lines events (start, progress, warning, done, dry_run, error) to stdout and nothing to stderr")
	return cmd
}

// pickTitle returns the requested title, or the main title.
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
	return nil, errors.New("no title qualifies as the main feature; run `zenvik info --all` and pass --title")
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
