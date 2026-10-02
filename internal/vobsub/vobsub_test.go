package vobsub

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pack returns a 2048-byte MPEG-2 pack header with nothing after it.
func pack() []byte {
	p := make([]byte, 2048)
	copy(p, []byte{0, 0, 1, 0xBA, 0x44, 0, 4, 0, 4, 1, 0x01, 0x89, 0xC3, 0xF8})
	return p
}

// navPack builds a NAV pack for VOB vob, cell cell, cell elapsed time
// elapsed (whole IFO seconds at NTSC: 30 frames of 1001/30000 s, so 3 is
// 3.003 s) and VOBU start PTS ptm.
func navPack(vob, cell, elapsed int, ptm uint32) []byte {
	p := pack()
	copy(p[0x0E:], []byte{0, 0, 1, 0xBB, 0x00, 0x12})
	copy(p[0x26:], []byte{0, 0, 1, 0xBF, 0x03, 0xD4, 0x00})
	binary.BigEndian.PutUint32(p[0x2D+12:], ptm)
	binary.BigEndian.PutUint32(p[0x2D+16:], ptm+90000) // vobu_e_ptm: a 1 s VOBU
	copy(p[0x400:], []byte{0, 0, 1, 0xBF, 0x03, 0xFA, 0x01})
	dsi := p[0x407:]
	binary.BigEndian.PutUint16(dsi[24:], uint16(vob))
	dsi[27] = byte(cell)
	bcd := func(n int) byte { return byte(n/10<<4 | n%10) }
	dsi[28], dsi[29], dsi[30], dsi[31] = bcd(elapsed/3600), bcd(elapsed/60%60), bcd(elapsed%60), 3<<6
	return p
}

// spuPack builds a private-stream-1 pack for subpicture stream id; a
// non-nil pts marks the first pack of a subpicture packet.
func spuPack(id int, pts *uint32) []byte {
	p := pack()
	copy(p[14:], []byte{0, 0, 1, 0xBD, 0x07, 0xEC, 0x81})
	o := 14
	if pts == nil {
		p[o+7], p[o+8] = 0x00, 0
		p[o+9] = 0x20 + byte(id)
		return p
	}
	v := *pts
	p[o+7], p[o+8] = 0x80, 5
	p[o+9] = 0x21 | byte(v>>29)&0x0E
	p[o+10] = byte(v >> 22)
	p[o+11] = byte(v>>14)&0xFE | 1
	p[o+12] = byte(v >> 7)
	p[o+13] = byte(v<<1)&0xFE | 1
	p[o+14] = 0x20 + byte(id)
	return p
}

func videoPack() []byte {
	p := pack()
	copy(p[14:], []byte{0, 0, 1, 0xE0, 0x07, 0xEC, 0x81, 0x00, 0x00})
	return p
}

func u32(v uint32) *uint32 { return &v }

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	var vob1, vob2 []byte
	vob1 = append(vob1, navPack(1, 1, 0, 900000)...)       // cell 1 starts at title 0; VOBU PTS 10 s
	vob1 = append(vob1, spuPack(0, u32(900000+180000))...) // stream 0 at 10 s + 2 s → title 2 s
	vob1 = append(vob1, spuPack(0, nil)...)                // continuation of that packet
	vob1 = append(vob1, videoPack()...)
	vob1 = append(vob1, spuPack(2, u32(900000))...) // stream 2 is not requested
	vob2 = append(vob2, navPack(1, 2, 3, 0)...)     // cell 2 (starts at 25 min), 00:00:03:00 = 3.003 s in, PTS reset to 0
	vob2 = append(vob2, spuPack(1, u32(45000))...)  // stream 1 at 0.5 s → 25:03.503
	p1, p2 := filepath.Join(dir, "VTS_01_1.VOB"), filepath.Join(dir, "VTS_01_2.VOB")
	if err := os.WriteFile(p1, vob1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, vob2, 0o644); err != nil {
		t.Fatal(err)
	}
	var palette [16]uint32
	palette[0], palette[1] = 0x00108080, 0x00EB8080
	out := t.TempDir()
	res, err := Extract(context.Background(), Params{
		Spans:   []Span{{Path: p1}, {Path: p2}},
		Cells:   []Cell{{VOBID: 1, CellID: 1, Start: 0}, {VOBID: 1, CellID: 2, Start: 25 * time.Minute}},
		Streams: []Stream{{ID: 0, Language: "en"}, {ID: 1, Language: "fr"}, {ID: 3, Language: "de"}},
		Palette: palette, Width: 720, Height: 480, Dir: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Streams) != 2 || res.Streams[0].ID != 0 || res.Streams[1].ID != 1 {
		t.Errorf("streams = %+v (stream 3 has no subtitles and must be left out)", res.Streams)
	}
	idx, err := os.ReadFile(res.IDX)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# VobSub index file, v7 (do not modify this line!)\n",
		"size: 720x480\n",
		"palette: 000000, ffffff, 000000,",
		"id: en, index: 0\ntimestamp: 00:00:02:000, filepos: 000000000\n",
		"id: fr, index: 1\ntimestamp: 00:25:03:503, filepos: 000001000\n",
	} {
		if !strings.Contains(string(idx), want) {
			t.Errorf("idx lacks %q:\n%s", want, idx)
		}
	}
	if strings.Contains(string(idx), "index: 2") || strings.Contains(string(idx), "index: 3") {
		t.Errorf("idx has unrequested or empty streams:\n%s", idx)
	}
	sub, err := os.ReadFile(strings.TrimSuffix(res.IDX, ".idx") + ".sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 3*2048 {
		t.Errorf(".sub is %d bytes, want 3 packs", len(sub))
	}
}

func TestExtractRejectsPartialPack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.vob")
	if err := os.WriteFile(p, make([]byte, 3000), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(context.Background(), Params{Spans: []Span{{Path: p}}, Streams: []Stream{{ID: 0}}, Width: 720, Height: 480, Dir: t.TempDir()}); err == nil {
		t.Error("want an error for a VOB that is not a whole number of packs")
	}
}

func TestPaletteRGB(t *testing.T) {
	for in, want := range map[uint32]string{0x00108080: "000000", 0x00EB8080: "ffffff", 0x00296EF0: "0000ff"} {
		if got := rgb(in); got != want {
			t.Errorf("rgb(%06X) = %s, want %s", in, got, want)
		}
	}
}

func TestPTSDelta(t *testing.T) {
	if got := ptsDelta(90000, 0); got != time.Second {
		t.Errorf("forward = %v", got)
	}
	if got := ptsDelta(10, 1<<33-80); got != 90*time.Second/90000 {
		t.Errorf("wrap = %v", got)
	}
	if got := ptsDelta(0, 90000); got != -time.Second {
		t.Errorf("backward = %v", got)
	}
}

func TestExtractSpanAndOffset(t *testing.T) {
	dir := t.TempDir()
	var vob []byte
	vob = append(vob, videoPack()...)                   // sector 0: outside the span
	vob = append(vob, spuPack(0, u32(900000))...)       // sector 1: outside the span
	vob = append(vob, navPack(1, 1, 0, 900000)...)      // sector 2: span starts here
	vob = append(vob, spuPack(0, u32(900000+90000))...) // 1 s into cell 1
	p := filepath.Join(dir, "v.vob")
	if err := os.WriteFile(p, vob, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Extract(context.Background(), Params{
		Spans: []Span{{Path: p, Offset: 2 * 2048, Length: 2 * 2048}}, TimeOffset: 10 * time.Minute,
		Cells: []Cell{{VOBID: 1, CellID: 1}}, Streams: []Stream{{ID: 0, Language: "en"}}, Width: 720, Height: 480, Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := os.ReadFile(res.IDX)
	if !strings.Contains(string(idx), "timestamp: 00:10:01:000, filepos: 000000000\n") || strings.Count(string(idx), "timestamp:") != 1 {
		t.Errorf("idx:\n%s", idx)
	}
}
