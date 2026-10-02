package zenvik_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/testdisc"
)

// TestOpenIgnoresDecoyPlaylists builds the sample movie plus two decoys of
// the kind some UHD discs carry: a 15-hour playlist looping one tiny clip
// with no audio, and an 11-hour looping playlist with audio whose data rate
// is a sliver of the feature's. Neither may become the main title.
func TestOpenIgnoresDecoyPlaylists(t *testing.T) {
	disc := testdisc.SampleMovie()
	loop := func(clip string, n int, each time.Duration) []testdisc.Segment {
		segs := make([]testdisc.Segment, n)
		for i := range segs {
			segs[i] = testdisc.Segment{Clip: clip, Length: each}
		}
		return segs
	}
	silent := testdisc.SimplePlaylist(loop("00173", 180, 5*time.Minute)...)
	for i := range silent.Items {
		silent.Items[i].STN.Audio = nil
	}
	disc.Playlists["00030"] = silent
	disc.Playlists["00149"] = testdisc.SimplePlaylist(loop("00175", 900, 45*time.Second)...)
	disc.AddClipsFor()

	d := openDisc(t, writeDisc(t, disc))
	if m := d.Main(); m == nil || m.ID != "00800" {
		t.Fatalf("Main = %+v, want 00800", m)
	}
	if in := mustTitle(t, d, "00030").Rank; !in.Filtered || !slices.Contains(in.Reasons, "no audio stream") {
		t.Errorf("00030 rank = %+v", in)
	}
	in := mustTitle(t, d, "00149").Rank
	if !in.Filtered || !slices.ContainsFunc(in.Reasons, func(r string) bool { return strings.HasPrefix(r, "data rate ") }) {
		t.Errorf("00149 rank = %+v", in)
	}
	if in := mustTitle(t, d, "00010").Rank; in.Filtered {
		t.Errorf("trailer 00010 should not be filtered: %+v", in)
	}
}
