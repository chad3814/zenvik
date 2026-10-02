//go:build integration

package zenvik_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func authoredDVD(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "AUTHORED_DVD")
	if err := testdisc.AuthorDVD(context.Background(), dir, 15); err != nil {
		t.Fatal(err)
	}
	return dir
}

// containerDuration returns the duration mkvmerge reports for a file.
func containerDuration(t *testing.T, path string) time.Duration {
	t.Helper()
	out, err := exec.Command("mkvmerge", "-J", path).Output()
	if err != nil && len(out) == 0 {
		t.Fatal(err)
	}
	var j struct {
		Container struct {
			Properties struct {
				Duration int64 `json:"duration"`
			} `json:"properties"`
		} `json:"container"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		t.Fatal(err)
	}
	return time.Duration(j.Container.Properties.Duration)
}

// chapterStarts extracts an MKV's chapter start times with mkvextract.
func chapterStarts(t *testing.T, path string) []time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "chapters.txt")
	if out, err := exec.Command("mkvextract", path, "chapters", "--simple", txt).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract: %v\n%s", err, out)
	}
	b, err := os.ReadFile(txt)
	if err != nil {
		t.Fatal(err)
	}
	var starts []time.Duration
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok || strings.HasSuffix(k, "NAME") {
			continue
		}
		var h, m, s, ms int
		if _, err := fmt.Sscanf(v, "%d:%d:%d.%d", &h, &m, &s, &ms); err != nil {
			t.Fatalf("chapter line %q: %v", sc.Text(), err)
		}
		starts = append(starts, time.Duration(h)*time.Hour+time.Duration(m)*time.Minute+time.Duration(s)*time.Second+time.Duration(ms)*time.Millisecond)
	}
	return starts
}

// firstTimestamp returns the first block timestamp of track tid in an MKV.
func firstTimestamp(t *testing.T, path string, tid int) time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "ts.txt")
	if out, err := exec.Command("mkvextract", path, "timestamps_v2", fmt.Sprintf("%d:%s", tid, txt)).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract timestamps: %v\n%s", err, out)
	}
	b, err := os.ReadFile(txt)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 2 {
		t.Fatalf("no timestamps for track %d:\n%s", tid, b)
	}
	var ms float64
	if _, err := fmt.Sscanf(lines[1], "%g", &ms); err != nil {
		t.Fatalf("timestamp line %q: %v", lines[1], err)
	}
	return time.Duration(ms * float64(time.Millisecond))
}

func assertDVDRipped(t *testing.T, out string, m *zenvik.Title) {
	t.Helper()
	id := identify(t, out)
	var types, langs []string
	for _, tr := range id.Tracks {
		types = append(types, tr.Type)
		if tr.Type == "audio" {
			langs = append(langs, tr.Language)
		}
	}
	if strings.Join(types, ",") != "video,audio,audio,subtitles" {
		t.Errorf("tracks = %v", types)
	}
	if strings.Join(langs, ",") != "eng,fre" {
		t.Errorf("audio languages = %v", langs)
	}
	if d := containerDuration(t, out); d < m.Duration-250*time.Millisecond || d > m.Duration+250*time.Millisecond {
		t.Errorf("duration = %v, title says %v", d, m.Duration)
	}
	if first := firstTimestamp(t, out, 3); first < 1900*time.Millisecond || first > 2100*time.Millisecond {
		t.Errorf("first subtitle at %v, want 2.0 s (spumux shows it from 2 s)", first)
	}
	starts := chapterStarts(t, out)
	if len(starts) != len(m.Chapters) {
		t.Fatalf("chapters = %v, title has %v", starts, m.Chapters)
	}
	const frame = 34 * time.Millisecond
	for i, c := range m.Chapters {
		if diff := starts[i] - c.Start; diff < -frame || diff > frame {
			t.Errorf("chapter %d at %v, IFO says %v", i+1, starts[i], c.Start)
		}
	}
}

func TestRipDVDDirectory(t *testing.T) {
	d := openDisc(t, authoredDVD(t))
	if d.Format != zenvik.DVD || d.Kind != zenvik.VideoTSDir {
		t.Fatalf("format %v, kind %v", d.Format, d.Kind)
	}
	// The authored title is 15 s, below the main-feature minimum, so Main()
	// is nil; pick the title by ID.
	m := mustTitle(t, d, "01")
	if m.Unsupported != "" || len(m.Clips) != 2 {
		t.Fatalf("title = %+v", m)
	}
	out := filepath.Join(t.TempDir(), "dvd.mkv")
	if _, err := d.Rip(context.Background(), m, zenvik.RipOptions{OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	assertDVDRipped(t, out, m)
}
