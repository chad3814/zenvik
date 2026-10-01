package zenvik

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/mux"
)

func TestMapTracks(t *testing.T) {
	title := &Title{
		ID:    "00800",
		Video: []VideoTrack{{PID: 0x1011, Codec: bluray.CodingAVC}},
		Audio: []AudioTrack{
			{PID: 0x1100, Codec: bluray.CodingTrueHD, Language: "eng", Channels: 6},
			{PID: 0x1101, Codec: bluray.CodingAC3, Language: "fra", Channels: 3},
		},
		Subtitles: []SubtitleTrack{
			{PID: 0x1200, Codec: bluray.CodingPG, Language: "eng"},
			{PID: 0x1201, Codec: bluray.CodingPG, Language: "deu"}, // not found by mkvmerge
		},
	}
	id := &mux.Identification{Tracks: []mux.IdentifiedTrack{
		{ID: 0, Type: "video", PID: 0x1011},
		{ID: 1, Type: "audio", PID: 0x1100, Channels: 8},
		{ID: 2, Type: "audio", PID: 0x1101, Channels: 2},
		{ID: 3, Type: "subtitles", PID: 0x1200},
		{ID: 4, Type: "audio", PID: 0x1102, Channels: 2}, // not in the STN table
	}}
	tracks, warnings := mapTracks(title, id)
	want := []mux.Track{
		{ID: 0, Type: "video", Default: true},
		{ID: 1, Type: "audio", Language: "eng", Name: "TrueHD 7.1", Default: true},
		{ID: 2, Type: "audio", Language: "fra", Name: "AC-3 Stereo"},
		{ID: 3, Type: "subtitles", Language: "eng"},
	}
	if !reflect.DeepEqual(tracks, want) {
		t.Errorf("tracks =\n%+v\nwant\n%+v", tracks, want)
	}
	joined := strings.Join(warnings, "\n")
	if len(warnings) != 2 || !strings.Contains(joined, "0x1201") || !strings.Contains(joined, "0x1102") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestAudioName(t *testing.T) {
	tests := []struct {
		a        AudioTrack
		channels int
		want     string
	}{
		{AudioTrack{Codec: bluray.CodingDTSHDMA}, 6, "DTS-HD MA 5.1"},
		{AudioTrack{Codec: bluray.CodingAC3}, 1, "AC-3 Mono"},
		{AudioTrack{Codec: bluray.CodingLPCM}, 4, "LPCM 4 ch"},
		{AudioTrack{Codec: bluray.CodingTrueHD, Channels: 6}, 0, "TrueHD multi-channel"},
	}
	for _, tt := range tests {
		if got := audioName(tt.a, tt.channels); got != tt.want {
			t.Errorf("audioName(%v, %d) = %q, want %q", tt.a.Codec, tt.channels, got, tt.want)
		}
	}
}

func TestPhaseString(t *testing.T) {
	if PhaseMounting.String() != "mounting" || PhaseScanning.String() != "scanning" ||
		PhaseMuxing.String() != "muxing" || PhaseFinalizing.String() != "finalizing" || Phase(9).String() != "unknown" {
		t.Error("Phase.String mismatch")
	}
}
