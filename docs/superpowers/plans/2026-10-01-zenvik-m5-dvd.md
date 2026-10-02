# zenvik M5 (DVD) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Open, rank and remux unencrypted DVD-Video discs (ISO image or `VIDEO_TS` folder) to MKV, for titles made of whole title VOB files.

**Architecture:**
- A new public `dvd` package parses `VIDEO_TS.IFO` and `VTS_nn_0.IFO`.
- `internal/source` recognizes VIDEO_TS trees.
- A new `scan_dvd.go` turns IFO data into `zenvik.Title`s, which are ranked with the existing `internal/rank`.
- `Rip` mounts DVD ISOs like Blu-ray ISOs. It hands mkvmerge the title's VOB files, appended with `+`, plus a generated OGM chapter file and per-track options mapped by MPEG-PS stream ID.

**Tech Stack:** Go 1.27 (no cgo), cobra, go-toml/v2, MKVToolNix ≥ 80, ffmpeg + dvdauthor/spumux (integration tests only).

**Spec:** `docs/superpowers/specs/2026-10-01-zenvik-m5-dvd-design.md` (extends `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`).

## Global Constraints

- Module `github.com/chad3814/zenvik`, Go 1.27, **no cgo**. `CGO_ENABLED=0 go build ./...` must pass.
- Third-party dependencies are limited to `github.com/spf13/cobra` and `github.com/pelletier/go-toml/v2`. The public packages `bluray`, `udf` and `dvd` use only the standard library.
- `dvd` knows nothing about zenvik and does no I/O. Its parsers take the IFO file bytes.
- Errors: return sentinel errors wrapped with `%w`. Library messages start with `zenvik:`, and `dvd` messages start with `dvd:`.
- Never commit copyrighted disc data. Build fixtures with `internal/testdisc`.
- Tests must not touch real user config or state. Set `XDG_CONFIG_HOME` and `XDG_STATE_HOME` to temp dirs; `cmd/zenvik` already has a `TestMain` for this. Detach anything you mount.
- DVD title IDs are two digits, `"01"`…`"99"`.
- Track PIDs for DVD titles are MPEG-PS stream keys:
  - video `0x00E0`
  - MPEG audio `0x00C0+n`
  - AC-3 `0xBD80+n`
  - DTS `0xBD88+n`
  - LPCM `0xBDA0+n`
  - subpicture `0xBD20+n`

  Here `n` is the physical stream number. The key equals `stream_id<<8 | sub_stream_id` for private stream 1, and `stream_id` otherwise.
- Unsupported-title reasons, verbatim:
  - `cells are not contiguous (not supported yet)`
  - `starts or ends mid-file (not supported yet)`
  - `spans several program chains (not supported yet)`
- Languages on `Title` tracks are ISO 639-2/B. IFO codes are ISO 639-1 and are converted with `dvd.Language6392`.
- Verification for every task, unless the task says otherwise:
  - `gofmt -l .` (empty)
  - `go vet ./...`
  - `GOOS=windows go vet ./...`
  - `GOOS=linux go vet ./...`
  - `golangci-lint run`
  - `golangci-lint run --build-tags integration`
  - `go test -race ./...`
  - `CGO_ENABLED=0 go build ./...`
- Commit after each task with a descriptive message. If signing fails, use `git -c commit.gpgsign=false commit …` and say so. Never push.

## Review Focus

1. **Lower-case `video_ts/vts_01_1.vob` names on a case-sensitive file system.** These must open and rip like upper-case ones. Pinned by the T3 `LowerCase` builder option and the T5 `TestOpenDVDLowerCase`.
2. **A title VOB whose size is not a multiple of 2048, or a missing VOB.** Only that title set's titles should be filtered, each with a reason, and the rest of the disc still opens. Pinned by T5 `TestOpenDVDBadTitleSet`.
3. **A missing or corrupt `VTS_nn_0.IFO`.** Only that set's titles should be filtered, and the disc still opens. Pinned by T5 `TestOpenDVDBadTitleSet`.
4. **`--title 3` typed without zero padding.** It should select title `03`. Pinned by T5 `TestDVDTitleLookup` and T7 `TestRipDVDTitleFlag`.
5. **A hybrid disc with both `BDMV` and `VIDEO_TS`.** Blu-ray wins. Pinned by T4 `TestOpenHybridPrefersBluray`.

## Waves (dependencies)

- **Wave A**, in parallel:
  - T1: verify mkvmerge and the dvdauthor fixture.
  - T2: the `dvd` parsers plus the IFO encoders.
  - T4: source detection.
- **Wave B:** T3, the synthetic DVD builder. It needs T2.
- **Wave C:** T5, the DVD scan. It needs T2, T3 and T4.
- **Wave D**, in parallel:
  - T6: DVD rip. It needs T5.
  - T7: CLI. It needs T5.
- **Wave D2:** T10, DVD subtitles (VobSub extraction). It needs T5 and T6, and can overlap T7.
- **Wave E:** T8, integration tests and CI. It needs T1, T6, T7 and T10.
- **Wave F:** T9, docs and spec sync.

---

### Task 1: Verify mkvmerge's DVD behavior and build the authored-DVD fixture

**Files:**
- Create: `internal/testdisc/dvdauthor.go`
- Create: `internal/testdisc/dvdauthor_integration_test.go`
- Create: `docs/superpowers/notes/2026-10-01-m5-mkvmerge-dvd.md`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `func AuthorDVD(ctx context.Context, dir string, seconds int) error`. It writes `dir/VIDEO_TS` with `VIDEO_TS.IFO`, `VTS_01_0.IFO`, `VTS_01_1.VOB` and `VTS_01_2.VOB`. The disc has one title with NTSC 4:3 MPEG-2 video, AC-3 stereo audio in eng and fre, one eng subtitle stream, and 3 chapters.
  - `func SplitTitleVOB(dir string) error`.
  - A notes file recording the observed mkvmerge behavior that later tasks rely on.

This task is a gate. Later tasks assume all of the following. If the test shows otherwise, report **BLOCKED** with the observed values, and do not change other files.
- (a) `mkvmerge -J` on a VOB reports:
  - video as `stream_id` 224
  - AC-3 as `stream_id` 189 with `sub_stream_id` 128+n
  - VobSub as `stream_id` 189 with `sub_stream_id` 32+n
- (b) Appending split VOBs with `+` gives the full duration (±0.25 s) and all tracks.

Palette handling (c) is only recorded, not gated.

- [ ] **Step 1: Install the tools**

Run: `brew install dvdauthor` (it provides `dvdauthor` and `spumux`). Then `dvdauthor -h 2>&1 | head -1` and `mkvmerge --version`.
Expected: a dvdauthor version line, and `mkvmerge v102.0 …` (or whatever is installed; note it).

- [ ] **Step 2: Write the failing integration test**

`internal/testdisc/dvdauthor_integration_test.go`:

```go
//go:build integration

package testdisc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

type mkvJSON struct {
	Container struct {
		Properties struct {
			Duration int64 `json:"duration"`
		} `json:"properties"`
	} `json:"container"`
	Tracks []struct {
		Type       string `json:"type"`
		Codec      string `json:"codec"`
		Properties struct {
			StreamID         int    `json:"stream_id"`
			SubStreamID      int    `json:"sub_stream_id"`
			CodecPrivateData string `json:"codec_private_data"`
		} `json:"properties"`
	} `json:"tracks"`
}

func mkvmergeJSON(t *testing.T, path string) mkvJSON {
	t.Helper()
	out, err := exec.Command("mkvmerge", "-J", path).Output()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
		t.Fatalf("mkvmerge -J %s: %v", path, err)
	}
	var j mkvJSON
	if err := json.Unmarshal(out, &j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestAuthorDVDAndMkvmerge(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "MY_DVD")
	if err := AuthorDVD(context.Background(), dir, 15); err != nil {
		t.Fatal(err)
	}
	vts := filepath.Join(dir, "VIDEO_TS")
	for _, n := range []string{"VIDEO_TS.IFO", "VTS_01_0.IFO", "VTS_01_1.VOB", "VTS_01_2.VOB"} {
		st, err := os.Stat(filepath.Join(vts, n))
		if err != nil {
			t.Fatal(err)
		}
		if st.Size()%2048 != 0 || st.Size() == 0 {
			t.Errorf("%s size %d is not a positive multiple of 2048", n, st.Size())
		}
	}

	// (a) stream IDs as mkvmerge reports them for a VOB.
	in := mkvmergeJSON(t, filepath.Join(vts, "VTS_01_1.VOB"))
	var got []string
	for _, tr := range in.Tracks {
		got = append(got, tr.Type+" "+itoa(tr.Properties.StreamID)+" "+itoa(tr.Properties.SubStreamID))
	}
	sort.Strings(got)
	want := []string{"audio 189 128", "audio 189 129", "subtitles 189 32", "video 224 0"}
	t.Logf("tracks: %q", got)
	if len(got) != len(want) {
		t.Fatalf("tracks = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tracks = %q, want %q", got, want)
			break
		}
	}

	// (b) appending the split VOBs keeps the full duration and every track.
	out := filepath.Join(t.TempDir(), "out.mkv")
	cmd := exec.Command("mkvmerge", "-o", out, filepath.Join(vts, "VTS_01_1.VOB"), "+", filepath.Join(vts, "VTS_01_2.VOB"))
	if b, err := cmd.CombinedOutput(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("mkvmerge: %v\n%s", err, b)
		}
	}
	res := mkvmergeJSON(t, out)
	secs := float64(res.Container.Properties.Duration) / 1e9
	t.Logf("muxed duration: %.3f s", secs)
	if math.Abs(secs-15) > 0.25 {
		t.Errorf("duration = %.3f s, want 15 ± 0.25", secs)
	}
	if len(res.Tracks) != 4 {
		t.Errorf("muxed %d tracks, want 4", len(res.Tracks))
	}

	// (c) record what mkvmerge writes as the VobSub codec private data.
	for _, tr := range res.Tracks {
		if tr.Type == "subtitles" {
			b, _ := hex.DecodeString(tr.Properties.CodecPrivateData)
			t.Logf("VobSub codec private (%s): %q", tr.Codec, b)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
```

- [ ] **Step 3: Run it to see it fail**

Run: `go test -tags integration -run TestAuthorDVDAndMkvmerge -v ./internal/testdisc/`
Expected: FAIL to compile with `undefined: AuthorDVD`.

- [ ] **Step 4: Implement the fixture builder**

`internal/testdisc/dvdauthor.go`:

```go
package testdisc

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AuthorDVD builds a real, playable DVD-Video folder at dir (dir/VIDEO_TS)
// with ffmpeg, spumux and dvdauthor: one title of seconds (at least 15) of
// NTSC 4:3 MPEG-2 video with two AC-3 stereo tracks (English at 440 Hz,
// French at 660 Hz), one English subtitle stream shown from 2 s to 6 s, and
// chapters requested at 0, 1/3 and 2/3 of the length (dvdauthor moves them
// to the nearest VOBU). The title VOB is then split by SplitTitleVOB.
func AuthorDVD(ctx context.Context, dir string, seconds int) error {
	if seconds < 15 {
		return fmt.Errorf("testdisc: AuthorDVD needs at least 15 seconds, got %d", seconds)
	}
	work, err := os.MkdirTemp("", "zenvik-dvd-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	if err := runTool(ctx, work, nil, nil, "ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=720x480:rate=30000/1001:duration=%d", seconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%d", seconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=660:sample_rate=48000:duration=%d", seconds),
		"-map", "0", "-map", "1", "-map", "2", "-target", "ntsc-dvd", "-aspect", "4:3",
		"-c:a", "ac3", "-b:a", "192k", "-ac", "2", "movie.mpg"); err != nil {
		return err
	}
	if err := runTool(ctx, work, nil, nil, "ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black@0.0:s=720x480,format=rgba,drawbox=x=200:y=400:w=320:h=40:color=white@1.0:t=fill",
		"-frames:v", "1", "sub.png"); err != nil {
		return err
	}
	spu := `<subpictures format="NTSC"><stream>` +
		`<spu start="00:00:02.00" end="00:00:06.00" image="sub.png" force="no"/>` +
		`</stream></subpictures>`
	if err := os.WriteFile(filepath.Join(work, "sub.xml"), []byte(spu), 0o644); err != nil {
		return err
	}
	in, err := os.Open(filepath.Join(work, "movie.mpg"))
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(filepath.Join(work, "movie_sub.mpg"))
	if err != nil {
		return err
	}
	err = runTool(ctx, work, in, out, "spumux", "-s", "0", "sub.xml")
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	xml := fmt.Sprintf(`<dvdauthor dest=%q>
  <vmgm />
  <titleset>
    <titles>
      <video format="ntsc" aspect="4:3" />
      <audio lang="en" />
      <audio lang="fr" />
      <subpicture lang="en" />
      <pgc>
        <vob file="movie_sub.mpg" chapters="0,%d,%d" />
      </pgc>
    </titles>
  </titleset>
</dvdauthor>
`, abs, seconds/3, 2*seconds/3)
	if err := os.WriteFile(filepath.Join(work, "dvd.xml"), []byte(xml), 0o644); err != nil {
		return err
	}
	if err := runTool(ctx, work, nil, nil, "dvdauthor", "-x", "dvd.xml"); err != nil {
		return err
	}
	return SplitTitleVOB(dir)
}

// SplitTitleVOB splits dir/VIDEO_TS/VTS_01_1.VOB at the NAV pack nearest its
// middle into VTS_01_1.VOB and VTS_01_2.VOB. Cell sector addresses in the
// IFO are relative to the concatenated title VOBs, so the IFO stays valid.
func SplitTitleVOB(dir string) error {
	p := filepath.Join(dir, "VIDEO_TS", "VTS_01_1.VOB")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	n := len(b) / 2048
	best := -1
	for s := 1; s < n; s++ {
		if isNavPack(b[s*2048:]) && (best < 0 || absInt(s-n/2) < absInt(best-n/2)) {
			best = s
		}
	}
	if best < 0 {
		return fmt.Errorf("testdisc: %s has no NAV pack to split at", p)
	}
	if err := os.WriteFile(filepath.Join(dir, "VIDEO_TS", "VTS_01_2.VOB"), b[best*2048:], 0o644); err != nil {
		return err
	}
	return os.WriteFile(p, b[:best*2048], 0o644)
}

// isNavPack reports whether p starts with an MPEG-2 pack header followed
// directly by a system header, which is how every DVD NAV pack begins.
func isNavPack(p []byte) bool {
	return len(p) >= 18 && bytes.Equal(p[0:4], []byte{0, 0, 1, 0xBA}) && bytes.Equal(p[14:18], []byte{0, 0, 1, 0xBB})
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// runTool runs name in dir with VIDEO_FORMAT=NTSC, optionally wiring stdin
// and stdout, and folds the tool's stderr into any error.
func runTool(ctx context.Context, dir string, stdin *os.File, stdout *os.File, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VIDEO_FORMAT=NTSC")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = &stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, stderr.Bytes())
	}
	return nil
}
```

- [ ] **Step 5: Run the test and read the logs**

Run: `go test -tags integration -run TestAuthorDVDAndMkvmerge -v ./internal/testdisc/`
Expected: PASS. The `t.Logf` lines show the tracks, the muxed duration and the VobSub codec private data.

If a tool invocation fails because of a version difference (an ffmpeg filter or spumux/dvdauthor syntax), fix the invocation minimally and record it under Deviations. If (a) or (b) fails, stop and report BLOCKED with the log output.

- [ ] **Step 6: Record the results**

`docs/superpowers/notes/2026-10-01-m5-mkvmerge-dvd.md` must contain these sections, filled in with the values you observed:

```markdown
# M5: mkvmerge DVD behavior (verified 2026-10-01)

- Tools: mkvmerge <version>, ffmpeg <version>, dvdauthor <version>, OS <os>.
- (a) `mkvmerge -J VTS_01_1.VOB` tracks (type, stream_id, sub_stream_id): <paste the logged list>.
- (b) `VTS_01_1.VOB + VTS_01_2.VOB` muxed duration: <seconds> s for a 15 s title; tracks: <n>.
- (c) VobSub codec private data after muxing: <paste>. Conclusion: one of
  "mkvmerge writes the DVD palette", "mkvmerge writes a default palette",
  or "no palette".
- Consequence for Task 6/9: <one sentence: no palette work needed / palette
  limitation to document>.
```

- [ ] **Step 7: Run the global verification and commit**

Run the Global Constraints verification commands.

```bash
git add internal/testdisc/dvdauthor.go internal/testdisc/dvdauthor_integration_test.go docs/superpowers/notes/2026-10-01-m5-mkvmerge-dvd.md
git commit -m "Add an authored-DVD test fixture and record mkvmerge's DVD behavior"
```

---

### Task 2: `dvd` package — IFO parsers, and IFO encoders for tests

**Files:**
- Create: `dvd/ifo.go`: types, `ParseVMG` and `ParseVTS`.
- Create: `dvd/time.go`: `Time`, `FrameRate` and `NewTime`.
- Create: `dvd/lang.go`: `Language6392`.
- Create: `internal/testdisc/dvdifo.go`: `VMGFile` and `VTSFile` encoders.
- Test: `dvd/ifo_test.go` (package `dvd_test`), `dvd/fuzz_test.go`.

**Interfaces:**
- Consumes: nothing.
- Produces (exact names; later tasks depend on them):
  - `dvd.ErrCorrupt`
  - `dvd.ParseVMG([]byte) (*dvd.VMG, error)`
  - `dvd.ParseVTS([]byte) (*dvd.VTS, error)`
  - the types `VMG`, `TitleEntry`, `VTS`, `VideoAttributes`, `AudioAttributes`, `SubpictureAttributes`, `PartOfTitle`, `PGC`, `AudioControl`, `SubpictureControl`, `Cell`, `BlockMode` and `Time`, with the constants below
  - `dvd.NewTime(time.Duration, dvd.FrameRate) dvd.Time`
  - `(dvd.Time).Duration() time.Duration`
  - `dvd.Language6392(string) string`
  - `testdisc.VMGFile(*dvd.VMG) []byte`
  - `testdisc.VTSFile(*dvd.VTS) []byte`

IFO layout reference: offsets are in bytes, multi-byte fields are big-endian, and a "sector" is 2048 bytes.
- **VMGI:**
  - magic `DVDVIDEO-VMG` at 0
  - title set count u16 at 0x3E
  - TT_SRPT sector u32 at 0xC4
- **TT_SRPT:**
  - count u16 at 0
  - end address u32 at 4 (the last byte, relative to the table start)
  - 12-byte entries from 8: `[1]` angles, `[2:4]` chapters, `[6]` VTSN, `[7]` VTS_TTN, `[8:12]` VTS start sector
- **VTSI:**
  - magic `DVDVIDEO-VTS` at 0
  - VTS_PTT_SRPT sector u32 at 0xC8
  - VTS_PGCI sector u32 at 0xCC
  - video attributes, 2 bytes at 0x200
  - audio count u16 at 0x202, then 8 × 8-byte audio attributes at 0x204
  - subpicture count u16 at 0x254, then 32 × 6-byte attributes at 0x256
- **VTS_PTT_SRPT:**
  - count u16 at 0
  - end u32 at 4
  - u32 offsets at 8+4i (relative to the table start) to each title's 4-byte entries: PGCN u16, PGN u16
- **VTS_PGCI:**
  - count u16 at 0
  - end u32 at 4
  - 8-byte search pointers at 8+8i, with the PGC offset u32 at +4 (relative to the PGCI start)
- **PGC:**
  - `[2]` programs, `[3]` cells, `[4:8]` playback time
  - audio control 8 × 2 bytes at 0x0C (byte0: bit7 available, bits 2–0 stream)
  - subpicture control 32 × 4 bytes at 0x1C (byte0: bit7 available, bits 4–0 stream for 4:3; byte1 wide; byte2 letterbox; byte3 pan-scan, each bits 4–0)
  - palette 16 × u32 at 0xA4
  - u16 offsets (relative to the PGC start) at 0xE6 (program map: one byte per program, its entry cell, from 1), 0xE8 (cell playback, 24 bytes per cell) and 0xEA (cell position, 4 bytes per cell)
- **Cell playback:**
  - `[0]` bits 7–6 block mode, bits 5–4 block type (1 = angle)
  - `[4:8]` time
  - `[8:12]` first sector
  - `[20:24]` last sector
- **Cell position:** `[0:2]` VOB ID, `[3]` cell ID.
- **Time:**
  - BCD hours, minutes and seconds.
  - Byte 3: bits 7–6 are the rate (1 = 25 fps, 3 = 29.97 fps, 0 only for an all-zero time); bits 5–0 are BCD frames.
- **Video attributes:**
  - byte0: bits 7–6 coding (0 MPEG-1, 1 MPEG-2), bits 5–4 standard (0 NTSC, 1 PAL), bits 3–2 aspect (0 = 4:3, 3 = 16:9)
  - byte1: bits 3–2 picture size (0 = 720, 1 = 704, 2 = 352 at full height, 3 = 352 at half height)
- **Audio attributes:**
  - byte0: bits 7–5 coding (0 AC-3, 2 MPEG-1, 3 MPEG-2 ext, 4 LPCM, 6 DTS), bits 3–2 language type (1 = code present)
  - byte1: bits 5–4 sample rate (0 = 48 kHz, 1 = 96 kHz), bits 2–0 channels−1
  - `[2:4]` language
  - `[5]` code extension
- **Subpicture attributes:**
  - byte0: bits 1–0 language type (1 = present)
  - `[2:4]` language
  - `[5]` code extension

- [ ] **Step 1: Write the time and language code**

`dvd/time.go`:

```go
package dvd

import (
	"fmt"
	"time"
)

// FrameRate is the frame rate of a playback time.
type FrameRate int

// Frame rates.
const (
	Rate25 FrameRate = 25 // PAL
	Rate30 FrameRate = 30 // NTSC, 29.97 frames per second
)

// Time is a BCD playback time from an IFO file.
type Time struct {
	Hours, Minutes, Seconds, Frames int
	Rate                            FrameRate // 0 only for an all-zero time
}

// Duration converts t to a time.Duration. NTSC frames last 1001/30000 s.
func (t Time) Duration() time.Duration {
	d := time.Duration(t.Hours)*time.Hour + time.Duration(t.Minutes)*time.Minute + time.Duration(t.Seconds)*time.Second
	switch t.Rate {
	case Rate25:
		d += time.Duration(t.Frames) * 40 * time.Millisecond
	case Rate30:
		d += time.Duration(t.Frames) * 1001 * time.Second / 30000
	}
	return d
}

// NewTime returns the Time for d at rate r, rounding down to a whole frame.
// Durations of 100 hours or more are clamped to 99:59:59.
func NewTime(d time.Duration, r FrameRate) Time {
	if d < 0 {
		d = 0
	}
	if d >= 100*time.Hour {
		d = 100*time.Hour - time.Second
	}
	t := Time{Rate: r}
	t.Hours = int(d / time.Hour)
	d -= time.Duration(t.Hours) * time.Hour
	t.Minutes = int(d / time.Minute)
	d -= time.Duration(t.Minutes) * time.Minute
	t.Seconds = int(d / time.Second)
	d -= time.Duration(t.Seconds) * time.Second
	switch r {
	case Rate25:
		t.Frames = int(d / (40 * time.Millisecond))
	case Rate30:
		t.Frames = int(d * 30000 / (1001 * time.Second))
	}
	return t
}

func parseTime(b []byte) (Time, error) {
	bcd := func(x byte) (int, bool) {
		hi, lo := x>>4, x&0x0F
		return int(hi)*10 + int(lo), hi <= 9 && lo <= 9
	}
	h, ok1 := bcd(b[0])
	m, ok2 := bcd(b[1])
	s, ok3 := bcd(b[2])
	f, ok4 := bcd(b[3] & 0x3F)
	if !ok1 || !ok2 || !ok3 || !ok4 || m > 59 || s > 59 {
		return Time{}, fmt.Errorf("%w: bad BCD time % X", ErrCorrupt, b[:4])
	}
	t := Time{Hours: h, Minutes: m, Seconds: s, Frames: f}
	switch b[3] >> 6 {
	case 1:
		t.Rate = Rate25
	case 3:
		t.Rate = Rate30
	case 0:
		if t != (Time{}) {
			return Time{}, fmt.Errorf("%w: time % X has no frame rate", ErrCorrupt, b[:4])
		}
		return t, nil
	default:
		return Time{}, fmt.Errorf("%w: time % X has an invalid frame rate", ErrCorrupt, b[:4])
	}
	if f >= int(t.Rate) {
		return Time{}, fmt.Errorf("%w: time % X has frame %d at %d fps", ErrCorrupt, b[:4], f, t.Rate)
	}
	return t, nil
}
```

`dvd/lang.go`:

```go
package dvd

import "strings"

// iso6391to6392 maps ISO 639-1 codes to ISO 639-2/B, plus the obsolete
// codes iw, in and ji that older DVDs use.
var iso6391to6392 = func() map[string]string {
	const table = `aa:aar ab:abk ae:ave af:afr ak:aka am:amh an:arg ar:ara as:asm av:ava ay:aym az:aze
ba:bak be:bel bg:bul bh:bih bi:bis bm:bam bn:ben bo:tib br:bre bs:bos ca:cat ce:che ch:cha co:cos
cr:cre cs:cze cu:chu cv:chv cy:wel da:dan de:ger dv:div dz:dzo ee:ewe el:gre en:eng eo:epo es:spa
et:est eu:baq fa:per ff:ful fi:fin fj:fij fo:fao fr:fre fy:fry ga:gle gd:gla gl:glg gn:grn gu:guj
gv:glv ha:hau he:heb hi:hin ho:hmo hr:hrv ht:hat hu:hun hy:arm hz:her ia:ina id:ind ie:ile ig:ibo
ii:iii ik:ipk io:ido is:ice it:ita iu:iku ja:jpn jv:jav ka:geo kg:kon ki:kik kj:kua kk:kaz kl:kal
km:khm kn:kan ko:kor kr:kau ks:kas ku:kur kv:kom kw:cor ky:kir la:lat lb:ltz lg:lug li:lim ln:lin
lo:lao lt:lit lu:lub lv:lav mg:mlg mh:mah mi:mao mk:mac ml:mal mn:mon mr:mar ms:may mt:mlt my:bur
na:nau nb:nob nd:nde ne:nep ng:ndo nl:dut nn:nno no:nor nr:nbl nv:nav ny:nya oc:oci oj:oji om:orm
or:ori os:oss pa:pan pi:pli pl:pol ps:pus pt:por qu:que rm:roh rn:run ro:rum ru:rus rw:kin sa:san
sc:srd sd:snd se:sme sg:sag si:sin sk:slo sl:slv sm:smo sn:sna so:som sq:alb sr:srp ss:ssw st:sot
su:sun sv:swe sw:swa ta:tam te:tel tg:tgk th:tha ti:tir tk:tuk tl:tgl tn:tsn to:ton tr:tur ts:tso
tt:tat tw:twi ty:tah ug:uig uk:ukr ur:urd uz:uzb ve:ven vi:vie vo:vol wa:wln wo:wol xh:xho yi:yid
yo:yor za:zha zh:chi zu:zul iw:heb in:ind ji:yid`
	m := map[string]string{}
	for _, f := range strings.Fields(table) {
		k, v, _ := strings.Cut(f, ":")
		m[k] = v
	}
	return m
}()

// Language6392 returns the ISO 639-2/B code for an ISO 639-1 code from an
// IFO file (case-insensitive), or "" if code is empty or unknown.
func Language6392(code string) string {
	return iso6391to6392[strings.ToLower(code)]
}
```

- [ ] **Step 2: Write the types and parsers**

`dvd/ifo.go`:

```go
// Package dvd parses DVD-Video navigation files: VIDEO_TS.IFO (the video
// manager) and VTS_nn_0.IFO (video title set information). It does no
// I/O: the parsers take an IFO file's bytes.
package dvd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// ErrCorrupt reports a malformed IFO file.
var ErrCorrupt = errors.New("dvd: corrupt IFO")

const sectorSize = 2048

// VMG is the video manager (VIDEO_TS.IFO).
type VMG struct {
	TitleSets int          // number of video title sets
	Titles    []TitleEntry // title search pointer table, in title order
}

// TitleEntry is one title of the title search pointer table.
type TitleEntry struct {
	Angles        int    // 1..9
	Chapters      int    // parts of title
	TitleSet      int    // video title set number, 1..99
	TitleSetTitle int    // the title's number within its title set, from 1
	StartSector   uint32 // where the title set starts on the disc
}

// VTS is a video title set (VTS_nn_0.IFO).
type VTS struct {
	Video       VideoAttributes
	Audio       []AudioAttributes      // logical audio streams, at most 8
	Subpictures []SubpictureAttributes // logical subpicture streams, at most 32
	Titles      [][]PartOfTitle        // [title-1][chapter-1]
	PGCs        []*PGC                 // PGC number n is PGCs[n-1]
}

// VideoCoding is the video compression.
type VideoCoding int

// Video codings.
const (
	MPEG1 VideoCoding = 1
	MPEG2 VideoCoding = 2
)

// VideoStandard is NTSC or PAL.
type VideoStandard int

// Video standards.
const (
	NTSC VideoStandard = 1
	PAL  VideoStandard = 2
)

// AspectRatio is the display aspect ratio.
type AspectRatio int

// Aspect ratios.
const (
	Aspect4x3  AspectRatio = 1
	Aspect16x9 AspectRatio = 2
)

// VideoAttributes describe the title set's video stream.
type VideoAttributes struct {
	Coding        VideoCoding
	Standard      VideoStandard
	Aspect        AspectRatio
	Width, Height int
}

// AudioCoding is an audio stream's compression.
type AudioCoding int

// Audio codings; 0 is a reserved or unknown value.
const (
	AC3        AudioCoding = 1
	MPEG1Audio AudioCoding = 2
	MPEG2Audio AudioCoding = 3
	LPCM       AudioCoding = 4
	DTS        AudioCoding = 5
)

// AudioExtension is an audio stream's code extension.
type AudioExtension int

// Audio code extensions; 0 is unspecified.
const (
	AudioNormal            AudioExtension = 1
	AudioVisuallyImpaired  AudioExtension = 2
	AudioDirectorsComments AudioExtension = 3
	AudioAlternateComments AudioExtension = 4
)

// AudioAttributes describe one logical audio stream.
type AudioAttributes struct {
	Coding        AudioCoding
	Channels      int    // 1..8
	SampleRate    int    // Hz: 48000 or 96000; 0 if reserved
	Language      string // ISO 639-1 as stored, lower case; "" if none
	CodeExtension AudioExtension
}

// SubpictureExtension is a subpicture stream's code extension.
type SubpictureExtension int

// Subpicture code extensions; 0 is unspecified.
const (
	SubpictureNormal                     SubpictureExtension = 1
	SubpictureLarge                      SubpictureExtension = 2
	SubpictureChildren                   SubpictureExtension = 3
	SubpictureNormalCaptions             SubpictureExtension = 5
	SubpictureLargeCaptions              SubpictureExtension = 6
	SubpictureChildrensCaptions          SubpictureExtension = 7
	SubpictureForced                     SubpictureExtension = 9
	SubpictureDirectorsComments          SubpictureExtension = 13
	SubpictureLargeDirectorsComments     SubpictureExtension = 14
	SubpictureChildrensDirectorsComments SubpictureExtension = 15
)

// SubpictureAttributes describe one logical subpicture stream.
type SubpictureAttributes struct {
	Language      string // ISO 639-1 as stored, lower case; "" if none
	CodeExtension SubpictureExtension
}

// PartOfTitle locates a chapter: a program of a PGC, both numbered from 1.
type PartOfTitle struct {
	PGC, Program int
}

// PGC is a program chain.
type PGC struct {
	Time        Time
	Audio       [8]AudioControl
	Subpictures [32]SubpictureControl
	Palette     [16]uint32 // 0x00YYCrCb
	Programs    []int      // entry cell of each program, from 1
	Cells       []Cell
}

// AudioControl maps a logical audio stream to its physical stream number.
type AudioControl struct {
	Available bool
	Stream    int // 0..7
}

// SubpictureControl maps a logical subpicture stream to a physical stream
// number for each display mode (0..31).
type SubpictureControl struct {
	Available                           bool
	Stream4x3, Wide, Letterbox, PanScan int
}

// BlockMode is a cell's position in a block (an angle block, for example).
type BlockMode int

// Block modes.
const (
	NotInBlock   BlockMode = 0
	FirstInBlock BlockMode = 1
	InBlock      BlockMode = 2
	LastInBlock  BlockMode = 3
)

// Cell is one cell of a PGC.
type Cell struct {
	BlockMode   BlockMode
	AngleBlock  bool // the cell's block is an angle block
	Time        Time
	FirstSector uint32 // relative to the start of the title set's title VOBs
	LastSector  uint32
	VOBID       int
	CellID      int
}

func be16(b []byte, off int) int    { return int(binary.BigEndian.Uint16(b[off:])) }
func be32(b []byte, off int) uint32 { return binary.BigEndian.Uint32(b[off:]) }

func corrupt(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrCorrupt}, args...)...)
}

// table returns the table that starts at sector, cut to its end address.
func table(b []byte, sector uint32, name string) ([]byte, error) {
	off := int64(sector) * sectorSize
	if sector == 0 || off+8 > int64(len(b)) {
		return nil, corrupt("%s at sector %d is outside the file", name, sector)
	}
	t := b[off:]
	end := int64(be32(t, 4))
	if end < 7 || end >= int64(len(t)) {
		return nil, corrupt("%s end address %d is outside the file", name, end)
	}
	return t[:end+1], nil
}

// ParseVMG parses VIDEO_TS.IFO.
func ParseVMG(b []byte) (*VMG, error) {
	if len(b) < 0x100 || string(b[:12]) != "DVDVIDEO-VMG" {
		return nil, corrupt("not a video manager (VIDEO_TS.IFO)")
	}
	v := &VMG{TitleSets: be16(b, 0x3E)}
	t, err := table(b, be32(b, 0xC4), "title table")
	if err != nil {
		return nil, err
	}
	n := be16(t, 0)
	if n < 1 || n > 99 {
		return nil, corrupt("title table has %d titles", n)
	}
	if len(t) < 8+12*n {
		return nil, corrupt("title table is truncated")
	}
	for i := 0; i < n; i++ {
		e := t[8+12*i:]
		te := TitleEntry{Angles: int(e[1]), Chapters: be16(e, 2), TitleSet: int(e[6]), TitleSetTitle: int(e[7]), StartSector: be32(e, 8)}
		if te.Angles < 1 || te.Angles > 9 || te.TitleSet < 1 || te.TitleSet > 99 || te.TitleSetTitle < 1 {
			return nil, corrupt("title %d: angles %d, title set %d, title %d", i+1, te.Angles, te.TitleSet, te.TitleSetTitle)
		}
		v.Titles = append(v.Titles, te)
	}
	return v, nil
}

// ParseVTS parses a VTS_nn_0.IFO file.
func ParseVTS(b []byte) (*VTS, error) {
	if len(b) < 0x318 || string(b[:12]) != "DVDVIDEO-VTS" {
		return nil, corrupt("not a video title set (VTS_nn_0.IFO)")
	}
	v := &VTS{}
	var err error
	if v.Video, err = parseVideo(b[0x200:]); err != nil {
		return nil, err
	}
	na := be16(b, 0x202)
	if na > 8 {
		return nil, corrupt("%d audio streams", na)
	}
	for i := 0; i < na; i++ {
		v.Audio = append(v.Audio, parseAudio(b[0x204+8*i:]))
	}
	ns := be16(b, 0x254)
	if ns > 32 {
		return nil, corrupt("%d subpicture streams", ns)
	}
	for i := 0; i < ns; i++ {
		v.Subpictures = append(v.Subpictures, parseSubpicture(b[0x256+6*i:]))
	}
	ptt, err := table(b, be32(b, 0xC8), "part-of-title table")
	if err != nil {
		return nil, err
	}
	if v.Titles, err = parsePTT(ptt); err != nil {
		return nil, err
	}
	pgci, err := table(b, be32(b, 0xCC), "program chain table")
	if err != nil {
		return nil, err
	}
	if v.PGCs, err = parsePGCI(pgci); err != nil {
		return nil, err
	}
	for ti, chapters := range v.Titles {
		for ci, p := range chapters {
			if p.PGC > len(v.PGCs) || p.Program > len(v.PGCs[p.PGC-1].Programs) {
				return nil, corrupt("title %d chapter %d points at PGC %d program %d", ti+1, ci+1, p.PGC, p.Program)
			}
		}
	}
	return v, nil
}

func parseVideo(a []byte) (VideoAttributes, error) {
	var v VideoAttributes
	switch a[0] >> 6 {
	case 0:
		v.Coding = MPEG1
	case 1:
		v.Coding = MPEG2
	default:
		return v, corrupt("video coding %d", a[0]>>6)
	}
	full := 480
	switch (a[0] >> 4) & 3 {
	case 0:
		v.Standard = NTSC
	case 1:
		v.Standard, full = PAL, 576
	default:
		return v, corrupt("video standard %d", (a[0]>>4)&3)
	}
	switch (a[0] >> 2) & 3 {
	case 0:
		v.Aspect = Aspect4x3
	case 3:
		v.Aspect = Aspect16x9
	default:
		return v, corrupt("aspect ratio %d", (a[0]>>2)&3)
	}
	switch (a[1] >> 2) & 3 {
	case 0:
		v.Width, v.Height = 720, full
	case 1:
		v.Width, v.Height = 704, full
	case 2:
		v.Width, v.Height = 352, full
	case 3:
		v.Width, v.Height = 352, full/2
	}
	return v, nil
}

func language(present bool, b []byte) string {
	if !present {
		return ""
	}
	s := strings.ToLower(string(b[:2]))
	for _, c := range s {
		if c < 'a' || c > 'z' {
			return ""
		}
	}
	return s
}

func parseAudio(a []byte) AudioAttributes {
	x := AudioAttributes{Channels: int(a[1]&7) + 1, CodeExtension: AudioExtension(a[5])}
	switch a[0] >> 5 {
	case 0:
		x.Coding = AC3
	case 2:
		x.Coding = MPEG1Audio
	case 3:
		x.Coding = MPEG2Audio
	case 4:
		x.Coding = LPCM
	case 6:
		x.Coding = DTS
	}
	switch (a[1] >> 4) & 3 {
	case 0:
		x.SampleRate = 48000
	case 1:
		x.SampleRate = 96000
	}
	x.Language = language((a[0]>>2)&3 == 1, a[2:4])
	return x
}

func parseSubpicture(a []byte) SubpictureAttributes {
	return SubpictureAttributes{Language: language(a[0]&3 == 1, a[2:4]), CodeExtension: SubpictureExtension(a[5])}
}

func parsePTT(t []byte) ([][]PartOfTitle, error) {
	n := be16(t, 0)
	if n < 1 || n > 99 || len(t) < 8+4*n {
		return nil, corrupt("part-of-title table has %d titles", n)
	}
	out := make([][]PartOfTitle, n)
	for i := 0; i < n; i++ {
		start := int64(be32(t, 8+4*i))
		stop := int64(len(t))
		if i+1 < n {
			stop = int64(be32(t, 8+4*(i+1)))
		}
		if start < int64(8+4*n) || stop < start || stop > int64(len(t)) || (stop-start)%4 != 0 {
			return nil, corrupt("part-of-title table: title %d spans bytes %d–%d", i+1, start, stop)
		}
		for o := start; o < stop; o += 4 {
			p := PartOfTitle{PGC: be16(t, int(o)), Program: be16(t, int(o)+2)}
			if p.PGC < 1 || p.Program < 1 {
				return nil, corrupt("title %d: chapter points at PGC %d program %d", i+1, p.PGC, p.Program)
			}
			out[i] = append(out[i], p)
		}
	}
	return out, nil
}

func parsePGCI(t []byte) ([]*PGC, error) {
	n := be16(t, 0)
	if n < 1 || len(t) < 8+8*n {
		return nil, corrupt("program chain table has %d chains", n)
	}
	out := make([]*PGC, n)
	for i := 0; i < n; i++ {
		off := int64(be32(t, 8+8*i+4))
		if off < int64(8+8*n) || off+0xEC > int64(len(t)) {
			return nil, corrupt("PGC %d at byte %d is outside its table", i+1, off)
		}
		p, err := parsePGC(t[off:])
		if err != nil {
			return nil, fmt.Errorf("PGC %d: %w", i+1, err)
		}
		out[i] = p
	}
	return out, nil
}

func parsePGC(p []byte) (*PGC, error) {
	nprog, ncell := int(p[2]), int(p[3])
	pg := &PGC{}
	var err error
	if pg.Time, err = parseTime(p[4:8]); err != nil {
		return nil, err
	}
	for i := range pg.Audio {
		c := p[0x0C+2*i]
		pg.Audio[i] = AudioControl{Available: c&0x80 != 0, Stream: int(c & 0x07)}
	}
	for i := range pg.Subpictures {
		c := p[0x1C+4*i:]
		pg.Subpictures[i] = SubpictureControl{Available: c[0]&0x80 != 0, Stream4x3: int(c[0] & 0x1F),
			Wide: int(c[1] & 0x1F), Letterbox: int(c[2] & 0x1F), PanScan: int(c[3] & 0x1F)}
	}
	for i := range pg.Palette {
		pg.Palette[i] = be32(p, 0xA4+4*i)
	}
	if ncell == 0 {
		if nprog != 0 {
			return nil, corrupt("%d programs but no cells", nprog)
		}
		return pg, nil
	}
	progOff, cellOff, posOff := be16(p, 0xE6), be16(p, 0xE8), be16(p, 0xEA)
	if progOff < 0xEC || progOff+nprog > len(p) || cellOff < 0xEC || cellOff+24*ncell > len(p) || posOff < 0xEC || posOff+4*ncell > len(p) {
		return nil, corrupt("program map, cell or position table outside the PGC")
	}
	for i := 0; i < nprog; i++ {
		c := int(p[progOff+i])
		if c < 1 || c > ncell {
			return nil, corrupt("program %d starts at cell %d of %d", i+1, c, ncell)
		}
		pg.Programs = append(pg.Programs, c)
	}
	for i := 0; i < ncell; i++ {
		c := p[cellOff+24*i:]
		t, err := parseTime(c[4:8])
		if err != nil {
			return nil, fmt.Errorf("cell %d: %w", i+1, err)
		}
		q := p[posOff+4*i:]
		cell := Cell{BlockMode: BlockMode(c[0] >> 6), AngleBlock: (c[0]>>4)&3 == 1, Time: t,
			FirstSector: be32(c, 8), LastSector: be32(c, 20), VOBID: be16(q, 0), CellID: int(q[3])}
		if cell.LastSector < cell.FirstSector {
			return nil, corrupt("cell %d ends at sector %d before it starts at %d", i+1, cell.LastSector, cell.FirstSector)
		}
		pg.Cells = append(pg.Cells, cell)
	}
	return pg, nil
}
```

- [ ] **Step 3: Write the encoders**

`internal/testdisc/dvdifo.go`:

```go
package testdisc

import (
	"encoding/binary"

	"github.com/chad3814/zenvik/dvd"
)

func put16(b []byte, off, v int)     { binary.BigEndian.PutUint16(b[off:], uint16(v)) }
func put32(b []byte, off int, v uint32) { binary.BigEndian.PutUint32(b[off:], v) }

// padSector pads b with zeros to a whole number of 2048-byte sectors.
func padSector(b []byte) []byte {
	if r := len(b) % 2048; r != 0 {
		b = append(b, make([]byte, 2048-r)...)
	}
	return b
}

func bcd(n int) byte { return byte(n/10<<4 | n%10) }

func putTime(b []byte, t dvd.Time) {
	b[0], b[1], b[2] = bcd(t.Hours), bcd(t.Minutes), bcd(t.Seconds)
	var r byte
	switch t.Rate {
	case dvd.Rate25:
		r = 1
	case dvd.Rate30:
		r = 3
	}
	b[3] = r<<6 | bcd(t.Frames)
}

// VMGFile encodes v as VIDEO_TS.IFO: the VMGI_MAT in sector 0 and the title
// table in sector 1.
func VMGFile(v *dvd.VMG) []byte {
	mat := make([]byte, 2048)
	copy(mat, "DVDVIDEO-VMG")
	put16(mat, 0x3E, v.TitleSets)
	put32(mat, 0xC4, 1)
	t := make([]byte, 8+12*len(v.Titles))
	put16(t, 0, len(v.Titles))
	put32(t, 4, uint32(len(t)-1))
	for i, e := range v.Titles {
		o := 8 + 12*i
		t[o+1] = byte(e.Angles)
		put16(t, o+2, e.Chapters)
		t[o+6] = byte(e.TitleSet)
		t[o+7] = byte(e.TitleSetTitle)
		put32(t, o+8, e.StartSector)
	}
	return append(mat, padSector(t)...)
}

// VTSFile encodes v as VTS_nn_0.IFO: the VTSI_MAT in sector 0, then the
// part-of-title table, then the PGC table, each starting on a sector.
func VTSFile(v *dvd.VTS) []byte {
	mat := make([]byte, 2048)
	copy(mat, "DVDVIDEO-VTS")
	encodeVideo(mat[0x200:], v.Video)
	put16(mat, 0x202, len(v.Audio))
	for i, a := range v.Audio {
		encodeAudio(mat[0x204+8*i:], a)
	}
	put16(mat, 0x254, len(v.Subpictures))
	for i, s := range v.Subpictures {
		encodeSubpicture(mat[0x256+6*i:], s)
	}
	ptt := padSector(encodePTT(v.Titles))
	pgci := padSector(encodePGCI(v.PGCs))
	put32(mat, 0xC8, 1)
	put32(mat, 0xCC, uint32(1+len(ptt)/2048))
	out := append(mat, ptt...)
	return append(out, pgci...)
}

func encodeVideo(b []byte, v dvd.VideoAttributes) {
	var c, s, a byte
	if v.Coding == dvd.MPEG2 {
		c = 1
	}
	full := 480
	if v.Standard == dvd.PAL {
		s, full = 1, 576
	}
	if v.Aspect == dvd.Aspect16x9 {
		a = 3
	}
	b[0] = c<<6 | s<<4 | a<<2
	var size byte
	switch {
	case v.Width == 704:
		size = 1
	case v.Width == 352 && v.Height == full:
		size = 2
	case v.Width == 352:
		size = 3
	}
	b[1] = size << 2
}

func encodeAudio(b []byte, a dvd.AudioAttributes) {
	coding := map[dvd.AudioCoding]byte{dvd.AC3: 0, dvd.MPEG1Audio: 2, dvd.MPEG2Audio: 3, dvd.LPCM: 4, dvd.DTS: 6}[a.Coding]
	b[0] = coding << 5
	if a.Language != "" {
		b[0] |= 1 << 2
		b[2], b[3] = a.Language[0], a.Language[1]
	}
	if a.SampleRate == 96000 {
		b[1] = 1 << 4
	}
	b[1] |= byte(a.Channels-1) & 7
	b[5] = byte(a.CodeExtension)
}

func encodeSubpicture(b []byte, s dvd.SubpictureAttributes) {
	if s.Language != "" {
		b[0] = 1
		b[2], b[3] = s.Language[0], s.Language[1]
	}
	b[5] = byte(s.CodeExtension)
}

func encodePTT(titles [][]dvd.PartOfTitle) []byte {
	hdr := 8 + 4*len(titles)
	size := hdr
	for _, t := range titles {
		size += 4 * len(t)
	}
	b := make([]byte, size)
	put16(b, 0, len(titles))
	put32(b, 4, uint32(size-1))
	o := hdr
	for i, t := range titles {
		put32(b, 8+4*i, uint32(o))
		for _, p := range t {
			put16(b, o, p.PGC)
			put16(b, o+2, p.Program)
			o += 4
		}
	}
	return b
}

func encodePGCI(pgcs []*dvd.PGC) []byte {
	hdr := 8 + 8*len(pgcs)
	b := make([]byte, hdr)
	put16(b, 0, len(pgcs))
	for i, p := range pgcs {
		b[8+8*i] = 0x80
		put32(b, 8+8*i+4, uint32(len(b)))
		b = append(b, encodePGC(p)...)
	}
	put32(b, 4, uint32(len(b)-1))
	return b
}

func encodePGC(p *dvd.PGC) []byte {
	progOff := 0xEC
	cellOff := progOff + len(p.Programs)
	posOff := cellOff + 24*len(p.Cells)
	b := make([]byte, posOff+4*len(p.Cells))
	b[2], b[3] = byte(len(p.Programs)), byte(len(p.Cells))
	putTime(b[4:], p.Time)
	for i, a := range p.Audio {
		if a.Available {
			b[0x0C+2*i] = 0x80 | byte(a.Stream&7)
		}
	}
	for i, s := range p.Subpictures {
		if s.Available {
			o := 0x1C + 4*i
			b[o] = 0x80 | byte(s.Stream4x3&0x1F)
			b[o+1], b[o+2], b[o+3] = byte(s.Wide&0x1F), byte(s.Letterbox&0x1F), byte(s.PanScan&0x1F)
		}
	}
	for i, c := range p.Palette {
		put32(b, 0xA4+4*i, c)
	}
	if len(p.Cells) > 0 {
		put16(b, 0xE6, progOff)
		put16(b, 0xE8, cellOff)
		put16(b, 0xEA, posOff)
	}
	for i, n := range p.Programs {
		b[progOff+i] = byte(n)
	}
	for i, c := range p.Cells {
		o := cellOff + 24*i
		b[o] = byte(c.BlockMode) << 6
		if c.AngleBlock {
			b[o] |= 1 << 4
		}
		putTime(b[o+4:], c.Time)
		put32(b, o+8, c.FirstSector)
		put32(b, o+20, c.LastSector)
		q := posOff + 4*i
		put16(b, q, c.VOBID)
		b[q+3] = byte(c.CellID)
	}
	return b
}
```

- [ ] **Step 4: Write the failing tests**

`dvd/ifo_test.go`:

```go
package dvd_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func sampleVTS() *dvd.VTS {
	t := func(d time.Duration) dvd.Time { return dvd.NewTime(d, dvd.Rate30) }
	var audio [8]dvd.AudioControl
	audio[0] = dvd.AudioControl{Available: true, Stream: 0}
	audio[1] = dvd.AudioControl{Available: true, Stream: 1}
	var subs [32]dvd.SubpictureControl
	subs[0] = dvd.SubpictureControl{Available: true, Stream4x3: 0, Wide: 1, Letterbox: 2, PanScan: 3}
	pgc := &dvd.PGC{
		Time: t(30 * time.Minute), Audio: audio, Subpictures: subs,
		Palette:  [16]uint32{0x00108080, 0x00EB8080},
		Programs: []int{1, 3},
		Cells: []dvd.Cell{
			{Time: t(10 * time.Minute), FirstSector: 0, LastSector: 99, VOBID: 1, CellID: 1},
			{BlockMode: dvd.FirstInBlock, AngleBlock: true, Time: t(10 * time.Minute), FirstSector: 100, LastSector: 199, VOBID: 1, CellID: 2},
			{BlockMode: dvd.LastInBlock, AngleBlock: true, Time: t(10 * time.Minute), FirstSector: 200, LastSector: 299, VOBID: 2, CellID: 1},
		},
	}
	return &dvd.VTS{
		Video: dvd.VideoAttributes{Coding: dvd.MPEG2, Standard: dvd.PAL, Aspect: dvd.Aspect16x9, Width: 720, Height: 576},
		Audio: []dvd.AudioAttributes{
			{Coding: dvd.AC3, Channels: 6, SampleRate: 48000, Language: "en", CodeExtension: dvd.AudioNormal},
			{Coding: dvd.DTS, Channels: 2, SampleRate: 96000, Language: "", CodeExtension: dvd.AudioDirectorsComments},
		},
		Subpictures: []dvd.SubpictureAttributes{{Language: "fr", CodeExtension: dvd.SubpictureForced}},
		Titles:      [][]dvd.PartOfTitle{{{PGC: 1, Program: 1}, {PGC: 1, Program: 2}}, {{PGC: 2, Program: 1}}},
		PGCs:        []*dvd.PGC{pgc, {Time: t(time.Minute), Programs: []int{1}, Cells: []dvd.Cell{{Time: t(time.Minute), FirstSector: 300, LastSector: 309, VOBID: 3, CellID: 1}}}},
	}
}

func TestVMGRoundTrip(t *testing.T) {
	want := &dvd.VMG{TitleSets: 2, Titles: []dvd.TitleEntry{
		{Angles: 1, Chapters: 12, TitleSet: 1, TitleSetTitle: 1, StartSector: 300},
		{Angles: 3, Chapters: 1, TitleSet: 2, TitleSetTitle: 1, StartSector: 9000},
	}}
	got, err := dvd.ParseVMG(testdisc.VMGFile(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestVTSRoundTrip(t *testing.T) {
	want := sampleVTS()
	got, err := dvd.ParseVTS(testdisc.VTSFile(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseCorrupt(t *testing.T) {
	vmg := func(mut func(b []byte)) []byte {
		b := testdisc.VMGFile(&dvd.VMG{TitleSets: 1, Titles: []dvd.TitleEntry{{Angles: 1, Chapters: 1, TitleSet: 1, TitleSetTitle: 1}}})
		mut(b)
		return b
	}
	vts := func(mut func(v *dvd.VTS, b []byte) []byte) []byte {
		v := sampleVTS()
		return mut(v, testdisc.VTSFile(v))
	}
	pgc := 2 * 2048 // sampleVTS's PTT table fits in one sector, so the PGC table starts at sector 2
	firstPGC := pgc + 8 + 8*2
	tests := map[string][]byte{
		"vmg magic":        vmg(func(b []byte) { b[0] = 'X' }),
		"vmg table sector": vmg(func(b []byte) { b[0xC6] = 0x40 }),
		"vmg no titles":    vmg(func(b []byte) { b[2048] = 0; b[2049] = 0 }),
		"vmg title set 0":  vmg(func(b []byte) { b[2048+8+6] = 0 }),
		"vmg truncated":    vmg(func(b []byte) {})[:2048+10],
		"vts magic":        vts(func(_ *dvd.VTS, b []byte) []byte { b[4] = 'X'; return b }),
		"vts 9 audio":      vts(func(_ *dvd.VTS, b []byte) []byte { b[0x203] = 9; return b }),
		"vts video coding": vts(func(_ *dvd.VTS, b []byte) []byte { b[0x200] |= 0xC0; return b }),
		"vts ptt to missing pgc": vts(func(v *dvd.VTS, _ []byte) []byte {
			v.Titles[1][0].PGC = 5
			return testdisc.VTSFile(v)
		}),
		"vts program cell 0": vts(func(v *dvd.VTS, _ []byte) []byte {
			v.PGCs[0].Programs[0] = 0
			return testdisc.VTSFile(v)
		}),
		"vts cell backwards": vts(func(v *dvd.VTS, _ []byte) []byte {
			v.PGCs[0].Cells[0].FirstSector = 500
			return testdisc.VTSFile(v)
		}),
		"vts bad bcd minutes": vts(func(_ *dvd.VTS, b []byte) []byte { b[firstPGC+5] = 0x6A; return b }),
		"vts frame rate 2":    vts(func(_ *dvd.VTS, b []byte) []byte { b[firstPGC+7] = 0x80; return b }),
		"vts truncated":       vts(func(_ *dvd.VTS, b []byte) []byte { return b[:pgc+20] }),
	}
	for name, b := range tests {
		t.Run(name, func(t *testing.T) {
			var err error
			if name[:3] == "vmg" {
				_, err = dvd.ParseVMG(b)
			} else {
				_, err = dvd.ParseVTS(b)
			}
			if !errors.Is(err, dvd.ErrCorrupt) {
				t.Errorf("err = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestTimeDuration(t *testing.T) {
	ntsc := dvd.Time{Hours: 1, Minutes: 2, Seconds: 3, Frames: 15, Rate: dvd.Rate30}
	if got, want := ntsc.Duration(), 3723*time.Second+15*1001*time.Second/30000; got != want {
		t.Errorf("NTSC = %v, want %v", got, want)
	}
	pal := dvd.Time{Seconds: 1, Frames: 24, Rate: dvd.Rate25}
	if got := pal.Duration(); got != time.Second+960*time.Millisecond {
		t.Errorf("PAL = %v", got)
	}
	if got := dvd.NewTime(3723*time.Second+500*time.Millisecond, dvd.Rate30); got != (dvd.Time{Hours: 1, Minutes: 2, Seconds: 3, Frames: 14, Rate: dvd.Rate30}) {
		t.Errorf("NewTime = %+v", got)
	}
}

func TestLanguage6392(t *testing.T) {
	for in, want := range map[string]string{"en": "eng", "FR": "fre", "de": "ger", "iw": "heb", "zh": "chi", "xx": "", "": ""} {
		if got := dvd.Language6392(in); got != want {
			t.Errorf("Language6392(%q) = %q, want %q", in, got, want)
		}
	}
}
```

`dvd/fuzz_test.go`:

```go
package dvd_test

import (
	"testing"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func FuzzParseVMG(f *testing.F) {
	f.Add(testdisc.VMGFile(&dvd.VMG{TitleSets: 1, Titles: []dvd.TitleEntry{{Angles: 1, Chapters: 3, TitleSet: 1, TitleSetTitle: 1}}}))
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = dvd.ParseVMG(b)
	})
}

func FuzzParseVTS(f *testing.F) {
	f.Add(testdisc.VTSFile(sampleVTS()))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := dvd.ParseVTS(b)
		if err != nil {
			return
		}
		for _, chapters := range v.Titles {
			for _, p := range chapters {
				_ = v.PGCs[p.PGC-1].Programs[p.Program-1] // must not panic: ParseVTS validated it
			}
		}
	})
}
```

- [ ] **Step 5: Run the tests**

Run: `go test -race ./dvd/ ./internal/testdisc/` and `go test -run=^$ -fuzz=FuzzParseVTS -fuzztime=20s ./dvd/` and `go test -run=^$ -fuzz=FuzzParseVMG -fuzztime=20s ./dvd/`
Expected: PASS, and no fuzz crashers. If the fuzzer finds a panic, fix the parser and add the crasher input under `dvd/testdata/fuzz/`, which `go test` writes for you. The TDD order is: write the Step 4 tests first, run them and see them fail to compile, then add Steps 1–3.

- [ ] **Step 6: Verify and commit**

Run the Global Constraints verification.

```bash
git add dvd internal/testdisc/dvdifo.go
git commit -m "Add the dvd package: VIDEO_TS.IFO and VTS IFO parsers"
```

---

### Task 3: Synthetic DVD builder

**Files:**
- Create: `internal/testdisc/dvd.go`
- Test: `internal/testdisc/dvd_test.go`

**Interfaces:**
- Consumes: Task 2's `dvd` types, `VMGFile` and `VTSFile`.
- Produces:
  - `type DVD struct { Titles []dvd.TitleEntry; TitleSets []DVDTitleSet; LowerCase, AppleDouble bool }`
  - `type DVDTitleSet struct { VTS dvd.VTS; VOBs []int; Scrambled bool }`
  - `(*DVD).Files() map[string][]byte`
  - `(*DVD).WriteDir(dir string) error`
  - `(*DVD).ISO(opt udfimage.Options) ([]byte, error)`
  - `VOBPacks(sectors int, scrambled bool) []byte`
  - `SampleDVD() *DVD`, which has eight titles:

    | ID | Contents | Expected handling |
    |----|----------|-------------------|
    | 01 | Movie, 100 min, VTS 1 VOBs `[40, 30]` sectors | Rippable |
    | 02 | Duplicate of 01 | Duplicate |
    | 03 | Episode, 20 min, VTS 2, starts at a file boundary and ends mid-file | Unsupported |
    | 04 | Episode, 20 min, spans the file boundary | Unsupported |
    | 05 | Episode, 20 min, starts mid-file and ends at the end | Unsupported |
    | 06 | Play-all, 60 min, covers VTS 2 VOBs `[30, 30]` | Rippable |
    | 07 | 90 s extra, VTS 3 | Short |
    | 08 | Two-angle, 7 min, VTS 4 | Non-contiguous angle-1 cells |

- [ ] **Step 1: Write the failing test**

`internal/testdisc/dvd_test.go`:

```go
package testdisc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func TestSampleDVDFiles(t *testing.T) {
	d := SampleDVD()
	files := d.Files()
	vmg, err := dvd.ParseVMG(files["VIDEO_TS/VIDEO_TS.IFO"])
	if err != nil {
		t.Fatal(err)
	}
	if len(vmg.Titles) != 8 || vmg.TitleSets != 4 {
		t.Fatalf("VMG = %+v", vmg)
	}
	for n := 1; n <= 4; n++ {
		name := fmt.Sprintf("VIDEO_TS/VTS_%02d_0.IFO", n)
		if _, err := dvd.ParseVTS(files[name]); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := len(files["VIDEO_TS/VTS_01_1.VOB"]); got != 40*2048 {
		t.Errorf("VTS_01_1.VOB is %d bytes, want %d", got, 40*2048)
	}
	if got := len(files["VIDEO_TS/VTS_01_2.VOB"]); got != 30*2048 {
		t.Errorf("VTS_01_2.VOB is %d bytes", got)
	}
}

func TestDVDNamesAndAppleDouble(t *testing.T) {
	d := SampleDVD()
	d.LowerCase, d.AppleDouble = true, true
	files := d.Files()
	for _, n := range []string{"video_ts/video_ts.ifo", "video_ts/vts_01_1.vob", "video_ts/._vts_01_1.vob"} {
		if _, ok := files[n]; !ok {
			t.Errorf("missing %s", n)
		}
	}
	if _, ok := files["VIDEO_TS/VIDEO_TS.IFO"]; ok {
		t.Error("upper-case name written with LowerCase set")
	}
}

func TestVOBPacks(t *testing.T) {
	clear := VOBPacks(2, false)
	scr := VOBPacks(2, true)
	if len(clear) != 4096 || clear[3] != 0xBA || clear[17] != 0xE0 {
		t.Fatalf("pack header wrong: % X", clear[:24])
	}
	if clear[20]&0x30 != 0 || scr[20]&0x30 != 0x10 || scr[2048+20]&0x30 != 0x10 {
		t.Errorf("scrambling bits: clear %02X, scrambled %02X", clear[20], scr[20])
	}
}

func TestDVDISO(t *testing.T) {
	img, err := SampleDVD().ISO(udfimage.Options{Revision: 0x0102, Label: "SAMPLE_DVD"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "dvd.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := udf.OpenImage(p)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if u.Label() != "SAMPLE_DVD" {
		t.Errorf("label = %q", u.Label())
	}
	b, err := fs.ReadFile(u, "VIDEO_TS/VIDEO_TS.IFO")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dvd.ParseVMG(b); err != nil {
		t.Error(err)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/testdisc/ -run 'DVD|VOB'`
Expected: FAIL to compile with `undefined: SampleDVD`.

- [ ] **Step 3: Implement**

`internal/testdisc/dvd.go`:

```go
package testdisc

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

// DVD describes a synthetic VIDEO_TS tree.
type DVD struct {
	Titles      []dvd.TitleEntry // VIDEO_TS.IFO title table
	TitleSets   []DVDTitleSet    // VTS_01, VTS_02, …
	LowerCase   bool             // write names in lower case (video_ts/vts_01_1.vob)
	AppleDouble bool             // also write a macOS "._" companion for every file
}

// DVDTitleSet is one video title set.
type DVDTitleSet struct {
	VTS       dvd.VTS
	VOBs      []int // sectors in each title VOB: VTS_nn_1.VOB, VTS_nn_2.VOB, …
	Scrambled bool  // packs carry PES_scrambling_control 01, as on a CSS disc
}

// VOBPacks returns sectors MPEG-2 program stream packs of 2048 bytes. Each
// pack holds one empty video PES packet (stream 0xE0); scrambled packs set
// PES_scrambling_control to 01.
func VOBPacks(sectors int, scrambled bool) []byte {
	b := make([]byte, sectors*2048)
	for s := 0; s < sectors; s++ {
		p := b[s*2048:]
		copy(p, []byte{0, 0, 1, 0xBA, 0x44, 0, 4, 0, 4, 1, 0x01, 0x89, 0xC3, 0xF8})
		copy(p[14:], []byte{0, 0, 1, 0xE0, 0x07, 0xEC, 0x81, 0x00, 0x00})
		if scrambled {
			p[20] |= 0x10
		}
	}
	return b
}

// Files returns the disc's files keyed by slash-separated path.
func (d *DVD) Files() map[string][]byte {
	name := func(s string) string {
		if d.LowerCase {
			return strings.ToLower(s)
		}
		return s
	}
	dir := name("VIDEO_TS") + "/"
	files := map[string][]byte{
		dir + name("VIDEO_TS.IFO"): VMGFile(&dvd.VMG{TitleSets: len(d.TitleSets), Titles: d.Titles}),
		dir + name("VIDEO_TS.VOB"): VOBPacks(1, false),
	}
	for i, ts := range d.TitleSets {
		vts := ts.VTS
		files[dir+name(fmt.Sprintf("VTS_%02d_0.IFO", i+1))] = VTSFile(&vts)
		for k, sectors := range ts.VOBs {
			files[dir+name(fmt.Sprintf("VTS_%02d_%d.VOB", i+1, k+1))] = VOBPacks(sectors, ts.Scrambled)
		}
	}
	if d.AppleDouble {
		names := make([]string, 0, len(files))
		for p := range files {
			names = append(names, p)
		}
		for _, p := range names {
			dirp, base := path.Split(p)
			files[dirp+"._"+base] = []byte("AppleDouble")
		}
	}
	return files
}

// WriteDir writes the disc's files under dir.
func (d *DVD) WriteDir(dir string) error {
	for name, data := range d.Files() {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ISO returns the disc as a UDF image (use Revision 0x0102 for DVD).
func (d *DVD) ISO(opt udfimage.Options) ([]byte, error) {
	files := map[string]udfimage.File{}
	for name, data := range d.Files() {
		files[name] = udfimage.File{Data: data}
	}
	return udfimage.Build(files, opt)
}

// SampleDVD returns a disc with eight titles (see the Task 3 brief): a
// 100-minute movie (01) and its duplicate (02) over two whole VOBs; three
// 20-minute episodes (03–05) that start or end mid-file and a 60-minute
// "play all" (06) over two whole VOBs; a 90-second extra (07); and a
// two-angle title (08) whose angle-1 cells are not contiguous.
func SampleDVD() *DVD {
	tm := func(d time.Duration) dvd.Time { return dvd.NewTime(d, dvd.Rate30) }
	cell := func(first, last uint32, d time.Duration, vob, id int) dvd.Cell {
		return dvd.Cell{Time: tm(d), FirstSector: first, LastSector: last, VOBID: vob, CellID: id}
	}
	audio := func(n int) (a [8]dvd.AudioControl) {
		for i := 0; i < n; i++ {
			a[i] = dvd.AudioControl{Available: true, Stream: i}
		}
		return a
	}
	subs := func(n int) (s [32]dvd.SubpictureControl) {
		for i := 0; i < n; i++ {
			s[i] = dvd.SubpictureControl{Available: true, Stream4x3: i, Wide: i, Letterbox: i, PanScan: i}
		}
		return s
	}
	pgc := func(na, ns int, cells ...dvd.Cell) *dvd.PGC {
		var total time.Duration
		var progs []int
		for i, c := range cells {
			if !c.AngleBlock || c.BlockMode == dvd.FirstInBlock {
				total += c.Time.Duration()
				progs = append(progs, i+1)
			}
		}
		return &dvd.PGC{Time: tm(total), Audio: audio(na), Subpictures: subs(ns), Programs: progs, Cells: cells}
	}
	chapters := func(pgcn, n int) []dvd.PartOfTitle {
		out := make([]dvd.PartOfTitle, n)
		for i := range out {
			out[i] = dvd.PartOfTitle{PGC: pgcn, Program: i + 1}
		}
		return out
	}
	video := dvd.VideoAttributes{Coding: dvd.MPEG2, Standard: dvd.NTSC, Aspect: dvd.Aspect16x9, Width: 720, Height: 480}
	stereo := func(lang string) dvd.AudioAttributes {
		return dvd.AudioAttributes{Coding: dvd.AC3, Channels: 2, SampleRate: 48000, Language: lang, CodeExtension: dvd.AudioNormal}
	}

	movie := []dvd.Cell{cell(0, 19, 25*time.Minute, 1, 1), cell(20, 39, 25*time.Minute, 1, 2), cell(40, 54, 25*time.Minute, 1, 3), cell(55, 69, 25*time.Minute, 1, 4)}
	vts1 := DVDTitleSet{VOBs: []int{40, 30}, VTS: dvd.VTS{
		Video: video,
		Audio: []dvd.AudioAttributes{
			{Coding: dvd.AC3, Channels: 6, SampleRate: 48000, Language: "en", CodeExtension: dvd.AudioNormal},
			stereo("fr"),
			{Coding: dvd.AC3, Channels: 2, SampleRate: 48000, Language: "en", CodeExtension: dvd.AudioDirectorsComments},
		},
		Subpictures: []dvd.SubpictureAttributes{
			{Language: "en", CodeExtension: dvd.SubpictureNormal},
			{Language: "fr", CodeExtension: dvd.SubpictureNormal},
			{Language: "en", CodeExtension: dvd.SubpictureForced},
		},
		Titles: [][]dvd.PartOfTitle{chapters(1, 4), chapters(2, 4)},
		PGCs:   []*dvd.PGC{pgc(3, 3, movie...), pgc(3, 3, movie...)},
	}}

	ep := []dvd.Cell{cell(0, 19, 20*time.Minute, 1, 1), cell(20, 39, 20*time.Minute, 1, 2), cell(40, 59, 20*time.Minute, 1, 3)}
	vts2 := DVDTitleSet{VOBs: []int{30, 30}, VTS: dvd.VTS{
		Video:  video,
		Audio:  []dvd.AudioAttributes{stereo("en")},
		Titles: [][]dvd.PartOfTitle{chapters(1, 1), chapters(2, 1), chapters(3, 1), chapters(4, 3)},
		PGCs:   []*dvd.PGC{pgc(1, 0, ep[0]), pgc(1, 0, ep[1]), pgc(1, 0, ep[2]), pgc(1, 0, ep...)},
	}}

	vts3 := DVDTitleSet{VOBs: []int{10}, VTS: dvd.VTS{
		Video:  video,
		Audio:  []dvd.AudioAttributes{stereo("en")},
		Titles: [][]dvd.PartOfTitle{chapters(1, 1)},
		PGCs:   []*dvd.PGC{pgc(1, 0, cell(0, 9, 90*time.Second, 1, 1))},
	}}

	angle1 := cell(5, 9, 3*time.Minute, 1, 2)
	angle1.BlockMode, angle1.AngleBlock = dvd.FirstInBlock, true
	angle2 := cell(10, 14, 3*time.Minute, 2, 1)
	angle2.BlockMode, angle2.AngleBlock = dvd.LastInBlock, true
	vts4 := DVDTitleSet{VOBs: []int{20}, VTS: dvd.VTS{
		Video:  video,
		Audio:  []dvd.AudioAttributes{stereo("en")},
		Titles: [][]dvd.PartOfTitle{chapters(1, 3)},
		PGCs:   []*dvd.PGC{pgc(1, 0, cell(0, 4, 2*time.Minute, 1, 1), angle1, angle2, cell(15, 19, 2*time.Minute, 1, 3))},
	}}

	entry := func(vts, ttn, chapters, angles int) dvd.TitleEntry {
		return dvd.TitleEntry{Angles: angles, Chapters: chapters, TitleSet: vts, TitleSetTitle: ttn}
	}
	return &DVD{
		Titles: []dvd.TitleEntry{
			entry(1, 1, 4, 1), entry(1, 2, 4, 1),
			entry(2, 1, 1, 1), entry(2, 2, 1, 1), entry(2, 3, 1, 1), entry(2, 4, 3, 1),
			entry(3, 1, 1, 1),
			entry(4, 1, 3, 2),
		},
		TitleSets: []DVDTitleSet{vts1, vts2, vts3, vts4},
	}
}
```

In `pgc`, the program map lists one program per angle-1 cell. For VTS 4 that gives programs `{1, 2, 4}`: the second program starts at the first angle cell, and the third skips the angle-2 cell.

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/testdisc/`
Expected: PASS.

- [ ] **Step 5: Verify and commit**

Run the Global Constraints verification.

```bash
git add internal/testdisc/dvd.go internal/testdisc/dvd_test.go
git commit -m "Add a synthetic DVD builder for tests"
```

---

### Task 4: Source detection for VIDEO_TS

**Files:**
- Modify: `internal/source/source.go`
- Test: `internal/source/source_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `source.Format` (`Bluray = 1`, `DVD = 2`) with `String()` returning `"Blu-ray"` or `"DVD"`.
  - `source.VideoTSDir Kind = 3`, whose `String()` is `"VIDEO_TS folder"`.
  - New `Source` fields: `Format Format` and `VideoTS string`. `VideoTS` is the actual name of the VIDEO_TS directory in `FS`, such as `"VIDEO_TS"` or `"video_ts"`, and is empty for Blu-ray.
  - `func FindName(fsys fs.FS, dir, name string, wantDir bool) string`.
  - The new error text for an unrecognized tree: `<where> has no BDMV/index.bdmv or VIDEO_TS/VIDEO_TS.IFO`.

- [ ] **Step 1: Replace the obsolete DVD tests and write the new ones**

In `internal/source/source_test.go`:
- Delete the `t.Run("dvd folder", …)` subtest.
- Delete `TestOpenDVDImage` and `TestExplainBeforeClose`. They asserted that DVDs are rejected, and DVDs now open.
- In the `"empty folder"` subtest, change the expected substring from `"no BDMV/index.bdmv"` to `"no BDMV/index.bdmv or VIDEO_TS/VIDEO_TS.IFO"`.
- Change `TestKindString` to also check `VideoTSDir.String() == "VIDEO_TS folder"`, `Bluray.String() == "Blu-ray"` and `DVD.String() == "DVD"`.

Then add:

```go
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenVideoTSFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "My Movie")
	writeFiles(t, root, map[string]string{"VIDEO_TS/VIDEO_TS.IFO": "DVDVIDEO-VMG"})
	for _, path := range []string{root, filepath.Join(root, "VIDEO_TS")} {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open(%s): %v", path, err)
		}
		if s.Kind != VideoTSDir || s.Format != DVD || s.VideoTS != "VIDEO_TS" || s.Path != root || s.Label != "My Movie" {
			t.Errorf("Open(%s) = %+v", path, s)
		}
		if _, err := fs.Stat(s.FS, "VIDEO_TS/VIDEO_TS.IFO"); err != nil {
			t.Error(err)
		}
	}
}

func TestOpenVideoTSLowerCase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disc")
	writeFiles(t, root, map[string]string{"video_ts/video_ts.ifo": "DVDVIDEO-VMG", "._VIDEO_TS": "x"})
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.VideoTS != "video_ts" || s.Format != DVD {
		t.Errorf("Source = %+v", s)
	}
}

func TestOpenVideoTSWithoutIFO(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"VIDEO_TS/VTS_01_1.VOB": "x"})
	_, err := Open(root)
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "VIDEO_TS/VIDEO_TS.IFO") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenHybridPrefersBluray(t *testing.T) {
	root := t.TempDir()
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, root, map[string]string{"VIDEO_TS/VIDEO_TS.IFO": "DVDVIDEO-VMG"})
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Format != Bluray || s.Kind != BDMVDir || s.VideoTS != "" {
		t.Errorf("Source = %+v", s)
	}
}

func TestOpenDVDImage(t *testing.T) {
	img, err := udfimage.Build(map[string]udfimage.File{"VIDEO_TS/VIDEO_TS.IFO": {Data: []byte("DVDVIDEO-VMG")}},
		udfimage.Options{Revision: 0x0102, Label: "SOME_DVD"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "dvd.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Kind != ISO || s.Format != DVD || s.VideoTS != "VIDEO_TS" || s.Label != "SOME_DVD" {
		t.Errorf("Source = %+v", s)
	}
}

func TestFindName(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"Video_TS/vts_01_1.vob": "x", "Video_TS/._VTS_01_1.VOB": "x"})
	fsys := os.DirFS(root)
	if got := FindName(fsys, ".", "VIDEO_TS", true); got != "Video_TS" {
		t.Errorf("dir = %q", got)
	}
	if got := FindName(fsys, "Video_TS", "VTS_01_1.VOB", false); got != "vts_01_1.vob" {
		t.Errorf("file = %q", got)
	}
	if got := FindName(fsys, "Video_TS", "VTS_01_1.VOB", true); got != "" {
		t.Errorf("file found as dir: %q", got)
	}
	if got := FindName(fsys, "missing", "x", false); got != "" {
		t.Errorf("missing dir: %q", got)
	}
}
```

If any of `fs`, `strings`, `testdisc` or `udfimage` aren't imported in the test file yet, add them.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/source/`
Expected: FAIL to compile with `undefined: VideoTSDir` (and `DVD`, `FindName`).

- [ ] **Step 3: Implement**

In `internal/source/source.go`:

1. Change the package doc to: `// Package source resolves a user-supplied path (a disc image, a folder containing BDMV or VIDEO_TS, or a BDMV or VIDEO_TS folder) to a file tree whose root holds BDMV or VIDEO_TS.`
2. Change `ErrUnsupported`'s doc comment to `reports a path that is not a Blu-ray or DVD image or folder.`
3. Add the following after the `Kind` constants:

```go
// VideoTSDir is a VIDEO_TS folder (or a folder that contains one).
const VideoTSDir Kind = 3

// Format is the disc format.
type Format int

// Disc formats.
const (
	Bluray Format = 1
	DVD    Format = 2
)

func (f Format) String() string {
	switch f {
	case Bluray:
		return "Blu-ray"
	case DVD:
		return "DVD"
	}
	return "unknown"
}
```

4. Add `case VideoTSDir: return "VIDEO_TS folder"` to `Kind.String`.
5. Change the `Source` struct to:

```go
// Source is an opened disc file tree.
type Source struct {
	Kind    Kind
	Format  Format
	Path    string // the ISO file, or the folder containing BDMV or VIDEO_TS
	Label   string // UDF volume identifier, or the folder's name
	FS      fs.FS  // root contains BDMV/index.bdmv or <VideoTS>/VIDEO_TS.IFO
	VideoTS string // name of the VIDEO_TS directory in FS (DVD only)
	close   func() error
}
```

6. Replace `openDir`, `openImage` and `explain` with the following code, and add `FindName` and `videoTSDir`. Add `"strings"` to the imports.

```go
func openDir(dir string) (*Source, error) {
	root := dir
	switch {
	case filepath.Base(dir) == "BDMV" && isFile(filepath.Join(dir, "index.bdmv")):
		root = filepath.Dir(dir)
	case strings.EqualFold(filepath.Base(dir), "VIDEO_TS") && FindName(os.DirFS(dir), ".", "VIDEO_TS.IFO", false) != "":
		root = filepath.Dir(dir)
	}
	fsys := os.DirFS(root)
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if isFile(filepath.Join(root, "BDMV", "index.bdmv")) {
		return &Source{Kind: BDMVDir, Format: Bluray, Path: root, Label: filepath.Base(abs), FS: fsys}, nil
	}
	if vts := videoTSDir(fsys); vts != "" {
		return &Source{Kind: VideoTSDir, Format: DVD, Path: root, Label: filepath.Base(abs), FS: fsys, VideoTS: vts}, nil
	}
	return nil, explain(root)
}

func openImage(name string) (*Source, error) {
	img, err := udf.OpenImage(name)
	switch {
	case errors.Is(err, udf.ErrNotUDF):
		return nil, fmt.Errorf("%w: %s is not a UDF disc image", ErrUnsupported, name)
	case errors.Is(err, udf.ErrUnsupported):
		return nil, fmt.Errorf("%w: %s: %w", ErrUnsupported, name, err)
	case err != nil:
		return nil, err
	}
	_, err = fs.Stat(img, "BDMV/index.bdmv")
	if err == nil {
		return &Source{Kind: ISO, Format: Bluray, Path: name, Label: img.Label(), FS: img, close: img.Close}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		img.Close()
		return nil, err
	}
	if vts := videoTSDir(img); vts != "" {
		return &Source{Kind: ISO, Format: DVD, Path: name, Label: img.Label(), FS: img, VideoTS: vts, close: img.Close}, nil
	}
	img.Close()
	return nil, explain(name)
}

// explain builds the ErrUnsupported error for a tree that is neither a
// Blu-ray nor a DVD.
func explain(where string) error {
	return fmt.Errorf("%w: %s has no BDMV/index.bdmv or VIDEO_TS/VIDEO_TS.IFO", ErrUnsupported, where)
}

// videoTSDir returns the name of fsys's VIDEO_TS directory if it holds
// VIDEO_TS.IFO (both matched ignoring case), or "".
func videoTSDir(fsys fs.FS) string {
	d := FindName(fsys, ".", "VIDEO_TS", true)
	if d == "" || FindName(fsys, d, "VIDEO_TS.IFO", false) == "" {
		return ""
	}
	return d
}

// FindName returns the name of the entry of directory dir in fsys that
// equals name ignoring case, preferring an exact match, or "" if there is
// none or dir can't be read. wantDir selects directories (true) or
// non-directories (false). AppleDouble "._" entries never match.
func FindName(fsys fs.FS, dir, name string, wantDir bool) string {
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return ""
	}
	found := ""
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "._") || e.IsDir() != wantDir || !strings.EqualFold(n, name) {
			continue
		}
		if n == name {
			return n
		}
		if found == "" {
			found = n
		}
	}
	return found
}
```

- [ ] **Step 4: Fix callers and run the tests**

Run: `go build ./... && go test -race ./...`
Expected: PASS. No root-package code reads `source.explain` or the old DVD message. If a root or CLI test asserts the old `"DVD support is planned"` text, run `grep -rn "planned" --include=*_test.go .` and update that test to expect `"no BDMV/index.bdmv or VIDEO_TS/VIDEO_TS.IFO"` (for a tree with neither) or to expect success.

- [ ] **Step 5: Verify and commit**

Run the Global Constraints verification.

```bash
git add internal/source
git commit -m "Recognize VIDEO_TS folders and DVD images as sources"
```

---

### Task 5: DVD scan — titles, tracks, chapters, the whole-file check, CSS and ranking

**Files:**
- Modify: `title.go`, `errors.go`, `disc.go`, `rip.go` (the unsupported-title guard only)
- Create: `scan_dvd.go`, `encrypt_dvd.go`
- Test: `dvd_test.go` (package `zenvik_test`), `scan_dvd_internal_test.go` (package `zenvik`)

**Interfaces:**
- Consumes:
  - Task 2: the `dvd` parsers and `dvd.Language6392`.
  - Task 3: `testdisc.SampleDVD`, `DVD.WriteDir` and `DVD.ISO`.
  - Task 4: `source.Format`, `source.DVD`, `source.VideoTSDir`, `Source.VideoTS` and `source.FindName`.
- Also consumes `rank.Candidate.HasAudio` and `rank.Candidate.Size` from the decoy-playlist fix, which is on `main` before M5 starts. DVD candidates set both, so titles without audio and decoy-rate titles are filtered the same way as on Blu-ray.
- Produces:
  - `zenvik.Format`, a type alias of `source.Format`, with the constants `zenvik.Bluray` and `zenvik.DVD`.
  - The constant `zenvik.VideoTSDir`.
  - The field `Disc.Format`.
  - The field `Title.Unsupported string`.
  - The fields `AudioTrack.Description`, `SubtitleTrack.Description` and `VideoTrack.AspectRatio`.
  - `zenvik.CodingVobSub` (`bluray.CodingType` 0xFF).
  - `zenvik.ErrUnsupportedTitle`.
  - `Disc.Title` accepts an unpadded DVD title number (`"3"` → `"03"`).
  - `Rip` returns `ErrUnsupportedTitle` for a title with `Unsupported != ""`, before any other work.
  - Internal: `scanDVD(ctx, fsys, dir string, minDuration, w) ([]*Title, error)`.

- [ ] **Step 1: Write the failing tests**

`dvd_test.go`:

```go
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
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

const (
	reasonMidFile       = "starts or ends mid-file (not supported yet)"
	reasonNotContiguous = "cells are not contiguous (not supported yet)"
)

func writeDVD(t *testing.T, d *testdisc.DVD) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_DVD")
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func checkSampleDVD(t *testing.T, d *zenvik.Disc, vob1, vob2 string) {
	t.Helper()
	if got, want := ids(d.Titles), []string{"01", "06", "02", "03", "04", "05", "07", "08"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("titles = %v, want %v", got, want)
	}
	m := d.Main()
	if m == nil || m.ID != "01" {
		t.Fatalf("Main = %+v", m)
	}
	if m.Duration != 100*time.Minute || m.Size != 70*2048 || m.Angles != 1 || m.Encrypted || m.Unsupported != "" {
		t.Errorf("main = duration %v, size %d, angles %d, encrypted %v, unsupported %q", m.Duration, m.Size, m.Angles, m.Encrypted, m.Unsupported)
	}
	if want := []zenvik.Clip{{ID: vob1}, {ID: vob2}}; !reflect.DeepEqual(m.Clips, want) {
		t.Errorf("clips = %+v, want %+v", m.Clips, want)
	}
	wantCh := []zenvik.Chapter{{Number: 1, Start: 0}, {Number: 2, Start: 25 * time.Minute}, {Number: 3, Start: 50 * time.Minute}, {Number: 4, Start: 75 * time.Minute}}
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
	for id, reason := range map[string]string{"03": reasonMidFile, "04": reasonMidFile, "05": reasonMidFile, "08": reasonNotContiguous} {
		ti := mustTitle(t, d, id)
		if ti.Unsupported != reason || !ti.Rank.Filtered || !slices.Contains(ti.Rank.Reasons, reason) {
			t.Errorf("%s: unsupported %q, rank %+v", id, ti.Unsupported, ti.Rank)
		}
	}
	all := mustTitle(t, d, "06")
	if all.Unsupported != "" || all.Duration != time.Hour || len(all.Clips) != 2 || len(all.Chapters) != 3 || all.Chapters[2].Start != 40*time.Minute {
		t.Errorf("06 = %+v", all)
	}
	if ex := mustTitle(t, d, "07"); !ex.Rank.Filtered || !slices.Contains(ex.Rank.Reasons, "shorter than 2m0s") {
		t.Errorf("07 rank = %+v", ex.Rank)
	}
	ang := mustTitle(t, d, "08")
	if ang.Angles != 2 || len(ang.Chapters) != 3 || ang.Chapters[1].Start != 2*time.Minute || ang.Chapters[2].Start != 5*time.Minute {
		t.Errorf("08 = angles %d, chapters %v", ang.Angles, ang.Chapters)
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
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	_, err := d.Rip(context.Background(), mustTitle(t, d, "03"), zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "x.mkv")})
	if !errors.Is(err, zenvik.ErrUnsupportedTitle) || !strings.Contains(err.Error(), reasonMidFile) {
		t.Errorf("err = %v", err)
	}
}
```

`ids`, `openDisc` and `mustTitle` are the root test package's existing helpers (in `zenvik_test.go`).

`scan_dvd_internal_test.go`:

```go
package zenvik

import (
	"testing"
	"time"

	"github.com/chad3814/zenvik/dvd"
)

func TestWholeFiles(t *testing.T) {
	vobs := []vobFile{{name: "1", size: 10 * 2048, first: 0, last: 9}, {name: "2", size: 10 * 2048, first: 10, last: 19}}
	c := func(first, last uint32) dvd.Cell { return dvd.Cell{FirstSector: first, LastSector: last} }
	tests := []struct {
		name   string
		cells  []dvd.Cell
		files  int
		reason string
	}{
		{"one whole file", []dvd.Cell{c(0, 4), c(5, 9)}, 1, ""},
		{"both files", []dvd.Cell{c(0, 12), c(13, 19)}, 2, ""},
		{"second file", []dvd.Cell{c(10, 19)}, 1, ""},
		{"gap", []dvd.Cell{c(0, 4), c(6, 9)}, 0, reasonNotContiguous},
		{"backwards", []dvd.Cell{c(10, 19), c(0, 9)}, 0, reasonNotContiguous},
		{"ends mid-file", []dvd.Cell{c(0, 14)}, 0, reasonMidFile},
		{"starts mid-file", []dvd.Cell{c(5, 19)}, 0, reasonMidFile},
		{"past the end", []dvd.Cell{c(10, 25)}, 0, reasonMidFile},
	}
	for _, tt := range tests {
		got, reason := wholeFiles(tt.cells, vobs)
		if len(got) != tt.files || reason != tt.reason {
			t.Errorf("%s: %d files, reason %q; want %d, %q", tt.name, len(got), reason, tt.files, tt.reason)
		}
	}
}

func TestCellStartsAndAngleOne(t *testing.T) {
	m := func(d time.Duration) dvd.Time { return dvd.NewTime(d, dvd.Rate25) }
	cells := []dvd.Cell{
		{Time: m(time.Minute)},
		{BlockMode: dvd.FirstInBlock, AngleBlock: true, Time: m(2 * time.Minute)},
		{BlockMode: dvd.InBlock, AngleBlock: true, Time: m(2 * time.Minute)},
		{BlockMode: dvd.LastInBlock, AngleBlock: true, Time: m(2 * time.Minute)},
		{Time: m(time.Minute)},
	}
	if got, want := cellStarts(cells), []time.Duration{0, time.Minute, time.Minute, time.Minute, 3 * time.Minute}; !equalDurations(got, want) {
		t.Errorf("cellStarts = %v, want %v", got, want)
	}
	if got := angleOne(cells); len(got) != 3 || got[1].BlockMode != dvd.FirstInBlock {
		t.Errorf("angleOne = %+v", got)
	}
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPackScrambled(t *testing.T) {
	pack := func(id byte, flags byte) []byte {
		p := make([]byte, 2048)
		copy(p, []byte{0, 0, 1, 0xBA, 0x44, 0, 4, 0, 4, 1, 0x01, 0x89, 0xC3, 0xF8})
		copy(p[14:], []byte{0, 0, 1, id, 0x07, 0xEC, flags, 0x00, 0x00})
		return p
	}
	if packScrambled(pack(0xE0, 0x81)) {
		t.Error("clear video pack reported scrambled")
	}
	if !packScrambled(pack(0xBD, 0x91)) || !packScrambled(pack(0xC0, 0xB1)) {
		t.Error("scrambled private/audio pack not detected")
	}
	if packScrambled(pack(0xBF, 0xB1)) {
		t.Error("NAV (private stream 2) packs are never scrambled")
	}
	mpeg1 := pack(0xE0, 0x91)
	mpeg1[4] = 0x21 // MPEG-1 pack header: no scrambling field
	if packScrambled(mpeg1) {
		t.Error("MPEG-1 pack reported scrambled")
	}
	if packScrambled(make([]byte, 2048)) {
		t.Error("zero sector reported scrambled")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./ -run 'DVD|WholeFiles|CellStarts|PackScrambled'`
Expected: FAIL to compile (`undefined: zenvik.DVD`, `wholeFiles`, …).

- [ ] **Step 3: Extend the public types**

In `title.go`:
- Change the `Title` doc to `// Title is one playlist (Blu-ray) or title (DVD) on the disc.`
- Change the `ID` comment to `// playlist number, e.g. "00800", or DVD title number, e.g. "01"`.
- Add the following field after `Encrypted bool`:

```go
	// Unsupported is empty when the title can be ripped; otherwise it says
	// why not (DVD titles that start or end mid-file, for example).
	Unsupported string
```

- Change the `Clip` doc to: `// Clip is one play item: a clip and its IN/OUT times on the clip's clock. For a DVD title, ID is a title VOB file name (as found on the disc) and In/Out are zero.`
- Add `AspectRatio string // DVD only: "4:3" or "16:9"` to `VideoTrack`.
- Add `Description string // DVD only: e.g. "Director's Commentary"` to `AudioTrack`.
- Add `Description string // DVD only: e.g. "Forced"` to `SubtitleTrack`.
- Append:

```go
// CodingVobSub is the Codec of DVD subtitle tracks (VobSub bitmaps). It is
// not a Blu-ray stream coding type, so bluray.CodingType.String reports
// it as "0xFF".
const CodingVobSub bluray.CodingType = 0xFF
```

In `errors.go`:
- Change the `ErrUnsupportedSource` doc to `the path is not a Blu-ray or DVD ISO image or folder.`
- Change the `ErrEncrypted` doc to `every usable title on the disc is AACS- or CSS-encrypted.`
- Add:

```go
	// ErrUnsupportedTitle: the title can't be ripped yet; Title.Unsupported says why.
	ErrUnsupportedTitle = errors.New("zenvik: title is not supported")
```

In `disc.go`:
- Add `VideoTSDir = source.VideoTSDir` to the source-kind constants.
- Add:

```go
// Format is the disc format.
type Format = source.Format

// Disc formats.
const (
	Bluray = source.Bluray
	DVD    = source.DVD
)
```

- Add the field `Format Format // Blu-ray or DVD` to `Disc`, after `Kind`.
- Change the `Disc` doc to `// Disc is an opened Blu-ray or DVD disc image or folder.`
- Change the `Open` doc to start `// Open reads the disc at path (an ISO image, a folder containing BDMV or VIDEO_TS, or a BDMV or VIDEO_TS folder), …`.
- Replace the scan call in `Open` with:

```go
	var titles []*Title
	var meta *bluray.DiscMeta
	if src.Format == source.DVD {
		titles, err = scanDVD(ctx, src.FS, src.VideoTS, cfg.minDuration, rank.DefaultWeights)
	} else {
		titles, meta, err = scanTitles(ctx, src.FS, cfg.minDuration, rank.DefaultWeights)
	}
	if err != nil {
		src.Close()
		return nil, err
	}
	return &Disc{Path: path, Kind: src.Kind, Format: src.Format, Label: src.Label, Meta: meta, Titles: titles, src: src}, nil
```

- Replace `Title` with:

```go
// Title returns the title with ID id: a playlist number such as "00800",
// or a DVD title number such as "03" (the leading zero may be omitted).
func (d *Disc) Title(id string) (*Title, error) {
	if d.Format == DVD && len(id) == 1 && id[0] >= '1' && id[0] <= '9' {
		id = "0" + id
	}
	for _, t := range d.Titles {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("zenvik: no title %q on this disc: %w", id, fs.ErrNotExist)
}
```

In `rip.go`, insert the following directly after the `does not belong` check:

```go
	if t.Unsupported != "" {
		return nil, fmt.Errorf("%w: title %s %s", ErrUnsupportedTitle, t.ID, t.Unsupported)
	}
```

- [ ] **Step 4: Implement the scan**

`encrypt_dvd.go`:

```go
package zenvik

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// vobEncrypted reports whether a VOB looks CSS-encrypted. It samples up to
// 64 packs spread evenly through the file's sectors and reports true if any
// video, audio or private stream 1 PES packet has PES_scrambling_control
// set.
func vobEncrypted(fsys fs.FS, name string, sectors int64) (bool, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	ra, ok := f.(io.ReaderAt)
	if !ok {
		return false, fmt.Errorf("zenvik: %s does not support random access", name)
	}
	n := min(sectors, 64)
	buf := make([]byte, 2048)
	for i := int64(0); i < n; i++ {
		s := i * sectors / n
		if _, err := ra.ReadAt(buf, s*2048); err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		if packScrambled(buf) {
			return true, nil
		}
	}
	return false, nil
}

// packScrambled reports whether an MPEG-2 program stream pack holds a
// video (0xE0–0xEF), audio (0xC0–0xDF) or private stream 1 (0xBD) PES
// packet whose PES_scrambling_control bits are set.
func packScrambled(p []byte) bool {
	if len(p) < 14 || p[0] != 0 || p[1] != 0 || p[2] != 1 || p[3] != 0xBA || p[4]&0xC0 != 0x40 {
		return false
	}
	o := 14 + int(p[13]&7)
	for o+9 <= len(p) {
		if p[o] != 0 || p[o+1] != 0 || p[o+2] != 1 {
			return false
		}
		id := p[o+3]
		if id == 0xBD || id >= 0xC0 && id <= 0xEF {
			if p[o+6]&0x30 != 0 {
				return true
			}
		}
		o += 6 + int(binary.BigEndian.Uint16(p[o+4:]))
	}
	return false
}
```

`scan_dvd.go`:

```go
package zenvik

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/dvd"
	"github.com/chad3814/zenvik/internal/rank"
	"github.com/chad3814/zenvik/internal/source"
)

// Reasons a DVD title can't be ripped yet (M5 spec section 5.1).
const (
	reasonNotContiguous = "cells are not contiguous (not supported yet)"
	reasonMidFile       = "starts or ends mid-file (not supported yet)"
	reasonMultiPGC      = "spans several program chains (not supported yet)"
)

// vobFile is one title VOB of a title set. Sectors are relative to the
// start of the set's first title VOB, as cell addresses are.
type vobFile struct {
	name        string
	size        int64
	first, last int64
}

// titleSet is what scanning learned about one VTS.
type titleSet struct {
	vts       *dvd.VTS
	vobs      []vobFile
	encrypted bool
	problem   string // non-empty: every title in the set is unsupported for this reason
}

// scanDVD reads the VIDEO_TS tree in directory dir of fsys and returns its
// titles ranked best-first.
func scanDVD(ctx context.Context, fsys fs.FS, dir string, minDuration time.Duration, w rank.Weights) ([]*Title, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := source.FindName(fsys, dir, "VIDEO_TS.IFO", false)
	b, err := fs.ReadFile(fsys, path.Join(dir, name))
	if err != nil {
		return nil, err
	}
	vmg, err := dvd.ParseVMG(b)
	if err != nil {
		return nil, fmt.Errorf("zenvik: %s: %w", name, err)
	}
	sets := map[int]*titleSet{}
	titles := make([]*Title, 0, len(vmg.Titles))
	cands := make([]rank.Candidate, 0, len(vmg.Titles))
	for i, e := range vmg.Titles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ts, ok := sets[e.TitleSet]
		if !ok {
			ts = loadTitleSet(fsys, dir, e.TitleSet)
			sets[e.TitleSet] = ts
		}
		t, c := dvdTitle(i+1, e, ts)
		titles = append(titles, t)
		cands = append(cands, c)
	}
	order, infos := rank.Rank(cands, minDuration, w)
	ranked := make([]*Title, len(order))
	for k, i := range order {
		titles[i].Rank = RankInfo(infos[i])
		ranked[k] = titles[i]
	}
	if allEncrypted(ranked) {
		return nil, ErrEncrypted
	}
	return ranked, nil
}

// loadTitleSet parses VTS_nn_0.IFO and sizes and probes the set's title
// VOBs (VTS_nn_1.VOB … VTS_nn_9.VOB, stopping at the first missing one).
func loadTitleSet(fsys fs.FS, dir string, n int) *titleSet {
	ts := &titleSet{}
	ifo := fmt.Sprintf("VTS_%02d_0.IFO", n)
	name := source.FindName(fsys, dir, ifo, false)
	if name == "" {
		ts.problem = "missing " + ifo
		return ts
	}
	b, err := fs.ReadFile(fsys, path.Join(dir, name))
	if err != nil {
		ts.problem = fmt.Sprintf("unreadable %s: %v", name, err)
		return ts
	}
	if ts.vts, err = dvd.ParseVTS(b); err != nil {
		ts.problem = fmt.Sprintf("%s: %v", name, err)
		return ts
	}
	var sector int64
	for k := 1; k <= 9; k++ {
		vn := source.FindName(fsys, dir, fmt.Sprintf("VTS_%02d_%d.VOB", n, k), false)
		if vn == "" {
			break
		}
		p := path.Join(dir, vn)
		st, err := fs.Stat(fsys, p)
		if err != nil {
			ts.problem = fmt.Sprintf("unreadable %s: %v", vn, err)
			return ts
		}
		if st.Size() == 0 || st.Size()%2048 != 0 {
			ts.problem = vn + ": size is not a multiple of 2048"
			return ts
		}
		count := st.Size() / 2048
		ts.vobs = append(ts.vobs, vobFile{name: vn, size: st.Size(), first: sector, last: sector + count - 1})
		sector += count
		enc, err := vobEncrypted(fsys, p, count)
		if err != nil {
			ts.problem = fmt.Sprintf("unreadable %s: %v", vn, err)
			return ts
		}
		ts.encrypted = ts.encrypted || enc
	}
	if len(ts.vobs) == 0 {
		ts.problem = fmt.Sprintf("missing VTS_%02d_1.VOB", n)
	}
	return ts
}

// dvdTitle builds DVD title num (from 1) and its ranking candidate.
func dvdTitle(num int, e dvd.TitleEntry, ts *titleSet) (*Title, rank.Candidate) {
	id := fmt.Sprintf("%02d", num)
	t := &Title{ID: id, Angles: e.Angles}
	c := rank.Candidate{ID: id}
	unsupported := func(reason string) (*Title, rank.Candidate) {
		t.Unsupported, c.Problem = reason, reason
		c.Duration = t.Duration
		return t, c
	}
	if ts.problem != "" {
		return unsupported(ts.problem)
	}
	if e.TitleSetTitle > len(ts.vts.Titles) || len(ts.vts.Titles[e.TitleSetTitle-1]) == 0 {
		return unsupported(fmt.Sprintf("title set %d has no title %d", e.TitleSet, e.TitleSetTitle))
	}
	ptts := ts.vts.Titles[e.TitleSetTitle-1]
	pgc := ts.vts.PGCs[ptts[0].PGC-1]
	t.Duration = pgc.Time.Duration()
	for _, p := range ptts {
		if p.PGC != ptts[0].PGC {
			return unsupported(reasonMultiPGC)
		}
	}
	starts := cellStarts(pgc.Cells)
	for i, p := range ptts {
		t.Chapters = append(t.Chapters, Chapter{Number: i + 1, Start: starts[pgc.Programs[p.Program-1]-1]})
	}
	fillDVDTracks(t, ts.vts, pgc)
	cells := angleOne(pgc.Cells)
	for _, cl := range cells {
		t.Size += (int64(cl.LastSector) - int64(cl.FirstSector) + 1) * 2048
		c.Clips = append(c.Clips, rank.Clip{ID: fmt.Sprintf("%d:%d:%d", e.TitleSet, cl.VOBID, cl.CellID), Out: cl.Time.Duration()})
	}
	files, reason := wholeFiles(cells, ts.vobs)
	for _, f := range files {
		t.Clips = append(t.Clips, Clip{ID: f.name})
	}
	t.Encrypted = ts.encrypted
	t.Unsupported = reason
	c.Problem = reason
	c.Duration = t.Duration
	c.Chapters = len(t.Chapters)
	c.Languages = countLanguages(t)
	c.HasVideo = true
	c.HasAudio = len(t.Audio) > 0
	c.Size = t.Size
	c.Encrypted = t.Encrypted
	return t, c
}

// angleOne returns the cells angle 1 plays: cells outside blocks, cells
// in non-angle blocks, and the first cell of each angle block.
func angleOne(cells []dvd.Cell) []dvd.Cell {
	var out []dvd.Cell
	for _, c := range cells {
		if !c.AngleBlock || c.BlockMode == dvd.FirstInBlock || c.BlockMode == dvd.NotInBlock {
			out = append(out, c)
		}
	}
	return out
}

// cellStarts returns each cell's start time in the title, counting only
// angle-1 cells; the other cells of an angle block start with its first.
func cellStarts(cells []dvd.Cell) []time.Duration {
	out := make([]time.Duration, len(cells))
	var t, blockStart time.Duration
	for i, c := range cells {
		if c.AngleBlock && c.BlockMode != dvd.FirstInBlock && c.BlockMode != dvd.NotInBlock {
			out[i] = blockStart
			continue
		}
		out[i], blockStart = t, t
		t += c.Time.Duration()
	}
	return out
}

// wholeFiles returns the title VOBs the cells cover when they play one
// contiguous run of sectors that starts at the start of a VOB and ends at
// the end of a VOB; otherwise it returns the reason the title is
// unsupported.
func wholeFiles(cells []dvd.Cell, vobs []vobFile) ([]vobFile, string) {
	if len(cells) == 0 {
		return nil, "has no cells"
	}
	for i := 1; i < len(cells); i++ {
		if int64(cells[i].FirstSector) != int64(cells[i-1].LastSector)+1 {
			return nil, reasonNotContiguous
		}
	}
	first, last := int64(cells[0].FirstSector), int64(cells[len(cells)-1].LastSector)
	start, end := -1, -1
	for k, v := range vobs {
		if v.first == first {
			start = k
		}
		if v.last == last {
			end = k
		}
	}
	if start < 0 || end < start {
		return nil, reasonMidFile
	}
	return vobs[start : end+1], ""
}

// fillDVDTracks sets t's tracks from the title set's attributes and the
// PGC's stream control tables. Track PIDs are MPEG-PS stream keys (see
// the plan's Global Constraints).
func fillDVDTracks(t *Title, vts *dvd.VTS, pgc *dvd.PGC) {
	v := vts.Video
	vt := VideoTrack{PID: 0x00E0, Codec: bluray.CodingMPEG2Video, Format: 1, FrameRate: 4, AspectRatio: "4:3"}
	if v.Coding == dvd.MPEG1 {
		vt.Codec = bluray.CodingMPEG1Video
	}
	if v.Standard == dvd.PAL {
		vt.Format, vt.FrameRate = 2, 3
	}
	if v.Aspect == dvd.Aspect16x9 {
		vt.AspectRatio = "16:9"
	}
	t.Video = []VideoTrack{vt}
	seen := map[uint16]bool{}
	for i, ctl := range pgc.Audio {
		if !ctl.Available || i >= len(vts.Audio) {
			continue
		}
		a := vts.Audio[i]
		pid, codec := audioStream(a.Coding, ctl.Stream)
		if pid == 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		at := AudioTrack{PID: pid, Codec: codec, Language: dvd.Language6392(a.Language), Channels: 6, SampleRate: 1,
			Description: audioDescriptions[a.CodeExtension]}
		switch {
		case a.Channels == 1:
			at.Channels = 1
		case a.Channels == 2:
			at.Channels = 3
		}
		if a.SampleRate == 96000 {
			at.SampleRate = 4
		}
		t.Audio = append(t.Audio, at)
	}
	for i, ctl := range pgc.Subpictures {
		if !ctl.Available || i >= len(vts.Subpictures) {
			continue
		}
		s := vts.Subpictures[i]
		n := ctl.Stream4x3
		if v.Aspect == dvd.Aspect16x9 {
			n = ctl.Wide
		}
		pid := 0xBD20 + uint16(n)
		if seen[pid] {
			continue
		}
		seen[pid] = true
		t.Subtitles = append(t.Subtitles, SubtitleTrack{PID: pid, Codec: CodingVobSub, Language: dvd.Language6392(s.Language),
			Description: subpictureDescriptions[s.CodeExtension]})
	}
}

// audioStream returns the stream key and coding type of physical audio
// stream n, or 0 for an unknown coding.
func audioStream(c dvd.AudioCoding, n int) (uint16, bluray.CodingType) {
	switch c {
	case dvd.AC3:
		return 0xBD80 + uint16(n), bluray.CodingAC3
	case dvd.DTS:
		return 0xBD88 + uint16(n), bluray.CodingDTS
	case dvd.LPCM:
		return 0xBDA0 + uint16(n), bluray.CodingLPCM
	case dvd.MPEG1Audio:
		return 0x00C0 + uint16(n), bluray.CodingMPEG1Audio
	case dvd.MPEG2Audio:
		return 0x00C0 + uint16(n), bluray.CodingMPEG2Audio
	}
	return 0, 0
}

var audioDescriptions = map[dvd.AudioExtension]string{
	dvd.AudioVisuallyImpaired:  "Visually Impaired",
	dvd.AudioDirectorsComments: "Director's Commentary",
	dvd.AudioAlternateComments: "Alternate Commentary",
}

var subpictureDescriptions = map[dvd.SubpictureExtension]string{
	dvd.SubpictureLarge:                      "Large",
	dvd.SubpictureChildren:                   "Children",
	dvd.SubpictureNormalCaptions:             "Captions",
	dvd.SubpictureLargeCaptions:              "Large Captions",
	dvd.SubpictureChildrensCaptions:          "Children's Captions",
	dvd.SubpictureForced:                     "Forced",
	dvd.SubpictureDirectorsComments:          "Commentary",
	dvd.SubpictureLargeDirectorsComments:     "Large Commentary",
	dvd.SubpictureChildrensDirectorsComments: "Children's Commentary",
}
```

- [ ] **Step 5: Run the tests**

Run: `go test -race ./...`
Expected: PASS, with the Blu-ray tests unchanged.

- [ ] **Step 6: Verify and commit**

Run the Global Constraints verification.

```bash
git add title.go errors.go disc.go rip.go scan_dvd.go encrypt_dvd.go dvd_test.go scan_dvd_internal_test.go
git commit -m "Scan DVD titles: tracks, chapters, rippability, CSS detection and ranking"
```

---

### Task 6: DVD rip — mount, mkvmerge job, track mapping, chapter file

**Files:**
- Modify: `internal/mux/args.go`, `internal/mux/identify.go`, `rip.go`
- Create: `rip_dvd.go`
- Test: `internal/mux/mux_test.go` (add tests), `rip_dvd_internal_test.go` (package `zenvik`), `rip_dvd_test.go` (package `zenvik_test`)

**Interfaces:**
- Consumes:
  - Task 5: `Disc.Format`, `zenvik.DVD`, and the `Title` DVD fields.
  - Task 4: `source.FindName`.
- Produces:
  - `mux.Job.Concat []string`: inputs read as one file and passed as `( a b … )`. mkvmerge chains sibling VOBs on its own, and only the parenthesized form turns that off (Task 1 notes).
  - `mux.Job.ChapterFile string`.
  - `mux.IdentifiedTrack.StreamID uint16` and `SubStreamID uint16`.
  - `Rip` works for DVD titles, from a folder or a mounted ISO.
  - In a dry run, the chapter argument is the literal placeholder `<chapters.txt>` and no chapter file is written.

Read `docs/superpowers/notes/2026-10-01-m5-mkvmerge-dvd.md` from Task 1 first. If it says mkvmerge reports stream IDs differently from `stream_id` 189 with `sub_stream_id` 128+n (AC-3) or 224 (video), stop and report NEEDS_CONTEXT.

mkvmerge does not read DVD subtitles from VOBs (Task 1). This task maps video and audio only. Task 10 extracts the subtitles into a VobSub `.idx`/`.sub` pair.

- [ ] **Step 1: Write the failing tests**

Add to `internal/mux/mux_test.go`. Keep that file's package clause; add `reflect` to its imports if it's missing.

```go
func TestArgsConcatAndChapters(t *testing.T) {
	got := Args(Job{Concat: []string{"a.vob", "b.vob", "c.vob"}, ChapterFile: "ch.txt", Output: "o.mkv",
		Tracks: []Track{{ID: 0, Type: "video", Default: true}}})
	want := []string{"-o", "o.mkv", "--chapters", "ch.txt", "--default-track-flag", "0:yes",
		"--video-tracks", "0", "--no-audio", "--no-subtitles", "--track-order", "0:0",
		"--no-chapters", "(", "a.vob", "b.vob", "c.vob", ")"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Args =\n%q\nwant\n%q", got, want)
	}
}

func TestIdentifyStreamIDs(t *testing.T) {
	id, err := parseIdentification([]byte(`{"tracks":[
		{"id":0,"type":"video","codec":"MPEG-1/2","properties":{"number":224,"stream_id":224}},
		{"id":1,"type":"audio","codec":"AC-3","properties":{"number":549755814077,"stream_id":189,"sub_stream_id":128,"audio_channels":6}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if id.Tracks[0].StreamID != 0xE0 || id.Tracks[0].SubStreamID != 0 || id.Tracks[1].StreamID != 0xBD || id.Tracks[1].SubStreamID != 0x80 {
		t.Errorf("tracks = %+v", id.Tracks)
	}
}
```

If `mux_test.go` is an external test package (`package mux_test`), put `TestIdentifyStreamIDs` in a new internal test file `internal/mux/identify_internal_test.go` (`package mux`) instead, and call `Args` as `mux.Args`.

`rip_dvd_internal_test.go`:

```go
package zenvik

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/mux"
)

func TestMapDVDTracks(t *testing.T) {
	ti := &Title{
		ID:    "01",
		Video: []VideoTrack{{PID: 0x00E0}},
		Audio: []AudioTrack{
			{PID: 0xBD80, Codec: bluray.CodingAC3, Language: "eng"},
			{PID: 0xBD81, Codec: bluray.CodingAC3, Language: "fre", Description: "Director's Commentary"},
		},
		Subtitles: []SubtitleTrack{{PID: 0xBD20, Codec: CodingVobSub, Language: "eng", Description: "Forced"}},
	}
	// mkvmerge reports no subtitle tracks for VOBs; Task 10 adds them from a VobSub file.
	id := &mux.Identification{Tracks: []mux.IdentifiedTrack{
		{ID: 0, Type: "video", StreamID: 0xE0},
		{ID: 1, Type: "audio", StreamID: 0xBD, SubStreamID: 0x80, Channels: 6},
		{ID: 2, Type: "audio", StreamID: 0xBD, SubStreamID: 0x81, Channels: 2},
		{ID: 3, Type: "audio", StreamID: 0xBD, SubStreamID: 0x82, Channels: 2},
	}}
	tracks, warnings := mapDVDTracks(ti, id)
	want := []mux.Track{
		{ID: 0, Type: "video", Default: true},
		{ID: 1, Type: "audio", Language: "eng", Name: "AC-3 5.1", Default: true},
		{ID: 2, Type: "audio", Language: "fre", Name: "AC-3 Stereo (Director's Commentary)"},
		{ID: 3, Type: "audio"},
	}
	if !reflect.DeepEqual(tracks, want) {
		t.Errorf("tracks =\n%+v\nwant\n%+v", tracks, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "not described by the IFO") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestWriteChapterFile(t *testing.T) {
	p, err := writeChapterFile([]Chapter{{Number: 1, Start: 0}, {Number: 2, Start: 25*time.Minute + 1500*time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "CHAPTER01=00:00:00.000\nCHAPTER01NAME=Chapter 01\nCHAPTER02=00:25:01.500\nCHAPTER02NAME=Chapter 02\n"
	if string(b) != want {
		t.Errorf("chapter file =\n%s\nwant\n%s", b, want)
	}
}
```

`rip_dvd_test.go`:

```go
package zenvik_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func hasPair(cmd []string, flag, val string) bool {
	for i := 0; i+1 < len(cmd); i++ {
		if cmd[i] == flag && cmd[i+1] == val {
			return true
		}
	}
	return false
}

func TestRipDVDDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mkvmerge stub is a shell script")
	}
	t.Setenv("PATH", "")
	stub := filepath.Join(t.TempDir(), "mkvmerge")
	script := `#!/bin/sh
case "$1" in
--version) echo "mkvmerge v90.0 ('Stub') 64-bit" ;;
-J) cat <<'EOF'
{"tracks":[
{"id":0,"type":"video","codec":"MPEG-1/2","properties":{"stream_id":224}},
{"id":1,"type":"audio","codec":"AC-3","properties":{"stream_id":189,"sub_stream_id":128,"audio_channels":6}},
{"id":2,"type":"audio","codec":"AC-3","properties":{"stream_id":189,"sub_stream_id":129,"audio_channels":2}},
{"id":3,"type":"audio","codec":"AC-3","properties":{"stream_id":189,"sub_stream_id":130,"audio_channels":2}}]}
EOF
;;
*) exit 2 ;;
esac
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, writeDVD(t, testdisc.SampleDVD()))
	out := filepath.Join(t.TempDir(), "movie.mkv")
	res, err := d.Rip(context.Background(), d.Main(), zenvik.RipOptions{OutputPath: out, DryRun: true, MkvmergePath: stub})
	if err != nil {
		t.Fatal(err)
	}
	cmd := res.Command
	if !hasPair(cmd, "--chapters", "<chapters.txt>") {
		t.Errorf("no chapter placeholder in %q", cmd)
	}
	for _, p := range [][2]string{
		{"--language", "1:eng"}, {"--language", "2:fre"}, {"--language", "3:eng"},
		{"--track-name", "1:AC-3 5.1"}, {"--track-name", "3:AC-3 Stereo (Director's Commentary)"},
		{"--default-track-flag", "0:yes"}, {"--default-track-flag", "1:yes"}, {"--default-track-flag", "2:no"},
	} {
		if !hasPair(cmd, p[0], p[1]) {
			t.Errorf("missing %s %s in %q", p[0], p[1], cmd)
		}
	}
	n := len(cmd)
	if n < 5 || cmd[n-5] != "--no-chapters" || cmd[n-4] != "(" || !strings.HasSuffix(cmd[n-3], filepath.Join("VIDEO_TS", "VTS_01_1.VOB")) ||
		!strings.HasSuffix(cmd[n-2], filepath.Join("VIDEO_TS", "VTS_01_2.VOB")) || cmd[n-1] != ")" {
		t.Errorf("inputs = %q", cmd[max(0, n-5):])
	}
	if _, err := os.Stat(out + ".partial"); err == nil {
		t.Error("dry run wrote a partial file")
	}
}
```

`writeDVD` comes from Task 5's `dvd_test.go`, in the same package.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/mux/ ./ -run 'ArgsConcat|IdentifyStreamIDs|MapDVDTracks|WriteChapterFile|RipDVDDryRun'`
Expected: FAIL to compile (`unknown field Concat`, `undefined: mapDVDTracks`, …).

- [ ] **Step 3: Extend `internal/mux`**

In `args.go`, replace `Job` with:

```go
// Job describes one remux.
type Job struct {
	Input       string   // the input file (a playlist, .mpls); ignored when Concat is set
	Concat      []string // files mkvmerge reads as one input, passed as "( a b … )" (DVD title VOBs)
	ChapterFile string   // chapters to use instead of the inputs' own; "" keeps the inputs' chapters
	Output      string   // output file path
	Tracks      []Track  // output tracks, in output order
}
```

In `Args`, after `args := []string{"-o", job.Output}`, add:

```go
	if job.ChapterFile != "" {
		args = append(args, "--chapters", job.ChapterFile)
	}
```

Replace the final `return append(args, job.Input)` with:

```go
	if job.ChapterFile != "" {
		args = append(args, "--no-chapters")
	}
	if len(job.Concat) > 0 {
		args = append(args, "(")
		args = append(args, job.Concat...)
		return append(args, ")")
	}
	return append(args, job.Input)
```

Update the `Args` doc to: `// Args returns mkvmerge's arguments for job (without --gui-mode): output, the chapter file, per-track options, the track selection, the track order, then the input (a "( … )" group for Concat).`

In `identify.go`:
- Add `SubStreamID uint64 \`json:"sub_stream_id"\`` to the raw `Properties` struct.
- Add the following to `IdentifiedTrack`:

```go
	StreamID    uint16 // MPEG program stream ID; 0 if not reported
	SubStreamID uint16 // MPEG program stream private sub-stream ID; 0 if none
```

- In `parseIdentification`, set `StreamID: uint16(t.Properties.StreamID), SubStreamID: uint16(t.Properties.SubStreamID)` in the `IdentifiedTrack` literal.

- [ ] **Step 4: Implement the DVD job and restructure `Rip`**

`rip_dvd.go`:

```go
package zenvik

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/source"
)

// chapterPlaceholder stands in for the chapter file in a dry run, which
// writes nothing.
const chapterPlaceholder = "<chapters.txt>"

// dvdJob builds the mux job for DVD title t from the files under root.
// The returned cleanup removes the chapter file; call it after muxing.
func dvdJob(ctx context.Context, mk *mux.Mkvmerge, root string, t *Title, output string, dryRun bool) (mux.Job, []string, func(), error) {
	none := func() {}
	fsys := os.DirFS(root)
	dir := source.FindName(fsys, ".", "VIDEO_TS", true)
	if dir == "" {
		return mux.Job{}, nil, none, fmt.Errorf("zenvik: no VIDEO_TS folder under %s", root)
	}
	var inputs []string
	for _, c := range t.Clips {
		n := source.FindName(fsys, dir, c.ID, false)
		if n == "" {
			return mux.Job{}, nil, none, fmt.Errorf("zenvik: %s is missing from %s", c.ID, filepath.Join(root, dir))
		}
		inputs = append(inputs, filepath.Join(root, dir, n))
	}
	if len(inputs) == 0 {
		return mux.Job{}, nil, none, fmt.Errorf("%w: title %s has no VOB files", ErrMuxFailed, t.ID)
	}
	ident, err := mk.Identify(ctx, inputs[0])
	if err != nil {
		if ctx.Err() != nil {
			return mux.Job{}, nil, none, ctx.Err()
		}
		return mux.Job{}, nil, none, err
	}
	tracks, warnings := mapDVDTracks(t, ident)
	warnings = append(warnings, ident.Warnings...)
	if len(tracks) == 0 {
		return mux.Job{}, nil, none, fmt.Errorf("%w: mkvmerge found no tracks in title %s", ErrMuxFailed, t.ID)
	}
	job := mux.Job{Concat: inputs, Output: output, Tracks: tracks}
	cleanup := none
	if len(t.Chapters) > 0 {
		job.ChapterFile = chapterPlaceholder
		if !dryRun {
			p, err := writeChapterFile(t.Chapters)
			if err != nil {
				return mux.Job{}, nil, none, err
			}
			job.ChapterFile = p
			cleanup = func() { os.Remove(p) }
		}
	}
	return job, warnings, cleanup, nil
}

// mapDVDTracks matches the title's video and audio tracks to mkvmerge's by
// MPEG-PS stream key (stream_id<<8 | sub_stream_id, or stream_id alone):
// video first, then audio in IFO order, then any track the IFO doesn't
// describe, kept without a language or name. The first video and audio
// tracks are default. Subtitles are not here: mkvmerge does not read them
// from VOBs, so they come from an extracted VobSub file.
func mapDVDTracks(t *Title, id *mux.Identification) ([]mux.Track, []string) {
	key := func(it mux.IdentifiedTrack) uint16 {
		if it.SubStreamID != 0 {
			return it.StreamID<<8 | it.SubStreamID
		}
		return it.StreamID
	}
	byKey := map[uint16]mux.IdentifiedTrack{}
	for _, it := range id.Tracks {
		byKey[key(it)] = it
	}
	var tracks []mux.Track
	var warnings []string
	used := map[int]bool{}
	haveVideo, haveAudio := false, false
	add := func(pid uint16, kind, lang, name string) {
		it, ok := byKey[pid]
		if !ok || it.Type != kind {
			warnings = append(warnings, fmt.Sprintf("title %s: %s stream 0x%04X not found by mkvmerge; skipped", t.ID, kind, pid))
			return
		}
		if used[it.ID] {
			return
		}
		used[it.ID] = true
		if norm, ok := normalizeLanguage(lang); ok {
			lang = norm
		} else {
			lang = ""
		}
		def := false
		switch kind {
		case "video":
			def, haveVideo = !haveVideo, true
		case "audio":
			def, haveAudio = !haveAudio, true
		}
		tracks = append(tracks, mux.Track{ID: it.ID, Type: kind, Language: lang, Name: name, Default: def})
	}
	for _, v := range t.Video {
		add(v.PID, "video", "", "")
	}
	for _, a := range t.Audio {
		name := audioName(a, byKey[a.PID].Channels)
		if a.Description != "" {
			name += " (" + a.Description + ")"
		}
		add(a.PID, "audio", a.Language, name)
	}
	for _, it := range id.Tracks {
		if !used[it.ID] {
			used[it.ID] = true
			tracks = append(tracks, mux.Track{ID: it.ID, Type: it.Type})
			warnings = append(warnings, fmt.Sprintf("title %s: mkvmerge track %d (%s) is not described by the IFO; kept without a language", t.ID, it.ID, it.Type))
		}
	}
	return tracks, warnings
}

// writeChapterFile writes chapters in mkvmerge's simple (OGM) chapter
// format to a temporary file and returns its path.
func writeChapterFile(chapters []Chapter) (string, error) {
	f, err := os.CreateTemp("", "zenvik-chapters-*.txt")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, c := range chapters {
		fmt.Fprintf(&b, "CHAPTER%02d=%s\nCHAPTER%02dNAME=Chapter %02d\n", i+1, ogmTime(c.Start), i+1, i+1)
	}
	_, werr := f.WriteString(b.String())
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(f.Name())
		if werr != nil {
			return "", werr
		}
		return "", cerr
	}
	return f.Name(), nil
}

// ogmTime formats d as HH:MM:SS.mmm.
func ogmTime(d time.Duration) string {
	ms := d.Milliseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, ms%1000)
}
```

In `rip.go`:

1. Add `"github.com/chad3814/zenvik/internal/source"` to the imports.
2. Replace everything from `playlist := filepath.Join(root, "BDMV", "PLAYLIST", t.ID+".mpls")` through `command := append([]string{mk.Path}, mux.Args(job)...)` with:

```go
	partial := opts.OutputPath + ".partial"
	var job mux.Job
	var warnings []string
	if d.Format == DVD {
		var cleanup func()
		job, warnings, cleanup, err = dvdJob(ctx, mk, root, t, partial, opts.DryRun)
		if err != nil {
			return nil, err
		}
		defer cleanup()
	} else {
		job, warnings, err = blurayJob(ctx, mk, root, t, partial)
		if err != nil {
			return nil, err
		}
	}
	command := append([]string{mk.Path}, mux.Args(job)...)
```

3. Add `blurayJob`, which holds the moved Blu-ray logic:

```go
// blurayJob identifies the title's playlist under root and maps its tracks.
func blurayJob(ctx context.Context, mk *mux.Mkvmerge, root string, t *Title, output string) (mux.Job, []string, error) {
	playlist := filepath.Join(root, "BDMV", "PLAYLIST", t.ID+".mpls")
	ident, err := mk.Identify(ctx, playlist)
	if err != nil {
		if ctx.Err() != nil {
			return mux.Job{}, nil, ctx.Err()
		}
		return mux.Job{}, nil, err
	}
	tracks, warnings := mapTracks(t, ident)
	warnings = append(warnings, ident.Warnings...)
	if len(tracks) == 0 {
		return mux.Job{}, nil, fmt.Errorf("%w: mkvmerge found none of title %s's streams", ErrMuxFailed, t.ID)
	}
	return mux.Job{Input: playlist, Output: output, Tracks: tracks}, warnings, nil
}
```

4. In `mountRoot`:
   - Change its doc to `// mountRoot returns the directory that holds BDMV or VIDEO_TS for mkvmerge to read, …`.
   - Replace the `BDMV/index.bdmv` check with:

```go
	if !hasDiscMarker(m.Dir, d.Format) {
		err := fmt.Errorf("zenvik: mounted %s at %s but found no %s", d.src.Path, m.Dir, discMarker(d.Format))
		return "", nil, errors.Join(err, release())
	}
```

   - Add:

```go
// discMarker is the file that identifies a mounted disc of format f.
func discMarker(f Format) string {
	if f == DVD {
		return "VIDEO_TS/VIDEO_TS.IFO"
	}
	return "BDMV/index.bdmv"
}

// hasDiscMarker reports whether root holds format f's marker file; DVD
// names are matched ignoring case.
func hasDiscMarker(root string, f Format) bool {
	if f == DVD {
		fsys := os.DirFS(root)
		dir := source.FindName(fsys, ".", "VIDEO_TS", true)
		return dir != "" && source.FindName(fsys, dir, "VIDEO_TS.IFO", false) != ""
	}
	st, err := os.Stat(filepath.Join(root, "BDMV", "index.bdmv"))
	return err == nil && st.Mode().IsRegular()
}
```

5. Update the phase comment for `PhaseScanning` to `// mkvmerge scanning the title's files`.

- [ ] **Step 5: Run the tests**

Run: `go test -race ./...`
Expected: PASS, and every existing Blu-ray rip test still passes.

- [ ] **Step 6: Verify and commit**

Run the Global Constraints verification, plus `go vet -tags integration ./...`.

```bash
git add internal/mux rip.go rip_dvd.go rip_dvd_internal_test.go rip_dvd_test.go
git commit -m "Rip DVD titles: concatenate title VOBs, map tracks by stream ID, add IFO chapters"
```

---

### Task 10: DVD subtitles — extract VobSub (.idx/.sub) and mux it

mkvmerge ignores DVD subpicture streams inside VOBs (Task 1 notes). zenvik therefore copies the title's subpicture packs into a temporary VobSub `.sub` and writes a matching `.idx`, then gives mkvmerge the `.idx` as a second input. The `.idx` carries the size, the IFO palette and per-subtitle timestamps.

Timestamps:
- Every VOBU starts with a NAV pack. Its PCI gives `vobu_s_ptm`, the PTS at the VOBU start, at data offset +12. Its DSI gives the VOB ID (+24), the cell ID (+27) and `c_eltm`, the BCD elapsed time within the cell (+28).
- A subpicture packet's title time is the cell's start in the title (from the IFO, angle-1 cells), plus `c_eltm`, plus `(PTS − vobu_s_ptm) / 90 kHz`.
- This survives PTS resets at cell and VOB boundaries.

**Files:**
- Create: `internal/vobsub/vobsub.go`, `internal/vobsub/vobsub_test.go`
- Modify:
  - `title.go`: an unexported `dvd *dvdInfo` on `Title`.
  - `scan_dvd.go`: fill it in.
  - `internal/mux/args.go`: `Job.Extra`.
  - `rip.go`: `PhaseSubtitles`.
  - `rip_dvd.go`: extraction and subtitle tracks.
- Test: `internal/mux/mux_test.go` (add), `rip_dvd_internal_test.go` (add), `rip_dvd_test.go` (update the dry-run expectations)

**Interfaces:**
- Consumes:
  - Task 5: `scan_dvd.go`'s `dvdTitle`, `cellStarts` and `fillDVDTracks`.
  - Task 6: `dvdJob`, `mux.Job.Concat` and `mapDVDTracks`.
- Produces:
  - `vobsub.Extract(ctx, vobsub.Params) (*vobsub.Result, error)`
  - `vobsub.Cell{VOBID, CellID int; Start time.Duration}`
  - `vobsub.Stream{ID int; Language string}`
  - `mux.Input{Path string; Tracks []Track}` and `mux.Job.Extra []mux.Input`
  - `zenvik.PhaseSubtitles` (5)
  - A DVD rip's subtitle tracks come from the extracted `.idx`. In a dry run, the `.idx` path is the placeholder `<subtitles.idx>`.

- [ ] **Step 1: Write the failing vobsub test**

`internal/vobsub/vobsub_test.go`:

```go
package vobsub

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pack returns a 2048-byte MPEG-2 pack header with nothing after it.
func pack() []byte {
	p := make([]byte, 2048)
	copy(p, []byte{0, 0, 1, 0xBA, 0x44, 0, 4, 0, 4, 1, 0x01, 0x89, 0xC3, 0xF8})
	return p
}

// navPack builds a NAV pack for VOB vob, cell cell, cell elapsed time
// elapsed (whole seconds, 30 fps) and VOBU start PTS ptm.
func navPack(vob, cell, elapsed int, ptm uint32) []byte {
	p := pack()
	copy(p[0x0E:], []byte{0, 0, 1, 0xBB, 0x00, 0x12})
	copy(p[0x26:], []byte{0, 0, 1, 0xBF, 0x03, 0xD4, 0x00})
	binary.BigEndian.PutUint32(p[0x2D+12:], ptm)
	copy(p[0x400:], []byte{0, 0, 1, 0xBF, 0x03, 0xFA, 0x01})
	dsi := p[0x407:]
	binary.BigEndian.PutUint16(dsi[24:], uint16(vob))
	dsi[27] = byte(cell)
	bcd := func(n int) byte { return byte(n/10<<4 | n%10) }
	dsi[28], dsi[29], dsi[30], dsi[31] = bcd(elapsed/3600), bcd(elapsed/60%60), bcd(elapsed%60), 3<<6
	return p
}

// spuPack builds a private-stream-1 pack for subpicture stream id; a
// non-nil pts marks the first pack of a subpicture packet.
func spuPack(id int, pts *uint32) []byte {
	p := pack()
	copy(p[14:], []byte{0, 0, 1, 0xBD, 0x07, 0xEC, 0x81})
	o := 14
	if pts == nil {
		p[o+7], p[o+8] = 0x00, 0
		p[o+9] = 0x20 + byte(id)
		return p
	}
	v := *pts
	p[o+7], p[o+8] = 0x80, 5
	p[o+9] = 0x21 | byte(v>>29)&0x0E
	p[o+10] = byte(v >> 22)
	p[o+11] = byte(v>>14)&0xFE | 1
	p[o+12] = byte(v >> 7)
	p[o+13] = byte(v<<1)&0xFE | 1
	p[o+14] = 0x20 + byte(id)
	return p
}

func videoPack() []byte {
	p := pack()
	copy(p[14:], []byte{0, 0, 1, 0xE0, 0x07, 0xEC, 0x81, 0x00, 0x00})
	return p
}

func u32(v uint32) *uint32 { return &v }

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	var vob1, vob2 []byte
	vob1 = append(vob1, navPack(1, 1, 0, 900000)...)              // cell 1 starts at title 0; VOBU PTS 10 s
	vob1 = append(vob1, spuPack(0, u32(900000+180000))...)       // stream 0 at 10 s + 2 s → title 2 s
	vob1 = append(vob1, spuPack(0, nil)...)                      // continuation of that packet
	vob1 = append(vob1, videoPack()...)
	vob1 = append(vob1, spuPack(2, u32(900000))...)              // stream 2 is not requested
	vob2 = append(vob2, navPack(1, 2, 3, 0)...)                  // cell 2 (starts at 25 min), 3 s in, PTS reset to 0
	vob2 = append(vob2, spuPack(1, u32(45000))...)               // stream 1 at 0.5 s → 25:03.500
	p1, p2 := filepath.Join(dir, "VTS_01_1.VOB"), filepath.Join(dir, "VTS_01_2.VOB")
	if err := os.WriteFile(p1, vob1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, vob2, 0o644); err != nil {
		t.Fatal(err)
	}
	var palette [16]uint32
	palette[0], palette[1] = 0x00108080, 0x00EB8080
	out := t.TempDir()
	res, err := Extract(context.Background(), Params{
		VOBs:    []string{p1, p2},
		Cells:   []Cell{{VOBID: 1, CellID: 1, Start: 0}, {VOBID: 1, CellID: 2, Start: 25 * time.Minute}},
		Streams: []Stream{{ID: 0, Language: "en"}, {ID: 1, Language: "fr"}, {ID: 3, Language: "de"}},
		Palette: palette, Width: 720, Height: 480, Dir: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Streams) != 2 || res.Streams[0].ID != 0 || res.Streams[1].ID != 1 {
		t.Errorf("streams = %+v (stream 3 has no subtitles and must be left out)", res.Streams)
	}
	idx, err := os.ReadFile(res.IDX)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# VobSub index file, v7 (do not modify this line!)\n",
		"size: 720x480\n",
		"palette: 000000, ffffff, 000000,",
		"id: en, index: 0\ntimestamp: 00:00:02:000, filepos: 000000000\n",
		"id: fr, index: 1\ntimestamp: 00:25:03:500, filepos: 000001000\n",
	} {
		if !strings.Contains(string(idx), want) {
			t.Errorf("idx lacks %q:\n%s", want, idx)
		}
	}
	if strings.Contains(string(idx), "index: 2") || strings.Contains(string(idx), "index: 3") {
		t.Errorf("idx has unrequested or empty streams:\n%s", idx)
	}
	sub, err := os.ReadFile(strings.TrimSuffix(res.IDX, ".idx") + ".sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 3*2048 {
		t.Errorf(".sub is %d bytes, want 3 packs", len(sub))
	}
}

func TestExtractRejectsPartialPack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.vob")
	if err := os.WriteFile(p, make([]byte, 3000), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(context.Background(), Params{VOBs: []string{p}, Streams: []Stream{{ID: 0}}, Width: 720, Height: 480, Dir: t.TempDir()}); err == nil {
		t.Error("want an error for a VOB that is not a whole number of packs")
	}
}

func TestPaletteRGB(t *testing.T) {
	for in, want := range map[uint32]string{0x00108080: "000000", 0x00EB8080: "ffffff", 0x00515AF0: "0000ff"} {
		if got := rgb(in); got != want {
			t.Errorf("rgb(%06X) = %s, want %s", in, got, want)
		}
	}
}

func TestPTSDelta(t *testing.T) {
	if got := ptsDelta(90000, 0); got != time.Second {
		t.Errorf("forward = %v", got)
	}
	if got := ptsDelta(10, 1<<33-80); got != 90*time.Second/90000 {
		t.Errorf("wrap = %v", got)
	}
	if got := ptsDelta(0, 90000); got != -time.Second {
		t.Errorf("backward = %v", got)
	}
}
```

Run: `go test ./internal/vobsub/`
Expected: FAIL to compile (`undefined: Extract`).

- [ ] **Step 2: Implement the extractor**

`internal/vobsub/vobsub.go`:

```go
// Package vobsub copies DVD subpicture streams out of title VOBs into a
// VobSub .idx/.sub pair, which mkvmerge reads (it ignores subpictures
// inside VOBs).
package vobsub

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const packSize = 2048

// Cell is an angle-1 cell of the title and where it starts in the title.
type Cell struct {
	VOBID, CellID int
	Start         time.Duration
}

// Stream is a subpicture stream to extract.
type Stream struct {
	ID       int    // physical stream number, 0..31 (sub-stream 0x20+ID)
	Language string // ISO 639-1 code for the .idx; "" writes "--"
}

// Params describe an extraction.
type Params struct {
	VOBs          []string // the title's VOB files, in order
	Cells         []Cell   // the title's angle-1 cells
	Streams       []Stream // streams to extract, in output order
	Palette       [16]uint32
	Width, Height int
	Dir           string                   // where subtitles.idx and subtitles.sub are written
	OnProgress    func(done, total int64) // optional: bytes read so far
}

// Result is a finished extraction.
type Result struct {
	IDX     string   // path of the .idx; the .sub sits beside it
	Streams []Stream // the streams that had at least one subtitle, in .idx order
}

type entry struct {
	at  time.Duration
	pos int64
}

// Extract copies every pack of the requested streams into Dir/subtitles.sub
// and writes Dir/subtitles.idx.
func Extract(ctx context.Context, p Params) (*Result, error) {
	want := map[int]bool{}
	for _, s := range p.Streams {
		want[s.ID] = true
	}
	starts := map[[2]int]time.Duration{}
	for _, c := range p.Cells {
		starts[[2]int{c.VOBID, c.CellID}] = c.Start
	}
	var total int64
	for _, v := range p.VOBs {
		st, err := os.Stat(v)
		if err != nil {
			return nil, err
		}
		if st.Size()%packSize != 0 {
			return nil, fmt.Errorf("vobsub: %s is not a whole number of 2048-byte packs", v)
		}
		total += st.Size()
	}
	subPath := filepath.Join(p.Dir, "subtitles.sub")
	sub, err := os.Create(subPath)
	if err != nil {
		return nil, err
	}
	w := bufio.NewWriterSize(sub, 1<<20)
	entries := map[int][]entry{}
	var subPos, done int64
	var base time.Duration
	var ptm uint32
	known := false
	buf := make([]byte, packSize)
	for _, v := range p.VOBs {
		f, err := os.Open(v)
		if err != nil {
			sub.Close()
			return nil, err
		}
		r := bufio.NewReaderSize(f, 1<<20)
		for n := 0; ; n++ {
			if n%4096 == 0 {
				if err := ctx.Err(); err != nil {
					f.Close()
					sub.Close()
					return nil, err
				}
				if p.OnProgress != nil {
					p.OnProgress(done, total)
				}
			}
			if _, err := io.ReadFull(r, buf); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				f.Close()
				sub.Close()
				return nil, fmt.Errorf("vobsub: reading %s: %w", v, err)
			}
			done += packSize
			if vob, cell, elapsed, s, ok := navInfo(buf); ok {
				start, found := starts[[2]int{vob, cell}]
				base, ptm, known = start+elapsed, s, found
				continue
			}
			id, pts, hasPTS, ok := spuInfo(buf)
			if !ok || !want[id] {
				continue
			}
			if hasPTS && known {
				at := max(base+ptsDelta(pts, ptm), 0)
				entries[id] = append(entries[id], entry{at: at, pos: subPos})
			}
			if _, err := w.Write(buf); err != nil {
				f.Close()
				sub.Close()
				return nil, err
			}
			subPos += packSize
		}
		f.Close()
	}
	if err := w.Flush(); err != nil {
		sub.Close()
		return nil, err
	}
	if err := sub.Close(); err != nil {
		return nil, err
	}
	if p.OnProgress != nil {
		p.OnProgress(total, total)
	}
	res := &Result{IDX: filepath.Join(p.Dir, "subtitles.idx")}
	var b strings.Builder
	b.WriteString("# VobSub index file, v7 (do not modify this line!)\n# Created by zenvik\n")
	fmt.Fprintf(&b, "size: %dx%d\n", p.Width, p.Height)
	colors := make([]string, len(p.Palette))
	for i, c := range p.Palette {
		colors[i] = rgb(c)
	}
	fmt.Fprintf(&b, "palette: %s\n", strings.Join(colors, ", "))
	b.WriteString("langidx: 0\n")
	for _, s := range p.Streams {
		es := entries[s.ID]
		if len(es) == 0 {
			continue
		}
		res.Streams = append(res.Streams, s)
		lang := s.Language
		if lang == "" {
			lang = "--"
		}
		fmt.Fprintf(&b, "\nid: %s, index: %d\n", lang, s.ID)
		for _, e := range es {
			ms := e.at.Milliseconds()
			fmt.Fprintf(&b, "timestamp: %02d:%02d:%02d:%03d, filepos: %09x\n", ms/3_600_000, ms/60_000%60, ms/1000%60, ms%1000, e.pos)
		}
	}
	if err := os.WriteFile(res.IDX, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	return res, nil
}

// navInfo reads a NAV pack: the DSI's VOB ID, cell ID and cell elapsed
// time, and the PCI's VOBU start PTS.
func navInfo(p []byte) (vob, cell int, elapsed time.Duration, ptm uint32, ok bool) {
	if !packHeader(p) || p[0x0E] != 0 || p[0x0F] != 0 || p[0x10] != 1 || p[0x11] != 0xBB ||
		p[0x26] != 0 || p[0x27] != 0 || p[0x28] != 1 || p[0x29] != 0xBF || p[0x2C] != 0x00 ||
		p[0x400] != 0 || p[0x401] != 0 || p[0x402] != 1 || p[0x403] != 0xBF || p[0x406] != 0x01 {
		return 0, 0, 0, 0, false
	}
	ptm = binary.BigEndian.Uint32(p[0x2D+12:])
	dsi := p[0x407:]
	return int(binary.BigEndian.Uint16(dsi[24:])), int(dsi[27]), bcdTime(dsi[28:32]), ptm, true
}

// spuInfo reports whether pack p carries a subpicture PES packet (private
// stream 1, sub-stream 0x20–0x3F), its stream number, and its PTS if the
// packet has one.
func spuInfo(p []byte) (id int, pts uint32, hasPTS, ok bool) {
	if !packHeader(p) {
		return 0, 0, false, false
	}
	o := 14 + int(p[13]&7)
	if o+9 > len(p) || p[o] != 0 || p[o+1] != 0 || p[o+2] != 1 || p[o+3] != 0xBD {
		return 0, 0, false, false
	}
	hl := int(p[o+8])
	if o+9+hl >= len(p) {
		return 0, 0, false, false
	}
	sub := p[o+9+hl]
	if sub&0xE0 != 0x20 {
		return 0, 0, false, false
	}
	if p[o+7]&0x80 != 0 && hl >= 5 {
		b := p[o+9 : o+14]
		pts = uint32(b[0]>>1&7)<<30 | uint32(b[1])<<22 | uint32(b[2]>>1)<<15 | uint32(b[3])<<7 | uint32(b[4]>>1)
		hasPTS = true
	}
	return int(sub & 0x1F), pts, hasPTS, true
}

func packHeader(p []byte) bool {
	return len(p) == packSize && p[0] == 0 && p[1] == 0 && p[2] == 1 && p[3] == 0xBA && p[4]&0xC0 == 0x40
}

// ptsDelta returns pts − ref on the 33-bit 90 kHz clock, choosing the
// shorter way around a wrap.
func ptsDelta(pts, ref uint32) time.Duration {
	const mod = int64(1) << 33
	d := (int64(pts) - int64(ref)) % mod
	if d < 0 {
		d += mod
	}
	if d >= mod/2 {
		d -= mod
	}
	return time.Duration(d) * time.Second / 90000
}

// bcdTime decodes a 4-byte BCD playback time, tolerating bad digits and
// rates (they decode as zero or 30 fps).
func bcdTime(b []byte) time.Duration {
	dec := func(x byte) int {
		hi, lo := int(x>>4), int(x&0x0F)
		if hi > 9 || lo > 9 {
			return 0
		}
		return hi*10 + lo
	}
	d := time.Duration(dec(b[0]))*time.Hour + time.Duration(dec(b[1]))*time.Minute + time.Duration(dec(b[2]))*time.Second
	frames := time.Duration(dec(b[3] & 0x3F))
	if b[3]>>6 == 1 {
		return d + frames*40*time.Millisecond
	}
	return d + frames*1001*time.Second/30000
}

// rgb converts a palette entry (0x00YYCrCb, studio range) to "rrggbb"
// with the BT.601 matrix.
func rgb(c uint32) string {
	y := float64(c>>16&0xFF) - 16
	cr := float64(c>>8&0xFF) - 128
	cb := float64(c&0xFF) - 128
	clamp := func(v float64) int { return int(math.Max(0, math.Min(255, math.Round(v)))) }
	r := clamp(1.164*y + 1.596*cr)
	g := clamp(1.164*y - 0.813*cr - 0.391*cb)
	b := clamp(1.164*y + 2.018*cb)
	return fmt.Sprintf("%02x%02x%02x", r, g, b)
}
```

Run: `go test -race ./internal/vobsub/`
Expected: PASS. In `TestPaletteRGB`, `0x00515AF0` is pure blue in BT.601 studio range: Y 81, Cb 240, Cr 90. If it rounds to a neighbor such as `0000fe`, fix the expected value to what the formula yields and record that in the report. The formula is the requirement.

- [ ] **Step 3: Extra mux inputs**

In `internal/mux/args.go`, add:

```go
// Input is a further input file with its own output tracks.
type Input struct {
	Path   string
	Tracks []Track // output tracks from this file, in output order
}
```

Add `Extra []Input // more input files, after the main one (DVD subtitles)` to `Job`. Then restructure `Args` so that:
- The per-track options and selection for the main input stay exactly where they are.
- `--track-order` lists the main input's tracks as `0:id`, then each extra input's tracks as `<n>:id` (n = 1, 2, …).
- After the main input or its `( … )` group, each extra input is emitted as its per-track options (`--language`, `--track-name`, `--default-track-flag`), then its selection (`--video-tracks`/`--no-video`, and so on, the same `selection` helper), then its path.

Factor the per-track-option and selection building into a helper, `fileArgs(tracks []Track) []string`, used for every input, so the logic isn't duplicated. All existing `Args` tests must still pass unchanged.

Add to `internal/mux/mux_test.go`:

```go
func TestArgsExtraInput(t *testing.T) {
	got := Args(Job{Concat: []string{"a.vob"}, Output: "o.mkv",
		Tracks: []Track{{ID: 0, Type: "video", Default: true}},
		Extra:  []Input{{Path: "s.idx", Tracks: []Track{{ID: 0, Type: "subtitles", Language: "eng", Name: "Forced"}}}}})
	want := []string{"-o", "o.mkv", "--default-track-flag", "0:yes",
		"--video-tracks", "0", "--no-audio", "--no-subtitles", "--track-order", "0:0,1:0",
		"(", "a.vob", ")",
		"--language", "0:eng", "--track-name", "0:Forced", "--default-track-flag", "0:no",
		"--no-video", "--no-audio", "--subtitle-tracks", "0", "s.idx"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Args =\n%q\nwant\n%q", got, want)
	}
}
```

- [ ] **Step 4: Carry the DVD data on the title**

In `title.go`, add to `Title` (unexported, after `Rank`):

```go
	dvd *dvdInfo // DVD only: what ripping needs beyond the public fields
```

and append:

```go
// dvdInfo is what ripping a DVD title needs beyond its public fields.
type dvdInfo struct {
	cells         []vobsub.Cell     // angle-1 cells and their start times
	palette       [16]uint32        // the PGC's subpicture palette
	width, height int               // video size
	subLang       map[uint16]string // subtitle PID → ISO 639-1 code from the IFO
}
```

Import `github.com/chad3814/zenvik/internal/vobsub`.

In `scan_dvd.go`:
- Give `fillDVDTracks` a fourth parameter, `info *dvdInfo`. Inside its subpicture loop, record `info.subLang[pid] = s.Language` for each subtitle track it adds.
- In `dvdTitle`, before calling `fillDVDTracks`, build:

```go
	info := &dvdInfo{palette: pgc.Palette, width: ts.vts.Video.Width, height: ts.vts.Video.Height, subLang: map[uint16]string{}}
	for i, cl := range pgc.Cells {
		if !cl.AngleBlock || cl.BlockMode == dvd.FirstInBlock || cl.BlockMode == dvd.NotInBlock {
			info.cells = append(info.cells, vobsub.Cell{VOBID: cl.VOBID, CellID: cl.CellID, Start: starts[i]})
		}
	}
	t.dvd = info
```

Then call `fillDVDTracks(t, ts.vts, pgc, info)`.

- [ ] **Step 5: Extract in the rip**

In `rip.go`:
- Add `PhaseSubtitles Phase = 5 // extracting DVD subtitles (before scanning)` after `PhaseFinalizing`.
- Add `case PhaseSubtitles: return "extracting subtitles"` to `Phase.String`.
- Change the `dvdJob` call to pass `report`: `dvdJob(ctx, mk, root, t, partial, opts.DryRun, report)`.

In `rip_dvd.go`:
- Add the parameter `report func(Phase, float64)` to `dvdJob`.
- Add the constant `subtitlePlaceholder = "<subtitles.idx>"`.
- Add the helper below, and import `internal/vobsub`.

```go
// subtitleTracks are the mux tracks of a VobSub input whose streams are
// subs, in .idx order: track i is subs[i]. None is default.
func subtitleTracks(subs []SubtitleTrack) []mux.Track {
	out := make([]mux.Track, len(subs))
	for i, s := range subs {
		lang, _ := normalizeLanguage(s.Language)
		out[i] = mux.Track{ID: i, Type: "subtitles", Language: lang, Name: s.Description}
	}
	return out
}
```

In `dvdJob`, after building `job` and before the chapter-file block, add:

```go
	if len(t.Subtitles) > 0 && t.dvd != nil {
		if dryRun {
			job.Extra = []mux.Input{{Path: subtitlePlaceholder, Tracks: subtitleTracks(t.Subtitles)}}
		} else {
			dir, err := os.MkdirTemp("", "zenvik-subtitles-")
			if err != nil {
				return mux.Job{}, nil, none, err
			}
			removeDir := func() { os.RemoveAll(dir) }
			streams := make([]vobsub.Stream, len(t.Subtitles))
			byID := map[int]SubtitleTrack{}
			for i, s := range t.Subtitles {
				id := int(s.PID - 0xBD20)
				streams[i] = vobsub.Stream{ID: id, Language: t.dvd.subLang[s.PID]}
				byID[id] = s
			}
			report(PhaseSubtitles, 0)
			res, err := vobsub.Extract(ctx, vobsub.Params{
				VOBs: inputs, Cells: t.dvd.cells, Streams: streams, Palette: t.dvd.palette,
				Width: t.dvd.width, Height: t.dvd.height, Dir: dir,
				OnProgress: func(done, total int64) {
					if total > 0 {
						report(PhaseSubtitles, float64(done)/float64(total))
					}
				},
			})
			if err != nil {
				removeDir()
				if ctx.Err() != nil {
					return mux.Job{}, nil, none, ctx.Err()
				}
				return mux.Job{}, nil, none, err
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
			cleanup = removeDir
		}
	}
```

Declare `cleanup := none` **before** this block (move it up from the chapter block). In the chapter block, wrap the existing cleanup so both run:

```go
			prev := cleanup
			cleanup = func() { os.Remove(p); prev() }
```

Also route any error that happens after `cleanup` is set (the chapter file write) through `cleanup()` before returning. Add `slices` to the imports.

- [ ] **Step 6: Update the rip tests**

In `rip_dvd_test.go` `TestRipDVDDryRun`:
- The last elements of the command are now `…, "(", in1, in2, ")", "--language", "0:eng", …, "<subtitles.idx>"`. Replace the inputs check so it finds `"("` and checks that `"--no-chapters"` precedes it, that the two VOB paths follow, and that `")"` follows them. Do not assume they are the last five elements.
- Add the checks below. Track IDs for the `.idx` input restart at 0, so use `slices.Index` to locate the subtitle options after the `)`.

```go
	tail := cmd[slices.Index(cmd, ")")+1:]
	if tail[len(tail)-1] != "<subtitles.idx>" || !hasPair(tail, "--language", "0:eng") || !hasPair(tail, "--language", "1:fre") ||
		!hasPair(tail, "--track-name", "2:Forced") || !hasPair(tail, "--default-track-flag", "0:no") || !hasPair(tail, "--subtitle-tracks", "0,1,2") {
		t.Errorf("subtitle input = %q", tail)
	}
	if !hasPair(cmd, "--track-order", "0:0,0:1,0:2,0:3,1:0,1:1,1:2") {
		t.Errorf("track order missing in %q", cmd)
	}
```

Add `slices` to that file's imports.

Add to `rip_dvd_internal_test.go`:

```go
func TestSubtitleTracks(t *testing.T) {
	got := subtitleTracks([]SubtitleTrack{{PID: 0xBD20, Language: "eng"}, {PID: 0xBD22, Language: "xx", Description: "Forced"}})
	want := []mux.Track{{ID: 0, Type: "subtitles", Language: "eng"}, {ID: 1, Type: "subtitles", Name: "Forced"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tracks = %+v", got)
	}
}
```

`"xx"` is not a valid 3-letter code, so its language is dropped.

- [ ] **Step 7: Run and commit**

Run: `go test -race ./...`
Expected: PASS.

Run the Global Constraints verification.

```bash
git add internal/vobsub internal/mux title.go scan_dvd.go rip.go rip_dvd.go rip_dvd_internal_test.go rip_dvd_test.go
git commit -m "Extract DVD subtitles to VobSub and mux them with the title"
```


---

### Task 7: CLI — `--title`, DVD info output, JSON `format`/`kind`

**Files:**
- Modify: `cmd/zenvik/rip.go`, `cmd/zenvik/info.go`, `cmd/zenvik/format.go`, `cmd/zenvik/main.go`
- Test: `cmd/zenvik/dvd_test.go`

**Interfaces:**
- Consumes:
  - Task 5: `zenvik.DVD`, `zenvik.Bluray`, `zenvik.VideoTSDir`, `Disc.Format`, `Title.Unsupported`, the track `Description` and `AspectRatio` fields, `zenvik.CodingVobSub` and `zenvik.ErrUnsupportedTitle`.
- Produces:
  - `rip --title/-t <id>`. It is mutually exclusive with `--playlist` (exit 2: `use --title or --playlist, not both`).
  - The `info` header kind is `DVD ISO image` for DVD ISOs.
  - JSON `kind` is `"video_ts"` for `VideoTSDir`.
  - New JSON fields:
    - `format` (`"bluray"` or `"dvd"`)
    - `unsupported` (omitempty)
    - `aspect_ratio` (omitempty)
    - `description` (omitempty) on audio and subtitle tracks
  - A subtitle `codec` of `"VobSub"`.

- [ ] **Step 1: Write the failing tests**

`cmd/zenvik/dvd_test.go`:

```go
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
	if code, _, errOut := runCLI("rip", "--title", "3", "-o", t.TempDir(), root); code != 1 || !strings.Contains(errOut, "starts or ends mid-file") {
		t.Errorf("unsupported title: code %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runCLI("rip", "--title", "1", "--playlist", "01", root); code != 2 || !strings.Contains(errOut, "not both") {
		t.Errorf("both flags: code %d, stderr %q", code, errOut)
	}
	t.Setenv("PATH", t.TempDir())
	code, out, _ := runCLI("rip", "-t", "1", "-o", t.TempDir(), root)
	if code != 4 || !strings.Contains(out, "Ripping 01") {
		t.Errorf("rip -t 1 without mkvmerge: code %d, out %q", code, out)
	}
}
```

`titleRow` is the existing helper in `main_test.go`. It skips the header line and returns the first title row containing its argument, so `"★"` finds the main row.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/zenvik/ -run DVD`
Expected: FAIL. The header says `VIDEO_TS folder` only once Task 4 is in (it is). The kind is `"unknown"`, there is no `format` field, `--title` is an unknown flag, and so on.

- [ ] **Step 3: Implement**

In `format.go`, add `"github.com/chad3814/zenvik/bluray"` to the imports, then add:

```go
// codecName names a track codec; DVD subtitles are "VobSub".
func codecName(c bluray.CodingType) string {
	if c == zenvik.CodingVobSub {
		return "VobSub"
	}
	return c.String()
}

// kindLabel names the source kind for people, e.g. "DVD ISO image".
func kindLabel(d *zenvik.Disc) string {
	if d.Kind == zenvik.ISO && d.Format == zenvik.DVD {
		return "DVD ISO image"
	}
	return d.Kind.String()
}
```

In `info.go`:
- `Short`: `"List the titles on a Blu-ray or DVD disc image or folder"`.
- `Long`: replace the first sentence with `List the titles (playlists or DVD titles) on a Blu-ray or DVD disc image or folder, ranked so the likely main feature (★) comes first.`, and keep the rest. Add a sentence: `DVD titles that zenvik can't rip yet are noted with the reason.`
- In `writeTable`, change the header line to `fmt.Fprintf(out, "%s  (%s: %s)\n\n", name, kindLabel(d), d.Path)`.
- Add `Format string \`json:"format"\`` to `jsonDisc` after `Kind`.
- Add `Unsupported string \`json:"unsupported,omitempty"\`` to `jsonTitle` after `Encrypted`.
- Add `AspectRatio string \`json:"aspect_ratio,omitempty"\`` to `jsonVideo`.
- Add `Description string \`json:"description,omitempty"\`` to both `jsonAudio` and `jsonSubtitle`.
- Add `case zenvik.VideoTSDir: return "video_ts"` to `kindName`.
- Add:

```go
// formatName is the machine-readable disc format for JSON output.
func formatName(f zenvik.Format) string {
	switch f {
	case zenvik.Bluray:
		return "bluray"
	case zenvik.DVD:
		return "dvd"
	}
	return "unknown"
}
```

- In `writeJSON`, set `Format: formatName(d.Format)` in the `jsonDisc` literal.
- In `toJSONTitle`:
  - Set `Unsupported: t.Unsupported`.
  - Use `codecName(...)` instead of `.Codec.String()` for video, audio and subtitles.
  - Set `AspectRatio: v.AspectRatio` on video, and `Description: a.Description` and `Description: s.Description` on audio and subtitles.

In `rip.go`:
- Add `titleID` to the `var playlist, …` line.
- Change `Long` to:

```go
		Long: `Remux one title of a Blu-ray or DVD disc image or folder to an MKV file
with mkvmerge, keeping every video, audio and subtitle track, chapters and
languages. Without --title (or --playlist), the main feature is chosen
automatically.`,
```

- Immediately after the `--year` check in `RunE`, add:

```go
			if playlist != "" && titleID != "" {
				return usageError{errors.New("use --title or --playlist, not both")}
			}
			id := titleID
			if id == "" {
				id = playlist
			}
```

- Replace `pickTitle(d, playlist)` with `pickTitle(d, id)`, and `auto := playlist == ""` with `auto := id == ""`.
- Change the ambiguity warning's suffix from `pass --playlist to choose` to `pass --title to choose`.
- Add the flag before `--playlist`:

```go
	cmd.Flags().StringVarP(&titleID, "title", "t", "", "rip this title instead of the main feature: a DVD title number (e.g. 3) or a Blu-ray playlist (e.g. 00800)")
```

- Change the `--playlist` help to `"same as --title, for Blu-ray playlists (e.g. 00800)"`.
- Change the `pickTitle` doc to `// pickTitle returns the requested title, or the main title.`, and change its final line to:

```go
	return nil, errors.New("no title qualifies as the main feature; run `zenvik info --all` and pass --title")
```

In `main.go`, change the root `Short` to `"Inspect and remux unencrypted Blu-ray and DVD disc images"`.

- [ ] **Step 4: Update the existing tests that assert old wording, then run all tests**

Run: `grep -rn 'pass --playlist\|Blu-ray ISO image or BDMV folder' cmd/zenvik/*_test.go`. Update each hit to the new wording (`pass --title`, and so on).
Run: `go test -race ./...`
Expected: PASS.

- [ ] **Step 5: Verify and commit**

Run the Global Constraints verification.

```bash
git add cmd/zenvik
git commit -m "CLI: add --title, show DVD sources in info, and add format to JSON"
```

---

### Task 8: Integration tests on real authored DVDs, and CI

**Files:**
- Modify: `internal/testdisc/dvdauthor.go` (add `ISOFromDir`), `.github/workflows/ci.yml`, `CLAUDE.md`
- Create: `rip_dvd_integration_test.go` (`//go:build integration`), `rip_dvd_iso_integration_test.go` (`//go:build integration && darwin`)

**Interfaces:**
- Consumes:
  - Task 1: `testdisc.AuthorDVD`.
  - Tasks 5–7 and 10: the DVD Open/Rip path, including the VobSub subtitles.
  - The existing integration helpers `identify`, `openDisc` and `assertDetached`.
- Produces: `func ISOFromDir(dir string, opt udfimage.Options) ([]byte, error)`, plus CI coverage.

- [ ] **Step 1: Add `ISOFromDir`**

Append to `internal/testdisc/dvdauthor.go`, adding the `io/fs` and `udfimage` imports:

```go
// ISOFromDir builds a UDF image of the files under dir, keeping their
// relative paths (use Revision 0x0102 for a DVD).
func ISOFromDir(dir string, opt udfimage.Options) ([]byte, error) {
	files := map[string]udfimage.File{}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = udfimage.File{Data: b}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return udfimage.Build(files, opt)
}
```

- [ ] **Step 2: Write the integration tests**

`rip_dvd_integration_test.go`:

```go
//go:build integration

package zenvik_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func authoredDVD(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "AUTHORED_DVD")
	if err := testdisc.AuthorDVD(context.Background(), dir, 15); err != nil {
		t.Fatal(err)
	}
	return dir
}

// containerDuration returns the duration mkvmerge reports for a file.
func containerDuration(t *testing.T, path string) time.Duration {
	t.Helper()
	out, err := exec.Command("mkvmerge", "-J", path).Output()
	if err != nil && len(out) == 0 {
		t.Fatal(err)
	}
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

// chapterStarts extracts an MKV's chapter start times with mkvextract.
func chapterStarts(t *testing.T, path string) []time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "chapters.txt")
	if out, err := exec.Command("mkvextract", path, "chapters", "--simple", txt).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract: %v\n%s", err, out)
	}
	b, err := os.ReadFile(txt)
	if err != nil {
		t.Fatal(err)
	}
	var starts []time.Duration
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok || strings.HasSuffix(k, "NAME") {
			continue
		}
		var h, m, s, ms int
		if _, err := fmt.Sscanf(v, "%d:%d:%d.%d", &h, &m, &s, &ms); err != nil {
			t.Fatalf("chapter line %q: %v", sc.Text(), err)
		}
		starts = append(starts, time.Duration(h)*time.Hour+time.Duration(m)*time.Minute+time.Duration(s)*time.Second+time.Duration(ms)*time.Millisecond)
	}
	return starts
}

// firstTimestamp returns the first block timestamp of track tid in an MKV.
func firstTimestamp(t *testing.T, path string, tid int) time.Duration {
	t.Helper()
	txt := filepath.Join(t.TempDir(), "ts.txt")
	if out, err := exec.Command("mkvextract", path, "timestamps_v2", fmt.Sprintf("%d:%s", tid, txt)).CombinedOutput(); err != nil {
		t.Fatalf("mkvextract timestamps: %v\n%s", err, out)
	}
	b, err := os.ReadFile(txt)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 2 {
		t.Fatalf("no timestamps for track %d:\n%s", tid, b)
	}
	var ms float64
	if _, err := fmt.Sscanf(lines[1], "%g", &ms); err != nil {
		t.Fatalf("timestamp line %q: %v", lines[1], err)
	}
	return time.Duration(ms * float64(time.Millisecond))
}

func assertDVDRipped(t *testing.T, out string, m *zenvik.Title) {
	t.Helper()
	id := identify(t, out)
	var types, langs []string
	for _, tr := range id.Tracks {
		types = append(types, tr.Type)
		if tr.Type == "audio" {
			langs = append(langs, tr.Language)
		}
	}
	if strings.Join(types, ",") != "video,audio,audio,subtitles" {
		t.Errorf("tracks = %v", types)
	}
	if strings.Join(langs, ",") != "eng,fre" {
		t.Errorf("audio languages = %v", langs)
	}
	if d := containerDuration(t, out); d < m.Duration-250*time.Millisecond || d > m.Duration+250*time.Millisecond {
		t.Errorf("duration = %v, title says %v", d, m.Duration)
	}
	if first := firstTimestamp(t, out, 3); first < 1900*time.Millisecond || first > 2100*time.Millisecond {
		t.Errorf("first subtitle at %v, want 2.0 s (spumux shows it from 2 s)", first)
	}
	starts := chapterStarts(t, out)
	if len(starts) != len(m.Chapters) {
		t.Fatalf("chapters = %v, title has %v", starts, m.Chapters)
	}
	const frame = 34 * time.Millisecond
	for i, c := range m.Chapters {
		if diff := starts[i] - c.Start; diff < -frame || diff > frame {
			t.Errorf("chapter %d at %v, IFO says %v", i+1, starts[i], c.Start)
		}
	}
}

func TestRipDVDDirectory(t *testing.T) {
	d := openDisc(t, authoredDVD(t))
	if d.Format != zenvik.DVD || d.Kind != zenvik.VideoTSDir {
		t.Fatalf("format %v, kind %v", d.Format, d.Kind)
	}
	m := d.Main()
	if m == nil || m.ID != "01" || m.Unsupported != "" || len(m.Clips) != 2 {
		t.Fatalf("main = %+v", m)
	}
	out := filepath.Join(t.TempDir(), "dvd.mkv")
	if _, err := d.Rip(context.Background(), m, zenvik.RipOptions{OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	assertDVDRipped(t, out, m)
}
```

`rip_dvd_iso_integration_test.go`:

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

func TestRipDVDISO(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	img, err := testdisc.ISOFromDir(authoredDVD(t), udfimage.Options{Revision: 0x0102, Label: "AUTHORED_DVD"})
	if err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(t.TempDir(), "authored dvd.iso")
	if err := os.WriteFile(iso, img, 0o644); err != nil {
		t.Fatal(err)
	}
	d := openDisc(t, iso)
	if d.Kind != zenvik.ISO || d.Format != zenvik.DVD {
		t.Fatalf("kind %v, format %v", d.Kind, d.Format)
	}
	m := d.Main()
	if m == nil {
		t.Fatal("no main title")
	}
	out := filepath.Join(t.TempDir(), "dvd.mkv")
	if _, err := d.Rip(context.Background(), m, zenvik.RipOptions{OutputPath: out}); err != nil {
		t.Fatal(err)
	}
	assertDVDRipped(t, out, m)
	assertDetached(t, iso)
}
```

If `hdiutil` refuses to attach the 0x0102 image built by `udfimage`, build the ISO with `hdiutil makehybrid -udf -udfversion 1.02 -default-volume-name AUTHORED_DVD -o <iso> <dir>` instead, and record that under Deviations. Do not change `udfimage`.

- [ ] **Step 3: Run the integration tests locally**

Run: `go test -tags integration -run 'DVD|AuthorDVD' -v ./ ./internal/testdisc/`
Expected: PASS. Afterwards `hdiutil info | grep -c zenvik-mount` prints 0.

If the chapter comparison fails by more than one frame, report DONE_WITH_CONCERNS with the observed and expected times. Do not loosen the tolerance.

- [ ] **Step 4: CI and CLAUDE.md**

In `.github/workflows/ci.yml`:
- integration-macos: `brew install mkvtoolnix ffmpeg dvdauthor`
- integration-linux: `sudo apt-get update && sudo apt-get install -y mkvtoolnix ffmpeg dvdauthor`

In `CLAUDE.md`, change the integration-tests bullet to:
`- Integration tests (need mkvmerge, mkvextract, ffmpeg and dvdauthor/spumux; macOS also exercises hdiutil): go test -tags integration ./...`

- [ ] **Step 5: Verify and commit**

Run the Global Constraints verification, plus `go vet -tags integration ./...` and `go test -tags integration ./...`. That last one runs every integration test, including the Blu-ray ones.

```bash
git add internal/testdisc/dvdauthor.go rip_dvd_integration_test.go rip_dvd_iso_integration_test.go .github/workflows/ci.yml CLAUDE.md
git commit -m "Test DVD rips on real authored discs and install dvdauthor in CI"
```

---

### Task 9: Documentation and spec sync

**Files:**
- Modify: `README.md`, `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`, `docs/superpowers/specs/2026-10-01-zenvik-m5-dvd-design.md`

**Interfaces:**
- Consumes: everything, plus the Task 1 notes and the controller's rulings passed in the dispatch.
- Produces: user docs for DVD, and specs that match the shipped code.

The code wins. Where a spec or this task's text disagrees with what shipped, read the code and document what it does.

- [ ] **Step 1: README**

1. Change the tagline and intro: zenvik remuxes unencrypted **Blu-ray and DVD** images and folders.
2. Inputs: list a DVD ISO, a `VIDEO_TS` folder, or a folder containing `VIDEO_TS`.
3. Add a `## DVDs` section:

````markdown
## DVDs

zenvik reads unencrypted DVD-Video discs: an ISO image, a `VIDEO_TS` folder,
or a folder that contains one. `zenvik info` lists the disc's titles
(numbered `01`, `02`, …) and picks the main feature the same way it does for
Blu-ray. Rip a specific title with `--title`:

```
zenvik rip --title 3 "/path/to/MY_DVD"
```

zenvik passes the title's VOB files straight to mkvmerge, so it can rip a
title only when the title is made of **whole VOB files**. That covers most
movie main features and "play all" titles. Titles that start or end in the
middle of a VOB file, which includes most individual TV episodes, are listed
as `not supported yet`. CSS-encrypted discs are detected and refused.

Each audio and subtitle track keeps its language from the disc. Commentary
and forced subtitles are named, for example `AC-3 Stereo (Director's
Commentary)` or `Forced`. Chapters come from the disc's program map.
````

   Add the sentence: `DVD subtitles are extracted to VobSub (with the disc's palette) during the rip, so ripping a DVD title reads it once before muxing.`

4. In the `info --json` description (or a new short paragraph), document:
   - `format` is `"bluray"` or `"dvd"`.
   - `kind` is `"iso"`, `"bdmv"` or `"video_ts"`.
   - `unsupported`, `aspect_ratio` and `description` appear when present.
5. Requirements: say that integration tests also need `dvdauthor`. Users need only MKVToolNix.

- [ ] **Step 2: v1 spec**

In `2026-10-01-zenvik-v1-design.md`:
- §1 Goals inputs: add `and, since M5, DVD ISO images and VIDEO_TS folders (see the M5 spec)`.
- §1 Non-goals: change the DVD bullet to `DVD (VIDEO_TS) arrived in milestone 5; see 2026-10-01-zenvik-m5-dvd-design.md.`
- §2: change `DVD later adds a dvd/ parser and a new source kind` to the past tense, and add `dvd/` to the tree.
- §3: change `type SourceKind int // ISO, BDMVDir (later: VideoTSDir)` to `// ISO, BDMVDir, VideoTSDir`, and mention `Disc.Format`.
- §11 milestone 5: `DVD: done; see the M5 spec.`

- [ ] **Step 3: M5 spec sync**

Change the status to `Implemented`. Edit the sections so they match the code, and list every deviation from the reviewed spec in a new `## 10. Implementation notes`, one bullet each:
- Track names carry the codec, layout and description, not a language name. The language is set on the track. Audio: `AC-3 5.1`, `AC-3 Stereo (Director's Commentary)`. Subtitles: `Forced`, `Large`, …, or no name.
- The chapter file uses mkvmerge's simple OGM text format, not Matroska XML.
- New public fields:
  - `Disc.Format`
  - `Title.Unsupported`
  - `VideoTrack.AspectRatio`
  - `AudioTrack.Description` and `SubtitleTrack.Description`
  - `CodingVobSub`
  - `ErrUnsupportedTitle`

  `Disc.Title` accepts unpadded DVD numbers.
- `rip --title`/`-t` alias of `--playlist`.
- The JSON adds `format`, `unsupported`, `aspect_ratio` and `description`.
- Dry run: like Blu-ray, it mounts ISOs (to identify tracks). The chapter file appears as the placeholder `<chapters.txt>`.
- Integration ISOs are built by `internal/testdisc/udfimage` (UDF 1.02), or by `hdiutil makehybrid` if Task 8 recorded that deviation, not by `genisoimage`. Linux CI does not mount ISOs, as for Blu-ray.
- mkvmerge ignores DVD subpictures inside VOBs. zenvik extracts them to a temporary VobSub `.idx`/`.sub` (Task 10), with the IFO palette and NAV-pack-based timestamps, and muxes that file as a second input. The dry run shows `<subtitles.idx>`. This replaces §6.1's palette fallback.
- Title VOBs are passed as mkvmerge's `( a.VOB b.VOB )` group, because mkvmerge chains sibling VOBs on its own and `+` duplicated content.
- Every controller ruling passed in the dispatch.

- [ ] **Step 4: Verify and commit**

Run the Global Constraints verification and `go test -tags integration ./...`. Smoke-test `go run ./cmd/zenvik info <a SampleDVD folder written by a scratch Go test or by AuthorDVD>`. This is optional when the integration tests pass.

```bash
git add README.md docs/superpowers/specs
git commit -m "Document DVD support and sync the specs with the shipped code"
```
