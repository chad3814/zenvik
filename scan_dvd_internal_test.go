package zenvik

import (
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
)

func TestWholeFiles(t *testing.T) {
	vobs := []vobFile{{name: "1", size: 10 * 2048, first: 0, last: 9}, {name: "2", size: 10 * 2048, first: 10, last: 19}}
	c := func(first, last uint32) dvd.Cell { return dvd.Cell{FirstSector: first, LastSector: last} }
	tests := []struct {
		name   string
		cells  []dvd.Cell
		files  int
		reason string
	}{
		{"one whole file", []dvd.Cell{c(0, 4), c(5, 9)}, 1, ""},
		{"both files", []dvd.Cell{c(0, 12), c(13, 19)}, 2, ""},
		{"second file", []dvd.Cell{c(10, 19)}, 1, ""},
		{"gap", []dvd.Cell{c(0, 4), c(6, 9)}, 0, reasonNotContiguous},
		{"backwards", []dvd.Cell{c(10, 19), c(0, 9)}, 0, reasonNotContiguous},
		{"ends mid-file", []dvd.Cell{c(0, 14)}, 0, reasonMidFile},
		{"starts mid-file", []dvd.Cell{c(5, 19)}, 0, reasonMidFile},
		{"past the end", []dvd.Cell{c(10, 25)}, 0, reasonMidFile},
	}
	for _, tt := range tests {
		got, reason := wholeFiles(tt.cells, vobs)
		if len(got) != tt.files || reason != tt.reason {
			t.Errorf("%s: %d files, reason %q; want %d, %q", tt.name, len(got), reason, tt.files, tt.reason)
		}
	}
}

func TestCellStartsAndAngleOne(t *testing.T) {
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
	if got := angleOne(cells); len(got) != 3 || got[1].BlockMode != dvd.FirstInBlock {
		t.Errorf("angleOne = %+v", got)
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
