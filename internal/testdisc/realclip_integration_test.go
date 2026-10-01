//go:build integration

package testdisc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik/bluray"
)

func TestRealMovie(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "REAL_MOVIE")
	if err := RealMovie(context.Background(), dir, 3); err != nil {
		t.Fatal(err)
	}
	for _, clip := range []string{"00001", "00002"} {
		b, err := os.ReadFile(filepath.Join(dir, "BDMV", "STREAM", clip+".m2ts"))
		if err != nil {
			t.Fatal(err)
		}
		if len(b) == 0 || len(b)%192 != 0 {
			t.Fatalf("%s: %d bytes is not a whole number of source packets", clip, len(b))
		}
		for p := 0; p < 32 && (p+1)*192 <= len(b); p++ {
			if b[p*192+4] != 0x47 {
				t.Fatalf("%s: packet %d has no sync byte", clip, p)
			}
		}
	}
	mpls, err := os.ReadFile(filepath.Join(dir, "BDMV", "PLAYLIST", "00800.mpls"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := bluray.ParsePlaylist(mpls)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Items[0].In != ClipStartTicks || len(p.Chapters()) != 3 || p.Items[0].STN.Audio[0].Language != "jpn" {
		t.Errorf("playlist = %+v", p)
	}
}
