package zenvik

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/testdisc"
)

// sampleTitle opens a written disc and returns title id and its VOB paths.
func sampleTitle(t *testing.T, disc *testdisc.DVD, id string) (*Disc, *Title, []string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "DVD")
	if err := disc.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	d, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	ti, err := d.Title(id)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := resolveVOBs(root, ti.dvd.files)
	if err != nil {
		t.Fatal(err)
	}
	return d, ti, paths
}

// frame is one NTSC frame, the margins' base.
const frame = 1001 * time.Second / 30000

// ticks90k converts 90 kHz ticks to a duration, as planCut does.
func ticks90k(n uint64) time.Duration { return time.Duration(n) * time.Second / 90000 }

// Synthetic VOBUs are whole cells, each its IFO time's NTSC frames × 3003 ticks.
const (
	vobu20 = 35964 * 3003 // a 20-minute cell: 35964 frames
	vobu24 = 43157 * 3003 // a 24-minute cell: 43157 frames
	vobu1  = 30 * 3003    // a 1-second cell: 00:00:01:00, 30 frames
)

func TestCutPoints(t *testing.T) {
	// SampleDVD's VTS 2 has one VOB ID, so each applicable margin is one
	// frame: well inside half of its whole-cell VOBUs.
	for _, tt := range []struct {
		id         string
		group      []string
		start, end time.Duration
	}{
		{"03", []string{"VTS_02_1.VOB"}, 0, ticks90k(vobu20) - frame},
		{"04", []string{"VTS_02_1.VOB", "VTS_02_2.VOB"}, ticks90k(vobu20) - frame, ticks90k(2*vobu20) - frame},
		{"05", []string{"VTS_02_1.VOB", "VTS_02_2.VOB"}, ticks90k(2*vobu20) - frame, ticks90k(3*vobu20) - frame}, // VTS_02_2 starts mid-VOBU
	} {
		_, ti, paths := sampleTitle(t, testdisc.SampleDVD(), tt.id)
		var names []string
		for _, i := range cutGroup(ti.dvd) {
			names = append(names, ti.dvd.files[i].name)
		}
		if strings.Join(names, ",") != strings.Join(tt.group, ",") {
			t.Errorf("%s: group %v, want %v", tt.id, names, tt.group)
		}
		start, end, err := cutPoints(ti.dvd, paths)
		if err != nil || start != tt.start || end != tt.end {
			t.Errorf("%s: cut %v–%v (%v), want %v–%v", tt.id, start, end, err, tt.start, tt.end)
		}
	}
	_, ti, paths := sampleTitle(t, testdisc.StrayCellDVD(), "01")
	// start₀ 1.001 s; end₀ = start₀ + 2 × 24 min. One VOB-ID change (A→B, into
	// R0's VOBU) counts for both margins: m_s = min(10 ms + frame, 1.001 s/2),
	// m_e = min(10 ms + frame, 24 min/2).
	m := 10*time.Millisecond + frame
	wantStart, wantEnd := ticks90k(vobu1)-m, ticks90k(vobu1+2*vobu24)-m
	if start, end, err := cutPoints(ti.dvd, paths); err != nil || start != wantStart || end != wantEnd {
		t.Errorf("stray 01: cut %v–%v (%v), want %v–%v", start, end, err, wantStart, wantEnd)
	}
}

func TestCutDriftGuard(t *testing.T) {
	if err := cutDriftGuard(3, 250*time.Millisecond); err != nil {
		t.Errorf("3 × 10 ms + a frame fits in 250 ms: %v", err)
	}
	if err := cutDriftGuard(21, 250*time.Millisecond); err != nil {
		t.Errorf("21 × 10 ms + a frame = 243 ms fits in 250 ms: %v", err)
	}
	if err := cutDriftGuard(22, 250*time.Millisecond); err == nil || !strings.Contains(err.Error(), "22 VOB ID changes") {
		t.Errorf("22 × 10 ms + a frame = 253 ms > 250 ms: err = %v", err)
	}
	if err := cutDriftGuard(0, frame); err != nil {
		t.Errorf("a budget equal to half the VOBU fits: %v", err)
	}
	if err := cutDriftGuard(0, frame-1); err == nil {
		t.Error("a VOBU shorter than two frames can't hold the one-frame margin")
	}
}

func TestCutPointsErrors(t *testing.T) {
	_, ti, paths := sampleTitle(t, testdisc.SampleDVD(), "04")
	noMap := *ti.dvd
	noMap.vobus = nil
	if _, _, err := cutPoints(&noMap, paths); err == nil || !strings.Contains(err.Error(), "VOBU address map") {
		t.Errorf("no map: err = %v", err)
	}
	noStart := *ti.dvd
	noStart.vobus = []uint32{0, 40}
	if _, _, err := cutPoints(&noStart, paths); err == nil || !strings.Contains(err.Error(), "no VOBU starts at") {
		t.Errorf("no VOBU at the run start: err = %v", err)
	}
	b, _ := os.ReadFile(paths[0])
	copy(b[20*2048:], make([]byte, 2048)) // wipe the NAV pack at sector 20
	if err := os.WriteFile(paths[0], b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cutPoints(ti.dvd, paths); err == nil || !strings.Contains(err.Error(), "no NAV pack") {
		t.Errorf("missing NAV: err = %v", err)
	}
}

func TestCopyTitle(t *testing.T) {
	_, ti, paths := sampleTitle(t, testdisc.StrayCellDVD(), "02")
	out := filepath.Join(t.TempDir(), "out", "Angle.mkv.partial")
	var last float64
	tmp, err := copyTitle(context.Background(), ti, paths, out, func(p Phase, f float64) {
		if p == PhaseCopying {
			last = f
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if tmp != filepath.Join(filepath.Dir(out), ".Angle.mkv.title.vob") || last != 1 {
		t.Errorf("tmp %q, last progress %v", tmp, last)
	}
	got, _ := os.ReadFile(tmp)
	src, _ := os.ReadFile(paths[0])
	var want []byte
	for _, r := range [][2]int{{26, 49}, {0, 1}, {2, 25}} {
		want = append(want, src[r[0]*2048:(r[1]+1)*2048]...)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("copied %d bytes, want the 50 sectors in play order", len(got))
	}
}

func TestCopyCleanup(t *testing.T) {
	_, ti, paths := sampleTitle(t, testdisc.StrayCellDVD(), "02")
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := copyTitle(ctx, ti, paths, filepath.Join(dir, "x.mkv.partial"), func(Phase, float64) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled copy: err = %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("canceled copy left %v", ents)
	}
	old := freeSpace
	t.Cleanup(func() { freeSpace = old })
	freeSpace = func(string) (uint64, bool) { return 1000, true }
	if _, err := copyTitle(context.Background(), ti, paths, filepath.Join(dir, "x.mkv.partial"), func(Phase, float64) {}); err == nil ||
		!strings.Contains(err.Error(), "copying title 02 needs") || !strings.Contains(err.Error(), "only 1000 B free") {
		t.Errorf("no space: err = %v", err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Errorf("no-space copy left %v", ents)
	}
}

// TestCutChaptersOnGroupTimeline checks that a cut title's chapters are
// shifted by start₀ (the group-timeline time of the run's first VOBU), not
// by the margin-adjusted --split start: mkvmerge shifts them back by the
// keyframe it actually cuts at, which is the one at start₀.
func TestCutChaptersOnGroupTimeline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mkvmerge stub is a shell script")
	}
	stub := filepath.Join(t.TempDir(), "mkvmerge")
	script := `#!/bin/sh
case "$1" in
--version) echo "mkvmerge v90.0 ('Stub') 64-bit" ;;
-J) printf '%s\n' '{"tracks":[{"id":0,"type":"video","codec":"MPEG-1/2","properties":{"stream_id":224}}]}' ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mk, err := mux.Find(ctx, stub)
	if err != nil {
		t.Fatal(err)
	}
	d, ti, paths := sampleTitle(t, testdisc.SampleDVD(), "05")
	job, _, cleanup, err := dvdJob(ctx, mk, d.src.Path, ti, filepath.Join(t.TempDir(), "ep.mkv.partial"), false, func(Phase, float64) {})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(job.Split) != 1 || job.Split[0] != (mux.TimeRange{Start: ticks90k(2*vobu20) - frame, End: ticks90k(3*vobu20) - frame}) {
		t.Errorf("split = %v", job.Split)
	}
	b, err := os.ReadFile(job.ChapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "CHAPTER01=00:39:59.997\n") { // start₀ = 2 × 35964 frames = 2399.9976 s
		t.Errorf("chapter file:\n%s", b)
	}

	// An open GOP: the run's first VOBU (sector 40, VTS_02_2 sector 10) shows
	// two B-frames before its I-frame, whose PTS is vobu_s_ptm + 6006. The
	// chapters move by that gap; the --split times don't.
	vob, err := os.ReadFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	copy(vob[11*2048:], videoPESPack(2*vobu20+6006))
	if err := os.WriteFile(paths[1], vob, 0o644); err != nil {
		t.Fatal(err)
	}
	job, _, cleanup2, err := dvdJob(ctx, mk, d.src.Path, ti, filepath.Join(t.TempDir(), "ep.mkv.partial"), false, func(Phase, float64) {})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if len(job.Split) != 1 || job.Split[0] != (mux.TimeRange{Start: ticks90k(2*vobu20) - frame, End: ticks90k(3*vobu20) - frame}) {
		t.Errorf("open GOP: split = %v", job.Split)
	}
	if b, err = os.ReadFile(job.ChapterFile); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "CHAPTER01=00:40:00.064\n") { // start₀ + 66.733 ms
		t.Errorf("open GOP: chapter file:\n%s", b)
	}
}

// videoPESPack is a pack holding one video PES packet (stream 0xE0) with PTS pts.
func videoPESPack(pts uint32) []byte {
	p := testdisc.VOBPacks(1, false)
	v := uint64(pts)
	copy(p[14:], []byte{0, 0, 1, 0xE0, 0x07, 0xEC, 0x81, 0x80, 0x05,
		0x21 | byte(v>>29)&0x0E, byte(v >> 22), byte(v>>14)&0xFE | 1, byte(v >> 7), byte(v<<1)&0xFE | 1})
	return p
}

func TestVOBULeadIn(t *testing.T) {
	nav := dvd.NAV{StartPTM: 25257, EndPTM: 61293, VOBID: 2, CellID: 1}
	reader := func(packs ...[]byte) func(int64) ([]byte, error) {
		b := bytes.Join(packs, nil)
		return func(sec int64) ([]byte, error) {
			if (sec+1)*2048 > int64(len(b)) {
				return nil, errors.New("past the end")
			}
			return b[sec*2048 : (sec+1)*2048], nil
		}
	}
	// The real-disc case: two leading B-frames, so the I-frame's PTS is vobu_s_ptm + 6006.
	gap, err := vobuLeadIn(reader(testdisc.NAVPack(nav), testdisc.VOBPacks(1, false), videoPESPack(25257+6006)), 0, 3, nav)
	if err != nil || gap != 6006 || ticks90k(gap) != 66733333*time.Nanosecond {
		t.Errorf("open GOP: gap %d ticks (%v), %v; want 6006 (66.7 ms)", gap, ticks90k(gap), err)
	}
	// No video PES with a PTS before the next VOBU (sector 2, which has one): no gap.
	if gap, err := vobuLeadIn(reader(testdisc.NAVPack(nav), testdisc.VOBPacks(1, false), videoPESPack(25257+6006)), 0, 2, nav); err != nil || gap != 0 {
		t.Errorf("no PTS: gap %d, %v; want 0", gap, err)
	}
	if gap, err := vobuLeadIn(reader(testdisc.NAVPack(nav), videoPESPack(25257-3003)), 0, 2, nav); err != nil || gap != 0 {
		t.Errorf("PTS before vobu_s_ptm: gap %d, %v; want 0", gap, err)
	}
	if _, err := vobuLeadIn(reader(testdisc.NAVPack(nav), videoPESPack(61293+1)), 0, 2, nav); err == nil || !strings.Contains(err.Error(), "longer than its VOBU") {
		t.Errorf("gap past the VOBU: err = %v", err)
	}
}
