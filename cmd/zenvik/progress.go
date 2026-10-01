package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chad3814/zenvik"
)

// progressPrinter shows Rip progress: an in-place bar on a terminal, or a
// line per phase change and per 10 percentage points otherwise.
type progressPrinter struct {
	w         io.Writer
	tty       bool
	drawn     bool
	lastPhase zenvik.Phase
	lastPct   int
}

func newProgressPrinter(w io.Writer, tty bool) *progressPrinter {
	return &progressPrinter{w: w, tty: tty, lastPct: -1}
}

func (p *progressPrinter) update(pr zenvik.Progress) {
	pct := int(pr.Fraction*100 + 0.5)
	if p.tty {
		filled := pct / 5
		fmt.Fprintf(p.w, "\r[%s%s] %3d%% %-10s %s / %s", strings.Repeat("#", filled), strings.Repeat(".", 20-filled),
			pct, pr.Phase, formatSize(pr.BytesDone), formatSize(pr.BytesTotal))
		p.drawn = true
		return
	}
	if pr.Phase != p.lastPhase || pct >= p.lastPct+10 || (pct == 100 && p.lastPct != 100) {
		fmt.Fprintf(p.w, "%s %d%%\n", pr.Phase, pct)
		p.lastPhase, p.lastPct = pr.Phase, pct
	}
}

// done ends an in-place bar with a newline.
func (p *progressPrinter) done() {
	if p.tty && p.drawn {
		fmt.Fprintln(p.w)
	}
}

// isTerminal reports whether w is a character device such as a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// shellQuote renders args as a POSIX shell command line. Arguments made only
// of letters, digits and _./:,@%+=- (and not starting with "=", which zsh
// expands) stay bare; anything else is single-quoted.
func shellQuote(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.HasPrefix(a, "=") && strings.IndexFunc(a, unsafeShellRune) < 0 {
			out[i] = a
			continue
		}
		out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(out, " ")
}

func unsafeShellRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	case strings.ContainsRune("_./:,@%+=-", r):
		return false
	}
	return true
}
