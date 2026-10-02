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

// ticks90k converts 90 kHz ticks to a duration, as planCut does.
func ticks90k(n uint64) time.Duration { return time.Duration(n) * time.Second / 90000 }

// Synthetic VOBUs are whole cells, each its IFO time's NTSC frames × 3003 ticks.
const (
	vobu20 = 35964 * 3003 // a 20-minute cell: 35964 frames
	vobu24 = 43157 * 3003 // a 24-minute cell: 43157 frames
	vobu1  = 30 * 3003    // a 1-second cell: 00:00:01:00, 30 frames
)

func TestCutPoints(t *testing.T) {
	half := ticks90k(vobu20) / 2
	for _, tt := range []struct {
		id         string
		group      []string
		start, end time.Duration
	}{
		// Synthetic VOBUs are whole 20-minute cells, so the half-VOBU margins are 10 minutes.
		{"03", []string{"VTS_02_1.VOB"}, 0, ticks90k(vobu20) - half},
		{"04", []string{"VTS_02_1.VOB", "VTS_02_2.VOB"}, ticks90k(vobu20) - half, ticks90k(2*vobu20) - half},
		{"05", []string{"VTS_02_1.VOB", "VTS_02_2.VOB"}, ticks90k(2*vobu20) - half, ticks90k(3*vobu20) - half}, // VTS_02_2 starts mid-VOBU
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
	// start₀ 1.001 s, m_s = min(1.001 s, 24 min)/2; end₀ = start₀ + 2 × 24 min, m_e = 24 min/2; one VOB-ID change (A→B), 10 ms ≪ margins.
	wantStart, wantEnd := ticks90k(vobu1)-ticks90k(vobu1)/2, ticks90k(vobu1+2*vobu24)-ticks90k(vobu24)/2
	if start, end, err := cutPoints(ti.dvd, paths); err != nil || start != wantStart || end != wantEnd {
		t.Errorf("stray 01: cut %v–%v (%v), want %v–%v", start, end, err, wantStart, wantEnd)
	}
}

func TestCutDriftGuard(t *testing.T) {
	if err := cutDriftGuard(3, 250*time.Millisecond, 300*time.Millisecond); err != nil {
		t.Errorf("3 changes under 250 ms margins: %v", err)
	}
	if err := cutDriftGuard(25, 250*time.Millisecond, 300*time.Millisecond); err == nil || !strings.Contains(err.Error(), "25 VOB ID changes") {
		t.Errorf("25 changes × 10 ms ≥ 250 ms: err = %v", err)
	}
	if err := cutDriftGuard(40, 0, 300*time.Millisecond); err == nil {
		t.Error("a zero margin is ignored (no start cut), but 400 ms ≥ 300 ms must fail")
	}
	if err := cutDriftGuard(1, 0, 300*time.Millisecond); err != nil {
		t.Errorf("start at 0 has no margin to guard: %v", err)
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
	_, ti, paths := sampleTitle(t, testdisc.SampleDVD(), "08")
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
	for _, r := range [][2]int{{0, 4}, {5, 9}, {15, 19}} {
		want = append(want, src[r[0]*2048:(r[1]+1)*2048]...)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("copied %d bytes, want the 15 angle-1 sectors in play order", len(got))
	}
}

func TestCopyCleanup(t *testing.T) {
	_, ti, paths := sampleTitle(t, testdisc.SampleDVD(), "08")
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
		!strings.Contains(err.Error(), "copying title 08 needs") || !strings.Contains(err.Error(), "only 1000 B free") {
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
	d, ti, _ := sampleTitle(t, testdisc.SampleDVD(), "05")
	job, _, cleanup, err := dvdJob(ctx, mk, d.src.Path, ti, filepath.Join(t.TempDir(), "ep.mkv.partial"), false, func(Phase, float64) {})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	half := ticks90k(vobu20) / 2
	if len(job.Split) != 1 || job.Split[0] != (mux.TimeRange{Start: ticks90k(2*vobu20) - half, End: ticks90k(3*vobu20) - half}) {
		t.Errorf("split = %v", job.Split)
	}
	b, err := os.ReadFile(job.ChapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "CHAPTER01=00:39:59.997\n") { // start₀ = 2 × 35964 frames = 2399.9976 s
		t.Errorf("chapter file:\n%s", b)
	}
}
