//go:build integration

package testdisc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

type mkvJSON struct {
	Container struct {
		Properties struct {
			Duration int64 `json:"duration"`
		} `json:"properties"`
	} `json:"container"`
	Tracks []struct {
		Type       string `json:"type"`
		Codec      string `json:"codec"`
		Properties struct {
			StreamID         int    `json:"stream_id"`
			SubStreamID      int    `json:"sub_stream_id"`
			CodecPrivateData string `json:"codec_private_data"`
		} `json:"properties"`
	} `json:"tracks"`
}

func mkvmergeJSON(t *testing.T, path string) mkvJSON {
	t.Helper()
	out, err := exec.Command("mkvmerge", "-J", path).Output()
	var ee *exec.ExitError
	if err != nil && (!errors.As(err, &ee) || ee.ExitCode() != 1) {
		t.Fatalf("mkvmerge -J %s: %v", path, err)
	}
	var j mkvJSON
	if err := json.Unmarshal(out, &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestAuthorDVDAndMkvmerge(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "MY_DVD")
	if err := AuthorDVD(context.Background(), dir, 15); err != nil {
		t.Fatal(err)
	}
	vts := filepath.Join(dir, "VIDEO_TS")
	for _, n := range []string{"VIDEO_TS.IFO", "VTS_01_0.IFO", "VTS_01_1.VOB", "VTS_01_2.VOB"} {
		st, err := os.Stat(filepath.Join(vts, n))
		if err != nil {
			t.Fatal(err)
		}
		if st.Size()%2048 != 0 || st.Size() == 0 {
			t.Errorf("%s size %d is not a positive multiple of 2048", n, st.Size())
		}
	}

	// Every title VOB must start with a NAV pack (VOBU start), and the
	// authored VOBs must really carry a subpicture stream, which a later
	// extractor relies on.
	subpackets := 0
	for _, n := range []string{"VTS_01_1.VOB", "VTS_01_2.VOB"} {
		b, err := os.ReadFile(filepath.Join(vts, n))
		if err != nil {
			t.Fatal(err)
		}
		if !isNavPack(b) {
			t.Errorf("%s: first pack is not a NAV pack", n)
		}
		subpackets += countSubpicturePackets(b)
	}
	if subpackets == 0 {
		t.Error("no private stream 1 pack with sub-stream 0x20 (subpicture) in the VOBs")
	}
	t.Logf("subpicture packs in VOBs: %d", subpackets)

	// (a) stream IDs as mkvmerge reports them for a VOB. mkvmerge ignores
	// DVD subpictures, so none are expected.
	in := mkvmergeJSON(t, filepath.Join(vts, "VTS_01_1.VOB"))
	var got []string
	for _, tr := range in.Tracks {
		if tr.Type == "subtitles" {
			t.Logf("mkvmerge reported a subtitle track: stream %d sub-stream %d", tr.Properties.StreamID, tr.Properties.SubStreamID)
			continue
		}
		got = append(got, tr.Type+" "+itoa(tr.Properties.StreamID)+" "+itoa(tr.Properties.SubStreamID))
	}
	sort.Strings(got)
	want := []string{"audio 189 128", "audio 189 129", "video 224 0"}
	t.Logf("tracks: %q", got)
	if len(got) != len(want) {
		t.Fatalf("tracks = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tracks = %q, want %q", got, want)
			break
		}
	}

	// (b) mkvmerge chains sibling VOBs itself; the parenthesized group
	// gives the full duration (a "+" append would duplicate the tail).
	out := filepath.Join(t.TempDir(), "out.mkv")
	cmd := exec.Command("mkvmerge", "-o", out, "(", filepath.Join(vts, "VTS_01_1.VOB"), filepath.Join(vts, "VTS_01_2.VOB"), ")")
	if b, err := cmd.CombinedOutput(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("mkvmerge: %v\n%s", err, b)
		}
	}
	res := mkvmergeJSON(t, out)
	secs := float64(res.Container.Properties.Duration) / 1e9
	t.Logf("muxed duration: %.3f s", secs)
	if math.Abs(secs-15) > 0.25 {
		t.Errorf("duration = %.3f s, want 15 ± 0.25", secs)
	}
	if len(res.Tracks) != 3 {
		t.Errorf("muxed %d tracks, want 3", len(res.Tracks))
	}
}

// countSubpicturePackets counts 2048-byte packs holding a private stream 1
// PES whose sub-stream byte is 0x20 (subpicture stream 0).
func countSubpicturePackets(b []byte) int {
	n := 0
	for s := 0; (s+1)*2048 <= len(b); s++ {
		p := b[s*2048 : (s+1)*2048]
		i := bytes.Index(p[14:], []byte{0, 0, 1, 0xBD})
		if i < 0 {
			continue
		}
		pes := p[14+i:]
		if len(pes) < 10 {
			continue
		}
		if off := 9 + int(pes[8]); off < len(pes) && pes[off] == 0x20 {
			n++
		}
	}
	return n
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
