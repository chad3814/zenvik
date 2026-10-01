package testdisc

import (
	"bytes"
	"io/fs"
	"reflect"
	"testing"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func TestScrambledM2TS(t *testing.T) {
	b := ScrambledM2TS(2)
	if len(b) != 2*6144 {
		t.Fatalf("len = %d", len(b))
	}
	for u := 0; u < 2; u++ {
		base := u * 6144
		if !bytes.Equal(b[base:base+16], CleanM2TS(1)[:16]) {
			t.Errorf("unit %d: first 16 bytes are not clear", u)
		}
		for p := 1; p < 32; p++ {
			if b[base+p*192+4] == 0x47 {
				t.Errorf("unit %d packet %d still has a sync byte", u, p)
			}
		}
	}
	if !bytes.Equal(ScrambledM2TS(1), ScrambledM2TS(1)) {
		t.Error("ScrambledM2TS is not deterministic")
	}
}

func TestSimplePlaylist(t *testing.T) {
	p := SimplePlaylist(Segment{"00001", 5 * time.Minute}, Segment{"00002", 90 * time.Second})
	got, err := bluray.ParsePlaylist(Playlist(p))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip =\n%+v\nwant\n%+v", got, p)
	}
	if got.Duration() != 6*time.Minute+30*time.Second {
		t.Errorf("Duration = %v", got.Duration())
	}
	if ch := got.Chapters(); !reflect.DeepEqual(ch, []time.Duration{0, 5 * time.Minute}) {
		t.Errorf("Chapters = %v", ch)
	}
	if !reflect.DeepEqual(got.Items[0].STN, StandardSTN()) {
		t.Errorf("STN = %+v", got.Items[0].STN)
	}
}

func TestSampleMovie(t *testing.T) {
	files := SampleMovie().Files()
	var mpls, m2ts int
	for name := range files {
		switch {
		case len(name) > 14 && name[:14] == "BDMV/PLAYLIST/":
			mpls++
		case len(name) > 12 && name[:12] == "BDMV/STREAM/":
			m2ts++
		}
	}
	if mpls != 4 || m2ts != 22 {
		t.Errorf("playlists = %d, streams = %d; want 4 and 22", mpls, m2ts)
	}
	idx, err := bluray.ParseIndex(files["BDMV/index.bdmv"])
	if err != nil || len(idx.Titles) != 1 {
		t.Fatalf("index = %+v, %v", idx, err)
	}
	mo, err := bluray.ParseMovieObjects(files["BDMV/MovieObject.bdmv"])
	if err != nil {
		t.Fatal(err)
	}
	if got := mo.Playlists(0); !reflect.DeepEqual(got, []int{800}) {
		t.Errorf("title 1 plays %v", got)
	}
	p, err := bluray.ParsePlaylist(files["BDMV/PLAYLIST/00800.mpls"])
	if err != nil || p.Duration() != 100*time.Minute || len(p.Items) != 20 {
		t.Errorf("00800 = %v items, %v, %v", len(p.Items), p.Duration(), err)
	}
}

func TestDiscISO(t *testing.T) {
	d := SampleMovie()
	img, err := d.ISO(udfimage.Options{Revision: 0x0250, Label: "SAMPLE_MOVIE"})
	if err != nil {
		t.Fatal(err)
	}
	fsys, err := udf.Open(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatal(err)
	}
	if fsys.Label() != "SAMPLE_MOVIE" {
		t.Errorf("Label = %q", fsys.Label())
	}
	for name, want := range d.Files() {
		got, err := fs.ReadFile(fsys, name)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: %d bytes, %v", name, len(got), err)
		}
	}
}
