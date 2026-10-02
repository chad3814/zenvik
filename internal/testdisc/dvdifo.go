package testdisc

import (
	"encoding/binary"

	"github.com/chad3814/zenvik/dvd"
)

func put16(b []byte, off, v int)        { binary.BigEndian.PutUint16(b[off:], uint16(v)) }
func put32(b []byte, off int, v uint32) { binary.BigEndian.PutUint32(b[off:], v) }

// padSector pads b with zeros to a whole number of 2048-byte sectors.
func padSector(b []byte) []byte {
	if r := len(b) % 2048; r != 0 {
		b = append(b, make([]byte, 2048-r)...)
	}
	return b
}

func bcd(n int) byte { return byte(n/10<<4 | n%10) }

func putTime(b []byte, t dvd.Time) {
	b[0], b[1], b[2] = bcd(t.Hours), bcd(t.Minutes), bcd(t.Seconds)
	var r byte
	switch t.Rate {
	case dvd.Rate25:
		r = 1
	case dvd.Rate30:
		r = 3
	}
	b[3] = r<<6 | bcd(t.Frames)
}

// VMGFile encodes v as VIDEO_TS.IFO: the VMGI_MAT in sector 0 and the title
// table in sector 1.
func VMGFile(v *dvd.VMG) []byte {
	mat := make([]byte, 2048)
	copy(mat, "DVDVIDEO-VMG")
	put16(mat, 0x3E, v.TitleSets)
	put32(mat, 0xC4, 1)
	t := make([]byte, 8+12*len(v.Titles))
	put16(t, 0, len(v.Titles))
	put32(t, 4, uint32(len(t)-1))
	for i, e := range v.Titles {
		o := 8 + 12*i
		t[o+1] = byte(e.Angles)
		put16(t, o+2, e.Chapters)
		t[o+6] = byte(e.TitleSet)
		t[o+7] = byte(e.TitleSetTitle)
		put32(t, o+8, e.StartSector)
	}
	return append(mat, padSector(t)...)
}

// VTSFile encodes v as VTS_nn_0.IFO: the VTSI_MAT in sector 0, then the
// part-of-title table, then the PGC table, each starting on a sector.
func VTSFile(v *dvd.VTS) []byte {
	mat := make([]byte, 2048)
	copy(mat, "DVDVIDEO-VTS")
	encodeVideo(mat[0x200:], v.Video)
	mat[0x203] = byte(len(v.Audio))
	for i, a := range v.Audio {
		encodeAudio(mat[0x204+8*i:], a)
	}
	mat[0x255] = byte(len(v.Subpictures))
	for i, s := range v.Subpictures {
		encodeSubpicture(mat[0x256+6*i:], s)
	}
	ptt := padSector(encodePTT(v.Titles))
	pgci := padSector(encodePGCI(v.PGCs))
	put32(mat, 0xC8, 1)
	put32(mat, 0xCC, uint32(1+len(ptt)/2048))
	out := append(mat, ptt...)
	return append(out, pgci...)
}

func encodeVideo(b []byte, v dvd.VideoAttributes) {
	var c, s, a byte
	if v.Coding == dvd.MPEG2 {
		c = 1
	}
	full := 480
	if v.Standard == dvd.PAL {
		s, full = 1, 576
	}
	if v.Aspect == dvd.Aspect16x9 {
		a = 3
	}
	b[0] = c<<6 | s<<4 | a<<2
	var size byte
	switch {
	case v.Width == 704:
		size = 1
	case v.Width == 352 && v.Height == full:
		size = 2
	case v.Width == 352:
		size = 3
	}
	b[1] = size << 2
}

func encodeAudio(b []byte, a dvd.AudioAttributes) {
	coding := map[dvd.AudioCoding]byte{dvd.AC3: 0, dvd.MPEG1Audio: 2, dvd.MPEG2Audio: 3, dvd.LPCM: 4, dvd.DTS: 6}[a.Coding]
	b[0] = coding << 5
	if a.Language != "" {
		b[0] |= 1 << 2
		b[2], b[3] = a.Language[0], a.Language[1]
	}
	if a.SampleRate == 96000 {
		b[1] = 1 << 4
	}
	b[1] |= byte(a.Channels-1) & 7
	b[5] = byte(a.CodeExtension)
}

func encodeSubpicture(b []byte, s dvd.SubpictureAttributes) {
	if s.Language != "" {
		b[0] = 1
		b[2], b[3] = s.Language[0], s.Language[1]
	}
	b[5] = byte(s.CodeExtension)
}

func encodePTT(titles [][]dvd.PartOfTitle) []byte {
	hdr := 8 + 4*len(titles)
	size := hdr
	for _, t := range titles {
		size += 4 * len(t)
	}
	b := make([]byte, size)
	put16(b, 0, len(titles))
	put32(b, 4, uint32(size-1))
	o := hdr
	for i, t := range titles {
		put32(b, 8+4*i, uint32(o))
		for _, p := range t {
			put16(b, o, p.PGC)
			put16(b, o+2, p.Program)
			o += 4
		}
	}
	return b
}

func encodePGCI(pgcs []*dvd.PGC) []byte {
	hdr := 8 + 8*len(pgcs)
	b := make([]byte, hdr)
	put16(b, 0, len(pgcs))
	for i, p := range pgcs {
		b[8+8*i] = 0x80
		put32(b, 8+8*i+4, uint32(len(b)))
		b = append(b, encodePGC(p)...)
	}
	put32(b, 4, uint32(len(b)-1))
	return b
}

func encodePGC(p *dvd.PGC) []byte {
	progOff := 0xEC
	cellOff := progOff + len(p.Programs)
	posOff := cellOff + 24*len(p.Cells)
	b := make([]byte, posOff+4*len(p.Cells))
	b[2], b[3] = byte(len(p.Programs)), byte(len(p.Cells))
	putTime(b[4:], p.Time)
	for i, a := range p.Audio {
		if a.Available {
			b[0x0C+2*i] = 0x80 | byte(a.Stream&7)
		}
	}
	for i, s := range p.Subpictures {
		if s.Available {
			o := 0x1C + 4*i
			b[o] = 0x80 | byte(s.Stream4x3&0x1F)
			b[o+1], b[o+2], b[o+3] = byte(s.Wide&0x1F), byte(s.Letterbox&0x1F), byte(s.PanScan&0x1F)
		}
	}
	for i, c := range p.Palette {
		put32(b, 0xA4+4*i, c)
	}
	if len(p.Cells) > 0 {
		put16(b, 0xE6, progOff)
		put16(b, 0xE8, cellOff)
		put16(b, 0xEA, posOff)
	}
	for i, n := range p.Programs {
		b[progOff+i] = byte(n)
	}
	for i, c := range p.Cells {
		o := cellOff + 24*i
		b[o] = byte(c.BlockMode) << 6
		if c.AngleBlock {
			b[o] |= 1 << 4
		}
		putTime(b[o+4:], c.Time)
		put32(b, o+8, c.FirstSector)
		put32(b, o+20, c.LastSector)
		q := posOff + 4*i
		put16(b, q, c.VOBID)
		b[q+3] = byte(c.CellID)
	}
	return b
}
