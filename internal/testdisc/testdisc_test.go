package testdisc

import (
	"io/fs"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/chad3814/zenvik/bluray"
)

func samplePlaylist() *bluray.Playlist {
	stn := bluray.STN{
		Video: []bluray.Stream{{PID: 0x1011, Coding: bluray.CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 1}},
		Audio: []bluray.Stream{
			{PID: 0x1100, Coding: bluray.CodingTrueHD, AudioFormat: 6, SampleRate: 1, Language: "eng"},
			{PID: 0x1101, Coding: bluray.CodingAC3, AudioFormat: 3, SampleRate: 1, Language: "deu"},
		},
		PG: []bluray.Stream{{PID: 0x1200, Coding: bluray.CodingPG, Language: "eng"}},
	}
	return &bluray.Playlist{
		Version: "0300",
		Items: []bluray.PlayItem{
			{ClipID: "00001", CodecID: "M2TS", ConnectionCondition: 1, In: 0, Out: 45000 * 600, STN: stn},
			{ClipID: "00002", CodecID: "M2TS", ConnectionCondition: 5, In: 0, Out: 45000 * 300,
				Angles: []string{"00003", "00004"}, STN: stn},
		},
		Marks: []bluray.Mark{
			{Type: bluray.MarkEntry, PlayItem: 0, Time: 0, PID: 0xFFFF},
			{Type: bluray.MarkEntry, PlayItem: 1, Time: 45000 * 60, PID: 0xFFFF},
		},
	}
}

func sampleClip() *bluray.Clip {
	return &bluray.Clip{
		Version: "0300", StreamType: 1, ApplicationType: 1, TSRecordingRate: 48_000_000, SourcePackets: 32,
		Programs: []bluray.Program{{PMTPID: 0x100, Streams: []bluray.Stream{
			{PID: 0x1011, Coding: bluray.CodingAVC, VideoFormat: 6, FrameRate: 1},
			{PID: 0x1012, Coding: bluray.CodingHEVC, VideoFormat: 8, FrameRate: 1, DynamicRange: 2},
			{PID: 0x1100, Coding: bluray.CodingDTSHDMA, AudioFormat: 6, SampleRate: 1, Language: "eng"},
			{PID: 0x1200, Coding: bluray.CodingPG, Language: "eng"},
			{PID: 0x1800, Coding: bluray.CodingTextST, Language: "fra"},
		}}},
	}
}

func TestIndexRoundTrip(t *testing.T) {
	in := &bluray.Index{
		Version:       "0300",
		FirstPlayback: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0},
		TopMenu:       bluray.Object{Type: bluray.ObjectBDJ, PlaybackType: 1, BDJOName: "00000"},
		Titles: []bluray.IndexTitle{
			{Object: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 1}},
			{Object: bluray.Object{Type: bluray.ObjectBDJ, BDJOName: "00001"}, AccessType: 2},
		},
	}
	got, err := bluray.ParseIndex(Index(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, in)
	}
}

func TestMovieObjectsRoundTrip(t *testing.T) {
	in := &bluray.MovieObjects{Version: "0200", Objects: []bluray.MovieObject{
		{ResumeIntention: true, Commands: []bluray.NavCommand{CmdMove(2, 800), CmdPlayPLReg(2), CmdJumpObject(1)}},
		{TitleSearchMask: true, Commands: []bluray.NavCommand{CmdPlayPL(801), CmdJumpTitle(2)}},
	}}
	got, err := bluray.ParseMovieObjects(MovieObjects(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, in)
	}
	if pls := got.Playlists(0); !reflect.DeepEqual(pls, []int{800, 801}) {
		t.Errorf("Playlists(0) = %v", pls)
	}
}

func TestPlaylistRoundTrip(t *testing.T) {
	in := samplePlaylist()
	got, err := bluray.ParsePlaylist(Playlist(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, in)
	}
}

func TestClipRoundTrip(t *testing.T) {
	in := sampleClip()
	got, err := bluray.ParseClip(Clip(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip =\n%+v\nwant\n%+v", got, in)
	}
}

func TestMetaRoundTrip(t *testing.T) {
	got, err := bluray.ParseMeta(Meta("Tom & Jerry <Deluxe>", "eng"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Tom & Jerry <Deluxe>" || got.Language != "eng" {
		t.Errorf("ParseMeta = %+v", got)
	}
}

func TestCleanM2TS(t *testing.T) {
	b := CleanM2TS(2)
	if len(b) != 2*6144 {
		t.Fatalf("len = %d", len(b))
	}
	for p := 0; p < 64; p++ {
		if b[p*192+4] != 0x47 {
			t.Fatalf("packet %d has no sync byte", p)
		}
	}
}

func sampleDisc() *Disc {
	return &Disc{
		FirstPlayback: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0},
		Titles:        []bluray.IndexTitle{{Object: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0}}},
		MovieObjects:  []bluray.MovieObject{{Commands: []bluray.NavCommand{CmdPlayPL(800)}}},
		Playlists:     map[string]*bluray.Playlist{"00800": samplePlaylist()},
		Clips:         map[string]*bluray.Clip{"00001": sampleClip(), "00002": sampleClip()},
		ClipData:      map[string][]byte{"00002": []byte("custom")},
		MetaTitle:     "Sample Disc",
	}
}

func TestDiscFiles(t *testing.T) {
	files := sampleDisc().Files()
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	want := []string{
		"BDMV/CLIPINF/00001.clpi", "BDMV/CLIPINF/00002.clpi",
		"BDMV/META/DL/bdmt_eng.xml", "BDMV/MovieObject.bdmv",
		"BDMV/PLAYLIST/00800.mpls",
		"BDMV/STREAM/00001.m2ts", "BDMV/STREAM/00002.m2ts",
		"BDMV/index.bdmv",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("files = %v\nwant %v", names, want)
	}
	if len(files["BDMV/STREAM/00001.m2ts"]) != 6144 {
		t.Errorf("default clip data length = %d", len(files["BDMV/STREAM/00001.m2ts"]))
	}
	if string(files["BDMV/STREAM/00002.m2ts"]) != "custom" {
		t.Errorf("custom clip data not used")
	}
	idx, err := bluray.ParseIndex(files["BDMV/index.bdmv"])
	if err != nil || len(idx.Titles) != 1 {
		t.Errorf("index = %+v, %v", idx, err)
	}
}

func TestDiscWriteDirMatchesMapFS(t *testing.T) {
	d := sampleDisc()
	dir := t.TempDir()
	if err := d.WriteDir(dir); err != nil {
		t.Fatal(err)
	}
	mfs := d.MapFS()
	err := fs.WalkDir(mfs, ".", func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return err
		}
		want, _ := fs.ReadFile(mfs, p)
		got, err := fs.ReadFile(os.DirFS(dir), p)
		if err != nil {
			return err
		}
		if string(got) != string(want) {
			t.Errorf("%s differs", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
