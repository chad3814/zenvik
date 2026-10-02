package zenvik

import (
	"slices"
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
)

// ntsc is the duration of d as an NTSC IFO time: rounded to a whole
// 1001/30000 s frame, as cell stores it.
func ntsc(d time.Duration) time.Duration { return dvd.NewTime(d, dvd.Rate30).Duration() }

func cell(first, last uint32, d time.Duration) dvd.Cell {
	return dvd.Cell{FirstSector: first, LastSector: last, Time: dvd.NewTime(d, dvd.Rate30)}
}

func TestWholeFiles(t *testing.T) {
	vobs := []vobFile{{name: "1", size: 10 * 2048, first: 0, last: 9}, {name: "2", size: 10 * 2048, first: 10, last: 19}}
	for _, tt := range []struct {
		name  string
		cells []dvd.Cell
		files int
		ok    bool
	}{
		{"one whole file", []dvd.Cell{cell(0, 4, 0), cell(5, 9, 0)}, 1, true},
		{"both files", []dvd.Cell{cell(0, 12, 0), cell(13, 19, 0)}, 2, true},
		{"gap", []dvd.Cell{cell(0, 4, 0), cell(6, 9, 0)}, 0, false},
		{"ends mid-file", []dvd.Cell{cell(0, 14, 0)}, 0, false},
		{"past the end", []dvd.Cell{cell(10, 25, 0)}, 0, false},
	} {
		got, ok := wholeFiles(tt.cells, vobs)
		if len(got) != tt.files || ok != tt.ok {
			t.Errorf("%s: %d files, ok %v; want %d, %v", tt.name, len(got), ok, tt.files, tt.ok)
		}
	}
}

func TestClassifyCells(t *testing.T) {
	vobs := []vobFile{{name: "1", first: 0, last: 49}}
	s, m := time.Second, time.Minute
	for _, tt := range []struct {
		name    string
		cells   []dvd.Cell
		method  string
		skipped []int // indexes into cells
	}{
		{"whole file", []dvd.Cell{cell(0, 1, s), cell(2, 49, m)}, "files", nil},
		{"trailing stray", []dvd.Cell{cell(2, 25, m), cell(26, 49, m), cell(0, 1, s)}, "cut", []int{2}},
		{"leading stray", []dvd.Cell{cell(48, 49, s), cell(0, 20, m), cell(21, 30, m)}, "cut", []int{0}},
		{"both ends", []dvd.Cell{cell(48, 49, s), cell(5, 20, m), cell(0, 1, s)}, "cut", []int{0, 2}},
		{"long stray kept", []dvd.Cell{cell(2, 25, m), cell(26, 49, m), cell(0, 1, 2*s)}, "copy", nil},
		{"middle stray kept", []dvd.Cell{cell(26, 49, m), cell(0, 1, s), cell(2, 25, m)}, "copy", nil},
		{"mid-file run", []dvd.Cell{cell(5, 9, m), cell(10, 20, m)}, "cut", nil},
		{"never empties", []dvd.Cell{cell(10, 11, s/2), cell(0, 1, s/2)}, "cut", []int{0}},
	} {
		method, skipped := classifyCells(tt.cells, vobs)
		if method != tt.method || !slices.Equal(skipped, tt.skipped) {
			t.Errorf("%s: method %q, skipped %v; want %q, %v", tt.name, method, skipped, tt.method, tt.skipped)
		}
	}
}

func TestRemapChapters(t *testing.T) {
	m := time.Minute
	pgc := &dvd.PGC{Cells: []dvd.Cell{cell(2, 25, 24*m), cell(26, 49, 24*m), cell(0, 1, time.Second)}, Programs: []int{1, 2, 3}}
	ptts := []dvd.PartOfTitle{{PGC: 1, Program: 1}, {PGC: 1, Program: 2}, {PGC: 1, Program: 3}}
	got := titleChapters(pgc, ptts, map[int]bool{2: true})
	want := []Chapter{{Number: 1, Start: 0}, {Number: 2, Start: ntsc(24 * m)}}
	if !slices.Equal(got, want) {
		t.Errorf("chapters = %v, want %v", got, want)
	}
	// A dropped leading cell moves its chapter to the next kept cell; duplicates collapse.
	pgc2 := &dvd.PGC{Cells: []dvd.Cell{cell(48, 49, time.Second), cell(0, 20, m), cell(21, 30, m)}, Programs: []int{1, 2, 3}}
	got = titleChapters(pgc2, ptts, map[int]bool{0: true})
	want = []Chapter{{Number: 1, Start: 0}, {Number: 2, Start: ntsc(m)}}
	if !slices.Equal(got, want) {
		t.Errorf("leading drop: chapters = %v, want %v", got, want)
	}
}

func TestCellStarts(t *testing.T) {
	m := func(d time.Duration) dvd.Time { return dvd.NewTime(d, dvd.Rate25) }
	cells := []dvd.Cell{
		{Time: m(time.Minute)},
		{BlockMode: dvd.FirstInBlock, AngleBlock: true, Time: m(2 * time.Minute)},
		{BlockMode: dvd.InBlock, AngleBlock: true, Time: m(2 * time.Minute)},
		{BlockMode: dvd.LastInBlock, AngleBlock: true, Time: m(2 * time.Minute)},
		{Time: m(time.Minute)},
	}
	if got, want := cellStarts(cells), []time.Duration{0, time.Minute, time.Minute, time.Minute, 3 * time.Minute}; !equalDurations(got, want) {
		t.Errorf("cellStarts = %v, want %v", got, want)
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPackScrambled(t *testing.T) {
	pack := func(id byte, flags byte) []byte {
		p := make([]byte, 2048)
		copy(p, []byte{0, 0, 1, 0xBA, 0x44, 0, 4, 0, 4, 1, 0x01, 0x89, 0xC3, 0xF8})
		copy(p[14:], []byte{0, 0, 1, id, 0x07, 0xEC, flags, 0x00, 0x00})
		return p
	}
	if packScrambled(pack(0xE0, 0x81)) {
		t.Error("clear video pack reported scrambled")
	}
	if !packScrambled(pack(0xBD, 0x91)) || !packScrambled(pack(0xC0, 0xB1)) {
		t.Error("scrambled private/audio pack not detected")
	}
	if packScrambled(pack(0xBF, 0xB1)) {
		t.Error("NAV (private stream 2) packs are never scrambled")
	}
	mpeg1 := pack(0xE0, 0x91)
	mpeg1[4] = 0x21 // MPEG-1 pack header: no scrambling field
	if packScrambled(mpeg1) {
		t.Error("MPEG-1 pack reported scrambled")
	}
	if packScrambled(make([]byte, 2048)) {
		t.Error("zero sector reported scrambled")
	}
}
