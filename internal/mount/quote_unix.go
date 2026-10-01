//go:build unix

package mount

import "strings"

// shQuote single-quotes s for a POSIX shell.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// removeRecordCommand returns the shell command that deletes a record file.
func removeRecordCommand(path string) string { return "rm -f " + shQuote(path) }
