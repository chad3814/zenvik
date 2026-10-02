package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
			if ti.Unsupported != "starts or ends mid-file (not supported yet)" {
				t.Errorf("03 unsupported = %q", ti.Unsupported)
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
	if code, _, errOut := runCLI("rip", "--title", "3", "-d", t.TempDir(), root); code != 1 || !strings.Contains(errOut, "starts or ends mid-file") {
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
