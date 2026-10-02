package dvd_test

import (
	"encoding/binary"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func sampleVTS() *dvd.VTS {
	t := func(d time.Duration) dvd.Time { return dvd.NewTime(d, dvd.Rate30) }
	var audio [8]dvd.AudioControl
	audio[0] = dvd.AudioControl{Available: true, Stream: 0}
	audio[1] = dvd.AudioControl{Available: true, Stream: 1}
	var subs [32]dvd.SubpictureControl
	subs[0] = dvd.SubpictureControl{Available: true, Stream4x3: 0, Wide: 1, Letterbox: 2, PanScan: 3}
	pgc := &dvd.PGC{
		Time: t(30 * time.Minute), Audio: audio, Subpictures: subs,
		Palette:  [16]uint32{0x00108080, 0x00EB8080},
		Programs: []int{1, 3},
		Cells: []dvd.Cell{
			{Time: t(10 * time.Minute), FirstSector: 0, LastSector: 99, VOBID: 1, CellID: 1},
			{BlockMode: dvd.FirstInBlock, AngleBlock: true, Time: t(10 * time.Minute), FirstSector: 100, LastSector: 199, VOBID: 1, CellID: 2},
			{BlockMode: dvd.LastInBlock, AngleBlock: true, Time: t(10 * time.Minute), FirstSector: 200, LastSector: 299, VOBID: 2, CellID: 1},
		},
	}
	return &dvd.VTS{
		Video: dvd.VideoAttributes{Coding: dvd.MPEG2, Standard: dvd.PAL, Aspect: dvd.Aspect16x9, Width: 720, Height: 576},
		Audio: []dvd.AudioAttributes{
			{Coding: dvd.AC3, Channels: 6, SampleRate: 48000, Language: "en", CodeExtension: dvd.AudioNormal},
			{Coding: dvd.DTS, Channels: 2, SampleRate: 96000, Language: "", CodeExtension: dvd.AudioDirectorsComments},
		},
		Subpictures: []dvd.SubpictureAttributes{{Language: "fr", CodeExtension: dvd.SubpictureForced}},
		Titles:      [][]dvd.PartOfTitle{{{PGC: 1, Program: 1}, {PGC: 1, Program: 2}}, {{PGC: 2, Program: 1}}},
		VOBUs:       []uint32{0, 100, 200, 300},
		PGCs:        []*dvd.PGC{pgc, {Time: t(time.Minute), Programs: []int{1}, Cells: []dvd.Cell{{Time: t(time.Minute), FirstSector: 300, LastSector: 309, VOBID: 3, CellID: 1}}}},
	}
}

func TestVMGRoundTrip(t *testing.T) {
	want := &dvd.VMG{TitleSets: 2, Titles: []dvd.TitleEntry{
		{Angles: 1, Chapters: 12, TitleSet: 1, TitleSetTitle: 1, StartSector: 300},
		{Angles: 3, Chapters: 1, TitleSet: 2, TitleSetTitle: 1, StartSector: 9000},
	}}
	got, err := dvd.ParseVMG(testdisc.VMGFile(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestVTSRoundTrip(t *testing.T) {
	want := sampleVTS()
	got, err := dvd.ParseVTS(testdisc.VTSFile(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseCorrupt(t *testing.T) {
	vmg := func(mut func(b []byte)) []byte {
		b := testdisc.VMGFile(&dvd.VMG{TitleSets: 1, Titles: []dvd.TitleEntry{{Angles: 1, Chapters: 1, TitleSet: 1, TitleSetTitle: 1}}})
		mut(b)
		return b
	}
	vts := func(mut func(v *dvd.VTS, b []byte) []byte) []byte {
		v := sampleVTS()
		return mut(v, testdisc.VTSFile(v))
	}
	pgc := 2 * 2048 // sampleVTS's PTT table fits in one sector, so the PGC table starts at sector 2
	firstPGC := pgc + 8 + 8*2
	tests := map[string][]byte{
		"vmg magic":        vmg(func(b []byte) { b[0] = 'X' }),
		"vmg table sector": vmg(func(b []byte) { b[0xC6] = 0x40 }),
		"vmg no titles":    vmg(func(b []byte) { b[2048] = 0; b[2049] = 0 }),
		"vmg title set 0":  vmg(func(b []byte) { b[2048+8+6] = 0 }),
		"vmg truncated":    vmg(func(b []byte) {})[:2048+10],
		"vts magic":        vts(func(_ *dvd.VTS, b []byte) []byte { b[4] = 'X'; return b }),
		"vts 9 audio":      vts(func(_ *dvd.VTS, b []byte) []byte { b[0x203] = 9; return b }),
		"vts video coding": vts(func(_ *dvd.VTS, b []byte) []byte { b[0x200] |= 0xC0; return b }),
		"vts ptt to missing pgc": vts(func(v *dvd.VTS, _ []byte) []byte {
			v.Titles[1][0].PGC = 5
			return testdisc.VTSFile(v)
		}),
		"vts program cell 0": vts(func(v *dvd.VTS, _ []byte) []byte {
			v.PGCs[0].Programs[0] = 0
			return testdisc.VTSFile(v)
		}),
		"vts cell backwards": vts(func(v *dvd.VTS, _ []byte) []byte {
			v.PGCs[0].Cells[0].FirstSector = 500
			return testdisc.VTSFile(v)
		}),
		"vts bad bcd minutes": vts(func(_ *dvd.VTS, b []byte) []byte { b[firstPGC+5] = 0x6A; return b }),
		"vts pgc offset outside table": vts(func(_ *dvd.VTS, b []byte) []byte {
			b[pgc+8+4], b[pgc+8+5] = 0x00, 0xFF
			return b
		}),
		"vts cell table outside pgc": vts(func(_ *dvd.VTS, b []byte) []byte {
			b[firstPGC+0xE8], b[firstPGC+0xE9] = 0xFF, 0xF0
			return b
		}),
		"vts position table outside pgc": vts(func(_ *dvd.VTS, b []byte) []byte {
			b[firstPGC+0xEA], b[firstPGC+0xEB] = 0xFF, 0xF0
			return b
		}),
		"vts truncated": vts(func(_ *dvd.VTS, b []byte) []byte { return b[:pgc+20] }),
	}
	for name, b := range tests {
		t.Run(name, func(t *testing.T) {
			var err error
			if name[:3] == "vmg" {
				_, err = dvd.ParseVMG(b)
			} else {
				_, err = dvd.ParseVTS(b)
			}
			if !errors.Is(err, dvd.ErrCorrupt) {
				t.Errorf("err = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestTimeDuration(t *testing.T) {
	// NTSC IFO times count frames at a nominal 30 fps; each lasts 1001/30000 s.
	ntsc := dvd.Time{Hours: 1, Minutes: 2, Seconds: 3, Frames: 15, Rate: dvd.Rate30}
	if got, want := ntsc.Duration(), time.Duration(3723*30+15)*1001*time.Second/30000; got != want {
		t.Errorf("NTSC = %v, want %v", got, want)
	}
	if got, want := (dvd.Time{Seconds: 6, Rate: dvd.Rate30}).Duration(), 6006*time.Millisecond; got != want {
		t.Errorf("NTSC 00:00:06:00 = %v, want %v (180 frames)", got, want)
	}
	if got, want := (dvd.Time{Seconds: 1, Rate: dvd.Rate30}).Duration(), 1001*time.Millisecond; got != want {
		t.Errorf("NTSC 00:00:01:00 = %v, want %v", got, want)
	}
	if got, want := (dvd.Time{Hours: 99, Minutes: 59, Seconds: 59, Frames: 29, Rate: dvd.Rate30}).Duration(), time.Duration(10799999)*100100000/3; got != want {
		t.Errorf("NTSC 99:59:59:29 = %v, want %v", got, want)
	}
	if got := (dvd.Time{}).Duration(); got != 0 {
		t.Errorf("zero time = %v", got)
	}
	pal := dvd.Time{Seconds: 1, Frames: 24, Rate: dvd.Rate25}
	if got := pal.Duration(); got != time.Second+960*time.Millisecond {
		t.Errorf("PAL = %v", got)
	}
}

func TestNewTime(t *testing.T) {
	ntsc := func(h, m, s, f int) dvd.Time {
		return dvd.Time{Hours: h, Minutes: m, Seconds: s, Frames: f, Rate: dvd.Rate30}
	}
	pal := func(h, m, s, f int) dvd.Time {
		return dvd.Time{Hours: h, Minutes: m, Seconds: s, Frames: f, Rate: dvd.Rate25}
	}
	for _, tt := range []struct {
		d    time.Duration
		r    dvd.FrameRate
		want dvd.Time
	}{
		{0, dvd.Rate30, ntsc(0, 0, 0, 0)},
		{-time.Second, dvd.Rate30, ntsc(0, 0, 0, 0)},
		{6006 * time.Millisecond, dvd.Rate30, ntsc(0, 0, 6, 0)}, // 180 frames
		{6 * time.Second, dvd.Rate30, ntsc(0, 0, 6, 0)},         // 179.82 frames → 180
		{time.Second, dvd.Rate30, ntsc(0, 0, 1, 0)},             // 29.97 frames → 30
		{20 * time.Minute, dvd.Rate30, ntsc(0, 19, 58, 24)},     // 35964.04 frames
		{24 * time.Minute, dvd.Rate30, ntsc(0, 23, 58, 17)},     // 43156.84 → 43157 frames
		{(3723*30 + 15) * 1001 * time.Second / 30000, dvd.Rate30, ntsc(1, 2, 3, 15)},
		{100 * time.Hour, dvd.Rate30, ntsc(99, 54, 0, 11)}, // 10789210.79 frames
		{360400 * time.Second, dvd.Rate30, ntsc(99, 59, 59, 29)},
		{time.Second + 979*time.Millisecond, dvd.Rate25, pal(0, 0, 1, 24)}, // 49.475 frames → 49
		{time.Second + 980*time.Millisecond, dvd.Rate25, pal(0, 0, 2, 0)},  // 49.5 frames → 50
		{100 * time.Hour, dvd.Rate25, pal(99, 59, 59, 24)},
	} {
		if got := dvd.NewTime(tt.d, tt.r); got != tt.want {
			t.Errorf("NewTime(%v, %d) = %+v, want %+v", tt.d, tt.r, got, tt.want)
		}
	}
	for _, d := range []time.Duration{time.Second, 20 * time.Minute, 24 * time.Minute, 100 * time.Minute, 7581 * time.Second} {
		got := dvd.NewTime(d, dvd.Rate30).Duration()
		if diff := got - d; diff < -1001*time.Second/60000 || diff > 1001*time.Second/60000 {
			t.Errorf("NewTime(%v).Duration() = %v, more than half a frame away", d, got)
		}
	}
}

func TestLanguage6392(t *testing.T) {
	for in, want := range map[string]string{"en": "eng", "FR": "fre", "de": "ger", "iw": "heb", "zh": "chi", "sh": "scr", "mo": "rum", "xx": "", "": ""} {
		if got := dvd.Language6392(in); got != want {
			t.Errorf("Language6392(%q) = %q, want %q", in, got, want)
		}
	}
}

const (
	pgcSector  = 2 * 2048
	firstPGCAt = pgcSector + 8 + 8*2
)

func TestErrorMessagesStartWithDvd(t *testing.T) {
	b := testdisc.VTSFile(sampleVTS())
	b[firstPGC0()+5] = 0x6A // bad BCD minutes in PGC 1
	_, err := dvd.ParseVTS(b)
	if err == nil || !errors.Is(err, dvd.ErrCorrupt) {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(err.Error(), "dvd: corrupt IFO: ") || strings.Count(err.Error(), "dvd:") != 1 {
		t.Errorf("message = %q", err.Error())
	}
	// A cell error too.
	b = testdisc.VTSFile(sampleVTS())
	b[firstPGC0()+0xEC+2+3*0+0+24*0+4+1] = 0x6A // cell 1 time minutes
	_, err = dvd.ParseVTS(b)
	if err == nil || !strings.HasPrefix(err.Error(), "dvd: ") || strings.Count(err.Error(), "dvd:") != 1 {
		t.Errorf("cell message = %v", err)
	}
}

func firstPGC0() int { return firstPGCAt }

func TestAliasedPGCPointersShare(t *testing.T) {
	b := testdisc.VTSFile(sampleVTS())
	copy(b[pgcSector+8+8+4:pgcSector+8+8+8], b[pgcSector+8+4:pgcSector+8+8])
	v, err := dvd.ParseVTS(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.PGCs) != 2 || v.PGCs[0] != v.PGCs[1] {
		t.Errorf("PGCs not shared: %p %p", v.PGCs[0], v.PGCs[1])
	}
}

func TestReservedCountBytesIgnored(t *testing.T) {
	b := testdisc.VTSFile(sampleVTS())
	b[0x202], b[0x254] = 0xFF, 0xFF
	v, err := dvd.ParseVTS(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Audio) != 2 || len(v.Subpictures) != 1 {
		t.Errorf("audio %d subpictures %d", len(v.Audio), len(v.Subpictures))
	}
}

func TestTimeTolerance(t *testing.T) {
	parse := func(minutes, frameByte byte) dvd.Time {
		b := testdisc.VTSFile(sampleVTS())
		b[firstPGCAt+5], b[firstPGCAt+6], b[firstPGCAt+7] = minutes, 0, frameByte
		v, err := dvd.ParseVTS(b)
		if err != nil {
			t.Fatal(err)
		}
		return v.PGCs[0].Time
	}
	if got := parse(0x30, 0x00); got != (dvd.Time{Minutes: 30, Rate: dvd.Rate30}) {
		t.Errorf("rate bits 0: %+v", got)
	}
	if got := parse(0x30, 0x80); got != (dvd.Time{Minutes: 30, Rate: dvd.Rate30}) {
		t.Errorf("rate bits 2: %+v", got)
	}
	if got := parse(0x30, 0xC0|0x35); got.Frames != 29 || got.Rate != dvd.Rate30 {
		t.Errorf("30 fps clamp: %+v", got)
	}
	if got := parse(0x30, 0x40|0x30); got.Frames != 24 || got.Rate != dvd.Rate25 {
		t.Errorf("25 fps clamp: %+v", got)
	}
}

func TestPTTQuirks(t *testing.T) {
	// Title 2's offset points past the end of the table: no chapters, no error.
	b := testdisc.VTSFile(sampleVTS())
	b[2048+8+6] = 0x10
	v, err := dvd.ParseVTS(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Titles) != 2 || len(v.Titles[1]) != 0 {
		t.Errorf("titles = %+v", v.Titles)
	}
	// A span that is not a multiple of 4 keeps only whole entries.
	b = testdisc.VTSFile(sampleVTS())
	b[2048+7] = 29 // table end: title 2 now spans 6 bytes
	v, err = dvd.ParseVTS(b)
	if err != nil {
		t.Fatal(err)
	}
	if want := []dvd.PartOfTitle{{PGC: 2, Program: 1}}; !reflect.DeepEqual(v.Titles[1], want) {
		t.Errorf("title 2 = %+v, want %+v", v.Titles[1], want)
	}
	// An offset inside the header area is still corrupt.
	b = testdisc.VTSFile(sampleVTS())
	b[2048+8+4], b[2048+8+5], b[2048+8+6], b[2048+8+7] = 0, 0, 0, 4
	if _, err = dvd.ParseVTS(b); !errors.Is(err, dvd.ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

// TestOverlappingPGCsRejectedCheaply builds a PGC table whose search pointers
// sit 24 bytes apart, so each one parses as a distinct 255-cell chain
// overlapping the others. ParseVTS must refuse it without parsing them all.
func TestOverlappingPGCsRejectedCheaply(t *testing.T) {
	const n = 8000
	base := 8 + 8*n // a multiple of 24, so the repeating pattern stays aligned
	if base%24 != 0 {
		t.Fatalf("base %d is not a multiple of 24", base)
	}
	var pattern [24]byte
	pattern[3] = 255                                   // ncell (nprog stays 0)
	copy(pattern[4:8], []byte{0x00, 0x00, 0x01, 0xC0}) // a valid BCD time
	binary.BigEndian.PutUint16(pattern[14:], 0xF0)     // 0xE6: program map
	binary.BigEndian.PutUint16(pattern[16:], 0xF0)     // 0xE8: cell playback table
	binary.BigEndian.PutUint16(pattern[18:], 0xF0)     // 0xEA: cell position table
	tbl := make([]byte, base+24*n+0x1000)
	binary.BigEndian.PutUint16(tbl[0:], n)
	binary.BigEndian.PutUint32(tbl[4:], uint32(len(tbl)-1))
	for i := 0; i < n; i++ {
		binary.BigEndian.PutUint32(tbl[8+8*i+4:], uint32(base+24*i))
	}
	for o := base; o < len(tbl); o++ {
		tbl[o] = pattern[(o-base)%24]
	}
	b := testdisc.VTSFile(sampleVTS())
	sector := int(binary.BigEndian.Uint32(b[0xCC:]))
	b = append(b[:sector*2048], tbl...)
	b = append(b, 0) // the table's end address must fall inside the file

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := dvd.ParseVTS(b)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, dvd.ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 64<<20 {
		t.Errorf("ParseVTS allocated %d MiB on a hostile table", got>>20)
	}
}

func TestVOBUMapCorrupt(t *testing.T) {
	v := sampleVTS()
	b := testdisc.VTSFile(v)
	sec := int(binary.BigEndian.Uint32(b[0xE4:])) * 2048
	for name, mut := range map[string]func([]byte) []byte{
		"pointer past end": func(b []byte) []byte { binary.BigEndian.PutUint32(b[0xE4:], 0x7FFF); return b },
		"end past file":    func(b []byte) []byte { binary.BigEndian.PutUint32(b[sec:], 0x7FFFFFF); return b },
		"not ascending":    func(b []byte) []byte { binary.BigEndian.PutUint32(b[sec+8:], 0); return b },
	} {
		t.Run(name, func(t *testing.T) {
			c := append([]byte(nil), b...)
			if _, err := dvd.ParseVTS(mut(c)); !errors.Is(err, dvd.ErrCorrupt) {
				t.Errorf("err = %v, want ErrCorrupt", err)
			}
		})
	}
	v.VOBUs = nil
	got, err := dvd.ParseVTS(testdisc.VTSFile(v))
	if err != nil || got.VOBUs != nil {
		t.Errorf("no map: VOBUs %v, err %v", got.VOBUs, err)
	}
}
