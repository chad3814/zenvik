package udfimage

import (
	"encoding/binary"
	"strings"
	"testing"
)

func sectorAt(img []byte, n int) []byte { return img[n*sector : (n+1)*sector] }

func checkTag(t *testing.T, d []byte, wantID uint16, wantLoc uint32) {
	t.Helper()
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	if sum != d[4] {
		t.Errorf("tag checksum %#x, want %#x", d[4], sum)
	}
	if id := binary.LittleEndian.Uint16(d); id != wantID {
		t.Errorf("tag id %d, want %d", id, wantID)
	}
	if loc := binary.LittleEndian.Uint32(d[12:]); loc != wantLoc {
		t.Errorf("tag location %d, want %d", loc, wantLoc)
	}
	n := int(binary.LittleEndian.Uint16(d[10:]))
	if got, want := crc16(d[16:16+n]), binary.LittleEndian.Uint16(d[8:]); got != want {
		t.Errorf("tag CRC %#x, want %#x", got, want)
	}
}

func TestCRC16KnownVector(t *testing.T) {
	// CRC-16/XMODEM (poly 0x1021, init 0) of "123456789".
	if got := crc16([]byte("123456789")); got != 0x31C3 {
		t.Errorf("crc16 = %#x, want 0x31C3", got)
	}
}

func TestBuildStructure(t *testing.T) {
	files := map[string]File{"BDMV/index.bdmv": {Data: []byte("INDX0200")}}
	for _, tc := range []struct {
		rev uint16
		nsr string
	}{{0x0102, "NSR02"}, {0x0250, "NSR03"}} {
		img, err := Build(files, Options{Revision: tc.rev, Label: "TEST"})
		if err != nil {
			t.Fatal(err)
		}
		if len(img)%sector != 0 {
			t.Fatalf("image length %d not a multiple of %d", len(img), sector)
		}
		for i, id := range []string{"BEA01", tc.nsr, "TEA01"} {
			if got := string(sectorAt(img, 16+i)[1:6]); got != id {
				t.Errorf("rev %#x: VRS sector %d = %q, want %q", tc.rev, 16+i, got, id)
			}
		}
		last := len(img)/sector - 1
		checkTag(t, sectorAt(img, 256), 2, 256)
		checkTag(t, sectorAt(img, last), 2, uint32(last))
		checkTag(t, sectorAt(img, 32), 1, 32) // PVD
		checkTag(t, sectorAt(img, 35), 6, 35) // LVD
		checkTag(t, sectorAt(img, 37), 8, 37) // TD
		checkTag(t, sectorAt(img, 64), 9, 64) // LVID
		if got := string(sectorAt(img, 32)[25:29]); got != "TEST" {
			t.Errorf("PVD volume identifier = %q", got)
		}
	}
}

func TestBuildRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]File
		opt   Options
		want  string
	}{
		{"revision", map[string]File{"a": {}}, Options{Revision: 0x0201}, "unsupported revision"},
		{"conflict", map[string]File{"a": {}, "a/b": {}}, Options{Revision: 0x0102}, "path conflict"},
		{"invalid path", map[string]File{"/abs": {}}, Options{Revision: 0x0102}, "invalid path"},
		{"inline ADs", map[string]File{"a": {}}, Options{Revision: 0x0102, MaxInlineADs: 1}, "MaxInlineADs"},
	}
	for _, tt := range tests {
		_, err := Build(tt.files, tt.opt)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want containing %q", tt.name, err, tt.want)
		}
	}
}
