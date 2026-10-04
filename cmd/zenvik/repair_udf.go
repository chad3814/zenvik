package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik/internal/udfrepair"
)

func newRepairUDFCmd() *cobra.Command {
	var dryRun, undo bool
	cmd := &cobra.Command{
		Use:   "repair-udf [--dry-run | --undo] <image>...",
		Short: "Fix ISO images whose directories Linux's UDF driver rejects",
		Long: `Some authoring tools write each directory entry's checksum length without the
entry's padding. macOS and zenvik read such images, but Linux's UDF driver rejects
their directories as corrupt, so Linux can't mount them for ripping. repair-udf
corrects those 16-byte entry headers in place; the video data isn't touched.

Before changing an image it writes <image>.udf-repair-backup with every byte it
replaces, and --undo restores the original exactly. Don't run it on an image that
is mounted or in use. --dry-run only reports what it would fix.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{errors.New("repair-udf needs at least one image")}
			}
			if dryRun && undo {
				return usageError{errors.New("--dry-run and --undo can't be combined")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepairUDF(cmd.OutOrStdout(), args, dryRun, undo)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be fixed; change nothing")
	cmd.Flags().BoolVar(&undo, "undo", false, "restore the original bytes from the backup")
	return cmd
}

func runRepairUDF(out io.Writer, images []string, dryRun, undo bool) error {
	failed := 0
	for _, image := range images {
		outcome, err := repairOne(image, dryRun, undo)
		if err != nil {
			failed++
			outcome = "failed: " + strings.TrimPrefix(err.Error(), "zenvik: ")
		}
		fmt.Fprintf(out, "%s: %s\n", image, outcome)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d images failed", failed, len(images))
	}
	return nil
}

func repairOne(image string, dryRun, undo bool) (string, error) {
	switch {
	case undo:
		if err := udfrepair.Undo(image); err != nil {
			return "", err
		}
		return "undone", nil
	case dryRun:
		plan, err := udfrepair.Check(image)
		if err != nil {
			return "", err
		}
		if len(plan.Patches) == 0 {
			return "nothing to repair", nil
		}
		dirs := make([]string, len(plan.Dirs))
		for i, d := range plan.Dirs {
			if d == "." {
				d = "(root)"
			}
			dirs[i] = d
		}
		return fmt.Sprintf("would repair %d entries in: %s", plan.Entries(), strings.Join(dirs, ", ")), nil
	}
	n, err := udfrepair.Repair(image)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "nothing to repair", nil
	}
	return fmt.Sprintf("repaired %d entries (backup: %s)", n, image+udfrepair.BackupSuffix), nil
}
