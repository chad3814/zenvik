package bluray

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// streamEntry builds an STN entry: a type-1 stream_entry (main clip PID)
// followed by its stream_attributes.
func streamEntry(pid uint16, attrs ...byte) []byte {
	return cat([]byte{9, 1}, be16(pid), zeros(6), []byte{byte(len(attrs))}, attrs)
}

func stnBytes() []byte {
	body := cat(
		zeros(2),
		[]byte{1, 2, 1, 0, 0, 0, 0}, // video, audio, PG, IG, secondary audio, secondary video, PiP PG
		zeros(5),
		streamEntry(0x1011, 0x1B, 0x61), // AVC 1080p 23.976
		streamEntry(0x1100, 0x83, 0x61, 'e', 'n', 'g'), // TrueHD multi-channel 48 kHz
		streamEntry(0x1101, 0x81, 0x31, 'f', 'r', 'a'), // AC-3 stereo 48 kHz
		streamEntry(0x1200, 0x90, 'e', 'n', 'g'),       // PGS
	)
	return cat(be16(uint16(len(body))), body)
}

func playItemBytes(clip string, in, out uint32, angles []string) []byte {
	flags := uint16(1) // connection condition 1
	var angleData []byte
	if len(angles) > 0 {
		flags |= 0x10
		angleData = []byte{byte(len(angles) + 1), 0}
		for _, a := range angles {
			angleData = cat(angleData, []byte(a), []byte("M2TS"), zeros(1))
		}
	}
	body := cat(
		[]byte(clip), []byte("M2TS"), be16(flags), []byte{0},
		be32(in), be32(out),
		zeros(8),     // UO mask
		[]byte{0, 0}, // random access flag, still mode
		be16(0),      // still time
		angleData, stnBytes(),
	)
	return cat(be16(uint16(len(body))), body)
}

func markBytes(typ byte, item uint16, ticks uint32) []byte {
	return cat([]byte{0, typ}, be16(item), be32(ticks), be16(0xFFFF), be32(0))
}

func buildPlaylist(items, marks [][]byte) []byte {
	pl := cat(zeros(2), be16(uint16(len(items))), be16(0), cat(items...))
	mk := cat(be16(uint16(len(marks))), cat(marks...))
	appInfo := cat(be32(14), zeros(14))
	plStart := 40 + len(appInfo)
	markStart := plStart + 4 + len(pl)
	head := cat([]byte("MPLS0300"), be32(uint32(plStart)), be32(uint32(markStart)), be32(0), zeros(20))
	return cat(head, appInfo, be32(uint32(len(pl))), pl, be32(uint32(len(mk))), mk)
}

const minute = 45000 * 60

func playlistBytes() []byte {
	return buildPlaylist(
		[][]byte{
			playItemBytes("00001", 900000, 900000+10*minute, []string{"00002"}),
			playItemBytes("00003", 0, 5*minute, nil),
		},
		[][]byte{
			markBytes(1, 0, 900000),
			markBytes(1, 0, 900000+minute),
			markBytes(2, 0, 900000+minute+minute/2), // link point, not a chapter
			markBytes(1, 1, 0),
		},
	)
}

func wantSTN() STN {
	return STN{
		Video: []Stream{{PID: 0x1011, Coding: CodingAVC, VideoFormat: 6, FrameRate: 1}},
		Audio: []Stream{
			{PID: 0x1100, Coding: CodingTrueHD, AudioFormat: 6, SampleRate: 1, Language: "eng"},
			{PID: 0x1101, Coding: CodingAC3, AudioFormat: 3, SampleRate: 1, Language: "fra"},
		},
		PG: []Stream{{PID: 0x1200, Coding: CodingPG, Language: "eng"}},
	}
}

func TestParsePlaylist(t *testing.T) {
	p, err := ParsePlaylist(playlistBytes())
	if err != nil {
		t.Fatal(err)
	}
	want := &Playlist{
		Version: "0300",
		Items: []PlayItem{
			{ClipID: "00001", CodecID: "M2TS", ConnectionCondition: 1, In: 900000, Out: 900000 + 10*minute,
				Angles: []string{"00002"}, STN: wantSTN()},
			{ClipID: "00003", CodecID: "M2TS", ConnectionCondition: 1, In: 0, Out: 5 * minute, STN: wantSTN()},
		},
		Marks: []Mark{
			{Type: MarkEntry, PlayItem: 0, Time: 900000, PID: 0xFFFF},
			{Type: MarkEntry, PlayItem: 0, Time: 900000 + minute, PID: 0xFFFF},
			{Type: MarkLink, PlayItem: 0, Time: 900000 + minute + minute/2, PID: 0xFFFF},
			{Type: MarkEntry, PlayItem: 1, Time: 0, PID: 0xFFFF},
		},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("ParsePlaylist =\n%+v\nwant\n%+v", p, want)
	}
	if got := p.Duration(); got != 15*time.Minute {
		t.Errorf("Duration = %v", got)
	}
	if got := p.Items[1].Duration(); got != 5*time.Minute {
		t.Errorf("item 1 Duration = %v", got)
	}
	if got, want := p.Chapters(), []time.Duration{0, time.Minute, 10 * time.Minute}; !reflect.DeepEqual(got, want) {
		t.Errorf("Chapters = %v, want %v", got, want)
	}
}

func TestParsePlaylistRejectsOutBeforeIn(t *testing.T) {
	b := buildPlaylist([][]byte{playItemBytes("00001", 100, 50, nil)}, nil)
	if _, err := ParsePlaylist(b); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestParsePlaylistErrors(t *testing.T) {
	b := playlistBytes()
	for name, in := range map[string][]byte{
		"truncated": b[:len(b)-3],
		"magic":     append([]byte("HDMV"), b[4:]...),
		"short":     b[:20],
	} {
		if _, err := ParsePlaylist(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestChaptersIgnoresBadMarks(t *testing.T) {
	p := &Playlist{
		Items: []PlayItem{
			{In: 0, Out: 100 * 45000},
			{In: 1000, Out: 1000 + 50*45000},
		},
		Marks: []Mark{
			{Type: MarkEntry, PlayItem: 0, Time: 0},
			{Type: MarkEntry, PlayItem: 0, Time: 0},               // duplicate
			{Type: MarkLink, PlayItem: 0, Time: 45000},            // not an entry mark
			{Type: MarkEntry, PlayItem: 7, Time: 0},               // no such play item
			{Type: MarkEntry, PlayItem: 1, Time: 500},             // before the item's IN time
			{Type: MarkEntry, PlayItem: 1, Time: 1000 + 51*45000}, // after the item's OUT time
			{Type: MarkEntry, PlayItem: 1, Time: 1000 + 10*45000},
		},
	}
	if got, want := p.Chapters(), []time.Duration{0, 110 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Errorf("Chapters = %v, want %v", got, want)
	}
}

func FuzzParsePlaylist(f *testing.F) {
	f.Add(playlistBytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ParsePlaylist(b)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("unexpected error type: %v", err)
			}
			return
		}
		if p.Duration() < 0 {
			t.Fatal("negative duration")
		}
		p.Chapters()
	})
}
