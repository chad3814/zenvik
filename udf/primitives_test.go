package udf

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

// makeTag fills in the 16-byte descriptor tag at the start of d.
func makeTag(id uint16, loc uint32, d []byte) []byte {
	binary.LittleEndian.PutUint16(d[0:], id)
	binary.LittleEndian.PutUint16(d[2:], 3)
	binary.LittleEndian.PutUint16(d[8:], crc16(d[16:]))
	binary.LittleEndian.PutUint16(d[10:], uint16(len(d)-16))
	binary.LittleEndian.PutUint32(d[12:], loc)
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	d[4] = sum
	return d
}

func TestCRC16(t *testing.T) {
	if got := crc16([]byte("123456789")); got != 0x31C3 {
		t.Errorf("crc16 = %#x, want 0x31C3", got)
	}
}

func TestParseTag(t *testing.T) {
	d := make([]byte, 64)
	copy(d[16:], "payload")
	makeTag(tagFSD, 7, d)
	if id, err := parseTag(d, 7); err != nil || id != tagFSD {
		t.Fatalf("parseTag = %d, %v", id, err)
	}
	if _, err := parseTag(d, anyLocation); err != nil {
		t.Errorf("anyLocation: %v", err)
	}
	if _, err := parseTag(d, 8); !errors.Is(err, ErrCorrupt) {
		t.Errorf("wrong location err = %v", err)
	}
	if _, err := expectTag(d, 7, tagFE, tagEFE); !errors.Is(err, ErrCorrupt) {
		t.Errorf("unexpected id err = %v", err)
	}

	bad := append([]byte(nil), d...)
	bad[20] ^= 0xFF // payload byte: CRC mismatch
	if _, err := parseTag(bad, 7); !errors.Is(err, ErrCorrupt) {
		t.Errorf("CRC err = %v", err)
	}
	bad = append([]byte(nil), d...)
	bad[4] ^= 0xFF // checksum byte
	if _, err := parseTag(bad, 7); !errors.Is(err, ErrCorrupt) {
		t.Errorf("checksum err = %v", err)
	}
	if _, err := parseTag(d[:40], 7); !errors.Is(err, ErrCorrupt) {
		t.Errorf("CRC length past end err = %v", err)
	}
	if _, err := parseTag(d[:10], 7); !errors.Is(err, ErrCorrupt) {
		t.Errorf("short err = %v", err)
	}
}

func TestDecodeDchars(t *testing.T) {
	tests := []struct {
		in   []byte
		want string
	}{
		{nil, ""},
		{[]byte{8, 'B', 'D', 'M', 'V'}, "BDMV"},
		{[]byte{254, 'a'}, "a"},
		{[]byte{8, 0xE9}, "é"},
		{[]byte{16, 0x30, 0xBF, 0x30, 0xA4}, "タイ"},
		{[]byte{255, 0x00, 'x'}, "x"},
	}
	for _, tt := range tests {
		got, err := decodeDchars(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("decodeDchars(%v) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range [][]byte{{7, 'a'}, {16, 0x30}} {
		if _, err := decodeDchars(bad); !errors.Is(err, ErrCorrupt) {
			t.Errorf("decodeDchars(%v) err = %v, want ErrCorrupt", bad, err)
		}
	}
}

func TestDecodeDstring(t *testing.T) {
	field := make([]byte, 32)
	copy(field, []byte{8, 'M', 'O', 'V', 'I', 'E'})
	field[31] = 6
	if got, err := decodeDstring(field); err != nil || got != "MOVIE" {
		t.Errorf("decodeDstring = %q, %v", got, err)
	}
	if got, err := decodeDstring(make([]byte, 32)); err != nil || got != "" {
		t.Errorf("empty dstring = %q, %v", got, err)
	}
	field[31] = 40
	if _, err := decodeDstring(field); !errors.Is(err, ErrCorrupt) {
		t.Errorf("overlong dstring err = %v", err)
	}
}

func TestDecodeTimestamp(t *testing.T) {
	b := make([]byte, 12)
	off := -300 // minutes: UTC-5
	binary.LittleEndian.PutUint16(b, 1<<12|uint16(off)&0x0FFF)
	binary.LittleEndian.PutUint16(b[2:], 2026)
	copy(b[4:], []byte{10, 1, 12, 30, 45, 50, 3, 7}) // 50 cs, 3 hundred-µs, 7 µs
	got := decodeTimestamp(b)
	want := time.Date(2026, 10, 1, 12, 30, 45, 500_307_000, time.FixedZone("", -5*3600))
	if !got.Equal(want) {
		t.Errorf("decodeTimestamp = %v, want %v", got, want)
	}
	if !decodeTimestamp(make([]byte, 12)).IsZero() {
		t.Error("all-zero timestamp should decode to the zero time")
	}
}
