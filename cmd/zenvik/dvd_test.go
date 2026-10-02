package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func writeDVD(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_DVD")
	if err := testdisc.SampleDVD().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInfoDVD(t *testing.T) {
	code, out, errOut := runCLI("info", writeDVD(t))
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if !strings.HasPrefix(out, "SAMPLE_DVD  (VIDEO_TS folder: ") {
		t.Errorf("header = %q", strings.SplitN(out, "\n", 2)[0])
	}
	main := titleRow(out, "★")
	for _, want := range []string{"01", "1:40:00", "MPEG-2 Video 480i", "eng,fre"} {
		if !strings.Contains(main, want) {
			t.Errorf("main row %q lacks %q", main, want)
		}
	}
}

func TestInfoDVDJSON(t *testing.T) {
	code, out, _ := runCLI("info", "--json", writeDVD(t))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var disc struct {
		Kind, Format, Main string
		Titles             []struct {
			ID          string
			Unsupported string
			RipMethod   string `json:"rip_method"`
			Video       []struct {
				AspectRatio string `json:"aspect_ratio"`
			}
			Subtitles []struct{ Codec, Description string }
		}
	}
	if err := json.Unmarshal([]byte(out), &disc); err != nil {
		t.Fatal(err)
	}
	if disc.Kind != "video_ts" || disc.Format != "dvd" || disc.Main != "01" {
		t.Errorf("kind %q, format %q, main %q", disc.Kind, disc.Format, disc.Main)
	}
	for _, ti := range disc.Titles {
		switch ti.ID {
		case "01":
			if ti.Unsupported != "" || ti.Video[0].AspectRatio != "16:9" || ti.Subtitles[2].Codec != "VobSub" || ti.Subtitles[2].Description != "Forced" {
				t.Errorf("01 = %+v", ti)
			}
		case "03":
			if ti.Unsupported != "" || ti.RipMethod != "cut" {
				t.Errorf("03 = %+v, want rippable by cut", ti)
			}
		case "08":
			if ti.RipMethod != "copy" {
				t.Errorf("08 rip_method = %q", ti.RipMethod)
			}
		}
	}
}

func TestInfoDVDISO(t *testing.T) {
	img, err := testdisc.SampleDVD().ISO(udfimage.Options{Revision: 0x0102, Label: "SAMPLE_DVD"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "sample.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI("info", p)
	if code != 0 || !strings.Contains(strings.SplitN(out, "\n", 2)[0], "(DVD ISO image: ") {
		t.Errorf("code %d, header %q", code, strings.SplitN(out, "\n", 2)[0])
	}
	_, js, _ := runCLI("info", "--json", p)
	if !strings.Contains(js, `"kind": "iso"`) || !strings.Contains(js, `"format": "dvd"`) {
		t.Errorf("json kind/format wrong:\n%s", js)
	}
}

func TestRipDVDTitleFlag(t *testing.T) {
	root := writeDVD(t)
	bad := writeDVD(t)
	if err := os.Remove(filepath.Join(bad, "VIDEO_TS", "VTS_03_0.IFO")); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runCLI("rip", "--title", "7", "-d", t.TempDir(), bad); code != 1 || !strings.Contains(errOut, "missing VTS_03_0.IFO") {
		t.Errorf("unsupported title: code %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runCLI("rip", "--title", "1", "--playlist", "01", root); code != 2 || !strings.Contains(errOut, "not both") {
		t.Errorf("both flags: code %d, stderr %q", code, errOut)
	}
	t.Setenv("PATH", t.TempDir())
	code, out, _ := runCLI("rip", "-t", "1", "-d", t.TempDir(), root)
	if code != 4 || !strings.Contains(out, "Ripping 01") {
		t.Errorf("rip -t 1 without mkvmerge: code %d, out %q", code, out)
	}
}

func writeStrayDVD(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "STRAY")
	if err := testdisc.StrayCellDVD().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInfoSkippedCells(t *testing.T) {
	root := writeStrayDVD(t)
	_, out, _ := runCLI("info", "--all", root)
	if row := titleRow(out, " 01 "); !strings.Contains(row, "skipped 1 short cell(s)") {
		t.Errorf("01 row = %q", row)
	}
	_, js, _ := runCLI("info", "--json", root)
	var disc struct {
		Titles []struct {
			ID           string
			RipMethod    string `json:"rip_method"`
			SkippedCells []struct {
				Cell            int     `json:"cell"`
				DurationSeconds float64 `json:"duration_seconds"`
				FirstSector     uint32  `json:"first_sector"`
				LastSector      uint32  `json:"last_sector"`
			} `json:"skipped_cells"`
		}
	}
	if err := json.Unmarshal([]byte(js), &disc); err != nil {
		t.Fatal(err)
	}
	sec := dvd.NewTime(time.Second, dvd.Rate30).Duration().Seconds() // 00:00:01:00, 1.001 s
	for _, ti := range disc.Titles {
		if ti.ID == "01" && (ti.RipMethod != "cut" || len(ti.SkippedCells) != 1 || ti.SkippedCells[0].Cell != 3 ||
			ti.SkippedCells[0].DurationSeconds != sec || ti.SkippedCells[0].LastSector != 1) {
			t.Errorf("01 = %+v", ti)
		}
	}
}

func TestRipVia(t *testing.T) {
	root := writeStrayDVD(t)
	t.Setenv("PATH", t.TempDir())
	if _, out, _ := runCLI("rip", "-t", "1", "-d", t.TempDir(), root); !strings.Contains(out, "  via: cut\n") {
		t.Errorf("cut: %q", out)
	}
	if _, out, _ := runCLI("rip", "-t", "2", "-d", t.TempDir(), root); !strings.Contains(out, "  via: copy (100.0 KiB temporary file)\n") {
		t.Errorf("copy: %q", out)
	}
	if _, out, _ := runCLI("rip", "-t", "3", "-d", t.TempDir(), root); strings.Contains(out, "via:") {
		t.Errorf("whole files print no via line: %q", out)
	}
}
