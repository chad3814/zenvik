package bluray

import (
	"errors"
	"reflect"
	"testing"
)

// indexBytes hand-assembles an index.bdmv following the layout in
// libbluray's index_parse.c: 40-byte header, AppInfoBDMV, then Indexes.
func indexBytes() []byte {
	appInfo := cat(be32(34), zeros(34))
	objects := cat(
		[]byte{0x40, 0, 0, 0}, be16(0x0000), be16(0), zeros(4), // first playback: HDMV, movie object 0
		[]byte{0x80, 0, 0, 0}, be16(0x4000), []byte("00000"), zeros(1), // top menu: BD-J, playback type 1
		be16(2),                                           // number of titles
		[]byte{0x40, 0, 0, 0}, be16(0), be16(1), zeros(4), // title 1: HDMV, movie object 1
		[]byte{0x90, 0, 0, 0}, be16(0xC000), []byte("00001"), zeros(1), // title 2: BD-J, access type 1, playback type 3
	)
	head := cat([]byte("INDX0200"), be32(uint32(40+len(appInfo))), be32(0), zeros(24))
	return cat(head, appInfo, be32(uint32(len(objects))), objects)
}

func TestParseIndex(t *testing.T) {
	got, err := ParseIndex(indexBytes())
	if err != nil {
		t.Fatal(err)
	}
	want := &Index{
		Version:       "0200",
		FirstPlayback: Object{Type: ObjectHDMV, MovieObjectID: 0},
		TopMenu:       Object{Type: ObjectBDJ, PlaybackType: 1, BDJOName: "00000"},
		Titles: []IndexTitle{
			{Object: Object{Type: ObjectHDMV, MovieObjectID: 1}},
			{Object: Object{Type: ObjectBDJ, PlaybackType: 3, BDJOName: "00001"}, AccessType: 1},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseIndex =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseIndexErrors(t *testing.T) {
	b := indexBytes()
	bad := append([]byte("XXXX"), b[4:]...)
	for name, in := range map[string][]byte{
		"truncated": b[:len(b)-5],
		"magic":     bad,
		"empty":     nil,
	} {
		if _, err := ParseIndex(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
}

func FuzzParseIndex(f *testing.F) {
	f.Add(indexBytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		idx, err := ParseIndex(b)
		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected error type: %v", err)
		}
		if err == nil && idx == nil {
			t.Fatal("nil index without error")
		}
	})
}
