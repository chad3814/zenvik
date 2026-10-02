package testdisc

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

// DVD describes a synthetic VIDEO_TS tree.
type DVD struct {
	Titles      []dvd.TitleEntry // VIDEO_TS.IFO title table
	TitleSets   []DVDTitleSet    // VTS_01, VTS_02, …
	LowerCase   bool             // write names in lower case (video_ts/vts_01_1.vob)
	AppleDouble bool             // also write a macOS "._" companion for every file
}

// DVDTitleSet is one video title set.
type DVDTitleSet struct {
	VTS       dvd.VTS
	VOBs      []int // sectors in each title VOB: VTS_nn_1.VOB, VTS_nn_2.VOB, …
	Scrambled bool  // packs carry PES_scrambling_control 01, as on a CSS disc
}

// VOBPacks returns sectors MPEG-2 program stream packs of 2048 bytes. Each
// pack holds one empty video PES packet (stream 0xE0); scrambled packs set
// PES_scrambling_control to 01.
func VOBPacks(sectors int, scrambled bool) []byte {
	b := make([]byte, sectors*2048)
	for s := 0; s < sectors; s++ {
		p := b[s*2048:]
		copy(p, []byte{0, 0, 1, 0xBA, 0x44, 0, 4, 0, 4, 1, 0x01, 0x89, 0xC3, 0xF8})
		copy(p[14:], []byte{0, 0, 1, 0xE0, 0x07, 0xEC, 0x81, 0x00, 0x00})
		if scrambled {
			p[20] |= 0x10
		}
	}
	return b
}

// Files returns the disc's files keyed by slash-separated path.
func (d *DVD) Files() map[string][]byte {
	name := func(s string) string {
		if d.LowerCase {
			return strings.ToLower(s)
		}
		return s
	}
	dir := name("VIDEO_TS") + "/"
	files := map[string][]byte{
		dir + name("VIDEO_TS.IFO"): VMGFile(&dvd.VMG{TitleSets: len(d.TitleSets), Titles: d.Titles}),
		dir + name("VIDEO_TS.VOB"): VOBPacks(1, false),
	}
	for i, ts := range d.TitleSets {
		vts := ts.VTS
		files[dir+name(fmt.Sprintf("VTS_%02d_0.IFO", i+1))] = VTSFile(&vts)
		for k, sectors := range ts.VOBs {
			files[dir+name(fmt.Sprintf("VTS_%02d_%d.VOB", i+1, k+1))] = VOBPacks(sectors, ts.Scrambled)
		}
	}
	if d.AppleDouble {
		names := make([]string, 0, len(files))
		for p := range files {
			names = append(names, p)
		}
		for _, p := range names {
			dirp, base := path.Split(p)
			files[dirp+"._"+base] = []byte("AppleDouble")
		}
	}
	return files
}

// WriteDir writes the disc's files under dir.
func (d *DVD) WriteDir(dir string) error {
	for name, data := range d.Files() {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ISO returns the disc as a UDF image (use Revision 0x0102 for DVD).
func (d *DVD) ISO(opt udfimage.Options) ([]byte, error) {
	files := map[string]udfimage.File{}
	for name, data := range d.Files() {
		files[name] = udfimage.File{Data: data}
	}
	return udfimage.Build(files, opt)
}

// SampleDVD returns a disc with eight titles (see the Task 3 brief): a
// 100-minute movie (01) and its duplicate (02) over two whole VOBs; three
// 20-minute episodes (03–05) that start or end mid-file and a 60-minute
// "play all" (06) over two whole VOBs; a 90-second extra (07); and a
// two-angle title (08) whose angle-1 cells are not contiguous.
func SampleDVD() *DVD {
	tm := func(d time.Duration) dvd.Time { return dvd.NewTime(d, dvd.Rate30) }
	cell := func(first, last uint32, d time.Duration, vob, id int) dvd.Cell {
		return dvd.Cell{Time: tm(d), FirstSector: first, LastSector: last, VOBID: vob, CellID: id}
	}
	audio := func(n int) (a [8]dvd.AudioControl) {
		for i := 0; i < n; i++ {
			a[i] = dvd.AudioControl{Available: true, Stream: i}
		}
		return a
	}
	subs := func(n int) (s [32]dvd.SubpictureControl) {
		for i := 0; i < n; i++ {
			s[i] = dvd.SubpictureControl{Available: true, Stream4x3: i, Wide: i, Letterbox: i, PanScan: i}
		}
		return s
	}
	pgc := func(na, ns int, cells ...dvd.Cell) *dvd.PGC {
		var total time.Duration
		var progs []int
		for i, c := range cells {
			if !c.AngleBlock || c.BlockMode == dvd.FirstInBlock {
				total += c.Time.Duration()
				progs = append(progs, i+1)
			}
		}
		return &dvd.PGC{Time: tm(total), Audio: audio(na), Subpictures: subs(ns), Programs: progs, Cells: cells}
	}
	chapters := func(pgcn, n int) []dvd.PartOfTitle {
		out := make([]dvd.PartOfTitle, n)
		for i := range out {
			out[i] = dvd.PartOfTitle{PGC: pgcn, Program: i + 1}
		}
		return out
	}
	video := dvd.VideoAttributes{Coding: dvd.MPEG2, Standard: dvd.NTSC, Aspect: dvd.Aspect16x9, Width: 720, Height: 480}
	stereo := func(lang string) dvd.AudioAttributes {
		return dvd.AudioAttributes{Coding: dvd.AC3, Channels: 2, SampleRate: 48000, Language: lang, CodeExtension: dvd.AudioNormal}
	}

	movie := []dvd.Cell{cell(0, 19, 25*time.Minute, 1, 1), cell(20, 39, 25*time.Minute, 1, 2), cell(40, 54, 25*time.Minute, 1, 3), cell(55, 69, 25*time.Minute, 1, 4)}
	vts1 := DVDTitleSet{VOBs: []int{40, 30}, VTS: dvd.VTS{
		Video: video,
		Audio: []dvd.AudioAttributes{
			{Coding: dvd.AC3, Channels: 6, SampleRate: 48000, Language: "en", CodeExtension: dvd.AudioNormal},
			stereo("fr"),
			{Coding: dvd.AC3, Channels: 2, SampleRate: 48000, Language: "en", CodeExtension: dvd.AudioDirectorsComments},
		},
		Subpictures: []dvd.SubpictureAttributes{
			{Language: "en", CodeExtension: dvd.SubpictureNormal},
			{Language: "fr", CodeExtension: dvd.SubpictureNormal},
			{Language: "en", CodeExtension: dvd.SubpictureForced},
		},
		Titles: [][]dvd.PartOfTitle{chapters(1, 4), chapters(2, 4)},
		PGCs:   []*dvd.PGC{pgc(3, 3, movie...), pgc(3, 3, movie...)},
	}}

	ep := []dvd.Cell{cell(0, 19, 20*time.Minute, 1, 1), cell(20, 39, 20*time.Minute, 1, 2), cell(40, 59, 20*time.Minute, 1, 3)}
	vts2 := DVDTitleSet{VOBs: []int{30, 30}, VTS: dvd.VTS{
		Video:  video,
		Audio:  []dvd.AudioAttributes{stereo("en")},
		Titles: [][]dvd.PartOfTitle{chapters(1, 1), chapters(2, 1), chapters(3, 1), chapters(4, 3)},
		PGCs:   []*dvd.PGC{pgc(1, 0, ep[0]), pgc(1, 0, ep[1]), pgc(1, 0, ep[2]), pgc(1, 0, ep...)},
	}}

	vts3 := DVDTitleSet{VOBs: []int{10}, VTS: dvd.VTS{
		Video:  video,
		Audio:  []dvd.AudioAttributes{stereo("en")},
		Titles: [][]dvd.PartOfTitle{chapters(1, 1)},
		PGCs:   []*dvd.PGC{pgc(1, 0, cell(0, 9, 90*time.Second, 1, 1))},
	}}

	angle1 := cell(5, 9, 3*time.Minute, 1, 2)
	angle1.BlockMode, angle1.AngleBlock = dvd.FirstInBlock, true
	angle2 := cell(10, 14, 3*time.Minute, 2, 1)
	angle2.BlockMode, angle2.AngleBlock = dvd.LastInBlock, true
	vts4 := DVDTitleSet{VOBs: []int{20}, VTS: dvd.VTS{
		Video:  video,
		Audio:  []dvd.AudioAttributes{stereo("en")},
		Titles: [][]dvd.PartOfTitle{chapters(1, 3)},
		PGCs:   []*dvd.PGC{pgc(1, 0, cell(0, 4, 2*time.Minute, 1, 1), angle1, angle2, cell(15, 19, 2*time.Minute, 1, 3))},
	}}

	entry := func(vts, ttn, chapters, angles int) dvd.TitleEntry {
		return dvd.TitleEntry{Angles: angles, Chapters: chapters, TitleSet: vts, TitleSetTitle: ttn}
	}
	return &DVD{
		Titles: []dvd.TitleEntry{
			entry(1, 1, 4, 1), entry(1, 2, 4, 1),
			entry(2, 1, 1, 1), entry(2, 2, 1, 1), entry(2, 3, 1, 1), entry(2, 4, 3, 1),
			entry(3, 1, 1, 1),
			entry(4, 1, 3, 2),
		},
		TitleSets: []DVDTitleSet{vts1, vts2, vts3, vts4},
	}
}
