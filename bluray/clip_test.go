package bluray

import (
	"errors"
	"reflect"
	"testing"
)

func clipBytes() []byte {
	clipInfo := cat(zeros(2), []byte{1, 1}, zeros(4), be32(48_000_000), be32(1000), zeros(128), be16(0))
	program := cat(
		zeros(1), []byte{1}, // reserved, number of programs
		be32(0), be16(0x0100), []byte{3, 0}, // SPN start, PMT PID, streams, groups
		be16(0x1011), []byte{4, 0x24, 0x81, 0x30, 0x10}, // HEVC 2160p 23.976, HDR10
		be16(0x1100), []byte{5, 0x86, 0x61, 'e', 'n', 'g'}, // DTS-HD MA multi-channel 48 kHz
		be16(0x1200), []byte{4, 0x90, 'e', 'n', 'g'}, // PGS
	)
	seq := cat(be32(2), zeros(2))
	seqStart := 40 + 4 + len(clipInfo)
	progStart := seqStart + len(seq)
	head := cat([]byte("HDMV0300"), be32(uint32(seqStart)), be32(uint32(progStart)), zeros(12), zeros(12))
	return cat(head, be32(uint32(len(clipInfo))), clipInfo, seq, be32(uint32(len(program))), program)
}

func TestParseClip(t *testing.T) {
	c, err := ParseClip(clipBytes())
	if err != nil {
		t.Fatal(err)
	}
	want := &Clip{
		Version: "0300", StreamType: 1, ApplicationType: 1,
		TSRecordingRate: 48_000_000, SourcePackets: 1000,
		Programs: []Program{{
			PMTPID: 0x0100,
			Streams: []Stream{
				{PID: 0x1011, Coding: CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 1},
				{PID: 0x1100, Coding: CodingDTSHDMA, AudioFormat: 6, SampleRate: 1, Language: "eng"},
				{PID: 0x1200, Coding: CodingPG, Language: "eng"},
			},
		}},
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("ParseClip =\n%+v\nwant\n%+v", c, want)
	}
	if c.Size() != 192000 {
		t.Errorf("Size = %d", c.Size())
	}
}

func TestParseClipErrors(t *testing.T) {
	b := clipBytes()
	for name, in := range map[string][]byte{
		"truncated": b[:len(b)-2],
		"magic":     append([]byte("MPLS"), b[4:]...),
	} {
		if _, err := ParseClip(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func FuzzParseClip(f *testing.F) {
	f.Add(clipBytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		if _, err := ParseClip(b); err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected error type: %v", err)
		}
	})
}
