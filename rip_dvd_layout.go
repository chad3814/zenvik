package zenvik

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/source"
	"github.com/chad3814/zenvik/internal/vobsub"
)

// titleVOBPlaceholder stands in for the copy path's temporary file in a
// dry run, which copies nothing.
const titleVOBPlaceholder = "<title.vob>"

// freeSpace reports the bytes free in a directory; tests replace it.
var freeSpace = freeBytes

// resolveVOBs returns the paths under root of the title set's title VOBs,
// matching names ignoring case.
func resolveVOBs(root string, files []vobFile) ([]string, error) {
	fsys := os.DirFS(root)
	dir := source.FindName(fsys, ".", "VIDEO_TS", true)
	if dir == "" {
		return nil, fmt.Errorf("zenvik: no VIDEO_TS folder under %s", root)
	}
	paths := make([]string, len(files))
	for i, f := range files {
		n := source.FindName(fsys, dir, f.name, false)
		if n == "" {
			return nil, fmt.Errorf("zenvik: %s is missing from %s", f.name, filepath.Join(root, dir))
		}
		paths[i] = filepath.Join(root, dir, n)
	}
	return paths, nil
}

// touchedFiles returns the indexes, ascending, of the files any range overlaps.
func touchedFiles(ranges []sectorRange, files []vobFile) []int {
	var out []int
	for i, f := range files {
		for _, r := range ranges {
			if r.first <= f.last && r.last >= f.first {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// cutGroup returns the indexes of the files mkvmerge reads for a cut title:
// from the latest file at or before the run's first file whose first
// sector starts a VOBU, through the run's last file.
func cutGroup(info *dvdInfo) []int {
	touched := touchedFiles(info.ranges, info.files)
	starts := map[int64]bool{}
	for _, s := range info.vobus {
		starts[int64(s)] = true
	}
	g := touched[0]
	for g > 0 && !starts[info.files[g].first] {
		g--
	}
	out := make([]int, 0, touched[len(touched)-1]-g+1)
	for i := g; i <= touched[len(touched)-1]; i++ {
		out = append(out, i)
	}
	return out
}

// byteSpans maps sector ranges, in order, to byte spans of the files.
func byteSpans(ranges []sectorRange, files []vobFile, paths []string) []vobsub.Span {
	var out []vobsub.Span
	for _, r := range ranges {
		for i, f := range files {
			a, z := max(r.first, f.first), min(r.last, f.last)
			if a > z {
				continue
			}
			out = append(out, vobsub.Span{Path: paths[i], Offset: (a - f.first) * 2048, Length: (z - a + 1) * 2048})
		}
	}
	return out
}

// driftPerVOBID bounds how far summed VOBU durations can run ahead of
// mkvmerge's timeline at each VOB ID change (observed: 4.67 ms of padding
// per VOB ID on authored discs; see docs/superpowers/notes/2026-10-02-m6-mkvmerge-cut.md).
const driftPerVOBID = 10 * time.Millisecond

// cutDriftGuard fails when changes VOB ID changes could drift the cut by at
// least the smallest non-zero margin, so mkvmerge might snap to the wrong
// keyframe.
func cutDriftGuard(changes int, margins ...time.Duration) error {
	drift := time.Duration(changes) * driftPerVOBID
	for _, m := range margins {
		if m > 0 && drift >= m {
			return fmt.Errorf("%d VOB ID changes before the cut could shift it by %v, more than the %v margin", changes, drift, m)
		}
	}
	return nil
}

// cutPoints returns the --split times for a cut title on the timeline of
// its cut group. mkvmerge cuts at the first keyframe at or after each time,
// and summed VOBU durations (from the NAV packs) run slightly ahead of its
// timeline at VOB ID changes, so each point is moved back by half a VOBU:
// start = (VOBUs before the run) − half the shorter of the VOBU before the
// run and the run's first VOBU (no margin at 0); end = start₀ + (the run's
// VOBUs) − half the run's last VOBU.
func cutPoints(info *dvdInfo, paths []string) (start, end time.Duration, err error) {
	_, start, end, err = planCut(info, paths)
	return start, end, err
}

// planCut computes cutPoints and also returns start₀, the run's start on
// the group timeline, by which the title's chapters and subtitles are
// shifted: mkvmerge moves them back by the keyframe it cuts at, which is
// the run's first (see docs/superpowers/notes/2026-10-02-m6-mkvmerge-cut.md).
func planCut(info *dvdInfo, paths []string) (start0, start, end time.Duration, err error) {
	if len(info.vobus) == 0 {
		return 0, 0, 0, errors.New("the IFO has no VOBU address map")
	}
	group := cutGroup(info)
	g0 := info.files[group[0]].first
	r0, r1 := info.ranges[0].first, info.ranges[len(info.ranges)-1].last
	open := map[int]*os.File{}
	defer func() {
		for _, f := range open {
			f.Close()
		}
	}()
	buf := make([]byte, 2048)
	var before, during, lastBefore, firstRun, lastRun uint64
	sawStart := false
	changes, prevVOB := 0, -1
	for _, s := range info.vobus {
		sec := int64(s)
		if sec < g0 || sec > r1 {
			continue
		}
		fi := -1
		for i, f := range info.files {
			if sec >= f.first && sec <= f.last {
				fi = i
			}
		}
		if fi < 0 {
			return 0, 0, 0, fmt.Errorf("VOBU at sector %d is outside the title VOBs", sec)
		}
		f, ok := open[fi]
		if !ok {
			if f, err = os.Open(paths[fi]); err != nil {
				return 0, 0, 0, err
			}
			open[fi] = f
		}
		if _, err := f.ReadAt(buf, (sec-info.files[fi].first)*2048); err != nil {
			return 0, 0, 0, fmt.Errorf("reading the NAV pack at sector %d: %w", sec, err)
		}
		nav, ok := dvd.ParseNAV(buf)
		if !ok {
			return 0, 0, 0, fmt.Errorf("no NAV pack at VOBU sector %d", sec)
		}
		if prevVOB >= 0 && nav.VOBID != prevVOB {
			changes++
		}
		prevVOB = nav.VOBID
		d := uint64(nav.EndPTM - nav.StartPTM)
		switch {
		case sec < r0:
			before += d
			lastBefore = d
		default:
			if sec == r0 {
				sawStart = true
				firstRun = d
			}
			during += d
			lastRun = d
		}
	}
	if !sawStart {
		return 0, 0, 0, fmt.Errorf("no VOBU starts at the title's first sector %d", r0)
	}
	ticks := func(n uint64) time.Duration { return time.Duration(n) * time.Second / 90000 }
	var ms time.Duration
	if before > 0 {
		ms = ticks(min(lastBefore, firstRun)) / 2
	}
	me := ticks(lastRun) / 2
	if err := cutDriftGuard(changes, ms, me); err != nil {
		return 0, 0, 0, err
	}
	return ticks(before), ticks(before) - ms, ticks(before+during) - me, nil
}

// copyTitle copies title t's kept cells, in play order, into a temporary
// VOB beside output and returns its path. It checks the free space first,
// reports PhaseCopying, honours ctx, and removes the file on any failure.
func copyTitle(ctx context.Context, t *Title, paths []string, output string, report func(Phase, float64)) (string, error) {
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	need := t.Size + 64<<20
	if free, ok := freeSpace(dir); ok && free < uint64(need) {
		return "", fmt.Errorf("zenvik: copying title %s needs %s in %s, only %s free", t.ID, byteSize(need), dir, byteSize(int64(free)))
	}
	tmp := filepath.Join(dir, "."+strings.TrimSuffix(filepath.Base(output), ".partial")+".title.vob")
	dst, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		dst.Close()
		os.Remove(tmp)
		return "", err
	}
	report(PhaseCopying, 0)
	buf := make([]byte, 1<<20)
	var done int64
	for _, sp := range byteSpans(t.dvd.ranges, t.dvd.files, paths) {
		src, err := os.Open(sp.Path)
		if err != nil {
			return fail(err)
		}
		if _, err := src.Seek(sp.Offset, io.SeekStart); err != nil {
			src.Close()
			return fail(err)
		}
		r := io.LimitReader(src, sp.Length)
		var copied int64
		for {
			if err := ctx.Err(); err != nil {
				src.Close()
				return fail(err)
			}
			n, rerr := r.Read(buf)
			if n > 0 {
				if _, err := dst.Write(buf[:n]); err != nil {
					src.Close()
					return fail(err)
				}
				copied += int64(n)
				done += int64(n)
				report(PhaseCopying, float64(done)/float64(t.Size))
			}
			if errors.Is(rerr, io.EOF) {
				break
			}
			if rerr != nil {
				src.Close()
				return fail(rerr)
			}
		}
		src.Close()
		if copied != sp.Length {
			return fail(fmt.Errorf("zenvik: %s: read %d of %d bytes", sp.Path, copied, sp.Length))
		}
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	report(PhaseCopying, 1)
	return tmp, nil
}

// byteSize renders n bytes with binary units, like "5.9 GiB".
func byteSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

// skipWarning is the rip warning for a dropped stray cell.
func skipWarning(id string, sc SkippedCell) string {
	return fmt.Sprintf("title %s: skipped cell %d (%.1f s at sectors %d–%d, out of order)", id, sc.Cell, sc.Duration.Seconds(), sc.FirstSector, sc.LastSector)
}
