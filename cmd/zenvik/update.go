package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/update"
)

// newUpdateChecker builds the checker the CLI uses; tests point it at a
// local server and a temporary cache.
var newUpdateChecker = func() *update.Checker {
	return &update.Checker{UserAgent: "zenvik/" + version}
}

// isTerminal reports whether f is a character device (a terminal rather
// than a pipe or file); tests override it.
var isTerminal = func(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// noticeWait bounds how long run waits for the check after the command.
// With a daily cache the wait happens once a day; it lets a fast command
// such as info still get (and cache) an answer.
var noticeWait = 2 * time.Second

// checkTimeout bounds the background request itself.
const checkTimeout = 5 * time.Second

// updateNotice carries the passive update check from the goroutine the
// root command's PersistentPreRun starts to run, which prints it after the
// command has finished.
type updateNotice struct {
	started bool
	result  chan update.Result // one value, Newer false on any failure
}

func newUpdateNotice() *updateNotice {
	return &updateNotice{result: make(chan update.Result, 1)}
}

// start begins the check for cmd when the conditions hold; it never blocks.
func (n *updateNotice) start(cmd *cobra.Command) {
	if !passiveCheckWanted(cmd) {
		return
	}
	n.started = true
	ctx, cancel := context.WithTimeout(cmd.Context(), checkTimeout)
	checker := newUpdateChecker()
	go func() {
		defer cancel()
		r, err := checker.Check(ctx, version)
		if err != nil {
			r = update.Result{}
		}
		n.result <- r
	}()
}

// passiveCheckWanted applies the design's conditions: a release build; one
// of info, rip or repair-udf (doctor runs its own explicit check); not
// --jsonl; no CI or ZENVIK_NO_UPDATE_CHECK in the environment; update_check
// not turned off in the config; and a terminal on stderr.
func passiveCheckWanted(cmd *cobra.Command) bool {
	if version == "" {
		return false
	}
	switch cmd.Name() {
	case "info", "rip", "repair-udf":
	default:
		return false
	}
	if f := cmd.Flags().Lookup("jsonl"); f != nil && f.Value.String() == "true" {
		return false
	}
	if os.Getenv("ZENVIK_NO_UPDATE_CHECK") != "" || os.Getenv("CI") != "" {
		return false
	}
	if !isTerminal(os.Stderr) {
		return false
	}
	s, err := loadSettings(config.Flags{})
	return err == nil && s.UpdateCheck
}

// print writes the notice to w when a newer release was found, waiting at
// most noticeWait for the check. It prints nothing when no check was
// started, the check failed, or this build is current.
func (n *updateNotice) print(w io.Writer) {
	if !n.started {
		return
	}
	select {
	case r := <-n.result:
		if r.Newer {
			fmt.Fprintf(w, "zenvik: %s is available (you have %s): %s\n", r.Latest, r.Current, update.LatestPage)
		}
	case <-time.After(noticeWait):
	}
}
