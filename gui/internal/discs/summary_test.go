package discs

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/config"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func writeBluray(t *testing.T, d *testdisc.Disc, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeDVD(t *testing.T, d *testdisc.DVD) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_DVD")
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func open(t *testing.T, path string) *zenvik.Disc {
	t.Helper()
	d, err := zenvik.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestTitlesNamesAndOrder(t *testing.T) {
	d := open(t, writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE"))
	ts := Titles(d, config.DefaultTemplate)
	if len(ts) < 3 {
		t.Fatalf("titles = %+v", ts)
	}
	if ts[0].ID != "00800" || !ts[0].Main || !ts[0].Rippable {
		t.Errorf("first = %+v, want main 00800", ts[0])
	}
	if ts[0].DefaultName != "Sample Movie.mkv" || ts[1].DefaultName != "Sample Movie (2).mkv" || ts[2].DefaultName != "Sample Movie (3).mkv" {
		t.Errorf("names = %q %q %q", ts[0].DefaultName, ts[1].DefaultName, ts[2].DefaultName)
	}
	if ts[0].DurationSeconds != 6000 || ts[0].Chapters == 0 || ts[0].SizeBytes == 0 {
		t.Errorf("main facts = %+v", ts[0])
	}
}

func TestTitlesUsePlaylistInTemplate(t *testing.T) {
	d := open(t, writeBluray(t, testdisc.SampleMovie(), "SAMPLE_MOVIE"))
	ts := Titles(d, "{name} - {playlist}.mkv")
	if ts[0].DefaultName != "Sample Movie - 00800.mkv" {
		t.Errorf("name = %q", ts[0].DefaultName)
	}
}

func TestTitlesMarkEncryptedAndUnsupported(t *testing.T) {
	enc := testdisc.SampleMovie()
	enc.ClipData["00030"] = testdisc.ScrambledM2TS(1) // the trailer's clip
	d := open(t, writeBluray(t, enc, "PARTLY_ENCRYPTED"))
	var trailer TitleSummary
	for _, ts := range Titles(d, config.DefaultTemplate) {
		if ts.ID == "00010" {
			trailer = ts
		}
	}
	if trailer.Rippable || trailer.Reason != "encrypted" {
		t.Errorf("trailer = %+v", trailer)
	}

	dvd := open(t, writeDVD(t, testdisc.SampleDVD()))
	for _, ts := range Titles(dvd, config.DefaultTemplate) {
		if ts.ID == "08" && (ts.Rippable || ts.Reason == "") {
			t.Errorf("title 08 = %+v, want unsupported with a reason", ts)
		}
	}
}

func TestTitlesListSkippedCells(t *testing.T) {
	d := open(t, writeDVD(t, testdisc.StrayCellDVD()))
	for _, ts := range Titles(d, config.DefaultTemplate) {
		if ts.ID == "01" {
			if len(ts.SkippedCells) != 1 || ts.SkippedCells[0] == "" {
				t.Errorf("skipped = %q", ts.SkippedCells)
			}
			return
		}
	}
	t.Fatal("no title 01")
}

func TestDefaultOutputDir(t *testing.T) {
	yes := func(string) bool { return true }
	no := func(string) bool { return false }
	cases := []struct {
		configured, goos string
		exists           func(string) bool
		want             string
	}{
		{".", "darwin", yes, absPath("home", "u", "Movies")},
		{".", "darwin", no, absPath("home", "u")},
		{".", "linux", yes, absPath("home", "u")},
		{"", "windows", yes, absPath("home", "u")},
		{absPath("srv", "rips"), "darwin", yes, absPath("srv", "rips")},
		{"rips", "linux", yes, absPath("home", "u", "rips")},
	}
	for _, c := range cases {
		got := DefaultOutputDir(c.configured, c.goos, absPath("home", "u"), c.exists)
		if got != c.want {
			t.Errorf("DefaultOutputDir(%q, %s) = %q, want %q", c.configured, c.goos, got, c.want)
		}
	}
}
