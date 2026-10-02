// Package vobsub copies DVD subpicture streams out of title VOBs into a
// VobSub .idx/.sub pair, which mkvmerge reads (it ignores subpictures
// inside VOBs).
package vobsub

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const packSize = 2048

// Cell is an angle-1 cell of the title and where it starts in the title.
type Cell struct {
	VOBID, CellID int
	Start         time.Duration
}

// Stream is a subpicture stream to extract.
type Stream struct {
	ID       int    // physical stream number, 0..31 (sub-stream 0x20+ID)
	Language string // ISO 639-1 code for the .idx; "" writes "--"
}

// Params describe an extraction.
type Params struct {
	VOBs          []string // the title's VOB files, in order
	Cells         []Cell   // the title's angle-1 cells
	Streams       []Stream // streams to extract, in output order
	Palette       [16]uint32
	Width, Height int
	Dir           string                  // where subtitles.idx and subtitles.sub are written
	OnProgress    func(done, total int64) // optional: bytes read so far
}

// Result is a finished extraction.
type Result struct {
	IDX     string   // path of the .idx; the .sub sits beside it
	Streams []Stream // the streams that had at least one subtitle, in .idx order
}

type entry struct {
	at  time.Duration
	pos int64
}

// Extract copies every pack of the requested streams into Dir/subtitles.sub
// and writes Dir/subtitles.idx.
func Extract(ctx context.Context, p Params) (*Result, error) {
	want := map[int]bool{}
	for _, s := range p.Streams {
		want[s.ID] = true
	}
	starts := map[[2]int]time.Duration{}
	for _, c := range p.Cells {
		starts[[2]int{c.VOBID, c.CellID}] = c.Start
	}
	var total int64
	for _, v := range p.VOBs {
		st, err := os.Stat(v)
		if err != nil {
			return nil, err
		}
		if st.Size()%packSize != 0 {
			return nil, fmt.Errorf("vobsub: %s is not a whole number of 2048-byte packs", v)
		}
		total += st.Size()
	}
	subPath := filepath.Join(p.Dir, "subtitles.sub")
	sub, err := os.Create(subPath)
	if err != nil {
		return nil, err
	}
	w := bufio.NewWriterSize(sub, 1<<20)
	entries := map[int][]entry{}
	var subPos, done int64
	var base time.Duration
	var ptm uint64 // vobu_s_ptm of the current VOBU
	known := false
	buf := make([]byte, packSize)
	for _, v := range p.VOBs {
		f, err := os.Open(v)
		if err != nil {
			sub.Close()
			return nil, err
		}
		r := bufio.NewReaderSize(f, 1<<20)
		for n := 0; ; n++ {
			if n%4096 == 0 {
				if err := ctx.Err(); err != nil {
					f.Close()
					sub.Close()
					return nil, err
				}
				if p.OnProgress != nil {
					p.OnProgress(done, total)
				}
			}
			if _, err := io.ReadFull(r, buf); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				f.Close()
				sub.Close()
				return nil, fmt.Errorf("vobsub: reading %s: %w", v, err)
			}
			done += packSize
			if vob, cell, elapsed, s, ok := navInfo(buf); ok {
				start, found := starts[[2]int{vob, cell}]
				base, ptm, known = start+elapsed, uint64(s), found
				continue
			}
			id, pts, hasPTS, ok := spuInfo(buf)
			if !ok || !want[id] {
				continue
			}
			if hasPTS && known {
				at := max(base+ptsDelta(pts, ptm), 0)
				entries[id] = append(entries[id], entry{at: at, pos: subPos})
			}
			if _, err := w.Write(buf); err != nil {
				f.Close()
				sub.Close()
				return nil, err
			}
			subPos += packSize
		}
		f.Close()
	}
	if err := w.Flush(); err != nil {
		sub.Close()
		return nil, err
	}
	if err := sub.Close(); err != nil {
		return nil, err
	}
	if p.OnProgress != nil {
		p.OnProgress(total, total)
	}
	res := &Result{IDX: filepath.Join(p.Dir, "subtitles.idx")}
	var b strings.Builder
	b.WriteString("# VobSub index file, v7 (do not modify this line!)\n# Created by zenvik\n")
	fmt.Fprintf(&b, "size: %dx%d\n", p.Width, p.Height)
	colors := make([]string, len(p.Palette))
	for i, c := range p.Palette {
		colors[i] = rgb(c)
	}
	fmt.Fprintf(&b, "palette: %s\n", strings.Join(colors, ", "))
	b.WriteString("langidx: 0\n")
	for _, s := range p.Streams {
		es := entries[s.ID]
		if len(es) == 0 {
			continue
		}
		res.Streams = append(res.Streams, s)
		lang := s.Language
		if lang == "" {
			lang = "--"
		}
		fmt.Fprintf(&b, "\nid: %s, index: %d\n", lang, s.ID)
		for _, e := range es {
			ms := e.at.Milliseconds()
			fmt.Fprintf(&b, "timestamp: %02d:%02d:%02d:%03d, filepos: %09x\n", ms/3_600_000, ms/60_000%60, ms/1000%60, ms%1000, e.pos)
		}
	}
	if err := os.WriteFile(res.IDX, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return res, nil
}

// navInfo reads a NAV pack: the DSI's VOB ID, cell ID and cell elapsed
// time, and the PCI's VOBU start PTS.
func navInfo(p []byte) (vob, cell int, elapsed time.Duration, ptm uint32, ok bool) {
	if !packHeader(p) || p[0x0E] != 0 || p[0x0F] != 0 || p[0x10] != 1 || p[0x11] != 0xBB ||
		p[0x26] != 0 || p[0x27] != 0 || p[0x28] != 1 || p[0x29] != 0xBF || p[0x2C] != 0x00 ||
		p[0x400] != 0 || p[0x401] != 0 || p[0x402] != 1 || p[0x403] != 0xBF || p[0x406] != 0x01 {
		return 0, 0, 0, 0, false
	}
	ptm = binary.BigEndian.Uint32(p[0x2D+12:])
	dsi := p[0x407:]
	return int(binary.BigEndian.Uint16(dsi[24:])), int(dsi[27]), bcdTime(dsi[28:32]), ptm, true
}

// spuInfo reports whether pack p carries a subpicture PES packet (private
// stream 1, sub-stream 0x20–0x3F), its stream number, and its PTS if the
// packet has one.
func spuInfo(p []byte) (id int, pts uint64, hasPTS, ok bool) {
	if !packHeader(p) {
		return 0, 0, false, false
	}
	o := 14 + int(p[13]&7)
	if o+9 > len(p) || p[o] != 0 || p[o+1] != 0 || p[o+2] != 1 || p[o+3] != 0xBD {
		return 0, 0, false, false
	}
	hl := int(p[o+8])
	if o+9+hl >= len(p) {
		return 0, 0, false, false
	}
	sub := p[o+9+hl]
	if sub&0xE0 != 0x20 {
		return 0, 0, false, false
	}
	if p[o+7]&0x80 != 0 && hl >= 5 {
		b := p[o+9 : o+14]
		pts = uint64(b[0]>>1&7)<<30 | uint64(b[1])<<22 | uint64(b[2]>>1)<<15 | uint64(b[3])<<7 | uint64(b[4]>>1)
		hasPTS = true
	}
	return int(sub & 0x1F), pts, hasPTS, true
}

func packHeader(p []byte) bool {
	return len(p) == packSize && p[0] == 0 && p[1] == 0 && p[2] == 1 && p[3] == 0xBA && p[4]&0xC0 == 0x40
}

// ptsDelta returns pts − ref on the 33-bit 90 kHz clock, choosing the
// shorter way around a wrap.
func ptsDelta(pts, ref uint64) time.Duration {
	const mod = int64(1) << 33
	d := (int64(pts) - int64(ref)) % mod
	if d < 0 {
		d += mod
	}
	if d >= mod/2 {
		d -= mod
	}
	return time.Duration(d) * time.Second / 90000
}

// bcdTime decodes a 4-byte BCD playback time, tolerating bad digits and
// rates (they decode as zero or 30 fps).
func bcdTime(b []byte) time.Duration {
	dec := func(x byte) int {
		hi, lo := int(x>>4), int(x&0x0F)
		if hi > 9 || lo > 9 {
			return 0
		}
		return hi*10 + lo
	}
	d := time.Duration(dec(b[0]))*time.Hour + time.Duration(dec(b[1]))*time.Minute + time.Duration(dec(b[2]))*time.Second
	frames := time.Duration(dec(b[3] & 0x3F))
	if b[3]>>6 == 1 {
		return d + frames*40*time.Millisecond
	}
	return d + frames*1001*time.Second/30000
}

// rgb converts a palette entry (0x00YYCrCb, studio range) to "rrggbb"
// with the BT.601 matrix. An all-zero entry is an unused palette slot
// (Y 0 is outside studio range) and is written as black.
func rgb(c uint32) string {
	if c&0xFFFFFF == 0 {
		return "000000"
	}
	y := float64(c>>16&0xFF) - 16
	cr := float64(c>>8&0xFF) - 128
	cb := float64(c&0xFF) - 128
	clamp := func(v float64) int { return int(math.Max(0, math.Min(255, math.Round(v)))) }
	r := clamp(1.164*y + 1.596*cr)
	g := clamp(1.164*y - 0.813*cr - 0.391*cb)
	b := clamp(1.164*y + 2.018*cb)
	return fmt.Sprintf("%02x%02x%02x", r, g, b)
}
