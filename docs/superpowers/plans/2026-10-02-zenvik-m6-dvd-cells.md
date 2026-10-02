# zenvik M6 (DVD cell layouts) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rip every single-PGC DVD title regardless of cell layout. Whole files work as today. One ascending run, possibly mid-file, is cut from the whole-VOB group with mkvmerge `--split parts`. Any other layout is muxed from an exact temporary copy. Short stray cells of 1 s or less at a title's edges are dropped with a warning.

**Architecture:**
- The scan classifies each DVD title's angle-1 cells and records `Title.RipMethod` (`files` | `cut` | `copy`) and `Title.SkippedCells`, plus private sector ranges, files and the title set's VOBU map.
- The cut path sums VOBU durations from NAV packs (`vobu_e_ptm − vobu_s_ptm`) to place `--split parts:<start>-<end>` on the `( … )` group's timeline. Chapters and the VobSub `.idx` are written on that same timeline.
- The copy path checks free space, copies the kept cells in play order into `.<output>.title.vob` and muxes that file.

**Tech Stack:** Go 1.27 (no cgo), mkvmerge ≥ 80; integration tests also need mkvextract, ffmpeg and dvdauthor/spumux.

**Spec:** `docs/superpowers/specs/2026-10-02-zenvik-m6-dvd-cells-design.md` (extends the v1 and M5 specs).

## Global Constraints

- Module `github.com/chad3814/zenvik`, Go 1.27, **no cgo**. `CGO_ENABLED=0 go build ./...` must pass.
- The only third-party dependencies are `spf13/cobra` and `pelletier/go-toml/v2`. The public packages `bluray`, `udf` and `dvd` use only the standard library and do no I/O beyond the bytes they are given.
- Errors are sentinel errors wrapped with `%w`. Library messages start with `zenvik:`, `dvd` messages with `dvd:`, and `vobsub` messages with `vobsub:`.
- Never commit copyrighted disc data. The Swiss Family Robinson ISO in the project root is for a local acceptance run only. Never copy, commit or upload it.
- Tests must not touch real user config or state. Set `XDG_CONFIG_HOME` and `XDG_STATE_HOME` to temp dirs; `cmd/zenvik` has a TestMain for this. Detach anything you mount.
- `Title.RipMethod` is one of `"files"`, `"cut"` or `"copy"` for DVD titles, and `""` for Blu-ray.
- **Stray-cell rule:**
  - A stray is a cell at the start or end of the title that breaks sector contiguity with its neighbour.
  - It is dropped if its IFO time is at most 1.0 s.
  - Dropping repeats from each end; middle cells are never dropped.
- **Skip warning, verbatim:** `title <id>: skipped cell <n> (<seconds> s at sectors <a>–<b>, out of order)`, with `<seconds>` formatted `%.1f`. `<n>` is the cell's 1-based number in its PGC. The dash is an en dash (–).
- **mkvmerge input:** title VOBs always go to mkvmerge as a `( … )` group, never with `+` (M5 ruling).
- **Cut path:** mkvmerge gets `--split parts:HH:MM:SS.nnnnnnnnn-HH:MM:SS.nnnnnnnnn`. For a single range, mkvmerge writes the output name exactly as given (verified 2026-10-02 with mkvmerge v102), so no rename is needed.
- **Copy path:**
  - The temp file is `.<base name of the final output>.title.vob`, in the output's directory.
  - It needs `Size + 64 MiB` of free space.
  - The error is `zenvik: copying title <id> needs <size> in <dir>, only <free> free`, with sizes formatted like `5.9 GiB`.
- **New progress phase:** `PhaseCopying` = 6, whose `String()` is `"copying"`.
- **Verification for every task:**
  - `gofmt -l .` (empty)
  - `go vet ./...`, `GOOS=windows go vet ./...`, `GOOS=linux go vet ./...`
  - `golangci-lint run`, plus `golangci-lint run --build-tags integration`
  - `go test -race ./...`
  - `CGO_ENABLED=0 go build ./...`
- Commit after each task with a descriptive message. If signing fails, use `git -c commit.gpgsign=false commit …` and say so. Never push.

## Review Focus

1. **A cut whose start isn't a VOBU start, or a VOBU map that disagrees with the NAV packs.** The rip must fail with a clear `cannot compute cut points` error, never cut at a wrong time. Pinned by Task 4 `TestCutPointsErrors`.
2. **A copy-path rip that fails or is cancelled midway.** The temp `.title.vob` must be gone afterwards. Pinned by Task 4 `TestCopyCleanup`.
3. **A title whose every cell is a short stray**, for example two 0.5 s cells out of order. At least one cell must be kept, so the title never becomes empty. Pinned by Task 3 `TestClassifyCells` ("never empties").
4. **Chapters whose entry cell is dropped.** The chapter moves to the next kept cell, or is removed, and numbers stay 1…n with no duplicate starts. Pinned by Task 3 `TestRemapChapters`.
5. **A dry run of a copy-path title.** It must write nothing and do no free-space check, and its command shows `<title.vob>`. Pinned by Task 4 `TestDVDCopyDryRun`.

## Waves (dependencies)

- **Wave A**, in parallel: T1 (verify mkvmerge cutting; authored episodes fixture) and T2 (`dvd` VOBU map, NAV parser and encoders).
- **Wave B:** T3 (classifier, chapter remap, SampleDVD NAV packs and the stray-cell fixture). Needs T2.
- **Wave C**, in parallel: T4 (cut and copy rip paths) and T5 (CLI and JSON). Both need T3, and T4 also needs T1's notes.
- **Wave D:** T6 (integration tests, docs, spec sync, Swiss Family acceptance). Needs T1, T4 and T5.

---

### Task 1: Verify mkvmerge cutting and copying on authored discs (gate)

**Files:**
- Create: `internal/testdisc/episodes.go`
- Create: `internal/testdisc/episodes_integration_test.go` (`//go:build integration`)
- Create: `docs/superpowers/notes/2026-10-02-m6-mkvmerge-cut.md`

**Interfaces:**
- Consumes: the existing `runTool`, `isNavPack` and `AuthorDVD` style from `internal/testdisc/dvdauthor.go`, `dvd.ParseVTS`, and `vobsub.Extract` (current API: `Params.VOBs []string`).
- Produces:
  - `func AuthorEpisodesDVD(ctx context.Context, dir string) error`. It writes `dir/VIDEO_TS` with one title set and four titles, each its own `<vob>` (so its own VOB ID and PTS start), all inside `VTS_01_1.VOB`:
    - title 1: 0.5 s black, with silent AC-3 (shorter than 1 s, so it counts as a droppable stray: 1 s of NTSC would be 30 frames, 1.001 s)
    - titles 2–4: 6 s solid `red`, `green` and `blue` with 440, 660 and 880 Hz AC-3; chapters at 0 and 3 s; an English subtitle from 1 s to 3 s
  - `var EpisodeColors = []string{"red", "green", "blue"}`
  - `func FrameColor(ctx context.Context, path string, fromEnd bool) ([3]byte, error)`, the average RGB of the first frame, or of the frame 0.5 s before the end.
  - `func Dominant(rgb [3]byte) string`, which returns `"red"`, `"green"`, `"blue"`, `"black"` or `""`.
  - The notes file: the observed results that Tasks 4 and 6 rely on.

This task is a gate. If any of checks (1)–(5) below fails, still commit the fixture, the test and the notes with the real observations, and report **BLOCKED** with the values.

- [ ] **Step 1: Write the integration test**

`internal/testdisc/episodes_integration_test.go`:

```go
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

func mkvTime(d time.Duration) string {
	ns := d.Nanoseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%09d", ns/3_600_000_000_000, ns/60_000_000_000%60, ns/1_000_000_000%60, ns%1_000_000_000)
}

func runMkvmerge(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("mkvmerge", args...).CombinedOutput()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
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
		start, end := groupTime(navs, r0), groupTime(navs, r1+1)
		out := filepath.Join(t.TempDir(), color+".mkv")
		runMkvmerge(t, "-o", out, "--split", "parts:"+mkvTime(start)+"-"+mkvTime(end), "(", vob, ")")
		got, want := duration(t, out), pgc.Time.Duration()
		t.Logf("%s: sectors %d–%d, cut %v–%v, duration %v (IFO %v)", color, r0, r1, start, end, got, want)
		if !near(got, want, frame) {
			t.Errorf("%s: duration %v, IFO says %v", color, got, want)
		}
		for _, fromEnd := range []bool{false, true} {
			rgb, err := FrameColor(ctx, out, fromEnd)
			if err != nil {
				t.Fatal(err)
			}
			if Dominant(rgb) != color {
				t.Errorf("%s (fromEnd=%v): frame colour %v", color, fromEnd, rgb)
			}
		}
	}

	// (3): chapters and a VobSub .idx on the group timeline are cut with the video.
	pgc := vts.PGCs[vts.Titles[2][0].PGC-1] // green
	r0, r1 := pgc.Cells[0].FirstSector, pgc.Cells[len(pgc.Cells)-1].LastSector
	start, end := groupTime(navs, r0), groupTime(navs, r1+1)
	ch := filepath.Join(t.TempDir(), "ch.txt")
	var chb strings.Builder
	cellStart := start
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
	runMkvmerge(t, "-o", out, "--chapters", ch, "--split", "parts:"+mkvTime(start)+"-"+mkvTime(end), "--no-chapters", "(", vob, ")", res.IDX)
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
	got := duration(t, out)
	t.Logf("copy: duration %v, want %v", got, want)
	if !near(got, want, 2*frame) {
		t.Errorf("copy duration %v, want %v", got, want)
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
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test -tags integration -run TestEpisodeCutting -v ./internal/testdisc/`
Expected: FAIL to compile with `undefined: AuthorEpisodesDVD`.

- [ ] **Step 3: Implement the fixture and the colour helpers**

`internal/testdisc/episodes.go`:

```go
package testdisc

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EpisodeColors are the solid colours of AuthorEpisodesDVD's titles 2–4.
var EpisodeColors = []string{"red", "green", "blue"}

var episodeTones = []int{440, 660, 880}

// AuthorEpisodesDVD builds a real DVD-Video folder at dir (dir/VIDEO_TS)
// with one title set and four titles, each its own <vob> (so its own VOB
// ID and its own PTS start), all inside VTS_01_1.VOB: title 1 is 0.5 s of
// black with silent AC-3; titles 2–4 are 6 s of solid red, green and blue
// with 440, 660 and 880 Hz AC-3, chapters at 0 and 3 s, and an English
// subtitle from 1 s to 3 s. Each episode therefore starts and ends
// mid-file.
func AuthorEpisodesDVD(ctx context.Context, dir string) error {
	work, err := os.MkdirTemp("", "zenvik-episodes-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	enc := []string{"-target", "ntsc-dvd", "-aspect", "4:3", "-c:a", "ac3", "-b:a", "192k", "-ac", "2"}
	if err := runTool(ctx, work, nil, nil, "ffmpeg", append([]string{"-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=720x480:r=30000/1001:d=0.5",
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "0.5"}, append(enc, "black.mpg")...)...); err != nil {
		return err
	}
	if err := runTool(ctx, work, nil, nil, "ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black@0.0:s=720x480,format=rgba,drawbox=x=200:y=400:w=320:h=40:color=white@1.0:t=fill",
		"-frames:v", "1", "sub.png"); err != nil {
		return err
	}
	spu := `<subpictures format="NTSC"><stream><spu start="00:00:01.00" end="00:00:03.00" image="sub.png" force="no"/></stream></subpictures>`
	if err := os.WriteFile(filepath.Join(work, "sub.xml"), []byte(spu), 0o644); err != nil {
		return err
	}
	for i, color := range EpisodeColors {
		raw := fmt.Sprintf("ep%d.mpg", i+1)
		if err := runTool(ctx, work, nil, nil, "ffmpeg", append([]string{"-loglevel", "error", "-y",
			"-f", "lavfi", "-i", "color=c=" + color + ":s=720x480:r=30000/1001:d=6",
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:sample_rate=48000:duration=6", episodeTones[i])}, append(enc, raw)...)...); err != nil {
			return err
		}
		in, err := os.Open(filepath.Join(work, raw))
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(work, fmt.Sprintf("ep%d_sub.mpg", i+1)))
		if err != nil {
			in.Close()
			return err
		}
		err = runTool(ctx, work, in, out, "spumux", "-s", "0", "sub.xml")
		in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var dest bytes.Buffer
	if err := xml.EscapeText(&dest, []byte(abs)); err != nil {
		return err
	}
	x := fmt.Sprintf(`<dvdauthor dest="%s">
  <vmgm />
  <titleset>
    <titles>
      <video format="ntsc" aspect="4:3" />
      <audio lang="en" />
      <subpicture lang="en" />
      <pgc><vob file="black.mpg" /></pgc>
      <pgc><vob file="ep1_sub.mpg" chapters="0,3" /></pgc>
      <pgc><vob file="ep2_sub.mpg" chapters="0,3" /></pgc>
      <pgc><vob file="ep3_sub.mpg" chapters="0,3" /></pgc>
    </titles>
  </titleset>
</dvdauthor>
`, dest.String())
	if err := os.WriteFile(filepath.Join(work, "dvd.xml"), []byte(x), 0o644); err != nil {
		return err
	}
	return runTool(ctx, work, nil, nil, "dvdauthor", "-x", "dvd.xml")
}

// FrameColor returns the average RGB of a video's first frame, or of the
// frame 0.5 s before its end when fromEnd is set, using ffmpeg.
func FrameColor(ctx context.Context, path string, fromEnd bool) ([3]byte, error) {
	args := []string{"-v", "error"}
	if fromEnd {
		args = append(args, "-sseof", "-0.5")
	}
	args = append(args, "-i", path, "-frames:v", "1", "-vf", "scale=1:1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-")
	out, err := exec.CommandContext(ctx, "ffmpeg", args...).Output()
	if err != nil {
		return [3]byte{}, fmt.Errorf("testdisc: ffmpeg frame colour of %s: %w", path, err)
	}
	if len(out) < 3 {
		return [3]byte{}, fmt.Errorf("testdisc: no frame from %s", path)
	}
	return [3]byte{out[0], out[1], out[2]}, nil
}

// Dominant names the clearly dominant primary of rgb, "black" when all
// channels are dark, or "" when none dominates.
func Dominant(rgb [3]byte) string {
	r, g, b := int(rgb[0]), int(rgb[1]), int(rgb[2])
	switch {
	case r < 40 && g < 40 && b < 40:
		return "black"
	case r > 90 && r > 2*g && r > 2*b:
		return "red"
	case g > 90 && g > 2*r && g > 2*b:
		return "green"
	case b > 90 && b > 2*r && b > 2*g:
		return "blue"
	}
	return ""
}
```

- [ ] **Step 4: Run the test and read the logs**

Run: `go test -tags integration -run TestEpisodeCutting -v ./internal/testdisc/`
Expected: PASS. The logs show the sector ranges, cut times, durations, chapters, the first subtitle time and the copy duration.

If a tool invocation fails because of a version difference, fix the invocation minimally and record the change under Deviations. If any assertion in (1)–(5) fails, investigate first: check the time arithmetic and the NAV offsets against `dvd/ifo.go` and `internal/vobsub/vobsub.go`. If it still fails, report BLOCKED with the logged values.

- [ ] **Step 5: Record the results**

`docs/superpowers/notes/2026-10-02-m6-mkvmerge-cut.md` must contain the following sections, filled in with the values you observed:

```markdown
# M6: mkvmerge cutting and copying DVD titles (verified 2026-10-02)

- Tools: mkvmerge <version>, ffmpeg <version>, dvdauthor <version>, OS <os>.
- (1) Cut accuracy: per episode — sectors, cut start/end from summed VOBU durations, muxed duration vs IFO, first/last frame colour.
- (2) PTS resets: VOB IDs seen in order; offsets stayed correct across them (yes/no).
- (3) Chapters and VobSub on the group timeline: chapter starts after the cut; first subtitle time.
- (4) Output naming: a single `parts:` range writes the given name (no number).
- (5) Copy: out-of-order temp VOB duration vs expected; first/last colours.
- Conclusion for Tasks 4/6: <one sentence>.
```

- [ ] **Step 6: Verify and commit**

Run the Global Constraints verification.

```bash
git add internal/testdisc/episodes.go internal/testdisc/episodes_integration_test.go docs/superpowers/notes/2026-10-02-m6-mkvmerge-cut.md
git commit -m "Add an authored episodes DVD fixture and verify mkvmerge cutting and copying"
```

---

### Task 2: `dvd` VOBU address map and NAV pack parsing, with encoders

**Files:**
- Modify: `dvd/ifo.go` (add `VTS.VOBUs`, parse table pointer 0xE4)
- Create: `dvd/nav.go` (`NAV`, `ParseNAV`)
- Modify: `internal/testdisc/dvdifo.go` (encode `VOBUs`; add `NAVPack`)
- Modify: `internal/vobsub/vobsub.go` (use `dvd.ParseNAV`, delete `navInfo` and `bcdTime`)
- Test: `dvd/ifo_test.go` (add), `dvd/nav_test.go`, `dvd/fuzz_test.go` (add `FuzzParseNAV`)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `VTS.VOBUs []uint32`: the start sector of every VOBU, relative to the title VOBs, in ascending order. It is nil when the IFO has no map (pointer 0).
  - `type NAV struct { StartPTM, EndPTM uint32; VOBID, CellID int; CellElapsed time.Duration }`
  - `func ParseNAV(p []byte) (NAV, bool)`
  - `func testdisc.NAVPack(n dvd.NAV) []byte`, a 2048-byte NAV pack that `ParseNAV` reads back. Its `CellElapsed` is encoded at 30 fps, whole seconds and frames.

Layouts:
- **VOBU map (`VTS_VOBU_ADMAP`):** the VTSI_MAT sector pointer is a u32 at 0xE4. At that sector, a u32 at +0 is the end address (the last byte, relative to the table start). It is followed by u32 sector entries from +4.
- **NAV pack:**
  - pack header (14 bytes)
  - system header `00 00 01 BB 00 12` at 0x0E
  - PCI PES `00 00 01 BF 03 D4` at 0x26, with sub-stream `00` at 0x2C and PCI data at 0x2D. `vobu_s_ptm` is at data+12 and `vobu_e_ptm` at data+16.
  - DSI PES `00 00 01 BF 03 FA` at 0x400, with sub-stream `01` at 0x406 and DSI data at 0x407. `vobu_vob_idn` (u16) is at +24, `vobu_c_idn` (u8) at +27, and `c_eltm` (BCD, 4 bytes) at +28.

- [ ] **Step 1: Write the failing tests**

Add to `dvd/ifo_test.go`. Extend `sampleVTS()` with `VOBUs: []uint32{0, 100, 200, 300}` in its returned struct literal, so `TestVTSRoundTrip` covers the map. Then add:

```go
func TestVOBUMapCorrupt(t *testing.T) {
	v := sampleVTS()
	b := testdisc.VTSFile(v)
	sec := int(binary.BigEndian.Uint32(b[0xE4:])) * 2048
	for name, mut := range map[string]func([]byte) []byte{
		"pointer past end": func(b []byte) []byte { binary.BigEndian.PutUint32(b[0xE4:], 0x7FFF); return b },
		"end past file":    func(b []byte) []byte { binary.BigEndian.PutUint32(b[sec:], 0x7FFFFFF); return b },
		"not ascending":    func(b []byte) []byte { binary.BigEndian.PutUint32(b[sec+8:], 0); return b },
	} {
		t.Run(name, func(t *testing.T) {
			c := append([]byte(nil), b...)
			if _, err := dvd.ParseVTS(mut(c)); !errors.Is(err, dvd.ErrCorrupt) {
				t.Errorf("err = %v, want ErrCorrupt", err)
			}
		})
	}
	v.VOBUs = nil
	got, err := dvd.ParseVTS(testdisc.VTSFile(v))
	if err != nil || got.VOBUs != nil {
		t.Errorf("no map: VOBUs %v, err %v", got.VOBUs, err)
	}
}
```

Add `"encoding/binary"` to that file's imports.

`dvd/nav_test.go`:

```go
package dvd_test

import (
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestNAVRoundTrip(t *testing.T) {
	want := dvd.NAV{StartPTM: 900000, EndPTM: 945045, VOBID: 3, CellID: 7, CellElapsed: 12*time.Second + 15*1001*time.Second/30000}
	got, ok := dvd.ParseNAV(testdisc.NAVPack(want))
	if !ok || got != want {
		t.Errorf("ParseNAV = %+v, %v; want %+v", got, ok, want)
	}
}

func TestParseNAVRejects(t *testing.T) {
	good := testdisc.NAVPack(dvd.NAV{VOBID: 1, CellID: 1})
	for name, mut := range map[string]func([]byte){
		"short":            nil,
		"no pack header":   func(p []byte) { p[3] = 0xBB },
		"no system header": func(p []byte) { p[0x11] = 0xE0 },
		"PCI substream":    func(p []byte) { p[0x2C] = 0x01 },
		"DSI substream":    func(p []byte) { p[0x406] = 0x00 },
	} {
		p := append([]byte(nil), good...)
		if mut == nil {
			p = p[:2000]
		} else {
			mut(p)
		}
		if _, ok := dvd.ParseNAV(p); ok {
			t.Errorf("%s: ParseNAV accepted it", name)
		}
	}
}
```

Add to `dvd/fuzz_test.go`:

```go
func FuzzParseNAV(f *testing.F) {
	f.Add(testdisc.NAVPack(dvd.NAV{StartPTM: 1, EndPTM: 2, VOBID: 1, CellID: 1}))
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = dvd.ParseNAV(b) })
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./dvd/`
Expected: FAIL to compile (`unknown field VOBUs`, `undefined: dvd.ParseNAV`).

- [ ] **Step 3: Implement the parsers**

In `dvd/ifo.go`, add the following field to `VTS` after `PGCs`:

```go
	VOBUs       []uint32               // VTS_VOBU_ADMAP: start sector of every VOBU, ascending; nil if absent
```

In `ParseVTS`, after the PGCI is parsed and before the PTT cross-check, add:

```go
	if v.VOBUs, err = parseVOBUMap(b, be32(b, 0xE4)); err != nil {
		return nil, err
	}
```

Then add:

```go
// parseVOBUMap reads VTS_VOBU_ADMAP at sector: a u32 end address (the last
// byte, relative to the table) and u32 VOBU start sectors from byte 4.
// Sector 0 means the IFO has no map.
func parseVOBUMap(b []byte, sector uint32) ([]uint32, error) {
	if sector == 0 {
		return nil, nil
	}
	off := int64(sector) * sectorSize
	if off+4 > int64(len(b)) {
		return nil, corrupt("VOBU address map at sector %d is outside the file", sector)
	}
	end := int64(be32(b, int(off)))
	if end < 3 || off+end >= int64(len(b)) {
		return nil, corrupt("VOBU address map end address %d is outside the file", end)
	}
	n := (end + 1 - 4) / 4
	out := make([]uint32, 0, n)
	for i := int64(0); i < n; i++ {
		s := be32(b, int(off+4+4*i))
		if i > 0 && s <= out[i-1] {
			return nil, corrupt("VOBU address map is not ascending at entry %d", i)
		}
		out = append(out, s)
	}
	return out, nil
}
```

`dvd/nav.go`:

```go
package dvd

import (
	"encoding/binary"
	"time"
)

// NAV is what zenvik reads from a NAV pack, the first pack of every VOBU.
type NAV struct {
	StartPTM, EndPTM uint32        // PCI vobu_s_ptm and vobu_e_ptm, 90 kHz
	VOBID, CellID    int           // DSI vobu_vob_idn and vobu_c_idn
	CellElapsed      time.Duration // DSI c_eltm: time since the cell's start
}

// ParseNAV reads a 2048-byte NAV pack. It reports false for any other pack.
// A malformed c_eltm reads as zero.
func ParseNAV(p []byte) (NAV, bool) {
	if len(p) != sectorSize || p[0] != 0 || p[1] != 0 || p[2] != 1 || p[3] != 0xBA || p[4]&0xC0 != 0x40 ||
		p[0x0E] != 0 || p[0x0F] != 0 || p[0x10] != 1 || p[0x11] != 0xBB ||
		p[0x26] != 0 || p[0x27] != 0 || p[0x28] != 1 || p[0x29] != 0xBF || p[0x2C] != 0x00 ||
		p[0x400] != 0 || p[0x401] != 0 || p[0x402] != 1 || p[0x403] != 0xBF || p[0x406] != 0x01 {
		return NAV{}, false
	}
	dsi := p[0x407:]
	n := NAV{
		StartPTM: binary.BigEndian.Uint32(p[0x2D+12:]),
		EndPTM:   binary.BigEndian.Uint32(p[0x2D+16:]),
		VOBID:    int(binary.BigEndian.Uint16(dsi[24:])),
		CellID:   int(dsi[27]),
	}
	if t, err := parseTime(dsi[28:32]); err == nil {
		n.CellElapsed = t.Duration()
	}
	return n, true
}
```

- [ ] **Step 4: Implement the encoders**

In `internal/testdisc/dvdifo.go`, change the tail of `VTSFile` to:

```go
	ptt := padSector(encodePTT(v.Titles))
	pgci := padSector(encodePGCI(v.PGCs))
	put32(mat, 0xC8, 1)
	put32(mat, 0xCC, uint32(1+len(ptt)/2048))
	out := append(mat, ptt...)
	out = append(out, pgci...)
	if len(v.VOBUs) > 0 {
		put32(mat, 0xE4, uint32(len(out)/2048))
		m := make([]byte, 4+4*len(v.VOBUs))
		put32(m, 0, uint32(len(m)-1))
		for i, s := range v.VOBUs {
			put32(m, 4+4*i, s)
		}
		out = append(out, padSector(m)...)
	}
	return out
```

`mat` is the first 2048 bytes of `out`, so it aliases them, and the `put32` after the `append`s is still visible. To make that explicit, write the pointer with `put32(out, 0xE4, …)`.

Then add:

```go
// NAVPack encodes n as a 2048-byte NAV pack (pack header, system header,
// PCI and DSI) that dvd.ParseNAV reads back. CellElapsed is written at
// 30 fps.
func NAVPack(n dvd.NAV) []byte {
	p := make([]byte, 2048)
	copy(p, []byte{0, 0, 1, 0xBA, 0x44, 0, 4, 0, 4, 1, 0x01, 0x89, 0xC3, 0xF8})
	copy(p[0x0E:], []byte{0, 0, 1, 0xBB, 0x00, 0x12})
	copy(p[0x26:], []byte{0, 0, 1, 0xBF, 0x03, 0xD4, 0x00})
	put32(p, 0x2D+12, n.StartPTM)
	put32(p, 0x2D+16, n.EndPTM)
	copy(p[0x400:], []byte{0, 0, 1, 0xBF, 0x03, 0xFA, 0x01})
	put16(p, 0x407+24, n.VOBID)
	p[0x407+27] = byte(n.CellID)
	putTime(p[0x407+28:], dvd.NewTime(n.CellElapsed, dvd.Rate30))
	return p
}
```

- [ ] **Step 5: Switch vobsub to `dvd.ParseNAV`**

In `internal/vobsub/vobsub.go`, replace the call `if vob, cell, elapsed, s, ok := navInfo(buf); ok {` and its body with:

```go
			if nav, ok := dvd.ParseNAV(buf); ok {
				start, found := starts[[2]int{nav.VOBID, nav.CellID}]
				base, ptm, known = start+nav.CellElapsed, uint64(nav.StartPTM), found
				continue
			}
```

Delete `navInfo` and `bcdTime`, and import `github.com/chad3814/zenvik/dvd`. Keep `TestExtract` unchanged; it must still pass. If the vobsub tests reference `bcdTime` directly, change them to go through `dvd.ParseNAV`, and say so in the report.

- [ ] **Step 6: Run the tests and fuzz**

Run:
- `go test -race ./dvd/ ./internal/testdisc/ ./internal/vobsub/ ./...`
- `go test -run=^$ -fuzz=FuzzParseNAV -fuzztime=30s -fuzzminimizetime=10x ./dvd/`
- `go test -run=^$ -fuzz=FuzzParseVTS -fuzztime=30s -fuzzminimizetime=10x ./dvd/`

Expected: PASS, with no crashers.

- [ ] **Step 7: Verify and commit**

Run the Global Constraints verification.

```bash
git add dvd internal/testdisc/dvdifo.go internal/vobsub
git commit -m "dvd: parse the VOBU address map and NAV packs; vobsub uses dvd.ParseNAV"
```

---

### Task 3: Classify DVD title cell layouts; drop short stray cells; remap chapters

**Files:**
- Modify: `title.go`, `scan_dvd.go`, `internal/testdisc/dvd.go`
- Test: `scan_dvd_internal_test.go`, `dvd_test.go`, `internal/testdisc/dvd_test.go`

**Interfaces:**
- Consumes: Task 2's `VTS.VOBUs`, `testdisc.NAVPack` and `dvd.NAV`.
- Produces:
  - `Title.RipMethod string` and `Title.SkippedCells []SkippedCell`.
  - `type SkippedCell struct { Cell int; Duration time.Duration; FirstSector, LastSector uint32 }`.
  - The `dvdInfo` fields that Task 4 uses: `method string`, `ranges []sectorRange` (the kept angle-1 cells' sectors, in play order), `files []vobFile` (the title set's title VOBs), `vobus []uint32`, and `cells` (kept cells only, with title-timeline starts).
  - `type sectorRange struct{ first, last int64 }`.
  - `testdisc.StrayCellDVD() *DVD`.
  - `DVD.Files()` now writes a NAV pack at every VOBU start, and the VOBU map into each `VTS_nn_0.IFO`.
  - The `reasonMidFile` and `reasonNotContiguous` constants are deleted.

**StrayCellDVD** has one title set, `VOBs [50]`, and these cells:

| Cell | Sectors | Length | VOB ID / cell ID |
|---|---|---|---|
| A | 0–1 | 1 s | 1/1 |
| B | 2–25 | 24 min | 2/1 |
| C | 26–49 | 24 min | 2/2 |

Its PGCs, each with one program per cell:

| PGC | Cells | Expected |
|---|---|---|
| 1 | B, C, A | cut; skips cell 3 (A); duration 48 min; chapters `[0, 24m]` |
| 2 | C, A, B | copy; a middle stray is kept; chapters `[0, 24m, 24m1s]` |
| 3 | A, B, C | files (A is contiguous with B) |

Titles 1–3 map to PGCs 1–3.

- [ ] **Step 1: Write the failing internal tests**

Replace `TestWholeFiles` in `scan_dvd_internal_test.go` (its reason constants are gone) with:

```go
func cell(first, last uint32, d time.Duration) dvd.Cell {
	return dvd.Cell{FirstSector: first, LastSector: last, Time: dvd.NewTime(d, dvd.Rate30)}
}

func TestWholeFiles(t *testing.T) {
	vobs := []vobFile{{name: "1", size: 10 * 2048, first: 0, last: 9}, {name: "2", size: 10 * 2048, first: 10, last: 19}}
	for _, tt := range []struct {
		name  string
		cells []dvd.Cell
		files int
		ok    bool
	}{
		{"one whole file", []dvd.Cell{cell(0, 4, 0), cell(5, 9, 0)}, 1, true},
		{"both files", []dvd.Cell{cell(0, 12, 0), cell(13, 19, 0)}, 2, true},
		{"gap", []dvd.Cell{cell(0, 4, 0), cell(6, 9, 0)}, 0, false},
		{"ends mid-file", []dvd.Cell{cell(0, 14, 0)}, 0, false},
		{"past the end", []dvd.Cell{cell(10, 25, 0)}, 0, false},
	} {
		got, ok := wholeFiles(tt.cells, vobs)
		if len(got) != tt.files || ok != tt.ok {
			t.Errorf("%s: %d files, ok %v; want %d, %v", tt.name, len(got), ok, tt.files, tt.ok)
		}
	}
}

func TestClassifyCells(t *testing.T) {
	vobs := []vobFile{{name: "1", first: 0, last: 49}}
	s, m := time.Second, time.Minute
	for _, tt := range []struct {
		name    string
		cells   []dvd.Cell
		method  string
		skipped []int // indexes into cells
	}{
		{"whole file", []dvd.Cell{cell(0, 1, s), cell(2, 49, m)}, "files", nil},
		{"trailing stray", []dvd.Cell{cell(2, 25, m), cell(26, 49, m), cell(0, 1, s)}, "cut", []int{2}},
		{"leading stray", []dvd.Cell{cell(48, 49, s), cell(0, 20, m), cell(21, 30, m)}, "cut", []int{0}},
		{"both ends", []dvd.Cell{cell(48, 49, s), cell(5, 20, m), cell(0, 1, s)}, "cut", []int{0, 2}},
		{"long stray kept", []dvd.Cell{cell(2, 25, m), cell(26, 49, m), cell(0, 1, 2*s)}, "copy", nil},
		{"middle stray kept", []dvd.Cell{cell(26, 49, m), cell(0, 1, s), cell(2, 25, m)}, "copy", nil},
		{"mid-file run", []dvd.Cell{cell(5, 9, m), cell(10, 20, m)}, "cut", nil},
		{"never empties", []dvd.Cell{cell(10, 11, s/2), cell(0, 1, s/2)}, "cut", []int{0}},
	} {
		method, skipped := classifyCells(tt.cells, vobs)
		if method != tt.method || !slices.Equal(skipped, tt.skipped) {
			t.Errorf("%s: method %q, skipped %v; want %q, %v", tt.name, method, skipped, tt.method, tt.skipped)
		}
	}
}

func TestRemapChapters(t *testing.T) {
	m := time.Minute
	pgc := &dvd.PGC{Cells: []dvd.Cell{cell(2, 25, 24*m), cell(26, 49, 24*m), cell(0, 1, time.Second)}, Programs: []int{1, 2, 3}}
	ptts := []dvd.PartOfTitle{{PGC: 1, Program: 1}, {PGC: 1, Program: 2}, {PGC: 1, Program: 3}}
	got := titleChapters(pgc, ptts, map[int]bool{2: true})
	want := []Chapter{{Number: 1, Start: 0}, {Number: 2, Start: 24 * m}}
	if !slices.Equal(got, want) {
		t.Errorf("chapters = %v, want %v", got, want)
	}
	// A dropped leading cell moves its chapter to the next kept cell; duplicates collapse.
	pgc2 := &dvd.PGC{Cells: []dvd.Cell{cell(48, 49, time.Second), cell(0, 20, m), cell(21, 30, m)}, Programs: []int{1, 2, 3}}
	got = titleChapters(pgc2, ptts, map[int]bool{0: true})
	want = []Chapter{{Number: 1, Start: 0}, {Number: 2, Start: m}}
	if !slices.Equal(got, want) {
		t.Errorf("leading drop: chapters = %v, want %v", got, want)
	}
}
```

Update the imports: `slices`, `time`, `dvd`.

In the "never empties" case, both cells are 0.5 s strays. The leading one is dropped first, and the remaining single cell must never be dropped. That leaves cell `(0,1)` alone, which is a mid-file run, so the method is `cut`.

- [ ] **Step 2: Write the failing public tests**

In `dvd_test.go`:
- Delete the `reasonMidFile` and `reasonNotContiguous` constants.
- In `checkSampleDVD`:
  - Expect the title order `[]string{"01", "06", "03", "04", "05", "08", "02", "07"}`.
  - Replace the unsupported loop with:

```go
	for id, method := range map[string]string{"01": "files", "06": "files", "03": "cut", "04": "cut", "05": "cut", "08": "copy"} {
		ti := mustTitle(t, d, id)
		if ti.RipMethod != method || ti.Unsupported != "" || ti.Rank.Filtered {
			t.Errorf("%s: method %q, unsupported %q, rank %+v; want %q", id, ti.RipMethod, ti.Unsupported, ti.Rank, method)
		}
	}
	if ep := mustTitle(t, d, "04"); ep.Size != 20*2048 || len(ep.Clips) != 2 {
		t.Errorf("04 spans both VOBs: size %d, clips %v", ep.Size, ep.Clips)
	}
	if ang := mustTitle(t, d, "08"); ang.Size != 15*2048 {
		t.Errorf("08 copies its 15 angle-1 sectors: size %d", ang.Size)
	}
```

- Keep the 08 chapter assertions.
- Change `TestRipUnsupportedDVDTitle` to remove `VTS_03_0.IFO` from the written disc. Then rip title `07`, and expect `ErrUnsupportedTitle` with `"missing VTS_03_0.IFO"` in the message.
- Add:

```go
func TestOpenStrayCellDVD(t *testing.T) {
	d := openDisc(t, writeDVD(t, testdisc.StrayCellDVD()))
	cut := mustTitle(t, d, "01")
	if cut.RipMethod != "cut" || cut.Duration != 48*time.Minute || cut.Size != 48*2048 {
		t.Errorf("01 = method %q, duration %v, size %d", cut.RipMethod, cut.Duration, cut.Size)
	}
	if want := []zenvik.SkippedCell{{Cell: 3, Duration: time.Second, FirstSector: 0, LastSector: 1}}; !reflect.DeepEqual(cut.SkippedCells, want) {
		t.Errorf("01 skipped = %+v", cut.SkippedCells)
	}
	if want := []zenvik.Chapter{{Number: 1, Start: 0}, {Number: 2, Start: 24 * time.Minute}}; !reflect.DeepEqual(cut.Chapters, want) {
		t.Errorf("01 chapters = %v", cut.Chapters)
	}
	cp := mustTitle(t, d, "02")
	if cp.RipMethod != "copy" || len(cp.SkippedCells) != 0 || len(cp.Chapters) != 3 || cp.Chapters[2].Start != 24*time.Minute+time.Second {
		t.Errorf("02 = method %q, skipped %v, chapters %v", cp.RipMethod, cp.SkippedCells, cp.Chapters)
	}
	if f := mustTitle(t, d, "03"); f.RipMethod != "files" {
		t.Errorf("03 method %q", f.RipMethod)
	}
}
```

In `internal/testdisc/dvd_test.go`, add:

```go
func TestSampleDVDNavigation(t *testing.T) {
	files := SampleDVD().Files()
	vts, err := dvd.ParseVTS(files["VIDEO_TS/VTS_02_0.IFO"])
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint32{0, 20, 40}; !slices.Equal(vts.VOBUs, want) {
		t.Errorf("VTS 2 VOBUs = %v, want %v", vts.VOBUs, want)
	}
	vob := files["VIDEO_TS/VTS_02_1.VOB"]
	nav, ok := dvd.ParseNAV(vob[20*2048 : 21*2048])
	if !ok || nav.VOBID != 1 || nav.CellID != 2 || nav.EndPTM-nav.StartPTM != 20*60*90000 || nav.StartPTM != 20*60*90000 {
		t.Errorf("NAV at sector 20 = %+v, %v", nav, ok)
	}
	if _, ok := dvd.ParseNAV(vob[2048:4096]); ok {
		t.Error("sector 1 is not a VOBU start")
	}
}
```

Add `"slices"` to its imports.

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./ ./internal/testdisc/`
Expected: FAIL to compile (`undefined: classifyCells`, `StrayCellDVD`, `RipMethod`, …).

- [ ] **Step 4: Builder — NAV packs and the VOBU map, plus the stray fixture**

In `internal/testdisc/dvd.go`, replace the loop in `Files()` that writes title sets with:

```go
	for i, ts := range d.TitleSets {
		vts := ts.VTS
		total := 0
		for _, n := range ts.VOBs {
			total += n
		}
		data := VOBPacks(total, ts.Scrambled)
		vobus := navigate(&vts, data)
		if vts.VOBUs == nil {
			vts.VOBUs = vobus
		}
		files[dir+name(fmt.Sprintf("VTS_%02d_0.IFO", i+1))] = VTSFile(&vts)
		off := 0
		for k, sectors := range ts.VOBs {
			files[dir+name(fmt.Sprintf("VTS_%02d_%d.VOB", i+1, k+1))] = data[off*2048 : (off+sectors)*2048]
			off += sectors
		}
	}
```

Then add:

```go
// navigate writes a NAV pack at the first sector of every cell of every
// PGC in vts (each such sector starts a VOBU) into data, the title set's
// title VOBs back to back, and returns those VOBU starts in ascending
// order. VOBU PTMs run in sector order; each VOBU lasts its cell's time.
func navigate(vts *dvd.VTS, data []byte) []uint32 {
	byStart := map[uint32]dvd.Cell{}
	for _, p := range vts.PGCs {
		for _, c := range p.Cells {
			if _, ok := byStart[c.FirstSector]; !ok {
				byStart[c.FirstSector] = c
			}
		}
	}
	starts := make([]uint32, 0, len(byStart))
	for s := range byStart {
		if int(s+1)*2048 <= len(data) {
			starts = append(starts, s)
		}
	}
	slices.Sort(starts)
	var ptm uint32
	for _, s := range starts {
		c := byStart[s]
		ticks := uint32(c.Time.Duration() * 90000 / time.Second)
		copy(data[int(s)*2048:], NAVPack(dvd.NAV{StartPTM: ptm, EndPTM: ptm + ticks, VOBID: c.VOBID, CellID: c.CellID}))
		ptm += ticks
	}
	return starts
}

// StrayCellDVD returns a one-title-set disc for cell-layout tests. Its VOB
// has 50 sectors in three cells: A 0–1 (1 s, VOB 1 cell 1), B 2–25 and
// C 26–49 (24 min each, VOB 2 cells 1 and 2). Title 1 plays B C A (A is a
// short trailing stray, so it's cut with A skipped), title 2 plays C A B
// (copy), and title 3 plays A B C (whole file).
func StrayCellDVD() *DVD {
	tm := func(d time.Duration) dvd.Time { return dvd.NewTime(d, dvd.Rate30) }
	a := dvd.Cell{Time: tm(time.Second), FirstSector: 0, LastSector: 1, VOBID: 1, CellID: 1}
	b := dvd.Cell{Time: tm(24 * time.Minute), FirstSector: 2, LastSector: 25, VOBID: 2, CellID: 1}
	c := dvd.Cell{Time: tm(24 * time.Minute), FirstSector: 26, LastSector: 49, VOBID: 2, CellID: 2}
	var audio [8]dvd.AudioControl
	audio[0] = dvd.AudioControl{Available: true}
	pgc := func(cells ...dvd.Cell) *dvd.PGC {
		var total time.Duration
		progs := make([]int, len(cells))
		for i, cl := range cells {
			total += cl.Time.Duration()
			progs[i] = i + 1
		}
		return &dvd.PGC{Time: tm(total), Audio: audio, Programs: progs, Cells: cells}
	}
	three := func(n int) []dvd.PartOfTitle {
		return []dvd.PartOfTitle{{PGC: n, Program: 1}, {PGC: n, Program: 2}, {PGC: n, Program: 3}}
	}
	return &DVD{
		Titles: []dvd.TitleEntry{
			{Angles: 1, Chapters: 3, TitleSet: 1, TitleSetTitle: 1},
			{Angles: 1, Chapters: 3, TitleSet: 1, TitleSetTitle: 2},
			{Angles: 1, Chapters: 3, TitleSet: 1, TitleSetTitle: 3},
		},
		TitleSets: []DVDTitleSet{{VOBs: []int{50}, VTS: dvd.VTS{
			Video:  dvd.VideoAttributes{Coding: dvd.MPEG2, Standard: dvd.NTSC, Aspect: dvd.Aspect4x3, Width: 720, Height: 480},
			Audio:  []dvd.AudioAttributes{{Coding: dvd.AC3, Channels: 2, SampleRate: 48000, Language: "en"}},
			Titles: [][]dvd.PartOfTitle{three(1), three(2), three(3)},
			PGCs:   []*dvd.PGC{pgc(b, c, a), pgc(c, a, b), pgc(a, b, c)},
		}}},
	}
}
```

Add `"slices"` to the imports (`time` is already imported).

VOBU durations come from the first cell found at each start, so SampleDVD's VTS 2 gives VOBUs at 0, 20 and 40, each 20 minutes long, with PTM 0, 20 min and 40 min. That matches `TestSampleDVDNavigation`.

- [ ] **Step 5: Classifier, chapters and title fields**

In `title.go`:

- Add to `Title`, after `Unsupported`:

```go
	// RipMethod says how a DVD title is ripped: "files" (whole title VOBs),
	// "cut" (one ascending run cut from its VOBs) or "copy" (cells copied in
	// play order to a temporary file). It is "" for Blu-ray titles.
	RipMethod string
	// SkippedCells lists short out-of-order cells at a DVD title's start or
	// end that are left out of the rip.
	SkippedCells []SkippedCell
```

- Add:

```go
// SkippedCell is a DVD cell left out of a title's rip.
type SkippedCell struct {
	Cell                    int // 1-based cell number in the title's PGC
	Duration                time.Duration
	FirstSector, LastSector uint32
}
```

- Extend `dvdInfo` with:

```go
	method string        // RipMethod
	ranges []sectorRange // kept angle-1 cells' sectors, in play order
	files  []vobFile     // the title set's title VOBs
	vobus  []uint32      // the title set's VOBU starts
```

In `scan_dvd.go`:

1. Delete `reasonNotContiguous` and `reasonMidFile`. Keep `reasonMultiPGC`.
2. Add `type sectorRange struct{ first, last int64 }`.
3. Change `wholeFiles` to return `([]vobFile, bool)`. Return `nil, false` wherever it returned a reason, and `vobs[start:end+1], true` on success. The old `"has no cells"` case also becomes `nil, false`.
4. Add:

```go
// maxStray is the longest cell that may be dropped from a title's edge.
const maxStray = time.Second

// classifyCells decides how a title whose angle-1 cells, in play order,
// are cells is ripped, and which of them (indexes into cells) are dropped
// as short strays at the edges. At least one cell is always kept.
func classifyCells(cells []dvd.Cell, vobs []vobFile) (method string, skipped []int) {
	contiguous := func(a, b dvd.Cell) bool { return int64(b.FirstSector) == int64(a.LastSector)+1 }
	lo, hi := 0, len(cells)-1
	for hi > lo && !contiguous(cells[lo], cells[lo+1]) && cells[lo].Time.Duration() <= maxStray {
		skipped = append(skipped, lo)
		lo++
	}
	for hi > lo && !contiguous(cells[hi-1], cells[hi]) && cells[hi].Time.Duration() <= maxStray {
		skipped = append(skipped, hi)
		hi--
	}
	kept := cells[lo : hi+1]
	if _, ok := wholeFiles(kept, vobs); ok {
		return "files", skipped
	}
	for i := 1; i < len(kept); i++ {
		if !contiguous(kept[i-1], kept[i]) {
			return "copy", skipped
		}
	}
	return "cut", skipped
}

// titleChapters returns the title's chapters on its own timeline. Cells in
// drop (indexes into pgc.Cells) are left out. A chapter whose entry cell
// is dropped moves to the next kept cell, or goes away if none follows.
// Chapters landing on the same cell collapse, and numbers run from 1.
func titleChapters(pgc *dvd.PGC, ptts []dvd.PartOfTitle, drop map[int]bool) []Chapter {
	var kept []dvd.Cell
	index := make([]int, len(pgc.Cells)) // pgc cell → index into kept, or -1
	for i, c := range pgc.Cells {
		index[i] = -1
		if !drop[i] {
			index[i] = len(kept)
			kept = append(kept, c)
		}
	}
	starts := cellStarts(kept)
	var out []Chapter
	last := -1
	for _, p := range ptts {
		e := pgc.Programs[p.Program-1] - 1
		for e < len(pgc.Cells) && index[e] < 0 {
			e++
		}
		if e >= len(pgc.Cells) || index[e] == last {
			continue
		}
		last = index[e]
		out = append(out, Chapter{Number: len(out) + 1, Start: starts[index[e]]})
	}
	return out
}
```

5. Rewrite the second half of `dvdTitle`, from `starts := cellStarts(pgc.Cells)` to the end, as:

```go
	var play []int // indexes into pgc.Cells of the angle-1 cells, in play order
	for i, cl := range pgc.Cells {
		if !cl.AngleBlock || cl.BlockMode == dvd.FirstInBlock || cl.BlockMode == dvd.NotInBlock {
			play = append(play, i)
		}
	}
	if len(play) == 0 {
		return unsupported("has no cells")
	}
	cells := make([]dvd.Cell, len(play))
	for k, i := range play {
		cells[k] = pgc.Cells[i]
	}
	method, skipped := classifyCells(cells, ts.vobs)
	drop := map[int]bool{}
	for _, k := range skipped {
		cl := cells[k]
		drop[play[k]] = true
		t.SkippedCells = append(t.SkippedCells, SkippedCell{Cell: play[k] + 1, Duration: cl.Time.Duration(), FirstSector: cl.FirstSector, LastSector: cl.LastSector})
		t.Duration -= cl.Time.Duration()
	}
	slices.SortFunc(t.SkippedCells, func(a, b SkippedCell) int { return a.Cell - b.Cell })
	t.RipMethod = method
	t.Chapters = titleChapters(pgc, ptts, drop)

	info := &dvdInfo{palette: pgc.Palette, width: ts.vts.Video.Width, height: ts.vts.Video.Height,
		subLang: map[uint16]string{}, method: method, files: ts.vobs, vobus: ts.vts.VOBUs}
	var keptAll []dvd.Cell
	for i, cl := range pgc.Cells {
		if !drop[i] {
			keptAll = append(keptAll, cl)
		}
	}
	starts := cellStarts(keptAll)
	for k, cl := range keptAll {
		if !cl.AngleBlock || cl.BlockMode == dvd.FirstInBlock || cl.BlockMode == dvd.NotInBlock {
			info.cells = append(info.cells, vobsub.Cell{VOBID: cl.VOBID, CellID: cl.CellID, Start: starts[k]})
			info.ranges = append(info.ranges, sectorRange{int64(cl.FirstSector), int64(cl.LastSector)})
			t.Size += (int64(cl.LastSector) - int64(cl.FirstSector) + 1) * 2048
			c.Clips = append(c.Clips, rank.Clip{ID: fmt.Sprintf("%d:%d:%d", e.TitleSet, cl.VOBID, cl.CellID), Out: cl.Time.Duration()})
		}
	}
	t.dvd = info
	fillDVDTracks(t, ts.vts, pgc, info)
	for _, f := range ts.vobs {
		for _, r := range info.ranges {
			if r.first <= f.last && r.last >= f.first {
				t.Clips = append(t.Clips, Clip{ID: f.name})
				break
			}
		}
	}
	t.Encrypted = ts.encrypted
	c.Duration = t.Duration
	c.Chapters = len(t.Chapters)
	c.Languages = countLanguages(t)
	c.HasVideo = true
	c.HasAudio = len(t.Audio) > 0
	c.Size = t.Size
	c.Encrypted = t.Encrypted
	return t, c
}
```

Add `"slices"` to the imports.

`t.Clips` now lists, in file order, every title VOB that a kept cell touches. Task 4 resolves those names. Remove the now-unused `angleOne` function, or keep it only if a test still uses it.

- [ ] **Step 6: Run the tests**

Run: `go test -race ./...`
Expected: PASS.

If an existing test still asserts the old `starts or ends mid-file` behavior, including `cmd/zenvik` tests on title 03, adjust only the root and testdisc tests in this task. For a `cmd/zenvik` test, change the assertion minimally to match the new behavior and note it in the report; Task 5 updates the CLI tests properly. Find them with `grep -rn "mid-file\|not contiguous" --include=*_test.go .`.

- [ ] **Step 7: Verify and commit**

Run the Global Constraints verification.

```bash
git add title.go scan_dvd.go scan_dvd_internal_test.go dvd_test.go internal/testdisc
git commit -m "Classify DVD title cell layouts, skip short stray cells, and remap chapters"
```

---

### Task 4: Rip the cut and copy paths

**Files:**
- Modify: `rip_dvd.go` (rewrite `dvdJob`), `rip.go` (`PhaseCopying`), `internal/mux/args.go` (`Split`), `internal/vobsub/vobsub.go` (`Spans`, `TimeOffset`), `cmd/zenvik/progress.go` (phase width), `internal/testdisc/episodes_integration_test.go` (`VOBs` becomes `Spans`)
- Create: `rip_dvd_layout.go` (resolving, spans, cut points, copy), `diskfree_statfs.go`, `diskfree_windows.go`, `diskfree_other.go`
- Test: `rip_dvd_layout_internal_test.go` (package `zenvik`), `rip_dvd_test.go` (add), `internal/mux/mux_test.go` (add), `internal/vobsub/vobsub_test.go` (update)

**Interfaces:**
- Consumes:
  - Task 3: `dvdInfo.method`, `ranges`, `files`, `vobus` and `cells`; `Title.RipMethod` and `SkippedCells`.
  - Task 2: `dvd.ParseNAV`.
  - Task 1's notes: a single `parts:` range is written under the given name.
- Produces:
  - `zenvik.PhaseCopying` (6, `"copying"`).
  - `mux.TimeRange{Start, End time.Duration}`, and `mux.Job.Split []TimeRange` (emitted as `--split parts:…`).
  - `vobsub.Span{Path string; Offset, Length int64}`, `Params.Spans []Span` (this replaces `Params.VOBs`), and `Params.TimeOffset time.Duration`.
  - DVD rips for all three methods.
  - A dry run of a copy title shows `<title.vob>`.

**Cut points (Task 1 ruling, see the M6 notes file).** `start = start₀ − m_s` and `end = end₀ − m_e`, where:
- start₀ is the summed VOBU durations before the run, and end₀ adds the run's VOBUs;
- m_s is half the shorter of the last VOBU before the run and the run's first VOBU, or 0 when start₀ is 0;
- m_e is half the run's last VOBU.

A drift guard fails the cut when (VOB ID changes from the group start to R1) × 10 ms reaches a non-zero margin.

**Cut group rule.** The `( … )` group starts at the latest title VOB, at or before the first file the run touches, whose first sector is a VOBU start. `VTS_nn_1` always qualifies. The group ends at the last file the run touches. Real discs split their VOB files at about 1 GB, which can fall in the middle of a VOBU, and mkvmerge must start reading on a VOBU boundary.

- [ ] **Step 1: Write the failing tests**

Add to `internal/mux/mux_test.go`. Use `mux.` prefixes if the file is an external test package.

```go
func TestArgsSplit(t *testing.T) {
	got := Args(Job{Concat: []string{"a.vob"}, Output: "o.mkv", Split: []TimeRange{{Start: 20 * time.Minute, End: 40*time.Minute + 1500*time.Millisecond}},
		Tracks: []Track{{ID: 0, Type: "video", Default: true}}})
	want := []string{"-o", "o.mkv", "--split", "parts:00:20:00.000000000-00:40:01.500000000", "--default-track-flag", "0:yes",
		"--video-tracks", "0", "--no-audio", "--no-subtitles", "--track-order", "0:0", "(", "a.vob", ")"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Args =\n%q\nwant\n%q", got, want)
	}
}
```

In `internal/vobsub/vobsub_test.go`:
- Change `Params{VOBs: []string{p1, p2}, …}` to `Params{Spans: []Span{{Path: p1}, {Path: p2}}, …}`.
- Change `TestExtractRejectsPartialPack` to `Spans: []Span{{Path: p}}`.
- Add:

```go
func TestExtractSpanAndOffset(t *testing.T) {
	dir := t.TempDir()
	var vob []byte
	vob = append(vob, videoPack()...)                        // sector 0: outside the span
	vob = append(vob, spuPack(0, u32(900000))...)            // sector 1: outside the span
	vob = append(vob, navPack(1, 1, 0, 900000)...)           // sector 2: span starts here
	vob = append(vob, spuPack(0, u32(900000+90000))...)      // 1 s into cell 1
	p := filepath.Join(dir, "v.vob")
	if err := os.WriteFile(p, vob, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Extract(context.Background(), Params{
		Spans: []Span{{Path: p, Offset: 2 * 2048, Length: 2 * 2048}}, TimeOffset: 10 * time.Minute,
		Cells: []Cell{{VOBID: 1, CellID: 1}}, Streams: []Stream{{ID: 0, Language: "en"}}, Width: 720, Height: 480, Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := os.ReadFile(res.IDX)
	if !strings.Contains(string(idx), "timestamp: 00:10:01:000, filepos: 000000000\n") || strings.Count(string(idx), "timestamp:") != 1 {
		t.Errorf("idx:\n%s", idx)
	}
}
```

`navPack` and `spuPack` are existing helpers in that file. If `u32` returns another type there, adapt the call.

`rip_dvd_layout_internal_test.go`:

```go
package zenvik

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestCutPoints(t *testing.T) {
	for _, tt := range []struct {
		id         string
		group      []string
		start, end time.Duration
	}{
		// Synthetic VOBUs are whole 20-minute cells, so the half-VOBU margins are 10 minutes.
		{"03", []string{"VTS_02_1.VOB"}, 0, 10 * time.Minute},
		{"04", []string{"VTS_02_1.VOB", "VTS_02_2.VOB"}, 10 * time.Minute, 30 * time.Minute},
		{"05", []string{"VTS_02_1.VOB", "VTS_02_2.VOB"}, 30 * time.Minute, 50 * time.Minute}, // VTS_02_2 starts mid-VOBU
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
	// start₀ 1 s, m_s = min(1 s, 24 min)/2; end₀ 48m1s, m_e = 24 min/2; one VOB-ID change (A→B), 10 ms ≪ margins.
	if start, end, err := cutPoints(ti.dvd, paths); err != nil || start != 500*time.Millisecond || end != 36*time.Minute+time.Second {
		t.Errorf("stray 01: cut %v–%v (%v)", start, end, err)
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
```

Add to `rip_dvd_test.go`. It reuses the existing `hasPair`, `openDisc` and `writeDVD` helpers.

```go
// layoutStub answers --version, -J (one video and one AC-3 track), and
// fails anything else (a mux), so non-dry-run rips stop after the job is built.
func layoutStub(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the mkvmerge stub is a shell script")
	}
	stub := filepath.Join(t.TempDir(), "mkvmerge")
	script := `#!/bin/sh
case "$1" in
--version) echo "mkvmerge v90.0 ('Stub') 64-bit" ;;
-J) printf '%s\n' '{"tracks":[{"id":0,"type":"video","codec":"MPEG-1/2","properties":{"stream_id":224}},{"id":1,"type":"audio","codec":"AC-3","properties":{"stream_id":189,"sub_stream_id":128,"audio_channels":2}}]}' ;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func TestDVDCutDryRun(t *testing.T) {
	t.Setenv("PATH", "")
	stub := layoutStub(t)
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	res, err := d.Rip(context.Background(), mustTitle(t, d, "05"), zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "ep.mkv"), DryRun: true, MkvmergePath: stub})
	if err != nil {
		t.Fatal(err)
	}
	cmd := res.Command
	if !hasPair(cmd, "--split", "parts:00:30:00.000000000-00:50:00.000000000") {
		t.Errorf("no split in %q", cmd)
	}
	i := slices.Index(cmd, "(")
	if i < 0 || len(cmd) < i+4 || !strings.HasSuffix(cmd[i+1], "VTS_02_1.VOB") || !strings.HasSuffix(cmd[i+2], "VTS_02_2.VOB") || cmd[i+3] != ")" {
		t.Errorf("group = %q", cmd[max(0, i):])
	}
}

func TestDVDCopyDryRun(t *testing.T) {
	t.Setenv("PATH", "")
	stub := layoutStub(t)
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	out := filepath.Join(t.TempDir(), "angle.mkv")
	res, err := d.Rip(context.Background(), mustTitle(t, d, "08"), zenvik.RipOptions{OutputPath: out, DryRun: true, MkvmergePath: stub})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(res.Command, "(")
	if i < 0 || res.Command[i+1] != "<title.vob>" || res.Command[i+2] != ")" {
		t.Errorf("command = %q", res.Command)
	}
	if ents, _ := os.ReadDir(filepath.Dir(out)); len(ents) != 0 {
		t.Errorf("dry run wrote %v", ents)
	}
}

func TestDVDCopyRemovesTempOnMuxFailure(t *testing.T) {
	t.Setenv("PATH", "")
	stub := layoutStub(t)
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	out := filepath.Join(t.TempDir(), "angle.mkv")
	if _, err := d.Rip(context.Background(), mustTitle(t, d, "08"), zenvik.RipOptions{OutputPath: out, MkvmergePath: stub}); err == nil {
		t.Fatal("the stub fails the mux; Rip must fail")
	}
	if ents, _ := os.ReadDir(filepath.Dir(out)); len(ents) != 0 {
		t.Errorf("failed copy rip left %v", ents)
	}
}

func TestDVDSkipWarning(t *testing.T) {
	t.Setenv("PATH", "")
	stub := layoutStub(t)
	d := openDisc(t, writeDVD(t, testdisc.StrayCellDVD()))
	res, err := d.Rip(context.Background(), mustTitle(t, d, "01"), zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "x.mkv"), DryRun: true, MkvmergePath: stub})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Warnings, "title 01: skipped cell 3 (1.0 s at sectors 0–1, out of order)") {
		t.Errorf("warnings = %q", res.Warnings)
	}
}
```

Add `"runtime"` and `"slices"` to that file's imports if they're missing.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./ ./internal/mux/ ./internal/vobsub/`
Expected: FAIL to compile (`unknown field Split`, `Spans`, `undefined: cutPoints`, …).

- [ ] **Step 3: mux `Split` and vobsub `Spans`/`TimeOffset`**

In `internal/mux/args.go`, add:

```go
// TimeRange is a span of the input timeline.
type TimeRange struct{ Start, End time.Duration }
```

Add `Split []TimeRange // keep only these ranges ("--split parts:"); nil keeps everything` to `Job`. In `Args`, directly after `args := []string{"-o", job.Output}`, add:

```go
	if len(job.Split) > 0 {
		parts := make([]string, len(job.Split))
		for i, r := range job.Split {
			parts[i] = timestamp(r.Start) + "-" + timestamp(r.End)
		}
		args = append(args, "--split", "parts:"+strings.Join(parts, ","))
	}
```

And add:

```go
// timestamp formats d as mkvmerge's HH:MM:SS.nnnnnnnnn.
func timestamp(d time.Duration) string {
	ns := d.Nanoseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%09d", ns/3_600_000_000_000, ns/60_000_000_000%60, ns/1_000_000_000%60, ns%1_000_000_000)
}
```

Add `fmt` and `time` to the imports.

In `internal/vobsub/vobsub.go`, replace `VOBs []string` in `Params` with:

```go
	Spans         []Span        // the title's VOB data, in order
	TimeOffset    time.Duration // added to every timestamp (a cut title's place on the mkvmerge timeline)
```

Add:

```go
// Span is a byte range of a VOB file; Length 0 means to the end of the file.
type Span struct {
	Path           string
	Offset, Length int64
}
```

In `Extract`, replace the size loop with:

```go
	var total int64
	spans := make([]Span, len(p.Spans))
	for i, sp := range p.Spans {
		st, err := os.Stat(sp.Path)
		if err != nil {
			return nil, err
		}
		if sp.Length == 0 {
			sp.Length = st.Size() - sp.Offset
		}
		if sp.Offset < 0 || sp.Length < 0 || sp.Offset%packSize != 0 || sp.Length%packSize != 0 || sp.Offset+sp.Length > st.Size() {
			return nil, fmt.Errorf("vobsub: %s: span %d+%d is not whole 2048-byte packs inside the file", sp.Path, sp.Offset, sp.Length)
		}
		spans[i] = sp
		total += sp.Length
	}
```

Also replace the reading loop's `for _, v := range p.VOBs { f, err := os.Open(v) … r := bufio.NewReaderSize(f, 1<<20)` with an `os.Open(sp.Path)`, then `f.Seek(sp.Offset, io.SeekStart)` (return its error), then `r := bufio.NewReaderSize(io.LimitReader(f, sp.Length), 1<<20)`. Use `sp.Path` wherever error messages used `v`.

Finally, change the entry timestamp to `at := max(base+ptsDelta(pts, ptm), 0) + p.TimeOffset`.

Update the Task 1 integration test (`internal/testdisc/episodes_integration_test.go`) from `VOBs: []string{vob}` to `Spans: []vobsub.Span{{Path: vob}}`.

- [ ] **Step 4: Layout helpers — resolve, spans, cut group, cut points, copy, free space**

`rip_dvd_layout.go`:

```go
package zenvik

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/source"
	"github.com/chad3814/zenvik/internal/vobsub"
)

// titleVOBPlaceholder stands in for the copy path's temporary file in a
// dry run, which copies nothing.
const titleVOBPlaceholder = "<title.vob>"

// freeSpace reports the bytes free in a directory; tests replace it.
var freeSpace = freeBytes

// resolveVOBs returns the paths under root of the title set's title VOBs,
// matching names ignoring case.
func resolveVOBs(root string, files []vobFile) ([]string, error) {
	fsys := os.DirFS(root)
	dir := source.FindName(fsys, ".", "VIDEO_TS", true)
	if dir == "" {
		return nil, fmt.Errorf("zenvik: no VIDEO_TS folder under %s", root)
	}
	paths := make([]string, len(files))
	for i, f := range files {
		n := source.FindName(fsys, dir, f.name, false)
		if n == "" {
			return nil, fmt.Errorf("zenvik: %s is missing from %s", f.name, filepath.Join(root, dir))
		}
		paths[i] = filepath.Join(root, dir, n)
	}
	return paths, nil
}

// touchedFiles returns the indexes, ascending, of the files any range overlaps.
func touchedFiles(ranges []sectorRange, files []vobFile) []int {
	var out []int
	for i, f := range files {
		for _, r := range ranges {
			if r.first <= f.last && r.last >= f.first {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// cutGroup returns the indexes of the files mkvmerge reads for a cut title:
// from the latest file at or before the run's first file whose first
// sector starts a VOBU, through the run's last file.
func cutGroup(info *dvdInfo) []int {
	touched := touchedFiles(info.ranges, info.files)
	starts := map[int64]bool{}
	for _, s := range info.vobus {
		starts[int64(s)] = true
	}
	g := touched[0]
	for g > 0 && !starts[info.files[g].first] {
		g--
	}
	out := make([]int, 0, touched[len(touched)-1]-g+1)
	for i := g; i <= touched[len(touched)-1]; i++ {
		out = append(out, i)
	}
	return out
}

// byteSpans maps sector ranges, in order, to byte spans of the files.
func byteSpans(ranges []sectorRange, files []vobFile, paths []string) []vobsub.Span {
	var out []vobsub.Span
	for _, r := range ranges {
		for i, f := range files {
			a, z := max(r.first, f.first), min(r.last, f.last)
			if a > z {
				continue
			}
			out = append(out, vobsub.Span{Path: paths[i], Offset: (a - f.first) * 2048, Length: (z - a + 1) * 2048})
		}
	}
	return out
}

// driftPerVOBID bounds how far summed VOBU durations can run ahead of
// mkvmerge's timeline at each VOB ID change (observed: 4.67 ms of padding
// per VOB ID on authored discs; see docs/superpowers/notes/2026-10-02-m6-mkvmerge-cut.md).
const driftPerVOBID = 10 * time.Millisecond

// cutDriftGuard fails when changes VOB ID changes could drift the cut by at
// least the smallest non-zero margin, so mkvmerge might snap to the wrong
// keyframe.
func cutDriftGuard(changes int, margins ...time.Duration) error {
	drift := time.Duration(changes) * driftPerVOBID
	for _, m := range margins {
		if m > 0 && drift >= m {
			return fmt.Errorf("%d VOB ID changes before the cut could shift it by %v, more than the %v margin", changes, drift, m)
		}
	}
	return nil
}

// cutPoints returns the --split times for a cut title on the timeline of
// its cut group. mkvmerge cuts at the first keyframe at or after each time,
// and summed VOBU durations (from the NAV packs) run slightly ahead of its
// timeline at VOB ID changes, so each point is moved back by half a VOBU:
// start = (VOBUs before the run) − half the shorter of the VOBU before the
// run and the run's first VOBU (no margin at 0); end = start₀ + (the run's
// VOBUs) − half the run's last VOBU.
func cutPoints(info *dvdInfo, paths []string) (start, end time.Duration, err error) {
	if len(info.vobus) == 0 {
		return 0, 0, errors.New("the IFO has no VOBU address map")
	}
	group := cutGroup(info)
	g0 := info.files[group[0]].first
	r0, r1 := info.ranges[0].first, info.ranges[len(info.ranges)-1].last
	open := map[int]*os.File{}
	defer func() {
		for _, f := range open {
			f.Close()
		}
	}()
	buf := make([]byte, 2048)
	var before, during, lastBefore, firstRun, lastRun uint64
	sawStart := false
	changes, prevVOB := 0, -1
	for _, s := range info.vobus {
		sec := int64(s)
		if sec < g0 || sec > r1 {
			continue
		}
		fi := -1
		for i, f := range info.files {
			if sec >= f.first && sec <= f.last {
				fi = i
			}
		}
		if fi < 0 {
			return 0, 0, fmt.Errorf("VOBU at sector %d is outside the title VOBs", sec)
		}
		f, ok := open[fi]
		if !ok {
			if f, err = os.Open(paths[fi]); err != nil {
				return 0, 0, err
			}
			open[fi] = f
		}
		if _, err := f.ReadAt(buf, (sec-info.files[fi].first)*2048); err != nil {
			return 0, 0, fmt.Errorf("reading the NAV pack at sector %d: %w", sec, err)
		}
		nav, ok := dvd.ParseNAV(buf)
		if !ok {
			return 0, 0, fmt.Errorf("no NAV pack at VOBU sector %d", sec)
		}
		if prevVOB >= 0 && nav.VOBID != prevVOB {
			changes++
		}
		prevVOB = nav.VOBID
		d := uint64(nav.EndPTM - nav.StartPTM)
		switch {
		case sec < r0:
			before += d
			lastBefore = d
		default:
			if sec == r0 {
				sawStart = true
				firstRun = d
			}
			during += d
			lastRun = d
		}
	}
	if !sawStart {
		return 0, 0, fmt.Errorf("no VOBU starts at the title's first sector %d", r0)
	}
	ticks := func(n uint64) time.Duration { return time.Duration(n) * time.Second / 90000 }
	var ms time.Duration
	if before > 0 {
		ms = ticks(min(lastBefore, firstRun)) / 2
	}
	me := ticks(lastRun) / 2
	if err := cutDriftGuard(changes, ms, me); err != nil {
		return 0, 0, err
	}
	return ticks(before) - ms, ticks(before+during) - me, nil
}

// copyTitle copies title t's kept cells, in play order, into a temporary
// VOB beside output and returns its path. It checks the free space first,
// reports PhaseCopying, honours ctx, and removes the file on any failure.
func copyTitle(ctx context.Context, t *Title, paths []string, output string, report func(Phase, float64)) (string, error) {
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	need := t.Size + 64<<20
	if free, ok := freeSpace(dir); ok && free < uint64(need) {
		return "", fmt.Errorf("zenvik: copying title %s needs %s in %s, only %s free", t.ID, byteSize(need), dir, byteSize(int64(free)))
	}
	tmp := filepath.Join(dir, "."+strings.TrimSuffix(filepath.Base(output), ".partial")+".title.vob")
	dst, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		dst.Close()
		os.Remove(tmp)
		return "", err
	}
	report(PhaseCopying, 0)
	buf := make([]byte, 1<<20)
	var done int64
	for _, sp := range byteSpans(t.dvd.ranges, t.dvd.files, paths) {
		src, err := os.Open(sp.Path)
		if err != nil {
			return fail(err)
		}
		if _, err := src.Seek(sp.Offset, io.SeekStart); err != nil {
			src.Close()
			return fail(err)
		}
		r := io.LimitReader(src, sp.Length)
		var copied int64
		for {
			if err := ctx.Err(); err != nil {
				src.Close()
				return fail(err)
			}
			n, rerr := r.Read(buf)
			if n > 0 {
				if _, err := dst.Write(buf[:n]); err != nil {
					src.Close()
					return fail(err)
				}
				copied += int64(n)
				done += int64(n)
				report(PhaseCopying, float64(done)/float64(t.Size))
			}
			if errors.Is(rerr, io.EOF) {
				break
			}
			if rerr != nil {
				src.Close()
				return fail(rerr)
			}
		}
		src.Close()
		if copied != sp.Length {
			return fail(fmt.Errorf("zenvik: %s: read %d of %d bytes", sp.Path, copied, sp.Length))
		}
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	report(PhaseCopying, 1)
	return tmp, nil
}

// byteSize renders n bytes with binary units, like "5.9 GiB".
func byteSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

// skipWarning is the rip warning for a dropped stray cell.
func skipWarning(id string, sc SkippedCell) string {
	return fmt.Sprintf("title %s: skipped cell %d (%.1f s at sectors %d–%d, out of order)", id, sc.Cell, sc.Duration.Seconds(), sc.FirstSector, sc.LastSector)
}
```

`diskfree_statfs.go`:

```go
//go:build darwin || linux || freebsd || dragonfly

package zenvik

import "syscall"

// freeBytes reports the bytes available to unprivileged users in dir.
func freeBytes(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
```

`diskfree_windows.go`:

```go
//go:build windows

package zenvik

import (
	"syscall"
	"unsafe"
)

var procGetDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// freeBytes reports the bytes available to the caller in dir.
func freeBytes(dir string) (uint64, bool) {
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var avail uint64
	r, _, _ := procGetDiskFreeSpaceEx.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), 0, 0)
	return avail, r != 0
}
```

`diskfree_other.go`:

```go
//go:build !(darwin || linux || freebsd || dragonfly || windows)

package zenvik

// freeBytes can't measure free space here; the copy path skips the check.
func freeBytes(string) (uint64, bool) { return 0, false }
```

- [ ] **Step 5: Rewrite `dvdJob`**

In `rip.go`:
- Add `PhaseCopying Phase = 6 // copying a DVD title's cells to a temporary file`.
- Add `case PhaseCopying: return "copying"` to `Phase.String`.

In `cmd/zenvik/progress.go`, extend the loop that computes the phase width so it runs through `zenvik.PhaseCopying`, not `zenvik.PhaseSubtitles`.

In `rip_dvd.go`, replace `dvdJob` with:

```go
// dvdJob builds the mux job for DVD title t from the files under root,
// using the title's rip method (whole files, a cut, or a temporary copy).
// The returned cleanup removes every temporary file; call it after muxing.
func dvdJob(ctx context.Context, mk *mux.Mkvmerge, root string, t *Title, output string, dryRun bool, report func(Phase, float64)) (mux.Job, []string, func(), error) {
	none := func() {}
	if t.dvd == nil {
		return mux.Job{}, nil, none, fmt.Errorf("zenvik: title %s has no DVD layout", t.ID)
	}
	info := t.dvd
	paths, err := resolveVOBs(root, info.files)
	if err != nil {
		return mux.Job{}, nil, none, err
	}
	var warnings []string
	for _, sc := range t.SkippedCells {
		warnings = append(warnings, skipWarning(t.ID, sc))
	}
	cleanup := none
	fail := func(err error) (mux.Job, []string, func(), error) {
		cleanup()
		if ctx.Err() != nil {
			return mux.Job{}, nil, none, ctx.Err()
		}
		return mux.Job{}, nil, none, err
	}
	job := mux.Job{Output: output}
	var subSpans []vobsub.Span
	var offset time.Duration
	identifyPath := paths[touchedFiles(info.ranges, info.files)[0]]
	switch info.method {
	case "files":
		for _, i := range touchedFiles(info.ranges, info.files) {
			job.Concat = append(job.Concat, paths[i])
			subSpans = append(subSpans, vobsub.Span{Path: paths[i]})
		}
	case "cut":
		for _, i := range cutGroup(info) {
			job.Concat = append(job.Concat, paths[i])
		}
		identifyPath = job.Concat[0]
		start, end, err := cutPoints(info, paths)
		if err != nil {
			return fail(fmt.Errorf("zenvik: title %s: cannot compute cut points: %w", t.ID, err))
		}
		job.Split = []mux.TimeRange{{Start: start, End: end}}
		offset = start
		subSpans = byteSpans(info.ranges, info.files, paths)
	case "copy":
		if dryRun {
			job.Concat = []string{titleVOBPlaceholder}
			break
		}
		tmp, err := copyTitle(ctx, t, paths, output, report)
		if err != nil {
			return fail(err)
		}
		cleanup = func() { os.Remove(tmp) }
		job.Concat = []string{tmp}
		identifyPath = tmp
		subSpans = []vobsub.Span{{Path: tmp}}
	default:
		return fail(fmt.Errorf("zenvik: title %s has unknown rip method %q", t.ID, info.method))
	}
	ident, err := mk.Identify(ctx, identifyPath)
	if err != nil {
		return fail(err)
	}
	tracks, more := mapDVDTracks(t, ident)
	warnings = append(warnings, more...)
	warnings = append(warnings, ident.Warnings...)
	if len(tracks) == 0 {
		return fail(fmt.Errorf("%w: mkvmerge found no tracks in title %s", ErrMuxFailed, t.ID))
	}
	job.Tracks = tracks
	if len(t.Subtitles) > 0 {
		if dryRun {
			job.Extra = []mux.Input{{Path: subtitlePlaceholder, Tracks: subtitleTracks(t.Subtitles)}}
		} else {
			dir, err := os.MkdirTemp("", "zenvik-subtitles-")
			if err != nil {
				return fail(err)
			}
			prev := cleanup
			cleanup = func() { os.RemoveAll(dir); prev() }
			streams := make([]vobsub.Stream, len(t.Subtitles))
			byID := map[int]SubtitleTrack{}
			for i, s := range t.Subtitles {
				id := int(s.PID - 0xBD20)
				streams[i] = vobsub.Stream{ID: id, Language: info.subLang[s.PID]}
				byID[id] = s
			}
			report(PhaseSubtitles, 0)
			res, err := vobsub.Extract(ctx, vobsub.Params{
				Spans: subSpans, TimeOffset: offset, Cells: info.cells, Streams: streams, Palette: info.palette,
				Width: info.width, Height: info.height, Dir: dir,
				OnProgress: func(done, total int64) {
					if total > 0 {
						report(PhaseSubtitles, float64(done)/float64(total))
					}
				},
			})
			if err != nil {
				return fail(err)
			}
			var kept []SubtitleTrack
			for _, s := range res.Streams {
				kept = append(kept, byID[s.ID])
			}
			for _, s := range t.Subtitles {
				if !slices.ContainsFunc(kept, func(k SubtitleTrack) bool { return k.PID == s.PID }) {
					warnings = append(warnings, fmt.Sprintf("title %s: subtitle stream 0x%04X has no subtitles in this title; skipped", t.ID, s.PID))
				}
			}
			if len(kept) > 0 {
				job.Extra = []mux.Input{{Path: res.IDX, Tracks: subtitleTracks(kept)}}
			}
		}
	}
	if len(t.Chapters) > 0 {
		job.ChapterFile = chapterPlaceholder
		if !dryRun {
			shifted := make([]Chapter, len(t.Chapters))
			for i, c := range t.Chapters {
				shifted[i] = Chapter{Number: c.Number, Start: c.Start + offset}
			}
			p, err := writeChapterFile(shifted)
			if err != nil {
				return fail(err)
			}
			prev := cleanup
			cleanup = func() { os.Remove(p); prev() }
			job.ChapterFile = p
		}
	}
	return job, warnings, cleanup, nil
}
```

Remove the now-unused `source` import from `rip_dvd.go` if nothing else there uses it, and add `time` if it's needed. `writeChapterFile` already writes `CHAPTERnn=HH:MM:SS.mmm`, so the shifted starts land on the group timeline.

- [ ] **Step 6: Run the tests**

Run: `go test -race ./...` and `go vet -tags integration ./...`
Expected: PASS. The existing `TestRipDVDDryRun` (SampleDVD title 01, files) keeps passing unchanged.

Then run `go test -tags integration ./...`. The M5 DVD integration tests must still pass, because the authored M5 disc's title 01 uses the `files` path.

- [ ] **Step 7: Verify and commit**

Run the Global Constraints verification.

```bash
git add rip.go rip_dvd.go rip_dvd_layout.go rip_dvd_layout_internal_test.go rip_dvd_test.go diskfree_*.go internal/mux internal/vobsub internal/testdisc/episodes_integration_test.go cmd/zenvik/progress.go
git commit -m "Rip DVD titles by cutting one run from its VOB group or copying out-of-order cells"
```

---

### Task 5: CLI and JSON for rip methods and skipped cells

**Files:**
- Modify: `cmd/zenvik/info.go`, `cmd/zenvik/rip.go`, `cmd/zenvik/events.go`
- Test: `cmd/zenvik/dvd_test.go`, `cmd/zenvik/jsonl_test.go`

**Interfaces:**
- Consumes: Task 3's `Title.RipMethod`, `Title.SkippedCells`, `zenvik.SkippedCell` and `testdisc.StrayCellDVD`.
- Produces:
  - JSON `rip_method` (omitempty) and `skipped_cells` (omitempty: `[{cell, duration_seconds, first_sector, last_sector}]`).
  - The table note `skipped <n> short cell(s)`.
  - The human `rip` line `  via: cut` or `  via: copy (<size> temporary file)`.
  - On the `--jsonl` `start` event: `rip_method` (omitempty), plus `temp_bytes` (omitempty, copy only, the title's `Size`).

- [ ] **Step 1: Write the failing tests**

In `cmd/zenvik/dvd_test.go`:
- In `TestInfoDVDJSON`, add `RipMethod string \`json:"rip_method"\`` to the decoded title struct. Replace the `"03"` case with:

```go
		case "03":
			if ti.Unsupported != "" || ti.RipMethod != "cut" {
				t.Errorf("03 = %+v, want rippable by cut", ti)
			}
		case "08":
			if ti.RipMethod != "copy" {
				t.Errorf("08 rip_method = %q", ti.RipMethod)
			}
```

- In `TestRipDVDTitleFlag`, change the first check so it uses a disc with `VTS_03_0.IFO` removed, and `--title 7`. Expect exit 1 and `"missing VTS_03_0.IFO"`:

```go
	bad := writeDVD(t)
	if err := os.Remove(filepath.Join(bad, "VIDEO_TS", "VTS_03_0.IFO")); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runCLI("rip", "--title", "7", "-d", t.TempDir(), bad); code != 1 || !strings.Contains(errOut, "missing VTS_03_0.IFO") {
		t.Errorf("unsupported title: code %d, stderr %q", code, errOut)
	}
```

- Add:

```go
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
	for _, ti := range disc.Titles {
		if ti.ID == "01" && (ti.RipMethod != "cut" || len(ti.SkippedCells) != 1 || ti.SkippedCells[0].Cell != 3 ||
			ti.SkippedCells[0].DurationSeconds != 1 || ti.SkippedCells[0].LastSector != 1) {
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
```

Make sure `encoding/json` and `os` are imported in that file. The copy title is 50 sectors (102400 bytes), which `formatSize` renders as `100.0 KiB`.

In `cmd/zenvik/jsonl_test.go`, add:

```go
func TestRipJSONLRipMethod(t *testing.T) {
	root := filepath.Join(t.TempDir(), "STRAY")
	if err := testdisc.StrayCellDVD().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	_, out, _ := runCLI("rip", "--jsonl", "-t", "2", "-d", t.TempDir(), root)
	start := events(t, out)[0]
	if start["event"] != "start" || start["rip_method"] != "copy" || start["temp_bytes"] != float64(50*2048) {
		t.Errorf("start = %v", start)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/zenvik/ -run 'DVD|Skipped|Via|RipMethod'`
Expected: FAIL. There is no `rip_method` JSON, no notes and no `via:` line yet.

- [ ] **Step 3: Implement**

In `cmd/zenvik/info.go`:
- In `notes()`, before the `Filtered` block, add:

```go
	if n := len(t.SkippedCells); n > 0 {
		notes = append(notes, fmt.Sprintf("skipped %d short cell(s)", n))
	}
```

  Use whatever name `notes()` gives its slice: it's `n`, so write `n = append(n, …)`.
- Add these to `jsonTitle`, after `Unsupported`:

```go
	RipMethod       string         `json:"rip_method,omitempty"`
	SkippedCells    []jsonSkipped  `json:"skipped_cells,omitempty"`
```

- Add the type:

```go
type jsonSkipped struct {
	Cell            int     `json:"cell"`
	DurationSeconds float64 `json:"duration_seconds"`
	FirstSector     uint32  `json:"first_sector"`
	LastSector      uint32  `json:"last_sector"`
}
```

- In `toJSONTitle`, set `RipMethod: t.RipMethod`, and append one `jsonSkipped` for each `t.SkippedCells` entry, with `DurationSeconds: sc.Duration.Seconds()`.

In `cmd/zenvik/events.go`, add these to `startEvent`, after `Output`:

```go
	RipMethod       string   `json:"rip_method,omitempty"`
	TempBytes       int64    `json:"temp_bytes,omitempty"`
```

In `cmd/zenvik/rip.go`:
- In the `ev != nil` branch, add `RipMethod: t.RipMethod` to the `startEvent` literal. Add `TempBytes: t.Size` when `t.RipMethod == "copy"`: set it on the struct before calling `ev.start`.
- In the human branch, after the `Ripping` line and before `chosen because`, add:

```go
				switch t.RipMethod {
				case "cut":
					fmt.Fprintln(stdout, "  via: cut")
				case "copy":
					fmt.Fprintf(stdout, "  via: copy (%s temporary file)\n", formatSize(t.Size))
				}
```

- [ ] **Step 4: Run all tests**

Run: `go test -race ./...`
Expected: PASS. If another `cmd/zenvik` test still expects title 03 to be unsupported, change it to match the new behavior and list it in the report.

- [ ] **Step 5: Verify and commit**

Run the Global Constraints verification.

```bash
git add cmd/zenvik
git commit -m "CLI: show DVD rip methods and skipped cells in info, rip and --jsonl"
```

---

### Task 6: Integration tests, docs, spec sync and the real-disc acceptance run

**Files:**
- Modify: `internal/testdisc/episodes.go` (add `AddMixedEpisodeTitles`)
- Create: `rip_dvd_cells_integration_test.go` (`//go:build integration`), `rip_dvd_cells_iso_integration_test.go` (`//go:build integration && darwin`)
- Modify: `README.md`, `docs/superpowers/specs/2026-10-02-zenvik-m6-dvd-cells-design.md`, `docs/superpowers/specs/2026-10-01-zenvik-m5-dvd-design.md`, `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`

**Interfaces:**
- Consumes: Task 1's `AuthorEpisodesDVD`, `FrameColor` and `Dominant`; Task 2's `VMGFile`/`VTSFile` with `VOBUs`; Tasks 3–5's complete DVD path; and the existing integration helpers `identify`, `openDisc`, `mustTitle`, `containerDuration`, `chapterStarts`, `firstTimestamp` and `assertDetached` (root package, integration tags).
- Produces:
  - `func AddMixedEpisodeTitles(dir string) error`. It rewrites `dir/VIDEO_TS`'s IFOs to add two titles. **Title 5** plays episode 1's cells and then the black title's cells, so it's cut with the black cell skipped. **Title 6** plays episode 3's cells and then episode 1's, so it's copied.
  - Docs that match the shipped behavior.

- [ ] **Step 1: Add the title patcher**

Append to `internal/testdisc/episodes.go`. Add `fmt`, `os`, `path/filepath` (already imported) and `github.com/chad3814/zenvik/dvd` to the imports.

```go
// AddMixedEpisodeTitles adds two titles to an AuthorEpisodesDVD folder by
// rewriting its IFOs: title 5 plays episode 1 then the 0.5 s black cell (a
// short trailing stray), and title 6 plays episode 3 then episode 1 (out of
// sector order). The IFOs are re-encoded with VMGFile/VTSFile, which keep
// the title table, attributes, PGCs, chapters and VOBU map that zenvik uses.
func AddMixedEpisodeTitles(dir string) error {
	vd := filepath.Join(dir, "VIDEO_TS")
	vb, err := os.ReadFile(filepath.Join(vd, "VIDEO_TS.IFO"))
	if err != nil {
		return err
	}
	vmg, err := dvd.ParseVMG(vb)
	if err != nil {
		return err
	}
	tb, err := os.ReadFile(filepath.Join(vd, "VTS_01_0.IFO"))
	if err != nil {
		return err
	}
	vts, err := dvd.ParseVTS(tb)
	if err != nil {
		return err
	}
	if len(vts.Titles) < 4 {
		return fmt.Errorf("testdisc: %s is not an AuthorEpisodesDVD folder", dir)
	}
	pgcOf := func(title int) *dvd.PGC { return vts.PGCs[vts.Titles[title-1][0].PGC-1] }
	join := func(parts ...*dvd.PGC) *dvd.PGC {
		p := *parts[0]
		p.Cells, p.Programs = nil, nil
		var total time.Duration
		for _, q := range parts {
			for _, c := range q.Cells {
				p.Cells = append(p.Cells, c)
				p.Programs = append(p.Programs, len(p.Cells))
				total += c.Time.Duration()
			}
		}
		p.Time = dvd.NewTime(total, dvd.Rate30)
		return &p
	}
	for _, parts := range [][]*dvd.PGC{{pgcOf(2), pgcOf(1)}, {pgcOf(4), pgcOf(2)}} {
		p := join(parts...)
		vts.PGCs = append(vts.PGCs, p)
		var ptt []dvd.PartOfTitle
		for k := range p.Programs {
			ptt = append(ptt, dvd.PartOfTitle{PGC: len(vts.PGCs), Program: k + 1})
		}
		vts.Titles = append(vts.Titles, ptt)
		vmg.Titles = append(vmg.Titles, dvd.TitleEntry{Angles: 1, Chapters: len(ptt), TitleSet: 1, TitleSetTitle: len(vts.Titles)})
	}
	if err := os.WriteFile(filepath.Join(vd, "VIDEO_TS.IFO"), VMGFile(vmg), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(vd, "VTS_01_0.IFO"), VTSFile(vts), 0o644)
}
```

Add `"time"` to the imports. Titles 2–4 of the authored disc are the red, green and blue episodes, and title 1 is the black cell. The new titles 5 and 6 come after them.

- [ ] **Step 2: Write the integration tests**

`rip_dvd_cells_integration_test.go`:

```go
//go:build integration

package zenvik_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

const oneFrame = 34 * time.Millisecond

// videoDuration is the video track's span (first to last frame timestamp,
// plus one NTSC frame). The container duration also counts audio, which
// mkvmerge appends after audio at VOB ID changes (Task 1 notes).
func videoDuration(t *testing.T, path string) time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "v.txt")
	if out, err := exec.Command("mkvextract", path, "timestamps_v2", "0:"+txt).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(txt)
	var ts []float64
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n")[1:] {
		if v, err := strconv.ParseFloat(strings.TrimSpace(l), 64); err == nil {
			ts = append(ts, v)
		}
	}
	if len(ts) == 0 {
		t.Fatalf("no video timestamps in %s", path)
	}
	slices.Sort(ts)
	return time.Duration((ts[len(ts)-1]-ts[0])*float64(time.Millisecond)) + 1001*time.Second/30000
}

func episodesDisc(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "EPISODES")
	if err := testdisc.AuthorEpisodesDVD(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err := testdisc.AddMixedEpisodeTitles(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func colours(t *testing.T, path string) (string, string) {
	t.Helper()
	a, err := testdisc.FrameColor(context.Background(), path, false)
	if err != nil {
		t.Fatal(err)
	}
	z, err := testdisc.FrameColor(context.Background(), path, true)
	if err != nil {
		t.Fatal(err)
	}
	return testdisc.Dominant(a), testdisc.Dominant(z)
}

func ripTo(t *testing.T, d *zenvik.Disc, id string) (string, *zenvik.RipResult) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "title.mkv")
	res, err := d.Rip(context.Background(), mustTitle(t, d, id), zenvik.RipOptions{OutputPath: out})
	if err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("output dir holds %v, want only the MKV", ents)
	}
	return out, res
}

func TestRipDVDCut(t *testing.T) {
	d := openDisc(t, episodesDisc(t))
	ti := mustTitle(t, d, "03") // green episode
	if ti.RipMethod != "cut" {
		t.Fatalf("03 method %q", ti.RipMethod)
	}
	out, _ := ripTo(t, d, "03")
	if got := videoDuration(t, out); got < ti.Duration-oneFrame || got > ti.Duration+oneFrame {
		t.Errorf("duration %v, title %v", got, ti.Duration)
	}
	if a, z := colours(t, out); a != "green" || z != "green" {
		t.Errorf("colours %s → %s, want green", a, z)
	}
	starts := chapterStarts(t, out)
	if len(starts) != len(ti.Chapters) {
		t.Fatalf("chapters %v, title %v", starts, ti.Chapters)
	}
	for i, c := range ti.Chapters {
		if diff := starts[i] - c.Start; diff < -oneFrame || diff > oneFrame {
			t.Errorf("chapter %d at %v, IFO %v", i+1, starts[i], c.Start)
		}
	}
	if sub := firstTimestamp(t, out, 2); sub < 900*time.Millisecond || sub > 1100*time.Millisecond {
		t.Errorf("first subtitle at %v, want 1.0 s", sub)
	}
}

func TestRipDVDCutSkipsStray(t *testing.T) {
	d := openDisc(t, episodesDisc(t))
	ti := mustTitle(t, d, "05")
	if ti.RipMethod != "cut" || len(ti.SkippedCells) != 1 {
		t.Fatalf("05 = method %q, skipped %v", ti.RipMethod, ti.SkippedCells)
	}
	out, res := ripTo(t, d, "05")
	if !slices.ContainsFunc(res.Warnings, func(w string) bool { return strings.Contains(w, "skipped cell") }) {
		t.Errorf("warnings = %q", res.Warnings)
	}
	if got := videoDuration(t, out); got < ti.Duration-oneFrame || got > ti.Duration+oneFrame {
		t.Errorf("duration %v, title %v", got, ti.Duration)
	}
	if a, z := colours(t, out); a != "red" || z != "red" {
		t.Errorf("colours %s → %s, want red", a, z)
	}
}

func TestRipDVDCopy(t *testing.T) {
	d := openDisc(t, episodesDisc(t))
	ti := mustTitle(t, d, "06")
	if ti.RipMethod != "copy" {
		t.Fatalf("06 method %q", ti.RipMethod)
	}
	out, _ := ripTo(t, d, "06")
	if got := videoDuration(t, out); got < ti.Duration-2*oneFrame || got > ti.Duration+2*oneFrame {
		t.Errorf("duration %v, title %v", got, ti.Duration)
	}
	if a, z := colours(t, out); a != "blue" || z != "red" {
		t.Errorf("colours %s → %s, want blue → red", a, z)
	}
}
```

`rip_dvd_cells_iso_integration_test.go`:

```go
//go:build integration && darwin

package zenvik_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func TestRipDVDCutISO(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	img, err := testdisc.ISOFromDir(episodesDisc(t), udfimage.Options{Revision: 0x0102, Label: "EPISODES"})
	if err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(t.TempDir(), "episodes.iso")
	if err := os.WriteFile(iso, img, 0o644); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, iso)
	out := filepath.Join(t.TempDir(), "blue.mkv")
	if _, err := d.Rip(context.Background(), mustTitle(t, d, "04"), zenvik.RipOptions{OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	if a, z := colours(t, out); a != "blue" || z != "blue" {
		t.Errorf("colours %s → %s, want blue", a, z)
	}
	assertDetached(t, iso)
}
```

- [ ] **Step 3: Run the integration tests**

Run: `go test -tags integration -run 'DVD|Episode|AuthorDVD' -v ./ ./internal/testdisc/` and then `go test -tags integration ./...`
Expected: PASS. Afterwards, `hdiutil info | grep -c zenvik-mount` prints 0.

If a colour or timing check fails, find the cause: compare `cutPoints` with Task 1's notes. Do not loosen a tolerance.

- [ ] **Step 4: Real-disc acceptance (local only, not committed)**

The project root has `/Users/chad/Projects/zenvik/Swiss Family Robinson (1960) (USA).iso`. Never copy it, commit it, or reference its contents in a test.

```bash
go build -o /tmp/zenvik-acc ./cmd/zenvik
cfg=$(mktemp -d); out=$(mktemp -d)
XDG_CONFIG_HOME=$cfg XDG_STATE_HOME=$cfg /tmp/zenvik-acc info "/Users/chad/Projects/zenvik/Swiss Family Robinson (1960) (USA).iso"
XDG_CONFIG_HOME=$cfg XDG_STATE_HOME=$cfg /tmp/zenvik-acc rip --jsonl -o "$out/sfr.mkv" "/Users/chad/Projects/zenvik/Swiss Family Robinson (1960) (USA).iso" | grep -v '"progress"'
mkvmerge -J "$out/sfr.mkv" | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["container"]["properties"]["duration"]/1e9, [(t["type"],t["properties"].get("language")) for t in d["tracks"]])'
mkvextract "$out/sfr.mkv" chapters --simple - | grep -c NAME
rm -rf "$cfg" "$out" /tmp/zenvik-acc; hdiutil info | grep -c zenvik-mount
```

The run must show all of the following:
- `info` marks `01` with ★, and its notes include `skipped 1 short cell(s)`.
- The `start` event has `"rip_method":"cut"` and `"title":"01"`.
- A `warning` reads `title 01: skipped cell 22 (1.0 s at sectors 0–438, out of order)`.
- The run ends with a `done` event.
- The video track's duration is 7573.13 s ± 0.05 (2:06:14.13 minus the 1 s cell); measure it from `mkvextract … timestamps_v2 0:…`. Also record the container duration.
- There are 19 chapters.
- The tracks are video, two or three audio tracks (eng, spa) and the English subtitle tracks.
- No mounts are left.

Record every observed value in the report. If something differs, investigate and fix it, or report DONE_WITH_CONCERNS with the details.

- [ ] **Step 5: Docs and spec sync**

**README `## DVDs`:**
- Replace the "whole VOB files" limitation paragraph with:

````markdown
zenvik rips any DVD title made of one program chain, angle 1, whatever
its cell layout:

- titles made of whole VOB files go to mkvmerge as they are;
- titles that start or end inside a VOB file (most TV episodes) are cut
  out of their VOB files with mkvmerge `--split`, with no temporary copy;
- titles whose cells play out of disc order are copied, in play order, to
  a temporary `.<name>.title.vob` beside the output first. zenvik checks
  that the output's drive has room for the title plus 64 MiB.

A cell of 1 second or less at a title's start or end that is out of disc
order (often a black filler) is skipped, with a warning.
````

- Document `rip_method` and `skipped_cells` in the JSON paragraph, and the `copying` phase in the `--jsonl` table.

**M6 spec:**
- Set the status to `Implemented`.
- In §3 step 1, change "the files the run touches" to: "from the latest title VOB at or before the run's first file whose first sector starts a VOBU (VTS_nn_1 always does), through the run's last file. Real discs split VOB files at about 1 GB, which can fall inside a VOBU."
- In §3 step 5, change it to: "mkvmerge writes a single `parts:` range under the given name (verified with v102), so no rename is needed."
- Add `## 8. Implementation notes`, with one bullet per deviation and observed fact from Task 1's notes and this task's acceptance run.

**M5 spec:** in §1 Non-goals, change the mid-file bullet to point to the M6 spec ("ripped since M6").

**v1 spec:** in §1 Goals, append "M6 lifts the DVD whole-file limit".

- [ ] **Step 6: Verify and commit**

Run the Global Constraints verification, plus `go test -tags integration ./...`.

```bash
git add internal/testdisc/episodes.go rip_dvd_cells_integration_test.go rip_dvd_cells_iso_integration_test.go README.md docs/superpowers/specs
git commit -m "Test DVD cut and copy rips on authored discs, and document DVD cell layouts"
```
