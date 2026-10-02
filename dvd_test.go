package zenvik_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func writeDVD(t *testing.T, d *testdisc.DVD) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_DVD")
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// ntsc is the duration of d as an NTSC IFO time, the way the fixtures
// store it: rounded to a whole 1001/30000 s frame.
func ntsc(d time.Duration) time.Duration { return dvd.NewTime(d, dvd.Rate30).Duration() }

func checkSampleDVD(t *testing.T, d *zenvik.Disc, vob1, vob2 string) {
	t.Helper()
	ch25 := ntsc(25 * time.Minute)
	if got, want := ids(d.Titles), []string{"01", "06", "03", "04", "05", "02", "07", "08"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("titles = %v, want %v", got, want)
	}
	m := d.Main()
	if m == nil || m.ID != "01" {
		t.Fatalf("Main = %+v", m)
	}
	if m.Duration != ntsc(4*ch25) || m.Size != 70*2048 || m.Angles != 1 || m.Encrypted || m.Unsupported != "" {
		t.Errorf("main = duration %v, size %d, angles %d, encrypted %v, unsupported %q", m.Duration, m.Size, m.Angles, m.Encrypted, m.Unsupported)
	}
	if want := []zenvik.Clip{{ID: vob1}, {ID: vob2}}; !reflect.DeepEqual(m.Clips, want) {
		t.Errorf("clips = %+v, want %+v", m.Clips, want)
	}
	wantCh := []zenvik.Chapter{{Number: 1, Start: 0}, {Number: 2, Start: ch25}, {Number: 3, Start: 2 * ch25}, {Number: 4, Start: 3 * ch25}}
	if !reflect.DeepEqual(m.Chapters, wantCh) {
		t.Errorf("chapters = %v", m.Chapters)
	}
	if want := []zenvik.VideoTrack{{PID: 0x00E0, Codec: bluray.CodingMPEG2Video, Format: 1, FrameRate: 4, AspectRatio: "16:9"}}; !reflect.DeepEqual(m.Video, want) {
		t.Errorf("video = %+v", m.Video)
	}
	wantAudio := []zenvik.AudioTrack{
		{PID: 0xBD80, Codec: bluray.CodingAC3, Language: "eng", Channels: 6, SampleRate: 1},
		{PID: 0xBD81, Codec: bluray.CodingAC3, Language: "fre", Channels: 3, SampleRate: 1},
		{PID: 0xBD82, Codec: bluray.CodingAC3, Language: "eng", Channels: 3, SampleRate: 1, Description: "Director's Commentary"},
	}
	if !reflect.DeepEqual(m.Audio, wantAudio) {
		t.Errorf("audio = %+v", m.Audio)
	}
	wantSubs := []zenvik.SubtitleTrack{
		{PID: 0xBD20, Codec: zenvik.CodingVobSub, Language: "eng"},
		{PID: 0xBD21, Codec: zenvik.CodingVobSub, Language: "fre"},
		{PID: 0xBD22, Codec: zenvik.CodingVobSub, Language: "eng", Description: "Forced"},
	}
	if !reflect.DeepEqual(m.Subtitles, wantSubs) {
		t.Errorf("subtitles = %+v", m.Subtitles)
	}
	if dup := mustTitle(t, d, "02"); dup.Rank.DuplicateOf != "01" {
		t.Errorf("02 rank = %+v", dup.Rank)
	}
	for id, method := range map[string]string{"01": "files", "06": "files", "03": "cut", "04": "cut", "05": "cut"} {
		ti := mustTitle(t, d, id)
		if ti.RipMethod != method || ti.Unsupported != "" || ti.Rank.Filtered {
			t.Errorf("%s: method %q, unsupported %q, rank %+v; want %q", id, ti.RipMethod, ti.Unsupported, ti.Rank, method)
		}
	}
	if ep := mustTitle(t, d, "04"); ep.Size != 20*2048 || len(ep.Clips) != 2 {
		t.Errorf("04 spans both VOBs: size %d, clips %v", ep.Size, ep.Clips)
	}
	if ang := mustTitle(t, d, "08"); ang.Unsupported != "interleaved angle block (not supported yet)" || ang.RipMethod != "" || !ang.Rank.Filtered {
		t.Errorf("08: unsupported %q, method %q, rank %+v", ang.Unsupported, ang.RipMethod, ang.Rank)
	}
	all := mustTitle(t, d, "06")
	if all.Unsupported != "" || all.Duration != ntsc(3*ntsc(20*time.Minute)) || len(all.Clips) != 2 || len(all.Chapters) != 3 || all.Chapters[2].Start != 2*ntsc(20*time.Minute) {
		t.Errorf("06 = %+v", all)
	}
	if ex := mustTitle(t, d, "07"); !ex.Rank.Filtered || !slices.Contains(ex.Rank.Reasons, "shorter than 2m0s") {
		t.Errorf("07 rank = %+v", ex.Rank)
	}
	if ang := mustTitle(t, d, "08"); ang.Angles != 2 {
		t.Errorf("08 angles = %d", ang.Angles)
	}
}

func TestOpenDVD(t *testing.T) {
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	if d.Format != zenvik.DVD || d.Kind != zenvik.VideoTSDir || d.Label != "SAMPLE_DVD" || d.Meta != nil {
		t.Errorf("disc = format %v, kind %v, label %q, meta %v", d.Format, d.Kind, d.Label, d.Meta)
	}
	checkSampleDVD(t, d, "VTS_01_1.VOB", "VTS_01_2.VOB")
}

func TestOpenDVDISO(t *testing.T) {
	img, err := testdisc.SampleDVD().ISO(udfimage.Options{Revision: 0x0102, Label: "SAMPLE_DVD"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "sample.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, p)
	if d.Format != zenvik.DVD || d.Kind != zenvik.ISO || d.Label != "SAMPLE_DVD" {
		t.Errorf("disc = format %v, kind %v, label %q", d.Format, d.Kind, d.Label)
	}
	checkSampleDVD(t, d, "VTS_01_1.VOB", "VTS_01_2.VOB")
}

func TestOpenDVDLowerCase(t *testing.T) {
	s := testdisc.SampleDVD()
	s.LowerCase, s.AppleDouble = true, true
	checkSampleDVD(t, openDisc(t, writeDVD(t, s)), "vts_01_1.vob", "vts_01_2.vob")
}

func TestOpenDVDEncrypted(t *testing.T) {
	all := testdisc.SampleDVD()
	for i := range all.TitleSets {
		all.TitleSets[i].Scrambled = true
	}
	if _, err := zenvik.Open(context.Background(), writeDVD(t, all)); !errors.Is(err, zenvik.ErrEncrypted) {
		t.Errorf("all scrambled: err = %v", err)
	}
	some := testdisc.SampleDVD()
	some.TitleSets[1].Scrambled = true
	d := openDisc(t, writeDVD(t, some))
	if m := d.Main(); m == nil || m.ID != "01" {
		t.Fatalf("Main = %+v", m)
	}
	ep := mustTitle(t, d, "06")
	if !ep.Encrypted {
		t.Error("06 should be encrypted")
	}
	if _, err := d.Rip(context.Background(), ep, zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "x.mkv")}); !errors.Is(err, zenvik.ErrEncrypted) {
		t.Errorf("rip encrypted: err = %v", err)
	}
}

func TestOpenDVDBadTitleSet(t *testing.T) {
	root := writeDVD(t, testdisc.SampleDVD())
	vob := filepath.Join(root, "VIDEO_TS", "VTS_02_2.VOB")
	if err := os.Truncate(vob, 30*2048-100); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "VIDEO_TS", "VTS_03_0.IFO")); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, root)
	if m := d.Main(); m == nil || m.ID != "01" {
		t.Fatalf("Main = %+v", m)
	}
	for _, id := range []string{"03", "04", "05", "06"} {
		ti := mustTitle(t, d, id)
		if !ti.Rank.Filtered || ti.Unsupported != "VTS_02_2.VOB: size is not a multiple of 2048" {
			t.Errorf("%s: unsupported %q, rank %+v", id, ti.Unsupported, ti.Rank)
		}
	}
	if ex := mustTitle(t, d, "07"); !ex.Rank.Filtered || ex.Unsupported != "missing VTS_03_0.IFO" {
		t.Errorf("07: unsupported %q", ex.Unsupported)
	}
}

func TestDVDTitleLookup(t *testing.T) {
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	for _, id := range []string{"3", "03"} {
		ti, err := d.Title(id)
		if err != nil || ti.ID != "03" {
			t.Errorf("Title(%q) = %v, %v", id, ti, err)
		}
	}
	if _, err := d.Title("10"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Title(10): err = %v", err)
	}
}

func TestRipUnsupportedDVDTitle(t *testing.T) {
	root := writeDVD(t, testdisc.SampleDVD())
	if err := os.Remove(filepath.Join(root, "VIDEO_TS", "VTS_03_0.IFO")); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, root)
	_, err := d.Rip(context.Background(), mustTitle(t, d, "07"), zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "x.mkv")})
	if !errors.Is(err, zenvik.ErrUnsupportedTitle) || !strings.Contains(err.Error(), "missing VTS_03_0.IFO") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenStrayCellDVD(t *testing.T) {
	d := openDisc(t, writeDVD(t, testdisc.StrayCellDVD()))
	ch24, sec := ntsc(24*time.Minute), ntsc(time.Second)
	cut := mustTitle(t, d, "01")
	if cut.RipMethod != "cut" || cut.Duration != ntsc(2*ch24+sec)-sec || cut.Size != 48*2048 {
		t.Errorf("01 = method %q, duration %v, size %d", cut.RipMethod, cut.Duration, cut.Size)
	}
	if want := []zenvik.SkippedCell{{Cell: 3, Duration: sec, FirstSector: 0, LastSector: 1}}; !reflect.DeepEqual(cut.SkippedCells, want) {
		t.Errorf("01 skipped = %+v", cut.SkippedCells)
	}
	if want := []zenvik.Chapter{{Number: 1, Start: 0}, {Number: 2, Start: ch24}}; !reflect.DeepEqual(cut.Chapters, want) {
		t.Errorf("01 chapters = %v", cut.Chapters)
	}
	cp := mustTitle(t, d, "02")
	if cp.RipMethod != "copy" || len(cp.SkippedCells) != 0 || len(cp.Chapters) != 3 || cp.Chapters[2].Start != ch24+sec {
		t.Errorf("02 = method %q, skipped %v, chapters %v", cp.RipMethod, cp.SkippedCells, cp.Chapters)
	}
	if f := mustTitle(t, d, "03"); f.RipMethod != "files" {
		t.Errorf("03 method %q", f.RipMethod)
	}
}

func TestOpenDVDCellsOutsideVOBs(t *testing.T) {
	disc := testdisc.SampleDVD()
	disc.TitleSets[2].VTS.PGCs[0].Cells[0].LastSector = 12 // VTS_03_1.VOB has sectors 0–9
	d := openDisc(t, writeDVD(t, disc))
	const reason = "cells point outside the title VOBs"
	ex := mustTitle(t, d, "07")
	if ex.Unsupported != reason || !ex.Rank.Filtered || !slices.Contains(ex.Rank.Reasons, reason) {
		t.Errorf("07: unsupported %q, rank %+v", ex.Unsupported, ex.Rank)
	}
	if _, err := d.Rip(context.Background(), ex, zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "x.mkv")}); !errors.Is(err, zenvik.ErrUnsupportedTitle) {
		t.Errorf("rip: err = %v", err)
	}
	if m := mustTitle(t, d, "01"); m.Unsupported != "" {
		t.Errorf("01: unsupported %q", m.Unsupported)
	}
}
