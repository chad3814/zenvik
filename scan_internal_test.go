package zenvik

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/rank"
	"github.com/chad3814/zenvik/internal/testdisc"
)

type countingFS struct {
	fs.FS
	mu    sync.Mutex
	opens map[string]int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.mu.Lock()
	c.opens[name]++
	c.mu.Unlock()
	return c.FS.Open(name)
}

func TestScanProbesEachClipOnce(t *testing.T) {
	var segs []testdisc.Segment
	for i := 1; i <= 20; i++ {
		segs = append(segs, testdisc.Segment{Clip: fmt.Sprintf("%05d", i), Length: 5 * time.Minute})
	}
	d := &testdisc.Disc{Playlists: map[string]*bluray.Playlist{}, ClipData: map[string][]byte{}}
	for n := 1; n <= 300; n++ {
		k := n % 20
		rotated := append(append([]testdisc.Segment{}, segs[k:]...), segs[:k]...)
		d.Playlists[fmt.Sprintf("%05d", n)] = testdisc.SimplePlaylist(rotated...)
	}
	d.AddClipsFor()
	cfs := &countingFS{FS: d.MapFS(), opens: map[string]int{}}
	titles, _, err := scanTitles(context.Background(), cfs, defaultMinDuration, rank.DefaultWeights)
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 300 {
		t.Fatalf("titles = %d", len(titles))
	}
	for _, s := range segs {
		if n := cfs.opens["BDMV/STREAM/"+s.Clip+".m2ts"]; n != 1 {
			t.Errorf("%s opened %d times, want 1", s.Clip, n)
		}
	}
}

func TestPlaylistFilesIgnoresStrayNames(t *testing.T) {
	d := testdisc.SampleMovie()
	fsys := d.MapFS()
	good := fsys["BDMV/PLAYLIST/00800.mpls"].Data
	fsys["BDMV/PLAYLIST/._00800.mpls"] = &fstest.MapFile{Data: []byte("\x00\x05AppleDouble garbage")}
	fsys["BDMV/PLAYLIST/README.mpls"] = &fstest.MapFile{Data: []byte("not a playlist")}
	fsys["BDMV/PLAYLIST/00800.MPLS"] = &fstest.MapFile{Data: good}
	fsys["BDMV/PLAYLIST/1234.mpls"] = &fstest.MapFile{Data: good}
	titles, _, err := scanTitles(context.Background(), fsys, defaultMinDuration, rank.DefaultWeights)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ti := range titles {
		got = append(got, ti.ID)
	}
	sort.Strings(got)
	if want := []string{"00010", "00099", "00800", "00801"}; !slices.Equal(got, want) {
		t.Errorf("titles = %v, want %v", got, want)
	}
}
