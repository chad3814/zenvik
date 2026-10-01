package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func writeDisc(t *testing.T, d *testdisc.Disc) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_MOVIE")
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func runCLI(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func lineWith(out, s string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func TestInfoTable(t *testing.T) {
	code, out, errOut := runCLI("info", writeDisc(t, testdisc.SampleMovie()))
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if !strings.HasPrefix(out, "Sample Movie  (BDMV folder: ") {
		t.Errorf("header = %q", strings.SplitN(out, "\n", 2)[0])
	}
	main := lineWith(out, "00800")
	for _, want := range []string{"★", "1:40:00", "120.0 KiB", "20", "H.264/AVC 1080p", "eng,fra", "eng"} {
		if !strings.Contains(main, want) {
			t.Errorf("main line %q lacks %q", main, want)
		}
	}
	if !strings.HasPrefix(main, "★") {
		t.Errorf("main line should start with ★: %q", main)
	}
	if dup := lineWith(out, "00801"); !strings.Contains(dup, "duplicate of 00800") || strings.Contains(dup, "★") {
		t.Errorf("duplicate line = %q", dup)
	}
	if strings.Contains(out, "00099") {
		t.Error("filtered title shown without --all")
	}
	if !strings.Contains(out, "1 filtered title(s) hidden; use --all to show them.") {
		t.Errorf("missing hidden footer in:\n%s", out)
	}
}

func TestInfoAll(t *testing.T) {
	code, out, _ := runCLI("info", "--all", writeDisc(t, testdisc.SampleMovie()))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if l := lineWith(out, "00099"); !strings.Contains(l, "shorter than 2m0s") {
		t.Errorf("filtered line = %q", l)
	}
	if strings.Contains(out, "hidden") {
		t.Error("footer shown with --all")
	}
}

func TestInfoJSON(t *testing.T) {
	code, out, _ := runCLI("info", "--json", writeDisc(t, testdisc.SampleMovie()))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var got jsonDisc
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Main != "00800" || got.Kind != "bdmv" || got.Label != "SAMPLE_MOVIE" || got.Title != "Sample Movie" {
		t.Errorf("disc = %+v", got)
	}
	if len(got.Titles) != 4 {
		t.Fatalf("titles = %d, want 4", len(got.Titles))
	}
	m := got.Titles[0]
	if m.ID != "00800" || !m.Rank.Main || m.DurationSeconds != 6000 || len(m.Chapters) != 20 ||
		m.Video[0].Codec != "H.264/AVC" || m.Audio[0].Language != "eng" || m.SizeBytes != 20*6144 {
		t.Errorf("main = %+v", m)
	}
	last := got.Titles[3]
	if last.ID != "00099" || !last.Rank.Filtered {
		t.Errorf("filtered title = %+v", last)
	}
	if !strings.Contains(out, `"chapters": [`) || strings.Contains(out, `"reasons": null`) {
		t.Errorf("JSON should use [] rather than null for empty lists:\n%s", out)
	}
}

func TestInfoAmbiguousWarning(t *testing.T) {
	d := testdisc.SampleMovie()
	d.Titles, d.MovieObjects = nil, nil // no title-1 signal
	var segs []testdisc.Segment
	for i := 201; i <= 220; i++ {
		segs = append(segs, testdisc.Segment{Clip: fmt.Sprintf("%05d", i), Length: 297 * time.Second})
	}
	d.Playlists["00802"] = testdisc.SimplePlaylist(segs...)
	d.AddClipsFor()
	code, out, errOut := runCLI("info", writeDisc(t, d))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(errOut, "warning: the main title is a close call (close second: 00802") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(lineWith(out, "00800"), "ambiguous") {
		t.Errorf("main line = %q", lineWith(out, "00800"))
	}
}

func TestExitCodes(t *testing.T) {
	good := writeDisc(t, testdisc.SampleMovie())
	enc := testdisc.SampleMovie()
	for id := range enc.Clips {
		enc.ClipData[id] = testdisc.ScrambledM2TS(1)
	}
	encrypted := writeDisc(t, enc)
	dvd := filepath.Join(t.TempDir(), "DVD")
	if err := os.MkdirAll(filepath.Join(dvd, "VIDEO_TS"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args []string
		want int
	}{
		{[]string{"info", good}, 0},
		{nil, 2},
		{[]string{"bogus"}, 2},
		{[]string{"info"}, 2},
		{[]string{"info", good, good}, 2},
		{[]string{"info", "--bogus", good}, 2},
		{[]string{"info", filepath.Join(good, "missing")}, 1},
		{[]string{"info", dvd}, 3},
		{[]string{"info", encrypted}, 3},
	}
	for _, tt := range tests {
		code, _, errOut := runCLI(tt.args...)
		if code != tt.want {
			t.Errorf("zenvik %v: exit %d, want %d (stderr %q)", tt.args, code, tt.want, errOut)
		}
		if tt.want != 0 {
			if strings.Contains(errOut, "zenvik: zenvik:") {
				t.Errorf("zenvik %v: doubled prefix in stderr: %q", tt.args, errOut)
			}
			// The error line is the last line; usage help may precede it.
			lines := strings.Split(strings.TrimSpace(errOut), "\n")
			if last := lines[len(lines)-1]; !strings.HasPrefix(last, "zenvik: ") {
				t.Errorf("zenvik %v: error line lacks prefix: %q", tt.args, last)
			}
		}
	}
}

func TestInfoNoMainTitle(t *testing.T) {
	d := &testdisc.Disc{Playlists: map[string]*bluray.Playlist{
		"00001": testdisc.SimplePlaylist(testdisc.Segment{Clip: "00001", Length: 90 * time.Second}),
		"00002": testdisc.SimplePlaylist(testdisc.Segment{Clip: "00002", Length: 30 * time.Second}),
	}, ClipData: map[string][]byte{}}
	d.AddClipsFor()
	code, out, errOut := runCLI("info", writeDisc(t, d))
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "No title qualifies as the main feature.") {
		t.Errorf("stdout = %q", out)
	}
}

func TestNotesFilteredEncryptedOnly(t *testing.T) {
	ti := &zenvik.Title{Encrypted: true, Rank: zenvik.RankInfo{Filtered: true, Reasons: []string{"encrypted"}}}
	if got := notes(ti); got != "encrypted" {
		t.Errorf("notes = %q, want %q", got, "encrypted")
	}
	ti.Rank.Reasons = []string{"encrypted", "no video stream"}
	if got := notes(ti); got != "encrypted, no video stream" {
		t.Errorf("notes = %q", got)
	}
}

// TestMain keeps tests away from the developer's real config and state.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "zenvik-cli-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
