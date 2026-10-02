// Package dvd parses DVD-Video navigation files: VIDEO_TS.IFO (the video
// manager) and VTS_nn_0.IFO (video title set information). It does no
// I/O: the parsers take an IFO file's bytes.
package dvd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// ErrCorrupt reports a malformed IFO file.
var ErrCorrupt = errors.New("dvd: corrupt IFO")

const sectorSize = 2048

// VMG is the video manager (VIDEO_TS.IFO).
type VMG struct {
	TitleSets int          // number of video title sets
	Titles    []TitleEntry // title search pointer table, in title order
}

// TitleEntry is one title of the title search pointer table.
type TitleEntry struct {
	Angles        int    // 1..9
	Chapters      int    // parts of title
	TitleSet      int    // video title set number, 1..99
	TitleSetTitle int    // the title's number within its title set, from 1
	StartSector   uint32 // where the title set starts on the disc
}

// VTS is a video title set (VTS_nn_0.IFO).
type VTS struct {
	Video       VideoAttributes
	Audio       []AudioAttributes      // logical audio streams, at most 8
	Subpictures []SubpictureAttributes // logical subpicture streams, at most 32
	Titles      [][]PartOfTitle        // [title-1][chapter-1]
	PGCs        []*PGC                 // PGC number n is PGCs[n-1]
}

// VideoCoding is the video compression.
type VideoCoding int

// Video codings.
const (
	MPEG1 VideoCoding = 1
	MPEG2 VideoCoding = 2
)

// VideoStandard is NTSC or PAL.
type VideoStandard int

// Video standards.
const (
	NTSC VideoStandard = 1
	PAL  VideoStandard = 2
)

// AspectRatio is the display aspect ratio.
type AspectRatio int

// Aspect ratios.
const (
	Aspect4x3  AspectRatio = 1
	Aspect16x9 AspectRatio = 2
)

// VideoAttributes describe the title set's video stream.
type VideoAttributes struct {
	Coding        VideoCoding
	Standard      VideoStandard
	Aspect        AspectRatio
	Width, Height int
}

// AudioCoding is an audio stream's compression.
type AudioCoding int

// Audio codings; 0 is a reserved or unknown value.
const (
	AC3        AudioCoding = 1
	MPEG1Audio AudioCoding = 2
	MPEG2Audio AudioCoding = 3
	LPCM       AudioCoding = 4
	DTS        AudioCoding = 5
)

// AudioExtension is an audio stream's code extension.
type AudioExtension int

// Audio code extensions; 0 is unspecified.
const (
	AudioNormal            AudioExtension = 1
	AudioVisuallyImpaired  AudioExtension = 2
	AudioDirectorsComments AudioExtension = 3
	AudioAlternateComments AudioExtension = 4
)

// AudioAttributes describe one logical audio stream.
type AudioAttributes struct {
	Coding        AudioCoding
	Channels      int    // 1..8
	SampleRate    int    // Hz: 48000 or 96000; 0 if reserved
	Language      string // ISO 639-1 as stored, lower case; "" if none
	CodeExtension AudioExtension
}

// SubpictureExtension is a subpicture stream's code extension.
type SubpictureExtension int

// Subpicture code extensions; 0 is unspecified.
const (
	SubpictureNormal                     SubpictureExtension = 1
	SubpictureLarge                      SubpictureExtension = 2
	SubpictureChildren                   SubpictureExtension = 3
	SubpictureNormalCaptions             SubpictureExtension = 5
	SubpictureLargeCaptions              SubpictureExtension = 6
	SubpictureChildrensCaptions          SubpictureExtension = 7
	SubpictureForced                     SubpictureExtension = 9
	SubpictureDirectorsComments          SubpictureExtension = 13
	SubpictureLargeDirectorsComments     SubpictureExtension = 14
	SubpictureChildrensDirectorsComments SubpictureExtension = 15
)

// SubpictureAttributes describe one logical subpicture stream.
type SubpictureAttributes struct {
	Language      string // ISO 639-1 as stored, lower case; "" if none
	CodeExtension SubpictureExtension
}

// PartOfTitle locates a chapter: a program of a PGC, both numbered from 1.
type PartOfTitle struct {
	PGC, Program int
}

// PGC is a program chain.
type PGC struct {
	Time        Time
	Audio       [8]AudioControl
	Subpictures [32]SubpictureControl
	Palette     [16]uint32 // 0x00YYCrCb
	Programs    []int      // entry cell of each program, from 1
	Cells       []Cell
}

// AudioControl maps a logical audio stream to its physical stream number.
type AudioControl struct {
	Available bool
	Stream    int // 0..7
}

// SubpictureControl maps a logical subpicture stream to a physical stream
// number for each display mode (0..31).
type SubpictureControl struct {
	Available                           bool
	Stream4x3, Wide, Letterbox, PanScan int
}

// BlockMode is a cell's position in a block (an angle block, for example).
type BlockMode int

// Block modes.
const (
	NotInBlock   BlockMode = 0
	FirstInBlock BlockMode = 1
	InBlock      BlockMode = 2
	LastInBlock  BlockMode = 3
)

// Cell is one cell of a PGC.
type Cell struct {
	BlockMode   BlockMode
	AngleBlock  bool // the cell's block is an angle block
	Time        Time
	FirstSector uint32 // relative to the start of the title set's title VOBs
	LastSector  uint32
	VOBID       int
	CellID      int
}

func be16(b []byte, off int) int    { return int(binary.BigEndian.Uint16(b[off:])) }
func be32(b []byte, off int) uint32 { return binary.BigEndian.Uint32(b[off:]) }

func corrupt(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrCorrupt}, args...)...)
}

// table returns the table that starts at sector, cut to its end address.
func table(b []byte, sector uint32, name string) ([]byte, error) {
	off := int64(sector) * sectorSize
	if sector == 0 || off+8 > int64(len(b)) {
		return nil, corrupt("%s at sector %d is outside the file", name, sector)
	}
	t := b[off:]
	end := int64(be32(t, 4))
	if end < 7 || end >= int64(len(t)) {
		return nil, corrupt("%s end address %d is outside the file", name, end)
	}
	return t[:end+1], nil
}

// ParseVMG parses VIDEO_TS.IFO.
func ParseVMG(b []byte) (*VMG, error) {
	if len(b) < 0x100 || string(b[:12]) != "DVDVIDEO-VMG" {
		return nil, corrupt("not a video manager (VIDEO_TS.IFO)")
	}
	v := &VMG{TitleSets: be16(b, 0x3E)}
	t, err := table(b, be32(b, 0xC4), "title table")
	if err != nil {
		return nil, err
	}
	n := be16(t, 0)
	if n < 1 || n > 99 {
		return nil, corrupt("title table has %d titles", n)
	}
	if len(t) < 8+12*n {
		return nil, corrupt("title table is truncated")
	}
	for i := 0; i < n; i++ {
		e := t[8+12*i:]
		te := TitleEntry{Angles: int(e[1]), Chapters: be16(e, 2), TitleSet: int(e[6]), TitleSetTitle: int(e[7]), StartSector: be32(e, 8)}
		if te.Angles < 1 || te.Angles > 9 || te.TitleSet < 1 || te.TitleSet > 99 || te.TitleSetTitle < 1 {
			return nil, corrupt("title %d: angles %d, title set %d, title %d", i+1, te.Angles, te.TitleSet, te.TitleSetTitle)
		}
		v.Titles = append(v.Titles, te)
	}
	return v, nil
}

// ParseVTS parses a VTS_nn_0.IFO file.
func ParseVTS(b []byte) (*VTS, error) {
	if len(b) < 0x318 || string(b[:12]) != "DVDVIDEO-VTS" {
		return nil, corrupt("not a video title set (VTS_nn_0.IFO)")
	}
	v := &VTS{}
	var err error
	if v.Video, err = parseVideo(b[0x200:]); err != nil {
		return nil, err
	}
	na := be16(b, 0x202)
	if na > 8 {
		return nil, corrupt("%d audio streams", na)
	}
	for i := 0; i < na; i++ {
		v.Audio = append(v.Audio, parseAudio(b[0x204+8*i:]))
	}
	ns := be16(b, 0x254)
	if ns > 32 {
		return nil, corrupt("%d subpicture streams", ns)
	}
	for i := 0; i < ns; i++ {
		v.Subpictures = append(v.Subpictures, parseSubpicture(b[0x256+6*i:]))
	}
	ptt, err := table(b, be32(b, 0xC8), "part-of-title table")
	if err != nil {
		return nil, err
	}
	if v.Titles, err = parsePTT(ptt); err != nil {
		return nil, err
	}
	pgci, err := table(b, be32(b, 0xCC), "program chain table")
	if err != nil {
		return nil, err
	}
	if v.PGCs, err = parsePGCI(pgci); err != nil {
		return nil, err
	}
	for ti, chapters := range v.Titles {
		for ci, p := range chapters {
			if p.PGC > len(v.PGCs) || p.Program > len(v.PGCs[p.PGC-1].Programs) {
				return nil, corrupt("title %d chapter %d points at PGC %d program %d", ti+1, ci+1, p.PGC, p.Program)
			}
		}
	}
	return v, nil
}

func parseVideo(a []byte) (VideoAttributes, error) {
	var v VideoAttributes
	switch a[0] >> 6 {
	case 0:
		v.Coding = MPEG1
	case 1:
		v.Coding = MPEG2
	default:
		return v, corrupt("video coding %d", a[0]>>6)
	}
	full := 480
	switch (a[0] >> 4) & 3 {
	case 0:
		v.Standard = NTSC
	case 1:
		v.Standard, full = PAL, 576
	default:
		return v, corrupt("video standard %d", (a[0]>>4)&3)
	}
	switch (a[0] >> 2) & 3 {
	case 0:
		v.Aspect = Aspect4x3
	case 3:
		v.Aspect = Aspect16x9
	default:
		return v, corrupt("aspect ratio %d", (a[0]>>2)&3)
	}
	switch (a[1] >> 2) & 3 {
	case 0:
		v.Width, v.Height = 720, full
	case 1:
		v.Width, v.Height = 704, full
	case 2:
		v.Width, v.Height = 352, full
	case 3:
		v.Width, v.Height = 352, full/2
	}
	return v, nil
}

func language(present bool, b []byte) string {
	if !present {
		return ""
	}
	s := strings.ToLower(string(b[:2]))
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return ""
		}
	}
	return s
}

func parseAudio(a []byte) AudioAttributes {
	x := AudioAttributes{Channels: int(a[1]&7) + 1, CodeExtension: AudioExtension(a[5])}
	switch a[0] >> 5 {
	case 0:
		x.Coding = AC3
	case 2:
		x.Coding = MPEG1Audio
	case 3:
		x.Coding = MPEG2Audio
	case 4:
		x.Coding = LPCM
	case 6:
		x.Coding = DTS
	}
	switch (a[1] >> 4) & 3 {
	case 0:
		x.SampleRate = 48000
	case 1:
		x.SampleRate = 96000
	}
	x.Language = language((a[0]>>2)&3 == 1, a[2:4])
	return x
}

func parseSubpicture(a []byte) SubpictureAttributes {
	return SubpictureAttributes{Language: language(a[0]&3 == 1, a[2:4]), CodeExtension: SubpictureExtension(a[5])}
}

func parsePTT(t []byte) ([][]PartOfTitle, error) {
	n := be16(t, 0)
	if n < 1 || n > 99 || len(t) < 8+4*n {
		return nil, corrupt("part-of-title table has %d titles", n)
	}
	out := make([][]PartOfTitle, n)
	for i := 0; i < n; i++ {
		start := int64(be32(t, 8+4*i))
		stop := int64(len(t))
		if i+1 < n {
			stop = int64(be32(t, 8+4*(i+1)))
		}
		if start < int64(8+4*n) || stop < start || stop > int64(len(t)) || (stop-start)%4 != 0 {
			return nil, corrupt("part-of-title table: title %d spans bytes %d–%d", i+1, start, stop)
		}
		for o := start; o < stop; o += 4 {
			p := PartOfTitle{PGC: be16(t, int(o)), Program: be16(t, int(o)+2)}
			if p.PGC < 1 || p.Program < 1 {
				return nil, corrupt("title %d: chapter points at PGC %d program %d", i+1, p.PGC, p.Program)
			}
			out[i] = append(out[i], p)
		}
	}
	return out, nil
}

func parsePGCI(t []byte) ([]*PGC, error) {
	n := be16(t, 0)
	if n < 1 || len(t) < 8+8*n {
		return nil, corrupt("program chain table has %d chains", n)
	}
	out := make([]*PGC, n)
	for i := 0; i < n; i++ {
		off := int64(be32(t, 8+8*i+4))
		if off < int64(8+8*n) || off+0xEC > int64(len(t)) {
			return nil, corrupt("PGC %d at byte %d is outside its table", i+1, off)
		}
		p, err := parsePGC(t[off:])
		if err != nil {
			return nil, fmt.Errorf("PGC %d: %w", i+1, err)
		}
		out[i] = p
	}
	return out, nil
}

func parsePGC(p []byte) (*PGC, error) {
	nprog, ncell := int(p[2]), int(p[3])
	pg := &PGC{}
	var err error
	if pg.Time, err = parseTime(p[4:8]); err != nil {
		return nil, err
	}
	for i := range pg.Audio {
		c := p[0x0C+2*i]
		pg.Audio[i] = AudioControl{Available: c&0x80 != 0, Stream: int(c & 0x07)}
	}
	for i := range pg.Subpictures {
		c := p[0x1C+4*i:]
		pg.Subpictures[i] = SubpictureControl{Available: c[0]&0x80 != 0, Stream4x3: int(c[0] & 0x1F),
			Wide: int(c[1] & 0x1F), Letterbox: int(c[2] & 0x1F), PanScan: int(c[3] & 0x1F)}
	}
	for i := range pg.Palette {
		pg.Palette[i] = be32(p, 0xA4+4*i)
	}
	if ncell == 0 {
		if nprog != 0 {
			return nil, corrupt("%d programs but no cells", nprog)
		}
		return pg, nil
	}
	progOff, cellOff, posOff := be16(p, 0xE6), be16(p, 0xE8), be16(p, 0xEA)
	if progOff < 0xEC || progOff+nprog > len(p) || cellOff < 0xEC || cellOff+24*ncell > len(p) || posOff < 0xEC || posOff+4*ncell > len(p) {
		return nil, corrupt("program map, cell or position table outside the PGC")
	}
	for i := 0; i < nprog; i++ {
		c := int(p[progOff+i])
		if c < 1 || c > ncell {
			return nil, corrupt("program %d starts at cell %d of %d", i+1, c, ncell)
		}
		pg.Programs = append(pg.Programs, c)
	}
	for i := 0; i < ncell; i++ {
		c := p[cellOff+24*i:]
		t, err := parseTime(c[4:8])
		if err != nil {
			return nil, fmt.Errorf("cell %d: %w", i+1, err)
		}
		q := p[posOff+4*i:]
		cell := Cell{BlockMode: BlockMode(c[0] >> 6), AngleBlock: (c[0]>>4)&3 == 1, Time: t,
			FirstSector: be32(c, 8), LastSector: be32(c, 20), VOBID: be16(q, 0), CellID: int(q[3])}
		if cell.LastSector < cell.FirstSector {
			return nil, corrupt("cell %d ends at sector %d before it starts at %d", i+1, cell.LastSector, cell.FirstSector)
		}
		pg.Cells = append(pg.Cells, cell)
	}
	return pg, nil
}
