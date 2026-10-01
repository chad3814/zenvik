package zenvik

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/mount"
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

func TestMapTracksDefaultSkipsMissingFirstAudio(t *testing.T) {
	title := &Title{
		ID:    "00800",
		Video: []VideoTrack{{PID: 0x1011}},
		Audio: []AudioTrack{
			{PID: 0x1100, Codec: bluray.CodingAC3, Language: "eng"},
			{PID: 0x1101, Codec: bluray.CodingAC3, Language: "fra"},
		},
	}
	id := &mux.Identification{Tracks: []mux.IdentifiedTrack{
		{ID: 0, Type: "video", PID: 0x1011},
		{ID: 1, Type: "audio", PID: 0x1101, Channels: 2},
	}}
	tracks, _ := mapTracks(title, id)
	if len(tracks) != 2 || !tracks[0].Default || !tracks[1].Default || tracks[1].Language != "fra" {
		t.Errorf("tracks = %+v", tracks)
	}
}

func TestMapTracksLanguageNormalization(t *testing.T) {
	tests := []struct {
		lang     string
		want     string
		wantWarn bool
	}{
		{"ENG", "eng", false},
		{"  ", "", true},
		{"", "", true},
		{"\x00\x00\x00", "", true},
		{"eng\x00", "eng", false},
		{"x1z", "", true},
		{"und", "", true},
	}
	for _, tt := range tests {
		title := &Title{ID: "00800", Audio: []AudioTrack{{PID: 0x1100, Codec: bluray.CodingAC3, Language: tt.lang}}}
		id := &mux.Identification{Tracks: []mux.IdentifiedTrack{{ID: 1, Type: "audio", PID: 0x1100, Channels: 2}}}
		tracks, warnings := mapTracks(title, id)
		if len(tracks) != 1 || tracks[0].Language != tt.want {
			t.Errorf("lang %q: tracks = %+v, want language %q", tt.lang, tracks, tt.want)
		}
		if gotWarn := len(warnings) > 0; gotWarn != tt.wantWarn {
			t.Errorf("lang %q: warnings = %q, want warning %v", tt.lang, warnings, tt.wantWarn)
		}
		if tt.wantWarn && (len(warnings) != 1 || !strings.Contains(warnings[0], "0x1100") || !strings.Contains(warnings[0], "invalid language code "+strconv.Quote(tt.lang))) {
			t.Errorf("lang %q: warning text = %q", tt.lang, warnings)
		}
	}
}

func TestMapTracksDuplicatePID(t *testing.T) {
	title := &Title{
		ID:    "00800",
		Video: []VideoTrack{{PID: 0x1011}},
		Audio: []AudioTrack{
			{PID: 0x1100, Codec: bluray.CodingAC3, Language: "eng"},
			{PID: 0x1100, Codec: bluray.CodingAC3, Language: "eng"},
		},
	}
	id := &mux.Identification{Tracks: []mux.IdentifiedTrack{
		{ID: 0, Type: "video", PID: 0x1011},
		{ID: 1, Type: "audio", PID: 0x1100, Channels: 2},
	}}
	tracks, warnings := mapTracks(title, id)
	if len(tracks) != 2 {
		t.Errorf("tracks = %+v, want video and one audio", tracks)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "0x1100") || !strings.Contains(warnings[0], "more than once") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestMountHint(t *testing.T) {
	unavailable := fmt.Errorf("%w: no hdiutil", mount.ErrUnavailable)
	if err := mountHint(context.Background(), unavailable); !errors.Is(err, mount.ErrUnavailable) || !strings.Contains(err.Error(), "mount the image yourself") {
		t.Errorf("unavailable: err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := mountHint(ctx, unavailable)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "mount the image yourself") {
		t.Errorf("canceled: err = %v", err)
	}
	other := errors.New("boom")
	if err := mountHint(context.Background(), other); !errors.Is(err, other) || strings.Contains(err.Error(), "mount the image") {
		t.Errorf("other: err = %v", err)
	}
}
