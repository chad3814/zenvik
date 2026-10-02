package zenvik

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/rank"
	"github.com/chad3814/zenvik/internal/source"
	"github.com/chad3814/zenvik/internal/vobsub"
)

// reasonMultiPGC is why a DVD title that spans several PGCs can't be
// ripped yet (M5 spec section 5.1).
const reasonMultiPGC = "spans several program chains (not supported yet)"

// reasonOutsideVOBs is why a DVD title whose kept cells address sectors
// past the title set's VOBs can't be ripped.
const reasonOutsideVOBs = "cells point outside the title VOBs"

type sectorRange struct{ first, last int64 }

// insideVOBs reports whether cell c's sectors lie within the title VOBs.
func insideVOBs(c dvd.Cell, vobs []vobFile) bool {
	return len(vobs) > 0 && int64(c.FirstSector) >= vobs[0].first && int64(c.LastSector) <= vobs[len(vobs)-1].last
}

// vobFile is one title VOB of a title set. Sectors are relative to the
// start of the set's first title VOB, as cell addresses are.
type vobFile struct {
	name        string
	size        int64
	first, last int64
}

// titleSet is what scanning learned about one VTS.
type titleSet struct {
	vts       *dvd.VTS
	vobs      []vobFile
	encrypted bool
	problem   string // non-empty: every title in the set is unsupported for this reason
}

// scanDVD reads the VIDEO_TS tree in directory dir of fsys and returns its
// titles ranked best-first.
func scanDVD(ctx context.Context, fsys fs.FS, dir string, minDuration time.Duration, w rank.Weights) ([]*Title, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := source.FindName(fsys, dir, "VIDEO_TS.IFO", false)
	b, err := fs.ReadFile(fsys, path.Join(dir, name))
	if err != nil {
		return nil, err
	}
	vmg, err := dvd.ParseVMG(b)
	if err != nil {
		return nil, fmt.Errorf("zenvik: %s: %w", name, err)
	}
	sets := map[int]*titleSet{}
	titles := make([]*Title, 0, len(vmg.Titles))
	cands := make([]rank.Candidate, 0, len(vmg.Titles))
	for i, e := range vmg.Titles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ts, ok := sets[e.TitleSet]
		if !ok {
			ts = loadTitleSet(fsys, dir, e.TitleSet)
			sets[e.TitleSet] = ts
		}
		t, c := dvdTitle(i+1, e, ts)
		titles = append(titles, t)
		cands = append(cands, c)
	}
	order, infos := rank.Rank(cands, minDuration, w)
	ranked := make([]*Title, len(order))
	for k, i := range order {
		titles[i].Rank = RankInfo(infos[i])
		ranked[k] = titles[i]
	}
	if allEncrypted(ranked) {
		return nil, ErrEncrypted
	}
	return ranked, nil
}

// loadTitleSet parses VTS_nn_0.IFO and sizes and probes the set's title
// VOBs (VTS_nn_1.VOB … VTS_nn_9.VOB, stopping at the first missing one).
func loadTitleSet(fsys fs.FS, dir string, n int) *titleSet {
	ts := &titleSet{}
	ifo := fmt.Sprintf("VTS_%02d_0.IFO", n)
	name := source.FindName(fsys, dir, ifo, false)
	if name == "" {
		ts.problem = "missing " + ifo
		return ts
	}
	b, err := fs.ReadFile(fsys, path.Join(dir, name))
	if err != nil {
		ts.problem = fmt.Sprintf("unreadable %s: %v", name, err)
		return ts
	}
	if ts.vts, err = dvd.ParseVTS(b); err != nil {
		ts.problem = fmt.Sprintf("%s: %v", name, err)
		return ts
	}
	var sector int64
	for k := 1; k <= 9; k++ {
		vn := source.FindName(fsys, dir, fmt.Sprintf("VTS_%02d_%d.VOB", n, k), false)
		if vn == "" {
			break
		}
		p := path.Join(dir, vn)
		st, err := fs.Stat(fsys, p)
		if err != nil {
			ts.problem = fmt.Sprintf("unreadable %s: %v", vn, err)
			return ts
		}
		if st.Size() == 0 || st.Size()%2048 != 0 {
			ts.problem = vn + ": size is not a multiple of 2048"
			return ts
		}
		count := st.Size() / 2048
		ts.vobs = append(ts.vobs, vobFile{name: vn, size: st.Size(), first: sector, last: sector + count - 1})
		sector += count
		enc, err := vobEncrypted(fsys, p, count)
		if err != nil {
			ts.problem = fmt.Sprintf("unreadable %s: %v", vn, err)
			return ts
		}
		ts.encrypted = ts.encrypted || enc
	}
	if len(ts.vobs) == 0 {
		ts.problem = fmt.Sprintf("missing VTS_%02d_1.VOB", n)
	}
	return ts
}

// dvdTitle builds DVD title num (from 1) and its ranking candidate.
func dvdTitle(num int, e dvd.TitleEntry, ts *titleSet) (*Title, rank.Candidate) {
	id := fmt.Sprintf("%02d", num)
	t := &Title{ID: id, Angles: e.Angles}
	c := rank.Candidate{ID: id}
	unsupported := func(reason string) (*Title, rank.Candidate) {
		t.Unsupported, c.Problem = reason, reason
		c.Duration = t.Duration
		return t, c
	}
	if ts.problem != "" {
		return unsupported(ts.problem)
	}
	if e.TitleSetTitle > len(ts.vts.Titles) || len(ts.vts.Titles[e.TitleSetTitle-1]) == 0 {
		return unsupported(fmt.Sprintf("title set %d has no title %d", e.TitleSet, e.TitleSetTitle))
	}
	ptts := ts.vts.Titles[e.TitleSetTitle-1]
	pgc := ts.vts.PGCs[ptts[0].PGC-1]
	t.Duration = pgc.Time.Duration()
	for _, p := range ptts {
		if p.PGC != ptts[0].PGC {
			return unsupported(reasonMultiPGC)
		}
	}
	var play []int // indexes into pgc.Cells of the angle-1 cells, in play order
	for i, cl := range pgc.Cells {
		if !cl.AngleBlock || cl.BlockMode == dvd.FirstInBlock || cl.BlockMode == dvd.NotInBlock {
			play = append(play, i)
		}
	}
	if len(play) == 0 {
		return unsupported("has no cells")
	}
	cells := make([]dvd.Cell, len(play))
	for k, i := range play {
		cells[k] = pgc.Cells[i]
	}
	method, skipped := classifyCells(cells, ts.vobs)
	drop := map[int]bool{}
	for _, k := range skipped {
		cl := cells[k]
		drop[play[k]] = true
		t.SkippedCells = append(t.SkippedCells, SkippedCell{Cell: play[k] + 1, Duration: cl.Time.Duration(), FirstSector: cl.FirstSector, LastSector: cl.LastSector})
		t.Duration -= cl.Time.Duration()
	}
	slices.SortFunc(t.SkippedCells, func(a, b SkippedCell) int { return a.Cell - b.Cell })
	for k, cl := range cells {
		if !drop[play[k]] && !insideVOBs(cl, ts.vobs) {
			return unsupported(reasonOutsideVOBs)
		}
	}
	t.RipMethod = method
	t.Chapters = titleChapters(pgc, ptts, drop)

	info := &dvdInfo{palette: pgc.Palette, width: ts.vts.Video.Width, height: ts.vts.Video.Height,
		subLang: map[uint16]string{}, method: method, files: ts.vobs, vobus: ts.vts.VOBUs}
	var keptAll []dvd.Cell
	for i, cl := range pgc.Cells {
		if !drop[i] {
			keptAll = append(keptAll, cl)
		}
	}
	starts := cellStarts(keptAll)
	for k, cl := range keptAll {
		if !cl.AngleBlock || cl.BlockMode == dvd.FirstInBlock || cl.BlockMode == dvd.NotInBlock {
			info.cells = append(info.cells, vobsub.Cell{VOBID: cl.VOBID, CellID: cl.CellID, Start: starts[k]})
			info.ranges = append(info.ranges, sectorRange{int64(cl.FirstSector), int64(cl.LastSector)})
			t.Size += (int64(cl.LastSector) - int64(cl.FirstSector) + 1) * 2048
			c.Clips = append(c.Clips, rank.Clip{ID: fmt.Sprintf("%d:%d:%d", e.TitleSet, cl.VOBID, cl.CellID), Out: cl.Time.Duration()})
		}
	}
	t.dvd = info
	fillDVDTracks(t, ts.vts, pgc, info)
	for _, f := range ts.vobs {
		for _, r := range info.ranges {
			if r.first <= f.last && r.last >= f.first {
				t.Clips = append(t.Clips, Clip{ID: f.name})
				break
			}
		}
	}
	t.Encrypted = ts.encrypted
	c.Duration = t.Duration
	c.Chapters = len(t.Chapters)
	c.Languages = countLanguages(t)
	c.HasVideo = true
	c.HasAudio = len(t.Audio) > 0
	c.Size = t.Size
	c.Encrypted = t.Encrypted
	return t, c
}

// cellStarts returns each cell's start time in the title, counting only
// angle-1 cells; the other cells of an angle block start with its first.
func cellStarts(cells []dvd.Cell) []time.Duration {
	out := make([]time.Duration, len(cells))
	var t, blockStart time.Duration
	for i, c := range cells {
		if c.AngleBlock && c.BlockMode != dvd.FirstInBlock && c.BlockMode != dvd.NotInBlock {
			out[i] = blockStart
			continue
		}
		out[i], blockStart = t, t
		t += c.Time.Duration()
	}
	return out
}

// wholeFiles returns the title VOBs the cells cover when they play one
// contiguous run of sectors that starts at the start of a VOB and ends at
// the end of a VOB; otherwise it returns false.
func wholeFiles(cells []dvd.Cell, vobs []vobFile) ([]vobFile, bool) {
	if len(cells) == 0 {
		return nil, false
	}
	for i := 1; i < len(cells); i++ {
		if int64(cells[i].FirstSector) != int64(cells[i-1].LastSector)+1 {
			return nil, false
		}
	}
	first, last := int64(cells[0].FirstSector), int64(cells[len(cells)-1].LastSector)
	start, end := -1, -1
	for k, v := range vobs {
		if v.first == first {
			start = k
		}
		if v.last == last {
			end = k
		}
	}
	if start < 0 || end < start {
		return nil, false
	}
	return vobs[start : end+1], true
}

// maxStray is the longest cell that may be dropped from a title's edge. The
// rule is on the time the IFO lists: at most 1 s, 00:00:01:00. At NTSC that
// is 30 frames of 1001/30000 s, 1.001 s; at PAL it is 25 frames, 1.0 s. So a
// 31-frame NTSC or 26-frame PAL cell is kept.
const maxStray = 1001 * time.Millisecond

// classifyCells decides how a title whose angle-1 cells, in play order,
// are cells is ripped, and which of them (indexes into cells) are dropped
// as short strays at the edges. At least one cell is always kept.
func classifyCells(cells []dvd.Cell, vobs []vobFile) (method string, skipped []int) {
	contiguous := func(a, b dvd.Cell) bool { return int64(b.FirstSector) == int64(a.LastSector)+1 }
	lo, hi := 0, len(cells)-1
	for hi > lo && !contiguous(cells[lo], cells[lo+1]) && cells[lo].Time.Duration() <= maxStray {
		skipped = append(skipped, lo)
		lo++
	}
	for hi > lo && !contiguous(cells[hi-1], cells[hi]) && cells[hi].Time.Duration() <= maxStray {
		skipped = append(skipped, hi)
		hi--
	}
	kept := cells[lo : hi+1]
	if _, ok := wholeFiles(kept, vobs); ok {
		return "files", skipped
	}
	for i := 1; i < len(kept); i++ {
		if !contiguous(kept[i-1], kept[i]) {
			return "copy", skipped
		}
	}
	return "cut", skipped
}

// titleChapters returns the title's chapters on its own timeline. Cells in
// drop (indexes into pgc.Cells) are left out. A chapter whose entry cell
// is dropped moves to the next kept cell, or goes away if none follows.
// Chapters landing on the same cell collapse, and numbers run from 1.
func titleChapters(pgc *dvd.PGC, ptts []dvd.PartOfTitle, drop map[int]bool) []Chapter {
	var kept []dvd.Cell
	index := make([]int, len(pgc.Cells)) // pgc cell → index into kept, or -1
	for i, c := range pgc.Cells {
		index[i] = -1
		if !drop[i] {
			index[i] = len(kept)
			kept = append(kept, c)
		}
	}
	starts := cellStarts(kept)
	var out []Chapter
	last := -1
	for _, p := range ptts {
		e := pgc.Programs[p.Program-1] - 1
		for e < len(pgc.Cells) && index[e] < 0 {
			e++
		}
		if e >= len(pgc.Cells) || index[e] == last {
			continue
		}
		last = index[e]
		out = append(out, Chapter{Number: len(out) + 1, Start: starts[index[e]]})
	}
	return out
}

// fillDVDTracks sets t's tracks from the title set's attributes and the
// PGC's stream control tables. Track PIDs are MPEG-PS stream keys (see
// the plan's Global Constraints).
func fillDVDTracks(t *Title, vts *dvd.VTS, pgc *dvd.PGC, info *dvdInfo) {
	v := vts.Video
	vt := VideoTrack{PID: 0x00E0, Codec: bluray.CodingMPEG2Video, Format: 1, FrameRate: 4, AspectRatio: "4:3"}
	if v.Coding == dvd.MPEG1 {
		vt.Codec = bluray.CodingMPEG1Video
	}
	if v.Standard == dvd.PAL {
		vt.Format, vt.FrameRate = 2, 3
	}
	if v.Aspect == dvd.Aspect16x9 {
		vt.AspectRatio = "16:9"
	}
	t.Video = []VideoTrack{vt}
	seen := map[uint16]bool{}
	for i, ctl := range pgc.Audio {
		if !ctl.Available || i >= len(vts.Audio) {
			continue
		}
		a := vts.Audio[i]
		pid, codec := audioStream(a.Coding, ctl.Stream)
		if pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		at := AudioTrack{PID: pid, Codec: codec, Language: dvd.Language6392(a.Language), Channels: 6, SampleRate: 1,
			Description: audioDescriptions[a.CodeExtension]}
		switch a.Channels {
		case 1:
			at.Channels = 1
		case 2:
			at.Channels = 3
		}
		if a.SampleRate == 96000 {
			at.SampleRate = 4
		}
		t.Audio = append(t.Audio, at)
	}
	for i, ctl := range pgc.Subpictures {
		if !ctl.Available || i >= len(vts.Subpictures) {
			continue
		}
		s := vts.Subpictures[i]
		n := ctl.Stream4x3
		if v.Aspect == dvd.Aspect16x9 {
			n = ctl.Wide
		}
		pid := 0xBD20 + uint16(n)
		if seen[pid] {
			continue
		}
		seen[pid] = true
		info.subLang[pid] = s.Language
		t.Subtitles = append(t.Subtitles, SubtitleTrack{PID: pid, Codec: CodingVobSub, Language: dvd.Language6392(s.Language),
			Description: subpictureDescriptions[s.CodeExtension]})
	}
}

// audioStream returns the stream key and coding type of physical audio
// stream n, or 0 for an unknown coding.
func audioStream(c dvd.AudioCoding, n int) (uint16, bluray.CodingType) {
	switch c {
	case dvd.AC3:
		return 0xBD80 + uint16(n), bluray.CodingAC3
	case dvd.DTS:
		return 0xBD88 + uint16(n), bluray.CodingDTS
	case dvd.LPCM:
		return 0xBDA0 + uint16(n), bluray.CodingLPCM
	case dvd.MPEG1Audio:
		return 0x00C0 + uint16(n), bluray.CodingMPEG1Audio
	case dvd.MPEG2Audio:
		return 0x00C0 + uint16(n), bluray.CodingMPEG2Audio
	}
	return 0, 0
}

var audioDescriptions = map[dvd.AudioExtension]string{
	dvd.AudioVisuallyImpaired:  "Visually Impaired",
	dvd.AudioDirectorsComments: "Director's Commentary",
	dvd.AudioAlternateComments: "Alternate Commentary",
}

var subpictureDescriptions = map[dvd.SubpictureExtension]string{
	dvd.SubpictureLarge:                      "Large",
	dvd.SubpictureChildren:                   "Children",
	dvd.SubpictureNormalCaptions:             "Captions",
	dvd.SubpictureLargeCaptions:              "Large Captions",
	dvd.SubpictureChildrensCaptions:          "Children's Captions",
	dvd.SubpictureForced:                     "Forced",
	dvd.SubpictureDirectorsComments:          "Commentary",
	dvd.SubpictureLargeDirectorsComments:     "Large Commentary",
	dvd.SubpictureChildrensDirectorsComments: "Children's Commentary",
}
