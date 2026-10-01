package bluray

import (
	"reflect"
	"testing"
)

func TestParseAttributes(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		clip bool
		want Stream
	}{
		{"avc", []byte{2, 0x1B, 0x61}, false,
			Stream{Coding: CodingAVC, VideoFormat: 6, FrameRate: 1}},
		{"hevc-mpls", []byte{3, 0x24, 0x81, 0x10}, false,
			Stream{Coding: CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 1}},
		{"hevc-clpi", []byte{4, 0x24, 0x81, 0x30, 0x20}, true,
			Stream{Coding: CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 2}},
		{"hevc-short", []byte{2, 0x24, 0x81}, false,
			Stream{Coding: CodingHEVC, VideoFormat: 8, FrameRate: 1}},
		{"truehd", []byte{5, 0x83, 0x61, 'e', 'n', 'g'}, false,
			Stream{Coding: CodingTrueHD, AudioFormat: 6, SampleRate: 1, Language: "eng"}},
		{"pgs", []byte{4, 0x90, 'f', 'r', 'a'}, false,
			Stream{Coding: CodingPG, Language: "fra"}},
		{"text", []byte{5, 0x92, 0x01, 'j', 'p', 'n'}, false,
			Stream{Coding: CodingTextST, Language: "jpn"}},
		{"unknown", []byte{3, 0x77, 0xFF, 0xFF}, false,
			Stream{Coding: 0x77}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newReader(append(tt.in, 0xEE))
			var s Stream
			parseAttributes(r, &s, tt.clip)
			if r.err() != nil {
				t.Fatal(r.err())
			}
			if !reflect.DeepEqual(s, tt.want) {
				t.Errorf("got %+v, want %+v", s, tt.want)
			}
			if got := r.u8(); got != 0xEE {
				t.Errorf("reader not positioned after block: next byte %#x", got)
			}
		})
	}
}

func TestParseAttributesSkipsPadding(t *testing.T) {
	r := newReader([]byte{8, 0x81, 0x31, 'e', 'n', 'g', 0, 0, 0, 0xEE})
	var s Stream
	parseAttributes(r, &s, false)
	if s.Coding != CodingAC3 || s.Language != "eng" || r.u8() != 0xEE {
		t.Errorf("padding not skipped: %+v", s)
	}
}

func TestStreamNames(t *testing.T) {
	tests := []struct{ got, want string }{
		{CodingTrueHD.String(), "TrueHD"},
		{CodingHEVC.String(), "HEVC"},
		{CodingType(0x77).String(), "0x77"},
		{VideoFormat(8).String(), "2160p"},
		{VideoFormat(15).String(), "unknown(15)"},
		{FrameRate(1).String(), "23.976"},
		{DynamicRange(1).String(), "HDR10"},
		{AudioFormat(6).String(), "multi-channel"},
		{SampleRate(5).String(), "192 kHz"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

func TestCodingKind(t *testing.T) {
	tests := map[CodingType]StreamKind{
		CodingAVC: KindVideo, CodingVC1: KindVideo, CodingHEVC: KindVideo,
		CodingAC3: KindAudio, CodingDTSHDMA: KindAudio, CodingLPCM: KindAudio,
		CodingPG: KindPG, CodingIG: KindIG, CodingTextST: KindText, 0x77: KindUnknown,
	}
	for c, want := range tests {
		if got := c.Kind(); got != want {
			t.Errorf("%v.Kind() = %d, want %d", c, got, want)
		}
	}
}
