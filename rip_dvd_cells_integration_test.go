//go:build integration

package zenvik_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

const oneFrame = 34 * time.Millisecond

// videoDuration is the video track's span (first to last frame timestamp,
// plus one NTSC frame; the trailing end-time value timestamps_v2 writes is
// dropped). The container duration also counts audio, which
// mkvmerge appends after audio at VOB ID changes (Task 1 notes).
func videoDuration(t *testing.T, path string) time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "v.txt")
	if out, err := exec.Command("mkvextract", path, "timestamps_v2", "0:"+txt).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(txt)
	var ts []float64
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n")[1:] {
		if v, err := strconv.ParseFloat(strings.TrimSpace(l), 64); err == nil {
			ts = append(ts, v)
		}
	}
	if len(ts) < 2 {
		t.Fatalf("no video timestamps in %s", path)
	}
	slices.Sort(ts)
	ts = ts[:len(ts)-1] // timestamps_v2 ends with the last frame's end time (Task 1 notes)
	return time.Duration((ts[len(ts)-1]-ts[0])*float64(time.Millisecond)) + 1001*time.Second/30000
}

func episodesDisc(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "EPISODES")
	if err := testdisc.AuthorEpisodesDVD(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err := testdisc.AddMixedEpisodeTitles(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func colours(t *testing.T, path string) (string, string) {
	t.Helper()
	a, err := testdisc.FrameColor(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	z, err := testdisc.FrameColor(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	return testdisc.Dominant(a), testdisc.Dominant(z)
}

func ripTo(t *testing.T, d *zenvik.Disc, id string) (string, *zenvik.RipResult) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "title.mkv")
	res, err := d.Rip(context.Background(), mustTitle(t, d, id), zenvik.RipOptions{OutputPath: out})
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("output dir holds %v, want only the MKV", ents)
	}
	return out, res
}

func TestRipDVDCut(t *testing.T) {
	d := openDisc(t, episodesDisc(t))
	ti := mustTitle(t, d, "03") // green episode
	if ti.RipMethod != "cut" {
		t.Fatalf("03 method %q", ti.RipMethod)
	}
	out, _ := ripTo(t, d, "03")
	if got := videoDuration(t, out); got < ti.Duration-oneFrame || got > ti.Duration+oneFrame {
		t.Errorf("duration %v, title %v", got, ti.Duration)
	}
	if a, z := colours(t, out); a != "green" || z != "green" {
		t.Errorf("colours %s → %s, want green", a, z)
	}
	starts := chapterStarts(t, out)
	if len(starts) != len(ti.Chapters) {
		t.Fatalf("chapters %v, title %v", starts, ti.Chapters)
	}
	for i, c := range ti.Chapters {
		if diff := starts[i] - c.Start; diff < -oneFrame || diff > oneFrame {
			t.Errorf("chapter %d at %v, IFO %v", i+1, starts[i], c.Start)
		}
	}
	if sub := firstTimestamp(t, out, 2); sub < 900*time.Millisecond || sub > 1100*time.Millisecond {
		t.Errorf("first subtitle at %v, want 1.0 s", sub)
	}
}

func TestRipDVDCutSkipsStray(t *testing.T) {
	d := openDisc(t, episodesDisc(t))
	ti := mustTitle(t, d, "05")
	if ti.RipMethod != "cut" || len(ti.SkippedCells) != 1 {
		t.Fatalf("05 = method %q, skipped %v", ti.RipMethod, ti.SkippedCells)
	}
	out, res := ripTo(t, d, "05")
	if !slices.ContainsFunc(res.Warnings, func(w string) bool { return strings.Contains(w, "skipped cell") }) {
		t.Errorf("warnings = %q", res.Warnings)
	}
	if got := videoDuration(t, out); got < ti.Duration-oneFrame || got > ti.Duration+oneFrame {
		t.Errorf("duration %v, title %v", got, ti.Duration)
	}
	if a, z := colours(t, out); a != "red" || z != "red" {
		t.Errorf("colours %s → %s, want red", a, z)
	}
}

func TestRipDVDCopy(t *testing.T) {
	d := openDisc(t, episodesDisc(t))
	ti := mustTitle(t, d, "06")
	if ti.RipMethod != "copy" {
		t.Fatalf("06 method %q", ti.RipMethod)
	}
	out, _ := ripTo(t, d, "06")
	if got := videoDuration(t, out); got < ti.Duration-2*oneFrame || got > ti.Duration+2*oneFrame {
		t.Errorf("duration %v, title %v", got, ti.Duration)
	}
	if a, z := colours(t, out); a != "blue" || z != "red" {
		t.Errorf("colours %s → %s, want blue → red", a, z)
	}
}
