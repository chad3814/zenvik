package zenvik

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/rank"
	"github.com/chad3814/zenvik/internal/source"
	"github.com/chad3814/zenvik/internal/vobsub"
)

// Reasons a DVD title can't be ripped yet (M5 spec section 5.1).
const (
	reasonNotContiguous = "cells are not contiguous (not supported yet)"
	reasonMidFile       = "starts or ends mid-file (not supported yet)"
	reasonMultiPGC      = "spans several program chains (not supported yet)"
)

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
	starts := cellStarts(pgc.Cells)
	for i, p := range ptts {
		t.Chapters = append(t.Chapters, Chapter{Number: i + 1, Start: starts[pgc.Programs[p.Program-1]-1]})
	}
	info := &dvdInfo{palette: pgc.Palette, width: ts.vts.Video.Width, height: ts.vts.Video.Height, subLang: map[uint16]string{}}
	for i, cl := range pgc.Cells {
		if !cl.AngleBlock || cl.BlockMode == dvd.FirstInBlock || cl.BlockMode == dvd.NotInBlock {
			info.cells = append(info.cells, vobsub.Cell{VOBID: cl.VOBID, CellID: cl.CellID, Start: starts[i]})
		}
	}
	t.dvd = info
	fillDVDTracks(t, ts.vts, pgc, info)
	cells := angleOne(pgc.Cells)
	for _, cl := range cells {
		t.Size += (int64(cl.LastSector) - int64(cl.FirstSector) + 1) * 2048
		c.Clips = append(c.Clips, rank.Clip{ID: fmt.Sprintf("%d:%d:%d", e.TitleSet, cl.VOBID, cl.CellID), Out: cl.Time.Duration()})
	}
	files, reason := wholeFiles(cells, ts.vobs)
	for _, f := range files {
		t.Clips = append(t.Clips, Clip{ID: f.name})
	}
	t.Encrypted = ts.encrypted
	t.Unsupported = reason
	c.Problem = reason
	c.Duration = t.Duration
	c.Chapters = len(t.Chapters)
	c.Languages = countLanguages(t)
	c.HasVideo = true
	c.HasAudio = len(t.Audio) > 0
	c.Size = t.Size
	c.Encrypted = t.Encrypted
	return t, c
}

// angleOne returns the cells angle 1 plays: cells outside blocks, cells
// in non-angle blocks, and the first cell of each angle block.
func angleOne(cells []dvd.Cell) []dvd.Cell {
	var out []dvd.Cell
	for _, c := range cells {
		if !c.AngleBlock || c.BlockMode == dvd.FirstInBlock || c.BlockMode == dvd.NotInBlock {
			out = append(out, c)
		}
	}
	return out
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
// the end of a VOB; otherwise it returns the reason the title is
// unsupported.
func wholeFiles(cells []dvd.Cell, vobs []vobFile) ([]vobFile, string) {
	if len(cells) == 0 {
		return nil, "has no cells"
	}
	for i := 1; i < len(cells); i++ {
		if int64(cells[i].FirstSector) != int64(cells[i-1].LastSector)+1 {
			return nil, reasonNotContiguous
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
		return nil, reasonMidFile
	}
	return vobs[start : end+1], ""
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
