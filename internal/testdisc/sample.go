package testdisc

import (
	"fmt"
	"time"

	"github.com/chad3814/zenvik/bluray"
)

// SampleMovie returns a typical single-movie disc:
//
//   - title 1 is HDMV movie object 0, which plays playlist 800
//   - 00800: 20 five-minute segments over clips 00001–00020 (100 min)
//   - 00801: identical to 00800 (a duplicate)
//   - 00010: one 2m30s segment on clip 00030 (a trailer)
//   - 00099: one 20 s segment on clip 00031 (a menu loop)
//
// Every referenced clip gets StubClip info and default clean M2TS data.
// ClipData is an empty map that tests may fill to override clip data.
func SampleMovie() *Disc {
	d := &Disc{
		Titles:       []bluray.IndexTitle{{Object: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0}}},
		MovieObjects: []bluray.MovieObject{{Commands: []bluray.NavCommand{CmdPlayPL(800)}}},
		Playlists:    map[string]*bluray.Playlist{},
		Clips:        map[string]*bluray.Clip{},
		ClipData:     map[string][]byte{},
		MetaTitle:    "Sample Movie",
	}
	var feature []Segment
	for i := 1; i <= 20; i++ {
		feature = append(feature, Segment{Clip: fmt.Sprintf("%05d", i), Length: 5 * time.Minute})
	}
	d.Playlists["00800"] = SimplePlaylist(feature...)
	d.Playlists["00801"] = SimplePlaylist(feature...)
	d.Playlists["00010"] = SimplePlaylist(Segment{Clip: "00030", Length: 150 * time.Second})
	d.Playlists["00099"] = SimplePlaylist(Segment{Clip: "00031", Length: 20 * time.Second})
	d.AddClipsFor()
	return d
}

// AddClipsFor registers StubClip info for every clip any playlist
// references and that has no clip info yet.
func (d *Disc) AddClipsFor() {
	if d.Clips == nil {
		d.Clips = map[string]*bluray.Clip{}
	}
	for _, p := range d.Playlists {
		for _, it := range p.Items {
			if _, ok := d.Clips[it.ClipID]; !ok {
				d.Clips[it.ClipID] = StubClip()
			}
		}
	}
}
