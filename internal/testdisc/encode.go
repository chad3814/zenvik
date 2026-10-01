// Package testdisc builds synthetic Blu-ray navigation files and disc
// trees for tests. Its encoders are the inverse of the bluray parsers.
package testdisc

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"

	"github.com/chad3814/zenvik/bluray"
)

type writer struct{ b []byte }

func (w *writer) u8(v uint8)   { w.b = append(w.b, v) }
func (w *writer) u16(v uint16) { w.b = binary.BigEndian.AppendUint16(w.b, v) }
func (w *writer) u32(v uint32) { w.b = binary.BigEndian.AppendUint32(w.b, v) }
func (w *writer) zero(n int)   { w.b = append(w.b, make([]byte, n)...) }

// str writes s padded or truncated to exactly n bytes.
func (w *writer) str(s string, n int) {
	b := make([]byte, n)
	copy(b, s)
	w.b = append(w.b, b...)
}

func (w *writer) patch32(at int, v uint32) { binary.BigEndian.PutUint32(w.b[at:], v) }

// block32, block16 and block8 write a length field, then fn's output, then
// patch the length to the number of bytes fn wrote.
func (w *writer) block32(fn func()) {
	at := len(w.b)
	w.u32(0)
	fn()
	w.patch32(at, uint32(len(w.b)-at-4))
}

func (w *writer) block16(fn func()) {
	at := len(w.b)
	w.u16(0)
	fn()
	binary.BigEndian.PutUint16(w.b[at:], uint16(len(w.b)-at-2))
}

func (w *writer) block8(fn func()) {
	at := len(w.b)
	w.u8(0)
	fn()
	w.b[at] = uint8(len(w.b) - at - 1)
}

func version(v string) string {
	if v == "" {
		return "0200"
	}
	return v
}

// Index encodes an index.bdmv.
func Index(idx *bluray.Index) []byte {
	var w writer
	w.str("INDX", 4)
	w.str(version(idx.Version), 4)
	w.u32(0) // indexes start address, patched below
	w.u32(0) // extension data start address
	w.zero(24)
	w.block32(func() { w.zero(34) }) // AppInfoBDMV
	w.patch32(8, uint32(len(w.b)))
	w.block32(func() {
		object(&w, idx.FirstPlayback, 0)
		object(&w, idx.TopMenu, 0)
		w.u16(uint16(len(idx.Titles)))
		for _, t := range idx.Titles {
			object(&w, t.Object, t.AccessType)
		}
	})
	return w.b
}

func object(w *writer, o bluray.Object, access uint8) {
	w.u32(uint32(o.Type)<<30 | uint32(access&0x3)<<28)
	w.u16(uint16(o.PlaybackType) << 14)
	if o.Type == bluray.ObjectBDJ {
		w.str(o.BDJOName, 5)
		w.u8(0)
		return
	}
	w.u16(o.MovieObjectID)
	w.u32(0)
}

// MovieObjects encodes a MovieObject.bdmv.
func MovieObjects(m *bluray.MovieObjects) []byte {
	var w writer
	w.str("MOBJ", 4)
	w.str(version(m.Version), 4)
	w.u32(0) // extension data start address
	w.zero(28)
	w.block32(func() {
		w.zero(4)
		w.u16(uint16(len(m.Objects)))
		for _, o := range m.Objects {
			var flags uint16
			if o.ResumeIntention {
				flags |= 0x8000
			}
			if o.MenuCallMask {
				flags |= 0x4000
			}
			if o.TitleSearchMask {
				flags |= 0x2000
			}
			w.u16(flags)
			w.u16(uint16(len(o.Commands)))
			for _, c := range o.Commands {
				w.u32(c.Opcode)
				w.u32(c.Dst)
				w.u32(c.Src)
			}
		}
	})
	return w.b
}

// Playlist encodes an MPLS file.
func Playlist(p *bluray.Playlist) []byte {
	var w writer
	w.str("MPLS", 4)
	w.str(version(p.Version), 4)
	w.u32(0) // playlist start address, patched below
	w.u32(0) // playlist mark start address, patched below
	w.u32(0) // extension data start address
	w.zero(20)
	w.block32(func() { // AppInfoPlayList
		w.zero(1)
		w.u8(1) // playback type: sequential
		w.zero(2 + 8 + 2)
	})
	w.patch32(8, uint32(len(w.b)))
	w.block32(func() {
		w.zero(2)
		w.u16(uint16(len(p.Items)))
		w.u16(0) // sub-paths
		for _, it := range p.Items {
			playItem(&w, it)
		}
	})
	w.patch32(12, uint32(len(w.b)))
	w.block32(func() {
		w.u16(uint16(len(p.Marks)))
		for _, m := range p.Marks {
			w.zero(1)
			w.u8(uint8(m.Type))
			w.u16(m.PlayItem)
			w.u32(uint32(m.Time))
			w.u16(m.PID)
			w.u32(uint32(m.Duration))
		}
	})
	return w.b
}

func playItem(w *writer, it bluray.PlayItem) {
	codec := it.CodecID
	if codec == "" {
		codec = "M2TS"
	}
	w.block16(func() {
		w.str(it.ClipID, 5)
		w.str(codec, 4)
		flags := uint16(it.ConnectionCondition & 0x0F)
		if len(it.Angles) > 0 {
			flags |= 0x10
		}
		w.u16(flags)
		w.u8(it.STCID)
		w.u32(uint32(it.In))
		w.u32(uint32(it.Out))
		w.zero(8) // UO mask
		w.u8(0)   // random access flag
		w.u8(it.StillMode)
		w.u16(0) // still time
		if len(it.Angles) > 0 {
			w.u8(uint8(len(it.Angles) + 1))
			w.u8(0)
			for _, a := range it.Angles {
				w.str(a, 5)
				w.str("M2TS", 4)
				w.u8(0)
			}
		}
		stn(w, it.STN)
	})
}

func stn(w *writer, s bluray.STN) {
	w.block16(func() {
		w.zero(2)
		w.u8(uint8(len(s.Video)))
		w.u8(uint8(len(s.Audio)))
		w.u8(uint8(len(s.PG)))
		w.zero(4) // IG, secondary audio, secondary video, PiP PG
		w.zero(5)
		for _, group := range [][]bluray.Stream{s.Video, s.Audio, s.PG} {
			for _, st := range group {
				w.block8(func() {
					w.u8(1) // stream in main clip
					w.u16(st.PID)
					w.zero(6)
				})
				attributes(w, st, false)
			}
		}
	})
}

func attributes(w *writer, s bluray.Stream, clip bool) {
	w.block8(func() {
		w.u8(uint8(s.Coding))
		switch s.Coding.Kind() {
		case bluray.KindVideo:
			w.u8(uint8(s.VideoFormat)<<4 | uint8(s.FrameRate)&0x0F)
			if clip {
				w.u8(0x30) // aspect ratio 16:9
			}
			if s.Coding == bluray.CodingHEVC {
				w.u8(uint8(s.DynamicRange) << 4)
			}
		case bluray.KindAudio:
			w.u8(uint8(s.AudioFormat)<<4 | uint8(s.SampleRate)&0x0F)
			w.str(s.Language, 3)
		case bluray.KindPG, bluray.KindIG:
			w.str(s.Language, 3)
		case bluray.KindText:
			w.u8(1) // UTF-8
			w.str(s.Language, 3)
		}
	})
}

// Clip encodes a CLPI file.
func Clip(c *bluray.Clip) []byte {
	var w writer
	w.str("HDMV", 4)
	w.str(version(c.Version), 4)
	w.u32(0)   // sequence info start address, patched below
	w.u32(0)   // program info start address, patched below
	w.zero(12) // CPI, clip mark, extension data start addresses
	w.zero(12)
	w.block32(func() { // ClipInfo
		w.zero(2)
		w.u8(c.StreamType)
		w.u8(c.ApplicationType)
		w.zero(4)
		w.u32(c.TSRecordingRate)
		w.u32(c.SourcePackets)
		w.zero(128)
		w.block16(func() {}) // TS type info block
	})
	w.patch32(8, uint32(len(w.b)))
	w.block32(func() { w.zero(2) }) // SequenceInfo with no ATC sequences
	w.patch32(12, uint32(len(w.b)))
	w.block32(func() {
		w.zero(1)
		w.u8(uint8(len(c.Programs)))
		for _, p := range c.Programs {
			w.u32(p.SPNStart)
			w.u16(p.PMTPID)
			w.u8(uint8(len(p.Streams)))
			w.u8(0)
			for _, s := range p.Streams {
				w.u16(s.PID)
				attributes(&w, s, true)
			}
		}
	})
	return w.b
}

// Meta encodes a disc library metadata file.
func Meta(title, lang string) []byte {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<disclib xmlns="urn:BDA:bdmv;disclib" xmlns:di="urn:BDA:bdmv;discinfo"><di:discinfo><di:title><di:name>`)
	_ = xml.EscapeText(&buf, []byte(title))
	buf.WriteString(`</di:name></di:title><di:language>`)
	_ = xml.EscapeText(&buf, []byte(lang))
	buf.WriteString(`</di:language></di:discinfo></disclib>`)
	return buf.Bytes()
}
