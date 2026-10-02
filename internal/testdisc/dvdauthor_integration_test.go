//go:build integration

package testdisc

import (
	"context"
	"encoding/hex"
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

	// (a) stream IDs as mkvmerge reports them for a VOB.
	in := mkvmergeJSON(t, filepath.Join(vts, "VTS_01_1.VOB"))
	var got []string
	for _, tr := range in.Tracks {
		got = append(got, tr.Type+" "+itoa(tr.Properties.StreamID)+" "+itoa(tr.Properties.SubStreamID))
	}
	sort.Strings(got)
	want := []string{"audio 189 128", "audio 189 129", "subtitles 189 32", "video 224 0"}
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

	// (b) appending the split VOBs keeps the full duration and every track.
	out := filepath.Join(t.TempDir(), "out.mkv")
	cmd := exec.Command("mkvmerge", "-o", out, filepath.Join(vts, "VTS_01_1.VOB"), "+", filepath.Join(vts, "VTS_01_2.VOB"))
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
	if len(res.Tracks) != 4 {
		t.Errorf("muxed %d tracks, want 4", len(res.Tracks))
	}

	// (c) record what mkvmerge writes as the VobSub codec private data.
	for _, tr := range res.Tracks {
		if tr.Type == "subtitles" {
			b, _ := hex.DecodeString(tr.Properties.CodecPrivateData)
			t.Logf("VobSub codec private (%s): %q", tr.Codec, b)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
