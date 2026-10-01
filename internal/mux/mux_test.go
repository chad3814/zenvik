package mux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want Version
	}{
		{"mkvmerge v102.0 ('Little Houses') 64-bit\n", Version{102, 0, 0}},
		{"mkvmerge v80.0 ('Roundabout') 64-bit", Version{80, 0, 0}},
		{"mkvmerge v9.8.0 ('Kuglblids') 64-bit", Version{9, 8, 0}},
	}
	for _, tt := range tests {
		got, err := ParseVersion(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	if _, err := ParseVersion("ffmpeg version 7.1"); err == nil {
		t.Error("ParseVersion accepted non-mkvmerge output")
	}
	if !(Version{79, 9, 9}).Less(MinVersion) || (Version{80, 0, 0}).Less(MinVersion) || MinVersion.String() != "80.0.0" {
		t.Error("Version comparison or String is wrong")
	}
}

func TestCheckVersion(t *testing.T) {
	ctx := context.Background()
	m := fake("version=mkvmerge v102.0 ('Little Houses') 64-bit")
	m.Version = Version{}
	if err := m.checkVersion(ctx); err != nil || m.Version != (Version{102, 0, 0}) {
		t.Errorf("checkVersion = %v, version %v", err, m.Version)
	}
	old := fake("version=mkvmerge v79.0 ('x') 64-bit")
	if err := old.checkVersion(ctx); !errors.Is(err, ErrTooOld) {
		t.Errorf("old mkvmerge: err = %v, want ErrTooOld", err)
	}
}

func TestFindNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Find(context.Background(), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty PATH: err = %v, want ErrNotFound", err)
	}
	if _, err := Find(context.Background(), filepath.Join(t.TempDir(), "mkvmerge")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file: err = %v, want ErrNotFound", err)
	}
}

const sampleIdentification = `{"chapters":[{"num_entries":3}],"container":{"properties":{"playlist":true},
"recognized":true,"supported":true,"type":"MPEG transport stream"},"errors":[],"tracks":[
{"codec":"AVC/H.264/MPEG-4p10","id":0,"properties":{"language":"und","number":4113,"stream_id":4113},"type":"video"},
{"codec":"TrueHD Atmos","id":1,"properties":{"audio_channels":8,"language":"eng","number":4352,"stream_id":4352},"type":"audio"},
{"codec":"HDMV PGS","id":2,"properties":{"language":"eng","stream_id":4608},"type":"subtitles"}],"warnings":[]}`

func TestIdentify(t *testing.T) {
	file := filepath.Join(t.TempDir(), "id.json")
	if err := os.WriteFile(file, []byte(sampleIdentification), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fake("identify="+file).Identify(context.Background(), "/disc/BDMV/PLAYLIST/00800.mpls")
	if err != nil {
		t.Fatal(err)
	}
	want := &Identification{
		Chapters: 3,
		Tracks: []IdentifiedTrack{
			{ID: 0, Type: "video", Codec: "AVC/H.264/MPEG-4p10", PID: 0x1011, Language: "und"},
			{ID: 1, Type: "audio", Codec: "TrueHD Atmos", PID: 0x1100, Language: "eng", Channels: 8},
			{ID: 2, Type: "subtitles", Codec: "HDMV PGS", PID: 0x1200, Language: "eng"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Identify =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseIdentificationErrors(t *testing.T) {
	if _, err := parseIdentification([]byte(`{"errors":["The file could not be opened"],"tracks":[]}`)); !errors.Is(err, ErrFailed) {
		t.Errorf("errors array: err = %v, want ErrFailed", err)
	}
	if _, err := parseIdentification([]byte("not json")); !errors.Is(err, ErrFailed) {
		t.Errorf("bad JSON: err = %v, want ErrFailed", err)
	}
}

func TestArgs(t *testing.T) {
	job := Job{
		Input:  "/d/BDMV/PLAYLIST/00800.mpls",
		Output: "/o/x.mkv.partial",
		Tracks: []Track{
			{ID: 0, Type: "video", Default: true},
			{ID: 1, Type: "audio", Language: "jpn", Name: "AC-3 Stereo", Default: true},
			{ID: 3, Type: "audio", Language: "eng", Name: "TrueHD 7.1"},
			{ID: 2, Type: "subtitles", Language: "eng"},
		},
	}
	want := []string{
		"-o", "/o/x.mkv.partial",
		"--default-track-flag", "0:yes",
		"--language", "1:jpn", "--track-name", "1:AC-3 Stereo", "--default-track-flag", "1:yes",
		"--language", "3:eng", "--track-name", "3:TrueHD 7.1", "--default-track-flag", "3:no",
		"--language", "2:eng", "--default-track-flag", "2:no",
		"--video-tracks", "0", "--audio-tracks", "1,3", "--subtitle-tracks", "2",
		"--track-order", "0:0,0:1,0:3,0:2",
		"/d/BDMV/PLAYLIST/00800.mpls",
	}
	if got := Args(job); !reflect.DeepEqual(got, want) {
		t.Errorf("Args =\n%q\nwant\n%q", got, want)
	}
	noSubs := Args(Job{Input: "in.mpls", Output: "out.mkv", Tracks: []Track{{ID: 0, Type: "video", Default: true}}})
	wantNoSubs := []string{"-o", "out.mkv", "--default-track-flag", "0:yes", "--video-tracks", "0", "--no-audio", "--no-subtitles", "--track-order", "0:0", "in.mpls"}
	if !reflect.DeepEqual(noSubs, wantNoSubs) {
		t.Errorf("Args (video only) = %q", noSubs)
	}
}
