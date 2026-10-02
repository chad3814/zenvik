package dvd_test

import (
	"errors"
	"reflect"
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
		"vts frame rate 2":    vts(func(_ *dvd.VTS, b []byte) []byte { b[firstPGC+7] = 0x80; return b }),
		"vts truncated":       vts(func(_ *dvd.VTS, b []byte) []byte { return b[:pgc+20] }),
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
	ntsc := dvd.Time{Hours: 1, Minutes: 2, Seconds: 3, Frames: 15, Rate: dvd.Rate30}
	if got, want := ntsc.Duration(), 3723*time.Second+15*1001*time.Second/30000; got != want {
		t.Errorf("NTSC = %v, want %v", got, want)
	}
	pal := dvd.Time{Seconds: 1, Frames: 24, Rate: dvd.Rate25}
	if got := pal.Duration(); got != time.Second+960*time.Millisecond {
		t.Errorf("PAL = %v", got)
	}
	if got := dvd.NewTime(3723*time.Second+500*time.Millisecond, dvd.Rate30); got != (dvd.Time{Hours: 1, Minutes: 2, Seconds: 3, Frames: 14, Rate: dvd.Rate30}) {
		t.Errorf("NewTime = %+v", got)
	}
}

func TestLanguage6392(t *testing.T) {
	for in, want := range map[string]string{"en": "eng", "FR": "fre", "de": "ger", "iw": "heb", "zh": "chi", "xx": "", "": ""} {
		if got := dvd.Language6392(in); got != want {
			t.Errorf("Language6392(%q) = %q, want %q", in, got, want)
		}
	}
}
