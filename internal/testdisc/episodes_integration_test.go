//go:build integration

package testdisc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/vobsub"
)

type navAt struct {
	sector     uint32
	vob, cell  int
	sptm, eptm uint32
}

// scanNAV finds every NAV pack in a VOB and reads its VOB/cell IDs (DSI)
// and VOBU start/end PTM (PCI).
func scanNAV(t *testing.T, path string) []navAt {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []navAt
	for s := 0; s+2048 <= len(b); s += 2048 {
		p := b[s : s+2048]
		if !isNavPack(p) || p[0x2C] != 0x00 || p[0x406] != 0x01 {
			continue
		}
		dsi := p[0x407:]
		out = append(out, navAt{sector: uint32(s / 2048), vob: int(binary.BigEndian.Uint16(dsi[24:])), cell: int(dsi[27]),
			sptm: binary.BigEndian.Uint32(p[0x2D+12:]), eptm: binary.BigEndian.Uint32(p[0x2D+16:])})
	}
	return out
}

// groupTime is the summed VOBU duration of every NAV before sector s.
func groupTime(navs []navAt, s uint32) time.Duration {
	var ticks uint64
	for _, n := range navs {
		if n.sector < s {
			ticks += uint64(n.eptm - n.sptm)
		}
	}
	return time.Duration(ticks) * time.Second / 90000
}

// cutPlan is the ruled cut for a run of sectors r0..r1 on the group timeline.
type cutPlan struct {
	start0, end0 time.Duration // summed VOBU durations before r0, and through r1
	ms, me       time.Duration // start and end margins; ms is 0 when start0 is 0
	start, end   time.Duration // start0-ms and end0-me: the times passed to mkvmerge
	changes      int           // VOB ID changes among the NAV packs from the group start up to r1
}

func vobuDur(n navAt) time.Duration {
	return time.Duration(n.eptm-n.sptm) * time.Second / 90000
}

// planCut applies the M6 ruling: mkvmerge cuts at the first keyframe at or
// after each time, so each end is moved back by half a VOBU to land before
// the run's own boundary keyframe despite the drift of summed VOBU time.
func planCut(navs []navAt, r0, r1 uint32) cutPlan {
	var before, first, last *navAt
	p := cutPlan{start0: groupTime(navs, r0), end0: groupTime(navs, r1+1)}
	for i := range navs {
		n := &navs[i]
		switch {
		case n.sector < r0:
			before = n
		case n.sector <= r1:
			if first == nil {
				first = n
			}
			last = n
		}
		if n.sector <= r1 && i > 0 && n.vob != navs[i-1].vob {
			p.changes++
		}
	}
	if before != nil && p.start0 > 0 {
		p.ms = min(vobuDur(*before), vobuDur(*first)) / 2
	}
	p.me = vobuDur(*last) / 2
	p.start, p.end = p.start0-p.ms, p.end0-p.me
	return p
}

// driftTripped reports whether the accumulated drift budget (10 ms per VOB
// ID change) reaches the smallest applicable margin, in which case the cut
// can't be trusted.
func (p cutPlan) driftTripped() bool {
	m := p.me
	if p.start0 > 0 {
		m = min(m, p.ms)
	}
	return time.Duration(p.changes)*10*time.Millisecond >= m
}

func mkvTime(d time.Duration) string {
	ns := d.Nanoseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%09d", ns/3_600_000_000_000, ns/60_000_000_000%60, ns/1_000_000_000%60, ns%1_000_000_000)
}

func runMkvmerge(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("mkvmerge", args...).CombinedOutput()
	var ee *exec.ExitError
	if err != nil && (!errors.As(err, &ee) || ee.ExitCode() != 1) {
		t.Fatalf("mkvmerge %q: %v\n%s", args, err, out)
	}
}

func duration(t *testing.T, path string) time.Duration {
	t.Helper()
	out, _ := exec.Command("mkvmerge", "-J", path).Output()
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

func chapterStarts(t *testing.T, path string) []time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "ch.txt")
	if out, err := exec.Command("mkvextract", path, "chapters", "--simple", txt).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(txt)
	var out []time.Duration
	for _, l := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(l, "=")
		if !ok || strings.HasSuffix(k, "NAME") {
			continue
		}
		var h, m, s, ms int
		if _, err := fmt.Sscanf(v, "%d:%d:%d.%d", &h, &m, &s, &ms); err == nil {
			out = append(out, time.Duration(h)*time.Hour+time.Duration(m)*time.Minute+time.Duration(s)*time.Second+time.Duration(ms)*time.Millisecond)
		}
	}
	return out
}

func firstTimestamp(t *testing.T, path string, tid int) time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "ts.txt")
	if out, err := exec.Command("mkvextract", path, "timestamps_v2", fmt.Sprintf("%d:%s", tid, txt)).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract timestamps: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(txt)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 2 {
		t.Fatalf("no timestamps:\n%s", b)
	}
	ms, err := strconv.ParseFloat(strings.TrimSpace(lines[1]), 64)
	if err != nil {
		t.Fatal(err)
	}
	return time.Duration(ms * float64(time.Millisecond))
}

// trackTimestamps returns the sorted timestamps of track tid in path.
func trackTimestamps(t *testing.T, path string, tid int) []time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "ts.txt")
	if out, err := exec.Command("mkvextract", path, "timestamps_v2", fmt.Sprintf("%d:%s", tid, txt)).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract timestamps: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(txt)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 2 {
		t.Fatalf("no timestamps:\n%s", b)
	}
	var out []time.Duration
	for _, l := range lines[1:] {
		ms, err := strconv.ParseFloat(strings.TrimSpace(l), 64)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, time.Duration(ms*float64(time.Millisecond)))
	}
	slices.Sort(out)
	return out
}

// ntscFrame is one NTSC frame, 1001/30000 s.
const ntscFrame = 1001 * time.Second / 30000

// videoDuration is the span from the first to the last frame of video
// track 0, plus one frame. mkvextract's timestamps_v2 output ends with one
// extra line, the end time of the last frame (181 lines for 180 frames on
// the episode cuts), so that line is dropped before measuring.
func videoDuration(t *testing.T, path string) time.Duration {
	t.Helper()
	ts := trackTimestamps(t, path, 0)
	ts = ts[:len(ts)-1]
	return ts[len(ts)-1] - ts[0] + ntscFrame
}

// audioOffset is how far audio track 1 starts after video track 0.
func audioOffset(t *testing.T, path string) time.Duration {
	t.Helper()
	return trackTimestamps(t, path, 1)[0] - trackTimestamps(t, path, 0)[0]
}

const frame = 34 * time.Millisecond

func near(a, b, tol time.Duration) bool { return math.Abs(float64(a-b)) <= float64(tol) }

func TestEpisodeCutting(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "EPISODES")
	if err := AuthorEpisodesDVD(ctx, dir); err != nil {
		t.Fatal(err)
	}
	vob := filepath.Join(dir, "VIDEO_TS", "VTS_01_1.VOB")
	if _, err := os.Stat(filepath.Join(dir, "VIDEO_TS", "VTS_01_2.VOB")); err == nil {
		t.Fatal("expected all titles inside VTS_01_1.VOB")
	}
	b, err := os.ReadFile(filepath.Join(dir, "VIDEO_TS", "VTS_01_0.IFO"))
	if err != nil {
		t.Fatal(err)
	}
	vts, err := dvd.ParseVTS(b)
	if err != nil {
		t.Fatal(err)
	}
	navs := scanNAV(t, vob)
	t.Logf("%d NAV packs; VOB IDs %v", len(navs), vobIDs(navs))

	// (1)+(2): cut each episode out of the group; VOB IDs differ, so PTS resets in between.
	for i, color := range EpisodeColors {
		pgc := vts.PGCs[vts.Titles[i+1][0].PGC-1]
		r0, r1 := pgc.Cells[0].FirstSector, pgc.Cells[len(pgc.Cells)-1].LastSector
		cp := planCut(navs, r0, r1)
		if cp.driftTripped() {
			t.Errorf("%s: drift guard tripped: %d VOB ID changes, m_s %v, m_e %v", color, cp.changes, cp.ms, cp.me)
		}
		out := filepath.Join(t.TempDir(), color+".mkv")
		runMkvmerge(t, "-o", out, "--split", "parts:"+mkvTime(cp.start)+"-"+mkvTime(cp.end), "(", vob, ")")
		got, want := videoDuration(t, out), pgc.Time.Duration()
		var colours [2][3]byte
		for j, fromEnd := range []bool{false, true} {
			rgb, err := FrameColor(ctx, out, fromEnd)
			if err != nil {
				t.Fatal(err)
			}
			colours[j] = rgb
			if Dominant(rgb) != color {
				t.Errorf("%s (fromEnd=%v): frame colour %v", color, fromEnd, rgb)
			}
		}
		t.Logf("%s: sectors %d–%d, start₀ %v, end₀ %v, m_s %v, m_e %v, cut %v–%v, VOB ID changes %d, video duration %v (IFO %v), first %v last %v; observed: container %v, audio starts %v after video",
			color, r0, r1, cp.start0, cp.end0, cp.ms, cp.me, cp.start, cp.end, cp.changes, got, want, colours[0], colours[1], duration(t, out), audioOffset(t, out))
		if !near(got, want, frame) {
			t.Errorf("%s: video duration %v, IFO says %v", color, got, want)
		}
	}

	// (3): chapters and a VobSub .idx on the group timeline are cut with the video.
	pgc := vts.PGCs[vts.Titles[2][0].PGC-1] // green
	r0, r1 := pgc.Cells[0].FirstSector, pgc.Cells[len(pgc.Cells)-1].LastSector
	cp := planCut(navs, r0, r1)
	if cp.driftTripped() {
		t.Errorf("green: drift guard tripped")
	}
	ch := filepath.Join(t.TempDir(), "ch.txt")
	var chb strings.Builder
	cellStart := cp.start0
	for i, c := range pgc.Cells {
		fmt.Fprintf(&chb, "CHAPTER%02d=%s\nCHAPTER%02dNAME=Chapter %02d\n", i+1, mkvTime(cellStart)[:12], i+1, i+1)
		cellStart += c.Time.Duration()
	}
	if err := os.WriteFile(ch, []byte(chb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var cells []vobsub.Cell
	for _, p := range vts.PGCs {
		for _, c := range p.Cells {
			cells = append(cells, vobsub.Cell{VOBID: c.VOBID, CellID: c.CellID, Start: groupTime(navs, c.FirstSector)})
		}
	}
	res, err := vobsub.Extract(ctx, vobsub.Params{VOBs: []string{vob}, Cells: cells, Streams: []vobsub.Stream{{ID: 0, Language: "en"}},
		Palette: pgc.Palette, Width: 720, Height: 480, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "green-subs.mkv")
	runMkvmerge(t, "-o", out, "--chapters", ch, "--split", "parts:"+mkvTime(cp.start)+"-"+mkvTime(cp.end), "--no-chapters", "(", vob, ")", res.IDX)
	t.Logf("green subs: start₀ %v, end₀ %v, m_s %v, m_e %v, cut %v–%v", cp.start0, cp.end0, cp.ms, cp.me, cp.start, cp.end)
	starts := chapterStarts(t, out)
	t.Logf("chapters after cut: %v", starts)
	if len(starts) != 2 || !near(starts[0], 0, frame) || !near(starts[1], pgc.Cells[0].Time.Duration(), frame) {
		t.Errorf("chapters = %v", starts)
	}
	sub := firstTimestamp(t, out, 2)
	t.Logf("first subtitle after cut: %v", sub)
	if !near(sub, time.Second, 100*time.Millisecond) {
		t.Errorf("first subtitle at %v, want 1.0 s", sub)
	}

	// (4): mkvmerge writes a single part under the given name.
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output not at the given name: %v", err)
	}

	// (5): an out-of-order copy (blue then red) is one continuous timeline.
	vb, err := os.ReadFile(vob)
	if err != nil {
		t.Fatal(err)
	}
	var tmp bytes.Buffer
	for _, ep := range []int{3, 1} {
		p := vts.PGCs[vts.Titles[ep][0].PGC-1]
		a, z := p.Cells[0].FirstSector, p.Cells[len(p.Cells)-1].LastSector
		tmp.Write(vb[int(a)*2048 : int(z+1)*2048])
	}
	tv := filepath.Join(t.TempDir(), "copy.title.vob")
	if err := os.WriteFile(tv, tmp.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	out = filepath.Join(t.TempDir(), "copy.mkv")
	runMkvmerge(t, "-o", out, "(", tv, ")")
	want := vts.PGCs[vts.Titles[3][0].PGC-1].Time.Duration() + vts.PGCs[vts.Titles[1][0].PGC-1].Time.Duration()
	got := videoDuration(t, out)
	t.Logf("copy: video duration %v, want %v; observed: container %v, audio starts %v after video", got, want, duration(t, out), audioOffset(t, out))
	if !near(got, want, frame) {
		t.Errorf("copy video duration %v, want %v", got, want)
	}
	first, _ := FrameColor(ctx, out, false)
	last, _ := FrameColor(ctx, out, true)
	if Dominant(first) != "blue" || Dominant(last) != "red" {
		t.Errorf("copy colours: first %v, last %v", first, last)
	}
}

func vobIDs(navs []navAt) []int {
	seen := map[int]bool{}
	var out []int
	for _, n := range navs {
		if !seen[n.vob] {
			seen[n.vob] = true
			out = append(out, n.vob)
		}
	}
	return out
}
