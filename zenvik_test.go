package zenvik_test

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func writeDisc(t *testing.T, d *testdisc.Disc) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_MOVIE")
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func openDisc(t *testing.T, path string) *zenvik.Disc {
	t.Helper()
	d, err := zenvik.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func ids(titles []*zenvik.Title) []string {
	var out []string
	for _, t := range titles {
		out = append(out, t.ID)
	}
	return out
}

func mustTitle(t *testing.T, d *zenvik.Disc, id string) *zenvik.Title {
	t.Helper()
	ti, err := d.Title(id)
	if err != nil {
		t.Fatal(err)
	}
	return ti
}

func checkSample(t *testing.T, d *zenvik.Disc) {
	t.Helper()
	if d.Meta == nil || d.Meta.Title != "Sample Movie" {
		t.Errorf("Meta = %+v", d.Meta)
	}
	if got, want := ids(d.Titles), []string{"00800", "00010", "00801", "00099"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("titles = %v, want %v", got, want)
	}
	m := d.Main()
	if m == nil || m.ID != "00800" {
		t.Fatalf("Main = %+v", m)
	}
	if m.Duration != 100*time.Minute || m.Size != 20*6144 || m.Angles != 1 || m.Encrypted {
		t.Errorf("main = duration %v, size %d, angles %d, encrypted %v", m.Duration, m.Size, m.Angles, m.Encrypted)
	}
	if len(m.Chapters) != 20 || m.Chapters[1] != (zenvik.Chapter{Number: 2, Start: 5 * time.Minute}) {
		t.Errorf("chapters = %v", m.Chapters)
	}
	if len(m.Clips) != 20 || m.Clips[0] != (zenvik.Clip{ID: "00001", In: 0, Out: 5 * time.Minute}) {
		t.Errorf("clips = %v", m.Clips)
	}
	wantVideo := zenvik.VideoTrack{PID: 0x1011, Codec: bluray.CodingAVC, Format: 6, FrameRate: 1}
	if len(m.Video) != 1 || m.Video[0] != wantVideo || m.Video[0].HDR() {
		t.Errorf("video = %+v", m.Video)
	}
	if len(m.Audio) != 2 || m.Audio[0].Language != "eng" || m.Audio[1].Language != "fra" ||
		m.Audio[0].Codec != bluray.CodingTrueHD || m.Audio[0].Channels != 6 {
		t.Errorf("audio = %+v", m.Audio)
	}
	if len(m.Subtitles) != 1 || m.Subtitles[0].Language != "eng" {
		t.Errorf("subtitles = %+v", m.Subtitles)
	}
	if !slices.Contains(m.Rank.Reasons, "played by title 1") || m.Rank.Ambiguous {
		t.Errorf("main rank = %+v", m.Rank)
	}
	if dup := mustTitle(t, d, "00801"); dup.Rank.DuplicateOf != "00800" {
		t.Errorf("00801 rank = %+v", dup.Rank)
	}
	if menu := mustTitle(t, d, "00099"); !menu.Rank.Filtered || !slices.Contains(menu.Rank.Reasons, "shorter than 2m0s") {
		t.Errorf("00099 rank = %+v", menu.Rank)
	}
	if _, err := d.Title("12345"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Title(12345) err = %v", err)
	}
}

func TestOpenDirectory(t *testing.T) {
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	if d.Kind != zenvik.BDMVDir || d.Label != "SAMPLE_MOVIE" {
		t.Errorf("Kind %v, Label %q", d.Kind, d.Label)
	}
	checkSample(t, d)
}

func TestOpenISO(t *testing.T) {
	for _, rev := range []uint16{0x0102, 0x0250} {
		img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: rev, Label: "SAMPLE_MOVIE"})
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "disc.iso")
		if err := os.WriteFile(p, img, 0o644); err != nil {
			t.Fatal(err)
		}
		d, err := zenvik.Open(context.Background(), p)
		if err != nil {
			t.Fatalf("rev %#x: %v", rev, err)
		}
		if d.Kind != zenvik.ISO || d.Label != "SAMPLE_MOVIE" || d.Path != p {
			t.Errorf("rev %#x: Kind %v, Label %q, Path %q", rev, d.Kind, d.Label, d.Path)
		}
		checkSample(t, d)
		if err := d.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		if err := d.Close(); err != nil {
			t.Errorf("second Close: %v", err)
		}
	}
}

func TestOpenEncrypted(t *testing.T) {
	disc := testdisc.SampleMovie()
	for id := range disc.Clips {
		disc.ClipData[id] = testdisc.ScrambledM2TS(1)
	}
	_, err := zenvik.Open(context.Background(), writeDisc(t, disc))
	if !errors.Is(err, zenvik.ErrEncrypted) {
		t.Errorf("err = %v, want ErrEncrypted", err)
	}
}

func TestOpenPartiallyEncrypted(t *testing.T) {
	disc := testdisc.SampleMovie()
	disc.ClipData["00030"] = testdisc.ScrambledM2TS(1)
	d := openDisc(t, writeDisc(t, disc))
	trailer := mustTitle(t, d, "00010")
	if !trailer.Encrypted || !slices.Contains(trailer.Rank.Reasons, "encrypted") || trailer.Rank.IsMain {
		t.Errorf("trailer = encrypted %v, rank %+v", trailer.Encrypted, trailer.Rank)
	}
	if m := d.Main(); m == nil || m.ID != "00800" {
		t.Errorf("Main = %+v", m)
	}
}

func TestOpenFiltersUnreadablePlaylist(t *testing.T) {
	disc := testdisc.SampleMovie()
	disc.Playlists["00500"] = testdisc.SimplePlaylist(testdisc.Segment{Clip: "00040", Length: 3 * time.Minute})
	disc.AddClipsFor()
	root := writeDisc(t, disc)
	if err := os.WriteFile(filepath.Join(root, "BDMV", "PLAYLIST", "00500.mpls"), []byte("MPLS0200garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, root)
	bad := mustTitle(t, d, "00500")
	if !bad.Rank.Filtered || len(bad.Rank.Reasons) == 0 || !strings.HasPrefix(bad.Rank.Reasons[0], "unreadable playlist") {
		t.Errorf("00500 rank = %+v", bad.Rank)
	}
	if bad.Angles != 1 {
		t.Errorf("00500 Angles = %d, want 1", bad.Angles)
	}
	if m := d.Main(); m == nil || m.ID != "00800" {
		t.Errorf("Main = %+v", m)
	}
}

func TestOpenFiltersMissingAndEmptyStreams(t *testing.T) {
	disc := testdisc.SampleMovie()
	disc.ClipData["00030"] = []byte{}
	root := writeDisc(t, disc)
	if err := os.Remove(filepath.Join(root, "BDMV", "STREAM", "00031.m2ts")); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, root)
	if tr := mustTitle(t, d, "00010"); !tr.Rank.Filtered || !slices.Contains(tr.Rank.Reasons, "empty stream file 00030.m2ts") {
		t.Errorf("00010 rank = %+v", tr.Rank)
	}
	if menu := mustTitle(t, d, "00099"); !menu.Rank.Filtered || !slices.Contains(menu.Rank.Reasons, "missing stream file 00031.m2ts") {
		t.Errorf("00099 rank = %+v", menu.Rank)
	}
	if m := d.Main(); m == nil || m.ID != "00800" {
		t.Errorf("Main = %+v", m)
	}
}

func TestOpenNoPlaylists(t *testing.T) {
	_, err := zenvik.Open(context.Background(), writeDisc(t, &testdisc.Disc{}))
	if !errors.Is(err, zenvik.ErrNoTitles) {
		t.Errorf("err = %v, want ErrNoTitles", err)
	}
}

func TestOpenUnsupported(t *testing.T) {
	dvd := filepath.Join(t.TempDir(), "DVD")
	if err := os.MkdirAll(filepath.Join(dvd, "VIDEO_TS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := zenvik.Open(context.Background(), dvd); !errors.Is(err, zenvik.ErrUnsupportedSource) {
		t.Errorf("err = %v, want ErrUnsupportedSource", err)
	}
}

func TestOpenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := zenvik.Open(ctx, writeDisc(t, testdisc.SampleMovie())); !errors.Is(err, context.Canceled) {
		t.Errorf("dir: err = %v, want context.Canceled", err)
	}
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0250, Label: "X"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "disc.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := zenvik.Open(ctx, p); !errors.Is(err, context.Canceled) {
		t.Errorf("iso: err = %v, want context.Canceled", err)
	}
	if err := os.Remove(p); err != nil { // fails on Windows if the image were left open
		t.Errorf("remove image after canceled Open: %v", err)
	}
}

func TestOpenWithoutTitleSignal(t *testing.T) {
	disc := testdisc.SampleMovie()
	disc.Titles, disc.MovieObjects = nil, nil
	d := openDisc(t, writeDisc(t, disc))
	m := d.Main()
	if m == nil || m.ID != "00800" || slices.Contains(m.Rank.Reasons, "played by title 1") {
		t.Errorf("Main = %+v", m)
	}
}

func TestTitleFieldsForMultiAngle(t *testing.T) {
	disc := testdisc.SampleMovie()
	p := disc.Playlists["00800"]
	p.Items[3].Angles = []string{"00050", "00051"}
	disc.AddClipsFor()
	d := openDisc(t, writeDisc(t, disc))
	if m := d.Main(); m.Angles != 3 {
		t.Errorf("Angles = %d, want 3 (%s)", m.Angles, fmt.Sprint(m.Rank.Reasons))
	}
}

func TestOpenEncryptedFeatureWithCleanMenu(t *testing.T) {
	disc := testdisc.SampleMovie()
	for id := range disc.Clips {
		if id != "00031" { // the 00099 menu clip stays clean
			disc.ClipData[id] = testdisc.ScrambledM2TS(1)
		}
	}
	_, err := zenvik.Open(context.Background(), writeDisc(t, disc))
	if !errors.Is(err, zenvik.ErrEncrypted) {
		t.Errorf("err = %v, want ErrEncrypted", err)
	}
}

func TestOpenOnlyShortTitles(t *testing.T) {
	d := openDisc(t, writeDisc(t, shortOnlyDisc()))
	if m := d.Main(); m != nil {
		t.Errorf("Main = %+v, want nil", m)
	}
	if len(d.Titles) != 2 {
		t.Errorf("titles = %v", ids(d.Titles))
	}
}

func shortOnlyDisc() *testdisc.Disc {
	d := &testdisc.Disc{Playlists: map[string]*bluray.Playlist{
		"00001": testdisc.SimplePlaylist(testdisc.Segment{Clip: "00001", Length: 90 * time.Second}),
		"00002": testdisc.SimplePlaylist(testdisc.Segment{Clip: "00002", Length: 30 * time.Second}),
	}, ClipData: map[string][]byte{}}
	d.AddClipsFor()
	return d
}

func TestOpenFlattened(t *testing.T) {
	normal := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	root := writeDisc(t, testdisc.SampleMovie())
	if err := testdisc.Flatten(root); err != nil {
		t.Fatal(err)
	}
	flat := openDisc(t, root)
	if flat.Kind != zenvik.FlatBDMVDir || flat.Format != zenvik.Bluray || flat.Label != "SAMPLE_MOVIE" {
		t.Errorf("Kind %v, Format %v, Label %q", flat.Kind, flat.Format, flat.Label)
	}
	checkSample(t, flat)
	if !reflect.DeepEqual(flat.Meta, normal.Meta) {
		t.Errorf("Meta = %+v, want %+v", flat.Meta, normal.Meta)
	}
	if !reflect.DeepEqual(flat.Titles, normal.Titles) {
		t.Errorf("titles differ:\n%v\nwant\n%v", ids(flat.Titles), ids(normal.Titles))
	}
}
