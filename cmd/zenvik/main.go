// Command zenvik inspects and remuxes unencrypted Blu-ray disc images and
// BDMV folders.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik"
)

// usageError marks errors caused by how the command was invoked (exit 2).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes the CLI and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	fmt.Fprintf(stderr, "zenvik: %v\n", err)
	return exitCode(err)
}

// exitCode maps an error to the exit codes in the design spec.
func exitCode(err error) int {
	var ue usageError
	switch {
	case errors.As(err, &ue):
		return 2
	case errors.Is(err, zenvik.ErrUnsupportedSource), errors.Is(err, zenvik.ErrEncrypted), errors.Is(err, zenvik.ErrNoTitles):
		return 3
	}
	return 1
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "zenvik",
		Short:         "Inspect and remux unencrypted Blu-ray disc images",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown command %q", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SetOut(cmd.ErrOrStderr())
			_ = cmd.Help()
			return usageError{errors.New("missing command")}
		},
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return usageError{err} })
	root.AddCommand(newInfoCmd())
	return root
}
