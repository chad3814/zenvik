package testdisc

import (
	"time"

	"github.com/chad3814/zenvik/bluray"
)

// Segment is one play item of a synthetic playlist.
type Segment struct {
	Clip   string // clip ID, e.g. "00001"
	Length time.Duration
}

// StandardSTN returns a typical stream table: H.264 1080p video, English
// TrueHD and French AC-3 audio, and English PGS subtitles.
func StandardSTN() bluray.STN {
	return bluray.STN{
		Video: []bluray.Stream{{PID: 0x1011, Coding: bluray.CodingAVC, VideoFormat: 6, FrameRate: 1}},
		Audio: []bluray.Stream{
			{PID: 0x1100, Coding: bluray.CodingTrueHD, AudioFormat: 6, SampleRate: 1, Language: "eng"},
			{PID: 0x1101, Coding: bluray.CodingAC3, AudioFormat: 3, SampleRate: 1, Language: "fra"},
		},
		PG: []bluray.Stream{{PID: 0x1200, Coding: bluray.CodingPG, Language: "eng"}},
	}
}

// SimplePlaylist builds a playlist with one play item per segment (IN at
// 0, OUT at the segment length), StandardSTN on every item, and a chapter
// entry mark at the start of each item.
func SimplePlaylist(segs ...Segment) *bluray.Playlist {
	p := &bluray.Playlist{Version: "0200"}
	for i, s := range segs {
		p.Items = append(p.Items, bluray.PlayItem{
			ClipID:  s.Clip,
			CodecID: "M2TS",
			In:      0,
			Out:     bluray.Ticks(int64(s.Length) * bluray.TicksPerSecond / int64(time.Second)),
			STN:     StandardSTN(),
		})
		p.Marks = append(p.Marks, bluray.Mark{Type: bluray.MarkEntry, PlayItem: uint16(i), Time: 0, PID: 0xFFFF})
	}
	return p
}
