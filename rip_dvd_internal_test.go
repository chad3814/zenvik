package zenvik

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/mux"
)

func TestMapDVDTracks(t *testing.T) {
	ti := &Title{
		ID:    "01",
		Video: []VideoTrack{{PID: 0x00E0}},
		Audio: []AudioTrack{
			{PID: 0xBD80, Codec: bluray.CodingAC3, Language: "eng"},
			{PID: 0xBD81, Codec: bluray.CodingAC3, Language: "fre", Description: "Director's Commentary"},
		},
		Subtitles: []SubtitleTrack{{PID: 0xBD20, Codec: CodingVobSub, Language: "eng", Description: "Forced"}},
	}
	// mkvmerge reports no subtitle tracks for VOBs; Task 10 adds them from a VobSub file.
	id := &mux.Identification{Tracks: []mux.IdentifiedTrack{
		{ID: 0, Type: "video", StreamID: 0xE0},
		{ID: 1, Type: "audio", StreamID: 0xBD, SubStreamID: 0x80, Channels: 6},
		{ID: 2, Type: "audio", StreamID: 0xBD, SubStreamID: 0x81, Channels: 2},
		{ID: 3, Type: "audio", StreamID: 0xBD, SubStreamID: 0x82, Channels: 2},
	}}
	tracks, warnings := mapDVDTracks(ti, id)
	want := []mux.Track{
		{ID: 0, Type: "video", Default: true},
		{ID: 1, Type: "audio", Language: "eng", Name: "AC-3 5.1", Default: true},
		{ID: 2, Type: "audio", Language: "fre", Name: "AC-3 Stereo (Director's Commentary)"},
		{ID: 3, Type: "audio"},
	}
	if !reflect.DeepEqual(tracks, want) {
		t.Errorf("tracks =\n%+v\nwant\n%+v", tracks, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "not described by the IFO") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestWriteChapterFile(t *testing.T) {
	p, err := writeChapterFile([]Chapter{{Number: 1, Start: 0}, {Number: 2, Start: 25*time.Minute + 1500*time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "CHAPTER01=00:00:00.000\nCHAPTER01NAME=Chapter 01\nCHAPTER02=00:25:01.500\nCHAPTER02NAME=Chapter 02\n"
	if string(b) != want {
		t.Errorf("chapter file =\n%s\nwant\n%s", b, want)
	}
}
