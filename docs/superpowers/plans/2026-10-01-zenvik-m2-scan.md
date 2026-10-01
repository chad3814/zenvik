# zenvik Milestone 2 (Scan) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `zenvik.Open(ctx, path)` turns an ISO image or BDMV folder into a ranked list of titles: main-feature detection, duplicate and fake-playlist handling, and encryption detection. `zenvik info <path>` shows that list as a table or as JSON.

**Architecture:** `internal/source` resolves the path to an `fs.FS` whose root holds `BDMV/`. ISOs are read through the `udf` package and never mounted; folders go through `os.DirFS`. The root package `zenvik` reads index, movie-object, playlist and metadata files with the `bluray` parsers. It samples each referenced clip once for AACS encryption and builds `Title` values. It then asks `internal/rank` to filter, de-duplicate, score and pick the main title. `cmd/zenvik` is a cobra CLI whose `info` command renders the result.

**Tech Stack:** Go 1.27; `github.com/spf13/cobra` (the first third-party dependency); the M1 packages `bluray`, `udf`, `internal/testdisc` and `internal/testdisc/udfimage`.

**Spec:** `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`, sections 2, 3 (`Open`, `Disc`, `Title`, errors, encryption detection), 4 (ranking), 6 (sources, scan only), 7 (`zenvik info`, exit codes) and 11 (milestone 2).

## Global Constraints

- Module `github.com/chad3814/zenvik`, `go 1.27`. No cgo: `CGO_ENABLED=0 go build ./...` must succeed.
- The only third-party dependency added in this milestone is `github.com/spf13/cobra`. Project-wide, only cobra and `github.com/pelletier/go-toml/v2` (arriving in M4) are allowed.
- `bluray` and `udf` stay standard-library only. `internal/rank` imports only the standard library.
- Never commit copyrighted disc data. All fixtures come from `internal/testdisc`.
- Errors are sentinel values wrapped with `%w`, checkable with `errors.Is`.
- Code passes `gofmt`, `go vet ./...`, `golangci-lint run` (v2.13.2) and `go test -race ./...`.
- Main-feature weights (spec §4), verbatim:
  - duration: `100 × duration / longest candidate duration`
  - any clip referenced more than once in the playlist: −40
  - fraction of play items shorter than 5 s: `−30 × fraction`
  - chapters: `+ min(chapters, 30) / 3`
  - distinct audio and subtitle languages: `+ 2 × min(languages, 10)`
  - played by HDMV title 1: +15
  - ties go to the lowest playlist ID
  - ambiguous when the best non-duplicate runner-up scores within 5 points
  - default minimum duration: 2m
- CLI exit codes (spec §7), verbatim: `0` success, `1` failure, `2` usage error, `3` unsupported or encrypted source, `4` missing or too-old dependency.
- Work in worktree `/Users/chad/Projects/zenvik/worktrees/m2-scan` on branch `feat/m2-scan`.
- Never `git push`.
- Commits are SSH-signed. If signing fails, commit with `git -c commit.gpgsign=false commit ...` and say so in the task report.

## Design Decisions (within the spec's latitude)

These are decided here so tasks don't re-litigate them:

1. **Encrypted titles are never the main title.** They are still scored and listed, with the reason `"encrypted"`. The ambiguity check skips them too. `Open` returns `ErrEncrypted` when some title is encrypted and no unfiltered title is unencrypted.
2. **Unusable titles are filtered.** A playlist that can't be read or parsed, or that references a missing or empty `STREAM/<clip>.m2ts`, becomes a filtered title whose reason says why. `Open` still succeeds for the rest of the disc.
3. **`ErrNoTitles` means no playlists were found at all** (`BDMV/PLAYLIST` is missing or has no `*.mpls`). If titles exist but every one is filtered, `Open` succeeds and `Disc.Main()` returns nil.
4. **Track fields reuse the `bluray` enums** (`bluray.CodingType`, `VideoFormat`, `FrameRate`, `DynamicRange`, `AudioFormat`, `SampleRate`) so callers get `String()` for free. The spec's "HDR flag" is `VideoTrack.HDR()`. The spec's audio "channels" is `AudioTrack.Channels bluray.AudioFormat` (mono, stereo, multi-channel); exact channel counts come from mkvmerge in M3.
5. **Title tracks come from the first play item's STN table.**
6. **`Title.Size`** is the sum of `STREAM/<clip>.m2ts` file sizes, counting each distinct clip once.
7. **CLPI files are not read in M2.** The STN table already carries codecs and languages. CLPI is used in M3 if mkvmerge mapping needs it.
8. **`Disc.Title(id)` returns an error wrapping `fs.ErrNotExist`** for unknown IDs. There is no new sentinel.
9. **`Open` keeps the spec's exact signature `Open(ctx, path)`.** A configurable minimum duration arrives in M4 as variadic options, without breaking callers.
10. **`zenvik info --json`** prints a CLI-owned view with durations in seconds and enums as strings, and always includes every title, filtered ones too. Library types carry no JSON tags.
11. **`zenvik` with no command** prints help to stderr and exits `2` (usage error).
12. **Cancellation.** Context cancellation during `Open` returns `ctx.Err()`, and the CLI exits `1`.

## Review Focus

These are input classes the spec implies but no feature test exercises. Each one has a test in the task that owns the code.

1. **A partial backup with a missing or zero-length `STREAM/*.m2ts`.** The affected titles are filtered with "missing stream file" or "empty stream file" and the rest of the disc still opens. Test: Task 5, `TestOpenFiltersMissingAndEmptyStreams`.
2. **A corrupt or truncated `*.mpls` among good ones.** That one title is filtered with "unreadable playlist" and the others are unaffected. Test: Task 5, `TestOpenFiltersUnreadablePlaylist`.
3. **Discs with hundreds of playlists that share clips.** Each clip is sampled for encryption once, so `Open` stays fast on obfuscated discs. Test: Task 5, `TestScanProbesEachClipOnce`.
4. **Path variants:** the folder containing `BDMV`, the `BDMV` folder itself, a trailing slash, a relative `.`, a file inside `BDMV`, a DVD folder, and a non-UDF file. Each resolves correctly or fails with a clear `ErrUnsupportedSource` message, never a panic or a confusing error. Test: Task 2, `TestOpenPathVariants`.
5. **Ctrl-C while scanning a large disc.** `Open` returns `context.Canceled` promptly and releases the image. Test: Task 5, `TestOpenCanceled`.

---

## File Map

| File | Responsibility |
|---|---|
| `internal/testdisc/disc.go` (modify) | Add `ScrambledM2TS`, `StubClip` |
| `internal/testdisc/playlist.go` | `Segment`, `StandardSTN`, `SimplePlaylist` |
| `internal/testdisc/sample.go` | `SampleMovie()`: the shared fixture disc |
| `internal/testdisc/iso.go` | `(*Disc).ISO`: the disc as a UDF image |
| `internal/source/source.go` | Resolve a path to `Source{Kind, Path, Label, FS}`; `ErrUnsupported` |
| `internal/rank/rank.go` | `Candidate`, `Info`, `Weights`, `DefaultWeights`, `Rank` |
| `encrypt.go` | `clipEncrypted`: AACS aligned-unit sync-byte probe |
| `errors.go` | `ErrUnsupportedSource`, `ErrEncrypted`, `ErrNoTitles` |
| `title.go` | `Title`, `Clip`, `Chapter`, track types, `RankInfo` |
| `disc.go` | `SourceKind`, `Disc`, `Open`, `Main`, `Title`, `Close` |
| `scan.go` | `scanTitles`: read the BDMV tree, build titles and candidates, rank |
| `cmd/zenvik/main.go` | `main`, `run`, root command, usage errors, exit codes |
| `cmd/zenvik/info.go` | `info` command: table and JSON output |
| `cmd/zenvik/format.go` | Duration, size, track and language formatting |
| `README.md` (modify) | Document `zenvik info` |

## Dependency Waves (for parallel execution)

- **A:** Task 1 (testdisc helpers) and Task 3 (rank), independent.
- **B:** Task 2 (source) and Task 4 (encryption probe), both after Task 1.
- **C:** Task 5 (`Open`), after Tasks 2, 3 and 4.
- **D:** Task 6 (CLI), after Task 5.
- **E:** Task 7 (verification).

---
### Task 1: Test-disc helpers for scanning

**Files:**
- Modify: `internal/testdisc/disc.go` (append `ScrambledM2TS`, `StubClip`)
- Create: `internal/testdisc/playlist.go`, `internal/testdisc/sample.go`, `internal/testdisc/iso.go`
- Test: `internal/testdisc/scan_helpers_test.go`

**Interfaces:**
- Consumes: the existing `testdisc.Disc`, `CleanM2TS`, `CmdPlayPL`, `Playlist` encoder; `bluray` types; `udfimage.Build`, `udfimage.File`, `udfimage.Options`.
- Produces (used by Tasks 2, 4, 5 and 6):
  - `func ScrambledM2TS(units int) []byte`
  - `func StubClip() *bluray.Clip`
  - `type Segment struct { Clip string; Length time.Duration }`
  - `func StandardSTN() bluray.STN`: AVC 1080p 23.976 video (PID 0x1011); TrueHD multi-channel 48 kHz `eng` (0x1100) and AC-3 stereo 48 kHz `fra` (0x1101) audio; PGS `eng` (0x1200)
  - `func SimplePlaylist(segs ...Segment) *bluray.Playlist`: one play item per segment (IN 0, OUT = length), `StandardSTN` on each item, and an entry mark at the start of each item
  - `func SampleMovie() *Disc`:
    - title 1 is HDMV movie object 0, which plays playlist 800
    - `00800`: 20 × 5-minute segments over clips `00001`–`00020` (100 min)
    - `00801`: identical to `00800`
    - `00010`: one 2m30s segment on clip `00030`
    - `00099`: one 20 s segment on clip `00031`
    - a `StubClip` for every referenced clip; `ClipData` is an empty non-nil map
    - `MetaTitle` `"Sample Movie"`
  - `func (d *Disc) ISO(opt udfimage.Options) ([]byte, error)`
  - `func (d *Disc) AddClipsFor()`: registers `StubClip` info for every clip any playlist references that has none yet (used when tests add playlists)

- [ ] **Step 1: Write the failing tests**

`internal/testdisc/scan_helpers_test.go`:
```go
package testdisc

import (
	"bytes"
	"io/fs"
	"reflect"
	"testing"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func TestScrambledM2TS(t *testing.T) {
	b := ScrambledM2TS(2)
	if len(b) != 2*6144 {
		t.Fatalf("len = %d", len(b))
	}
	for u := 0; u < 2; u++ {
		base := u * 6144
		if !bytes.Equal(b[base:base+16], CleanM2TS(1)[:16]) {
			t.Errorf("unit %d: first 16 bytes are not clear", u)
		}
		for p := 1; p < 32; p++ {
			if b[base+p*192+4] == 0x47 {
				t.Errorf("unit %d packet %d still has a sync byte", u, p)
			}
		}
	}
	if !bytes.Equal(ScrambledM2TS(1), ScrambledM2TS(1)) {
		t.Error("ScrambledM2TS is not deterministic")
	}
}

func TestSimplePlaylist(t *testing.T) {
	p := SimplePlaylist(Segment{"00001", 5 * time.Minute}, Segment{"00002", 90 * time.Second})
	got, err := bluray.ParsePlaylist(Playlist(p))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip =\n%+v\nwant\n%+v", got, p)
	}
	if got.Duration() != 6*time.Minute+30*time.Second {
		t.Errorf("Duration = %v", got.Duration())
	}
	if ch := got.Chapters(); !reflect.DeepEqual(ch, []time.Duration{0, 5 * time.Minute}) {
		t.Errorf("Chapters = %v", ch)
	}
	if !reflect.DeepEqual(got.Items[0].STN, StandardSTN()) {
		t.Errorf("STN = %+v", got.Items[0].STN)
	}
}

func TestSampleMovie(t *testing.T) {
	files := SampleMovie().Files()
	var mpls, m2ts int
	for name := range files {
		switch {
		case len(name) > 14 && name[:14] == "BDMV/PLAYLIST/":
			mpls++
		case len(name) > 12 && name[:12] == "BDMV/STREAM/":
			m2ts++
		}
	}
	if mpls != 4 || m2ts != 22 {
		t.Errorf("playlists = %d, streams = %d; want 4 and 22", mpls, m2ts)
	}
	idx, err := bluray.ParseIndex(files["BDMV/index.bdmv"])
	if err != nil || len(idx.Titles) != 1 {
		t.Fatalf("index = %+v, %v", idx, err)
	}
	mo, err := bluray.ParseMovieObjects(files["BDMV/MovieObject.bdmv"])
	if err != nil {
		t.Fatal(err)
	}
	if got := mo.Playlists(0); !reflect.DeepEqual(got, []int{800}) {
		t.Errorf("title 1 plays %v", got)
	}
	p, err := bluray.ParsePlaylist(files["BDMV/PLAYLIST/00800.mpls"])
	if err != nil || p.Duration() != 100*time.Minute || len(p.Items) != 20 {
		t.Errorf("00800 = %v items, %v, %v", len(p.Items), p.Duration(), err)
	}
}

func TestDiscISO(t *testing.T) {
	d := SampleMovie()
	img, err := d.ISO(udfimage.Options{Revision: 0x0250, Label: "SAMPLE_MOVIE"})
	if err != nil {
		t.Fatal(err)
	}
	fsys, err := udf.Open(bytes.NewReader(img), int64(len(img)))
	if err != nil {
		t.Fatal(err)
	}
	if fsys.Label() != "SAMPLE_MOVIE" {
		t.Errorf("Label = %q", fsys.Label())
	}
	for name, want := range d.Files() {
		got, err := fs.ReadFile(fsys, name)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: %d bytes, %v", name, len(got), err)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/testdisc/`
Expected: FAIL to compile (`undefined: ScrambledM2TS`, `undefined: SimplePlaylist`, ...).

- [ ] **Step 3: Implement**

Append to `internal/testdisc/disc.go`:
```go
// ScrambledM2TS returns units aligned units that look AACS-encrypted: each
// unit keeps the 16 clear bytes a CleanM2TS unit starts with, and the rest
// is deterministic pseudo-random data with no 0x47 sync byte at any source
// packet's sync offset.
func ScrambledM2TS(units int) []byte {
	b := CleanM2TS(units)
	x := uint32(2463534242)
	for u := 0; u < units; u++ {
		base := u * 6144
		for i := 16; i < 6144; i++ {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			v := byte(x)
			if i%192 == 4 && v == 0x47 {
				v = 0x48
			}
			b[base+i] = v
		}
	}
	return b
}

// StubClip returns minimal clip information for a one-unit M2TS clip.
func StubClip() *bluray.Clip {
	return &bluray.Clip{Version: "0200", StreamType: 1, ApplicationType: 1, TSRecordingRate: 48_000_000, SourcePackets: 32}
}
```

`internal/testdisc/playlist.go`:
```go
package testdisc

import (
	"time"

	"github.com/chad3814/zenvik/bluray"
)

// Segment is one play item of a synthetic playlist.
type Segment struct {
	Clip   string // clip ID, e.g. "00001"
	Length time.Duration
}

// StandardSTN returns a typical stream table: H.264 1080p video, English
// TrueHD and French AC-3 audio, and English PGS subtitles.
func StandardSTN() bluray.STN {
	return bluray.STN{
		Video: []bluray.Stream{{PID: 0x1011, Coding: bluray.CodingAVC, VideoFormat: 6, FrameRate: 1}},
		Audio: []bluray.Stream{
			{PID: 0x1100, Coding: bluray.CodingTrueHD, AudioFormat: 6, SampleRate: 1, Language: "eng"},
			{PID: 0x1101, Coding: bluray.CodingAC3, AudioFormat: 3, SampleRate: 1, Language: "fra"},
		},
		PG: []bluray.Stream{{PID: 0x1200, Coding: bluray.CodingPG, Language: "eng"}},
	}
}

// SimplePlaylist builds a playlist with one play item per segment (IN at
// 0, OUT at the segment length), StandardSTN on every item, and a chapter
// entry mark at the start of each item.
func SimplePlaylist(segs ...Segment) *bluray.Playlist {
	p := &bluray.Playlist{Version: "0200"}
	for i, s := range segs {
		p.Items = append(p.Items, bluray.PlayItem{
			ClipID:  s.Clip,
			CodecID: "M2TS",
			In:      0,
			Out:     bluray.Ticks(int64(s.Length) * bluray.TicksPerSecond / int64(time.Second)),
			STN:     StandardSTN(),
		})
		p.Marks = append(p.Marks, bluray.Mark{Type: bluray.MarkEntry, PlayItem: uint16(i), Time: 0, PID: 0xFFFF})
	}
	return p
}
```

`internal/testdisc/sample.go`:
```go
package testdisc

import (
	"fmt"
	"time"

	"github.com/chad3814/zenvik/bluray"
)

// SampleMovie returns a typical single-movie disc:
//
//   - title 1 is HDMV movie object 0, which plays playlist 800
//   - 00800: 20 five-minute segments over clips 00001–00020 (100 min)
//   - 00801: identical to 00800 (a duplicate)
//   - 00010: one 2m30s segment on clip 00030 (a trailer)
//   - 00099: one 20 s segment on clip 00031 (a menu loop)
//
// Every referenced clip gets StubClip info and default clean M2TS data.
// ClipData is an empty map that tests may fill to override clip data.
func SampleMovie() *Disc {
	d := &Disc{
		Titles:       []bluray.IndexTitle{{Object: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0}}},
		MovieObjects: []bluray.MovieObject{{Commands: []bluray.NavCommand{CmdPlayPL(800)}}},
		Playlists:    map[string]*bluray.Playlist{},
		Clips:        map[string]*bluray.Clip{},
		ClipData:     map[string][]byte{},
		MetaTitle:    "Sample Movie",
	}
	var feature []Segment
	for i := 1; i <= 20; i++ {
		feature = append(feature, Segment{Clip: fmt.Sprintf("%05d", i), Length: 5 * time.Minute})
	}
	d.Playlists["00800"] = SimplePlaylist(feature...)
	d.Playlists["00801"] = SimplePlaylist(feature...)
	d.Playlists["00010"] = SimplePlaylist(Segment{Clip: "00030", Length: 150 * time.Second})
	d.Playlists["00099"] = SimplePlaylist(Segment{Clip: "00031", Length: 20 * time.Second})
	d.AddClipsFor()
	return d
}

// AddClipsFor registers StubClip info for every clip any playlist
// references and that has no clip info yet.
func (d *Disc) AddClipsFor() {
	if d.Clips == nil {
		d.Clips = map[string]*bluray.Clip{}
	}
	for _, p := range d.Playlists {
		for _, it := range p.Items {
			if _, ok := d.Clips[it.ClipID]; !ok {
				d.Clips[it.ClipID] = StubClip()
			}
		}
	}
}
```

`internal/testdisc/iso.go`:
```go
package testdisc

import "github.com/chad3814/zenvik/internal/testdisc/udfimage"

// ISO returns the disc as a UDF image built by udfimage.
func (d *Disc) ISO(opt udfimage.Options) ([]byte, error) {
	files := map[string]udfimage.File{}
	for name, data := range d.Files() {
		files[name] = udfimage.File{Data: data}
	}
	return udfimage.Build(files, opt)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/testdisc/... && go vet ./... && golangci-lint run`
Expected: PASS; 0 lint issues.

- [ ] **Step 5: Commit**

```bash
git add internal/testdisc
git commit -m "Add scrambled clips, simple playlists, sample movie and ISO helpers to testdisc"
```

---

### Task 2: Source resolution (`internal/source`)

**Files:**
- Create: `internal/source/source.go`
- Test: `internal/source/source_test.go`

**Interfaces:**
- Consumes: `udf.OpenImage`, `udf.ErrNotUDF`, `udf.ErrUnsupported`, `(*udf.Image).Label`, `(*udf.Image).Close`; in tests, `testdisc.SampleMovie`, `(*testdisc.Disc).WriteDir`, `(*testdisc.Disc).ISO`, `udfimage.Build`.
- Produces (used by Task 5):
  - `var ErrUnsupported = errors.New("zenvik: unsupported source")`
  - `type Kind int`; constants `ISO Kind = 1`, `BDMVDir Kind = 2`; `func (k Kind) String() string` returns `"ISO image"`, `"BDMV folder"` or `"unknown"`
  - `type Source struct { Kind Kind; Path string; Label string; FS fs.FS }` with `func (s *Source) Close() error`
  - `func Open(path string) (*Source, error)`
- Behavior:
  - **Directory:** if its base name is `BDMV` and it contains `index.bdmv`, the root is its parent. Otherwise the root is the directory itself, and it must contain `BDMV/index.bdmv`. `Label` is the root's base name (taken from the absolute path), and `FS` is `os.DirFS(root)`.
  - **File:** opened with `udf.OpenImage` and must contain `BDMV/index.bdmv`. `Label` is the UDF label, and `Close` closes the image.
  - **Errors:**
    - a missing path returns the `os.Stat` error (matches `fs.ErrNotExist`, not `ErrUnsupported`)
    - a non-UDF file: `ErrUnsupported` with message `"<path> is not a UDF disc image"`
    - a UDF feature we don't support wraps both `ErrUnsupported` and `udf.ErrUnsupported`
    - a tree with `VIDEO_TS` and no `BDMV`: `ErrUnsupported` with a message containing `"DVD"`
    - any other tree without `BDMV/index.bdmv`: `ErrUnsupported` with `"no BDMV/index.bdmv"`
    - corrupt UDF errors (`udf.ErrCorrupt`) pass through unchanged

- [ ] **Step 1: Write the failing tests**

`internal/source/source_test.go`:
```go
package source

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
	"github.com/chad3814/zenvik/udf"
)

func writeSample(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_MOVIE")
	if err := testdisc.SampleMovie().WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertBDMV(t *testing.T, s *Source) {
	t.Helper()
	if _, err := fs.Stat(s.FS, "BDMV/index.bdmv"); err != nil {
		t.Errorf("FS has no BDMV/index.bdmv: %v", err)
	}
}

func TestOpenPathVariants(t *testing.T) {
	root := writeSample(t)
	for name, path := range map[string]string{
		"parent":         root,
		"bdmv":           filepath.Join(root, "BDMV"),
		"trailing slash": root + string(filepath.Separator),
		"bdmv slash":     filepath.Join(root, "BDMV") + string(filepath.Separator),
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if s.Kind != BDMVDir || s.Label != "SAMPLE_MOVIE" || filepath.Clean(s.Path) != root {
				t.Errorf("got Kind %v, Label %q, Path %q", s.Kind, s.Label, s.Path)
			}
			assertBDMV(t, s)
		})
	}

	t.Run("relative dot", func(t *testing.T) {
		t.Chdir(root)
		s, err := Open(".")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if s.Label != "SAMPLE_MOVIE" {
			t.Errorf("Label = %q", s.Label)
		}
		assertBDMV(t, s)
	})

	t.Run("file inside BDMV", func(t *testing.T) {
		_, err := Open(filepath.Join(root, "BDMV", "index.bdmv"))
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "not a UDF disc image") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, err := Open(filepath.Join(root, "nope"))
		if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("dvd folder", func(t *testing.T) {
		dvd := filepath.Join(t.TempDir(), "DVD")
		if err := os.MkdirAll(filepath.Join(dvd, "VIDEO_TS"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Open(dvd)
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "DVD") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("empty folder", func(t *testing.T) {
		_, err := Open(t.TempDir())
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "no BDMV/index.bdmv") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("random file", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "junk.iso")
		if err := os.WriteFile(p, make([]byte, 1<<20), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Open(p)
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("err = %v", err)
		}
	})
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
		s, err := Open(p)
		if err != nil {
			t.Fatalf("rev %#x: %v", rev, err)
		}
		if s.Kind != ISO || s.Label != "SAMPLE_MOVIE" || s.Path != p {
			t.Errorf("rev %#x: got Kind %v, Label %q, Path %q", rev, s.Kind, s.Label, s.Path)
		}
		assertBDMV(t, s)
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
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
	_, err = Open(p)
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "DVD") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenCorruptImage(t *testing.T) {
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0250, Label: "X"})
	if err != nil {
		t.Fatal(err)
	}
	img = img[:300*2048] // keeps the volume structure, cuts off the partition
	p := filepath.Join(t.TempDir(), "cut.iso")
	if err := os.WriteFile(p, img, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Open(p)
	if err == nil || errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want a non-unsupported error", err)
	}
	if err != nil && !errors.Is(err, udf.ErrCorrupt) && !errors.Is(err, udf.ErrNotUDF) {
		t.Errorf("err = %v, want udf.ErrCorrupt or udf.ErrNotUDF", err)
	}
}

func TestKindString(t *testing.T) {
	if ISO.String() != "ISO image" || BDMVDir.String() != "BDMV folder" || Kind(0).String() != "unknown" {
		t.Error("Kind.String mismatch")
	}
}
```

Note: if `TestOpenCorruptImage`'s cut image happens to yield `udf.ErrNotUDF`, `source.Open` maps that to `ErrUnsupported` and the test fails. In that case, pick a cut that keeps sector 256 and the partition start but removes file data (for example `len(img) - 4*2048`), so that `fs.Stat(BDMV/index.bdmv)` fails with `udf.ErrCorrupt`. Keep the assertion that the error is not `ErrUnsupported`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/source/`
Expected: FAIL to compile (`undefined: Open`).

- [ ] **Step 3: Implement**

`internal/source/source.go`:
```go
// Package source resolves a user-supplied path (an ISO image, a folder
// containing BDMV, or a BDMV folder) to a file tree whose root holds BDMV.
package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/chad3814/zenvik/udf"
)

// ErrUnsupported reports a path that is not a Blu-ray image or folder.
var ErrUnsupported = errors.New("zenvik: unsupported source")

// Kind is the type of source.
type Kind int

// Source kinds.
const (
	ISO     Kind = 1
	BDMVDir Kind = 2
)

func (k Kind) String() string {
	switch k {
	case ISO:
		return "ISO image"
	case BDMVDir:
		return "BDMV folder"
	}
	return "unknown"
}

// Source is an opened Blu-ray file tree.
type Source struct {
	Kind  Kind
	Path  string // the ISO file, or the folder containing BDMV
	Label string // UDF volume identifier, or the folder's name
	FS    fs.FS  // root contains BDMV/index.bdmv
	close func() error
}

// Close releases the source.
func (s *Source) Close() error {
	if s.close == nil {
		return nil
	}
	err := s.close()
	s.close = nil
	return err
}

// Open resolves path to a Blu-ray source.
func Open(path string) (*Source, error) {
	clean := filepath.Clean(path)
	st, err := os.Stat(clean)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return openDir(clean)
	}
	return openImage(clean)
}

func openDir(dir string) (*Source, error) {
	root := dir
	if filepath.Base(dir) == "BDMV" && isFile(filepath.Join(dir, "index.bdmv")) {
		root = filepath.Dir(dir)
	}
	fsys := os.DirFS(root)
	if !isFile(filepath.Join(root, "BDMV", "index.bdmv")) {
		return nil, explain(fsys, root)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Source{Kind: BDMVDir, Path: root, Label: filepath.Base(abs), FS: fsys}, nil
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
	if _, err := fs.Stat(img, "BDMV/index.bdmv"); err != nil {
		img.Close()
		if errors.Is(err, fs.ErrNotExist) {
			return nil, explain(img, name)
		}
		return nil, err
	}
	return &Source{Kind: ISO, Path: name, Label: img.Label(), FS: img, close: img.Close}, nil
}

// explain builds the ErrUnsupported error for a tree without BDMV.
func explain(fsys fs.FS, where string) error {
	if st, err := fs.Stat(fsys, "VIDEO_TS"); err == nil && st.IsDir() {
		return fmt.Errorf("%w: %s is a DVD (VIDEO_TS); DVD support is planned but not available yet", ErrUnsupported, where)
	}
	return fmt.Errorf("%w: %s has no BDMV/index.bdmv", ErrUnsupported, where)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/source/ && go vet ./... && golangci-lint run`
Expected: PASS; 0 lint issues.

- [ ] **Step 5: Commit**

```bash
git add internal/source
git commit -m "Add source resolution for ISO images and BDMV folders"
```

---

### Task 3: Main-feature ranking (`internal/rank`)

**Files:**
- Create: `internal/rank/rank.go`
- Test: `internal/rank/rank_test.go`

**Interfaces:**
- Consumes: nothing (standard library only).
- Produces (used by Task 5):
  - `type Clip struct { ID string; In, Out time.Duration }`
  - `type Candidate struct { ID string; Clips []Clip; Duration time.Duration; Chapters, Languages int; HasVideo, Encrypted, PlayedByTitle1 bool; Problem string }`
  - `type Info struct { Score float64; IsMain, Ambiguous, Filtered bool; DuplicateOf string; Reasons []string }`. Field order and types are fixed: Task 5 converts `rank.Info` to `zenvik.RankInfo` with a struct conversion.
  - `type Weights struct { Duration, RepeatedClip, TinySegments float64; TinySegment time.Duration; ChapterCap int; ChapterDivisor, LanguageWeight float64; LanguageCap int; Title1Bonus, AmbiguityMargin float64 }`
  - `var DefaultWeights Weights`, holding the Global Constraints values
  - `func Rank(cands []Candidate, minDuration time.Duration, w Weights) (order []int, infos []Info)`
- Ordering contract:
  - `infos[i]` belongs to `cands[i]`.
  - `order` lists candidate indexes best-first: the main title (if any) first, then the other scored candidates by score descending (ties by ID), then duplicates by ID, then filtered candidates by ID.
  - Reasons are human-readable, for example `"shorter than 2m0s"`, `"no video stream"`, `"encrypted"`, `"duplicate of 00800"`, `"100% of the longest title"`, `"repeats clip 00003"`, `"5 of 26 segments under 5s"`, `"20 chapters"`, `"3 audio/subtitle languages"`, `"played by title 1"` and `"close second: 00801 (score 111.8 vs 112.7)"`.

- [ ] **Step 1: Write the failing tests**

`internal/rank/rank_test.go`:
```go
package rank

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// seq returns n clips with consecutive five-digit IDs starting at first.
func seq(first, n int, each time.Duration) []Clip {
	out := make([]Clip, n)
	for i := range out {
		out[i] = Clip{ID: fmt.Sprintf("%05d", first+i), In: 0, Out: each}
	}
	return out
}

// movie builds a candidate with video, three languages and one chapter per clip.
func movie(id string, clips []Clip) Candidate {
	var d time.Duration
	for _, c := range clips {
		d += c.Out - c.In
	}
	return Candidate{ID: id, Clips: clips, Duration: d, Chapters: len(clips), Languages: 3, HasVideo: true}
}

func mainOf(t *testing.T, cands []Candidate, order []int, infos []Info) (string, Info) {
	t.Helper()
	if len(order) == 0 || !infos[order[0]].IsMain {
		return "", Info{}
	}
	for k, i := range order {
		if k > 0 && infos[i].IsMain {
			t.Fatalf("second main title %s", cands[i].ID)
		}
	}
	return cands[order[0]].ID, infos[order[0]]
}

func TestRankScenarios(t *testing.T) {
	encrypted := movie("00800", seq(1, 20, 330*time.Second))
	encrypted.Encrypted = true
	title1 := movie("00801", seq(101, 20, 5*time.Minute))
	title1.PlayedByTitle1 = true

	tests := []struct {
		name      string
		cands     []Candidate
		wantMain  string
		ambiguous bool
	}{
		{"simple movie", []Candidate{
			movie("00800", seq(1, 20, 330*time.Second)),
			movie("00001", seq(100, 1, 150*time.Second)),
			movie("00002", seq(101, 1, 30*time.Second)),
		}, "00800", false},
		{"three duplicate copies", []Candidate{
			movie("00800", seq(1, 20, 5*time.Minute)),
			movie("00801", seq(1, 20, 5*time.Minute)),
			movie("00802", seq(1, 20, 5*time.Minute)),
			movie("00900", seq(201, 2, 5*time.Minute)),
		}, "00800", false},
		{"99-playlist obfuscation", obfuscated(), "00057", false},
		{"tv episodes with play-all", []Candidate{
			movie("00001", seq(1, 1, 22*time.Minute)),
			movie("00002", seq(2, 1, 22*time.Minute)),
			movie("00003", seq(3, 1, 22*time.Minute)),
			movie("00004", seq(4, 1, 22*time.Minute)),
			movie("00005", seq(1, 4, 22*time.Minute)),
		}, "00005", false},
		{"extras only", []Candidate{
			movie("00001", seq(1, 1, 3*time.Minute)),
			movie("00002", seq(2, 1, 5*time.Minute)),
			movie("00003", seq(3, 1, 8*time.Minute)),
		}, "00003", false},
		{"ambiguous pair", []Candidate{
			movie("00800", seq(1, 20, 330*time.Second)),
			movie("00801", seq(101, 20, 327*time.Second)),
		}, "00800", true},
		{"title 1 breaks the tie", []Candidate{
			movie("00800", seq(1, 20, 5*time.Minute)),
			title1,
		}, "00801", false},
		{"encrypted is never main", []Candidate{
			encrypted,
			movie("00801", seq(101, 20, 5*time.Minute)),
		}, "00801", false},
		{"exact tie goes to lowest ID", []Candidate{
			movie("00801", seq(101, 20, 5*time.Minute)),
			movie("00800", seq(1, 20, 5*time.Minute)),
		}, "00800", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, infos := Rank(tt.cands, 2*time.Minute, DefaultWeights)
			got, info := mainOf(t, tt.cands, order, infos)
			if got != tt.wantMain {
				t.Fatalf("main = %q, want %q (order %v)", got, tt.wantMain, ids(tt.cands, order))
			}
			if info.Ambiguous != tt.ambiguous {
				t.Errorf("Ambiguous = %v, want %v (reasons %q)", info.Ambiguous, tt.ambiguous, info.Reasons)
			}
		})
	}
}

// obfuscated builds 99 playlists: 00057 plays clips 1–20 in order (100
// min); every other playlist plays a rotation of the same clips plus a
// repeat of one clip and five 3-second segments.
func obfuscated() []Candidate {
	real := seq(1, 20, 5*time.Minute)
	var out []Candidate
	for n := 1; n <= 99; n++ {
		id := fmt.Sprintf("%05d", n)
		if n == 57 {
			out = append(out, movie(id, real))
			continue
		}
		k := n % 20
		clips := append(append([]Clip{}, real[k:]...), real[:k]...)
		clips = append(clips, real[0])
		for j := 0; j < 5; j++ {
			clips = append(clips, Clip{ID: fmt.Sprintf("%05d", 900+j), In: 0, Out: 3 * time.Second})
		}
		out = append(out, movie(id, clips))
	}
	return out
}

func ids(cands []Candidate, order []int) []string {
	out := make([]string, len(order))
	for k, i := range order {
		out[k] = cands[i].ID
	}
	return out
}

func TestRankAmbiguityNamesRunnerUp(t *testing.T) {
	cands := []Candidate{
		movie("00800", seq(1, 20, 330*time.Second)),
		movie("00801", seq(101, 20, 327*time.Second)),
	}
	order, infos := Rank(cands, 2*time.Minute, DefaultWeights)
	reasons := strings.Join(infos[order[0]].Reasons, "; ")
	if !strings.Contains(reasons, "close second: 00801") {
		t.Errorf("reasons = %q", reasons)
	}
}

func TestRankFiltersAndOrder(t *testing.T) {
	noVideo := movie("00003", seq(30, 1, 10*time.Minute))
	noVideo.HasVideo = false
	broken := Candidate{ID: "00004", Problem: "unreadable playlist: bad data"}
	cands := []Candidate{
		broken,
		noVideo,
		movie("00002", seq(20, 1, 20*time.Second)),
		movie("00801", seq(1, 20, 5*time.Minute)),
		movie("00800", seq(1, 20, 5*time.Minute)),
		movie("00010", seq(40, 2, 5*time.Minute)),
	}
	order, infos := Rank(cands, 2*time.Minute, DefaultWeights)
	if got, want := ids(cands, order), []string{"00800", "00010", "00801", "00002", "00003", "00004"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	byID := map[string]Info{}
	for i, c := range cands {
		byID[c.ID] = infos[i]
	}
	if in := byID["00002"]; !in.Filtered || !slices.Contains(in.Reasons, "shorter than 2m0s") {
		t.Errorf("00002 = %+v", in)
	}
	if in := byID["00003"]; !in.Filtered || !slices.Contains(in.Reasons, "no video stream") {
		t.Errorf("00003 = %+v", in)
	}
	if in := byID["00004"]; !in.Filtered || !slices.Contains(in.Reasons, "unreadable playlist: bad data") {
		t.Errorf("00004 = %+v", in)
	}
	if in := byID["00801"]; in.DuplicateOf != "00800" || in.Score != 0 || !slices.Contains(in.Reasons, "duplicate of 00800") {
		t.Errorf("00801 = %+v", in)
	}
	if in := byID["00800"]; !in.IsMain || !slices.Contains(in.Reasons, "100% of the longest title") || !slices.Contains(in.Reasons, "20 chapters") {
		t.Errorf("00800 = %+v", in)
	}
}

func TestRankScores(t *testing.T) {
	c := movie("00001", seq(1, 4, 5*time.Minute))
	c.Clips = append(c.Clips, c.Clips[0], Clip{ID: "00009", In: 0, Out: 2 * time.Second})
	c.Duration = 25*time.Minute + 2*time.Second
	c.Chapters = 40
	c.Languages = 12
	c.PlayedByTitle1 = true
	_, infos := Rank([]Candidate{c}, 2*time.Minute, DefaultWeights)
	// 100 (longest) - 40 (repeat) - 30*(1/6) + 30/3 + 2*10 + 15 = 100
	if got := infos[0].Score; got < 99.999 || got > 100.001 {
		t.Errorf("Score = %v, want 100", got)
	}
	for _, want := range []string{"repeats clip 00001", "1 of 6 segments under 5s", "40 chapters", "12 audio/subtitle languages", "played by title 1"} {
		if !slices.Contains(infos[0].Reasons, want) {
			t.Errorf("missing reason %q in %q", want, infos[0].Reasons)
		}
	}
}

func TestRankEncryptedReasonAndOrder(t *testing.T) {
	enc := movie("00800", seq(1, 20, 330*time.Second))
	enc.Encrypted = true
	cands := []Candidate{enc, movie("00801", seq(101, 20, 5*time.Minute))}
	order, infos := Rank(cands, 2*time.Minute, DefaultWeights)
	if got := ids(cands, order); !reflect.DeepEqual(got, []string{"00801", "00800"}) {
		t.Errorf("order = %v", got)
	}
	if !slices.Contains(infos[0].Reasons, "encrypted") || infos[0].IsMain {
		t.Errorf("00800 = %+v", infos[0])
	}
}

func TestRankNoMain(t *testing.T) {
	cands := []Candidate{movie("00002", seq(1, 1, time.Minute)), movie("00001", seq(2, 1, time.Minute))}
	order, infos := Rank(cands, 2*time.Minute, DefaultWeights)
	if got := ids(cands, order); !reflect.DeepEqual(got, []string{"00001", "00002"}) {
		t.Errorf("order = %v", got)
	}
	for _, in := range infos {
		if in.IsMain {
			t.Error("unexpected main title")
		}
	}
	if order, infos := Rank(nil, 2*time.Minute, DefaultWeights); len(order) != 0 || len(infos) != 0 {
		t.Error("empty input should give empty output")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rank/`
Expected: FAIL to compile (`undefined: Rank`).

- [ ] **Step 3: Implement**

`internal/rank/rank.go`:
```go
// Package rank picks the main feature among a disc's playlists: filter
// unusable candidates, mark duplicates, score the rest, and choose the
// best unencrypted one (design spec section 4).
package rank

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Clip is one play item: a clip and its IN/OUT times on the clip's clock.
type Clip struct {
	ID      string
	In, Out time.Duration
}

func (c Clip) length() time.Duration {
	if c.Out < c.In {
		return 0
	}
	return c.Out - c.In
}

// Candidate describes one playlist.
type Candidate struct {
	ID             string // playlist ID, e.g. "00800"
	Clips          []Clip // play items in playback order
	Duration       time.Duration
	Chapters       int
	Languages      int // distinct audio and subtitle languages
	HasVideo       bool
	Encrypted      bool
	PlayedByTitle1 bool
	// Problem, when non-empty, filters the candidate with this reason
	// (for example an unreadable playlist or a missing stream file).
	Problem string
}

// Info is the ranking result for one candidate.
type Info struct {
	Score       float64
	IsMain      bool
	Ambiguous   bool // on the main title: a runner-up scored within the ambiguity margin
	Filtered    bool
	DuplicateOf string
	Reasons     []string
}

// Weights tunes scoring.
type Weights struct {
	Duration        float64       // points for the longest candidate, scaled by relative duration
	RepeatedClip    float64       // penalty when any clip appears more than once
	TinySegments    float64       // penalty scaled by the fraction of items shorter than TinySegment
	TinySegment     time.Duration //
	ChapterCap      int           // chapters beyond this count earn nothing
	ChapterDivisor  float64       // points per chapter = 1 / ChapterDivisor
	LanguageWeight  float64       // points per distinct language
	LanguageCap     int           // languages beyond this count earn nothing
	Title1Bonus     float64       // bonus when HDMV title 1 plays the playlist
	AmbiguityMargin float64       // runner-up within this many points makes the choice ambiguous
}

// DefaultWeights are the initial weights from the design spec.
var DefaultWeights = Weights{
	Duration:        100,
	RepeatedClip:    40,
	TinySegments:    30,
	TinySegment:     5 * time.Second,
	ChapterCap:      30,
	ChapterDivisor:  3,
	LanguageWeight:  2,
	LanguageCap:     10,
	Title1Bonus:     15,
	AmbiguityMargin: 5,
}

// Rank scores cands. infos[i] belongs to cands[i]; order lists candidate
// indexes best-first: the main title, other scored candidates by score
// (ties by ID), duplicates by ID, then filtered candidates by ID.
func Rank(cands []Candidate, minDuration time.Duration, w Weights) (order []int, infos []Info) {
	infos = make([]Info, len(cands))
	filter(cands, infos, minDuration)
	dedupe(cands, infos)
	var longest time.Duration
	for i, c := range cands {
		if group(infos[i]) == 0 && c.Duration > longest {
			longest = c.Duration
		}
	}
	for i, c := range cands {
		if group(infos[i]) == 0 {
			s, why := score(c, longest, w)
			infos[i].Score = s
			infos[i].Reasons = append(infos[i].Reasons, why...)
		}
	}
	order = sortOrder(cands, infos)
	return decide(cands, infos, order, w), infos
}

// group is 0 for scored candidates, 1 for duplicates and 2 for filtered ones.
func group(in Info) int {
	switch {
	case in.Filtered:
		return 2
	case in.DuplicateOf != "":
		return 1
	}
	return 0
}

func filter(cands []Candidate, infos []Info, minDuration time.Duration) {
	for i, c := range cands {
		in := &infos[i]
		switch {
		case c.Problem != "":
			in.Filtered = true
			in.Reasons = append(in.Reasons, c.Problem)
		case c.Duration < minDuration:
			in.Filtered = true
			in.Reasons = append(in.Reasons, fmt.Sprintf("shorter than %s", minDuration))
		case !c.HasVideo:
			in.Filtered = true
			in.Reasons = append(in.Reasons, "no video stream")
		}
		if c.Encrypted {
			in.Reasons = append(in.Reasons, "encrypted")
		}
	}
}

// dedupe marks candidates whose clip sequence (IDs and IN/OUT times)
// matches a candidate with a lower ID.
func dedupe(cands []Candidate, infos []Info) {
	var idx []int
	for i := range cands {
		if !infos[i].Filtered {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool { return cands[idx[a]].ID < cands[idx[b]].ID })
	first := map[string]string{}
	for _, i := range idx {
		k := clipKey(cands[i].Clips)
		if id, ok := first[k]; ok {
			infos[i].DuplicateOf = id
			infos[i].Reasons = append(infos[i].Reasons, "duplicate of "+id)
			continue
		}
		first[k] = cands[i].ID
	}
}

func clipKey(clips []Clip) string {
	var b strings.Builder
	for _, c := range clips {
		fmt.Fprintf(&b, "%s@%d-%d;", c.ID, c.In, c.Out)
	}
	return b.String()
}

func score(c Candidate, longest time.Duration, w Weights) (float64, []string) {
	var s float64
	var why []string
	if longest > 0 {
		frac := float64(c.Duration) / float64(longest)
		s += w.Duration * frac
		why = append(why, fmt.Sprintf("%.0f%% of the longest title", 100*frac))
	}
	if id := repeatedClip(c.Clips); id != "" {
		s -= w.RepeatedClip
		why = append(why, "repeats clip "+id)
	}
	if n := len(c.Clips); n > 0 {
		tiny := 0
		for _, cl := range c.Clips {
			if cl.length() < w.TinySegment {
				tiny++
			}
		}
		if tiny > 0 {
			s -= w.TinySegments * float64(tiny) / float64(n)
			why = append(why, fmt.Sprintf("%d of %d segments under %s", tiny, n, w.TinySegment))
		}
	}
	if c.Chapters > 0 {
		s += float64(min(c.Chapters, w.ChapterCap)) / w.ChapterDivisor
		why = append(why, fmt.Sprintf("%d chapters", c.Chapters))
	}
	if c.Languages > 0 {
		s += w.LanguageWeight * float64(min(c.Languages, w.LanguageCap))
		why = append(why, fmt.Sprintf("%d audio/subtitle languages", c.Languages))
	}
	if c.PlayedByTitle1 {
		s += w.Title1Bonus
		why = append(why, "played by title 1")
	}
	return s, why
}

func repeatedClip(clips []Clip) string {
	seen := map[string]bool{}
	for _, c := range clips {
		if seen[c.ID] {
			return c.ID
		}
		seen[c.ID] = true
	}
	return ""
}

func sortOrder(cands []Candidate, infos []Info) []int {
	order := make([]int, len(cands))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := order[a], order[b]
		gx, gy := group(infos[x]), group(infos[y])
		if gx != gy {
			return gx < gy
		}
		if gx == 0 && infos[x].Score != infos[y].Score {
			return infos[x].Score > infos[y].Score
		}
		return cands[x].ID < cands[y].ID
	})
	return order
}

// decide marks the best unencrypted scored candidate as main, moves it to
// the front of order, and flags ambiguity against the next unencrypted
// scored candidate.
func decide(cands []Candidate, infos []Info, order []int, w Weights) []int {
	pos := -1
	for k, i := range order {
		if group(infos[i]) == 0 && !cands[i].Encrypted {
			pos = k
			break
		}
	}
	if pos < 0 {
		return order
	}
	main := order[pos]
	infos[main].IsMain = true
	for _, i := range order[pos+1:] {
		if group(infos[i]) != 0 {
			break
		}
		if cands[i].Encrypted {
			continue
		}
		if infos[main].Score-infos[i].Score <= w.AmbiguityMargin {
			infos[main].Ambiguous = true
			infos[main].Reasons = append(infos[main].Reasons, fmt.Sprintf("close second: %s (score %.1f vs %.1f)",
				cands[i].ID, infos[i].Score, infos[main].Score))
		}
		break
	}
	out := make([]int, 0, len(order))
	out = append(out, main)
	out = append(out, order[:pos]...)
	return append(out, order[pos+1:]...)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/rank/ && go vet ./... && golangci-lint run`
Expected: PASS; 0 lint issues. If a scenario fails, print the scores, check them by hand against the Global Constraints weights, and fix the code, not the expectation. If a fixture's arithmetic is truly wrong, explain the correction in your report.

- [ ] **Step 5: Commit**

```bash
git add internal/rank
git commit -m "Add main-feature ranking"
```

---

### Task 4: Clip encryption probe

**Files:**
- Create: `encrypt.go`
- Test: `encrypt_test.go`

**Interfaces:**
- Consumes: `testdisc.CleanM2TS`, `testdisc.ScrambledM2TS` (tests only).
- Produces (package `zenvik`, used by Task 5): `func clipEncrypted(r io.Reader) (bool, error)`.
- Behavior (spec §3, "Encryption detection"):
  - It reads up to one 6144-byte aligned unit.
  - It reports encrypted when any source packet after the first lacks `0x47` at byte 4 of its 192-byte packet.
  - Fewer than two complete packets cannot be judged and count as not encrypted.
  - Read errors other than EOF are returned.

- [ ] **Step 1: Write the failing test**

`encrypt_test.go`:
```go
package zenvik

import (
	"bytes"
	"errors"
	"testing"
	"testing/iotest"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestClipEncrypted(t *testing.T) {
	broken := testdisc.CleanM2TS(1)
	broken[31*192+4] = 0x00 // only the last packet of the unit lacks its sync byte
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"clean", testdisc.CleanM2TS(1), false},
		{"clean multi-unit", testdisc.CleanM2TS(3), false},
		{"scrambled", testdisc.ScrambledM2TS(1), true},
		{"last packet scrambled", broken, true},
		{"shorter than a unit", testdisc.CleanM2TS(1)[:5*192+17], false},
		{"single packet", testdisc.ScrambledM2TS(1)[:192], false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		got, err := clipEncrypted(bytes.NewReader(tt.data))
		if err != nil || got != tt.want {
			t.Errorf("%s: clipEncrypted = %v, %v; want %v", tt.name, got, err, tt.want)
		}
	}
}

func TestClipEncryptedReadError(t *testing.T) {
	boom := errors.New("boom")
	if _, err := clipEncrypted(iotest.ErrReader(boom)); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run ClipEncrypted`
Expected: FAIL to compile (`undefined: clipEncrypted`).

- [ ] **Step 3: Implement**

`encrypt.go`:
```go
package zenvik

import (
	"errors"
	"io"
)

const (
	sourcePacketSize = 192
	alignedUnitSize  = 6144 // 32 source packets
	tsSyncByte       = 0x47
)

// clipEncrypted reports whether an M2TS stream looks AACS-encrypted. AACS
// encrypts each 6144-byte aligned unit except its first 16 bytes, so in an
// encrypted unit the TS sync byte at offset 4 of every source packet after
// the first is scrambled. Only the first aligned unit is read. Streams with
// fewer than two complete source packets cannot be judged and are reported
// as not encrypted.
func clipEncrypted(r io.Reader) (bool, error) {
	buf := make([]byte, alignedUnitSize)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	for p := 1; p < n/sourcePacketSize; p++ {
		if buf[p*sourcePacketSize+4] != tsSyncByte {
			return true, nil
		}
	}
	return false, nil
}
```

The root package has only `doc.go` so far. `encrypt.go` joins it in `package zenvik`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race . -run ClipEncrypted && go vet ./... && golangci-lint run`
Expected: PASS. `golangci-lint` may report `clipEncrypted` as unused, because its only caller arrives in Task 5. If it does, record the finding in your report, leave the code as it is, and don't add a fake caller.

- [ ] **Step 5: Commit**

```bash
git add encrypt.go encrypt_test.go
git commit -m "Add AACS encryption probe for M2TS clips"
```

---

### Task 5: `zenvik.Open`: scanning, titles and ranking

**Files:**
- Create: `errors.go`, `title.go`, `disc.go`, `scan.go`
- Test: `zenvik_test.go` (package `zenvik_test`), `scan_internal_test.go` (package `zenvik`)

**Interfaces:**
- Consumes:
  - Task 2: `source.Open`, `source.Source{Kind, Path, Label, FS}`, `(*source.Source).Close`, `source.ErrUnsupported`, `source.Kind`, `source.ISO`, `source.BDMVDir`
  - Task 3: `rank.Rank`, `rank.Candidate`, `rank.Clip`, `rank.Info`, `rank.DefaultWeights`
  - Task 4: `clipEncrypted`
  - `bluray`: `ParseIndex`, `ParseMovieObjects`, `(*MovieObjects).Playlists`, `ParsePlaylist`, `(*Playlist).Duration`, `(*Playlist).Chapters`, `ParseMeta`, `PickMetaFile`, `DiscMeta`, `STN`, the stream enums, `ObjectHDMV`
  - Task 1 (tests): `SampleMovie`, `SimplePlaylist`, `Segment`, `ScrambledM2TS`, `AddClipsFor`, `ISO`, `WriteDir`, `MapFS`
- Produces (public API; Task 6 and later milestones rely on it):
  - `var ErrUnsupportedSource = source.ErrUnsupported`, `var ErrEncrypted error`, `var ErrNoTitles error`
  - `type SourceKind = source.Kind`; constants `ISO`, `BDMVDir`
  - `type Disc struct { Path string; Kind SourceKind; Label string; Meta *bluray.DiscMeta; Titles []*Title }` (plus an unexported source)
  - `func Open(ctx context.Context, path string) (*Disc, error)`
  - `func (d *Disc) Main() *Title`
  - `func (d *Disc) Title(id string) (*Title, error)`, where unknown IDs wrap `fs.ErrNotExist`
  - `func (d *Disc) Close() error`, which is idempotent
  - `type Title struct { ID string; Duration time.Duration; Size int64; Clips []Clip; Chapters []Chapter; Video []VideoTrack; Audio []AudioTrack; Subtitles []SubtitleTrack; Angles int; Encrypted bool; Rank RankInfo }`
  - `type Clip struct { ID string; In, Out time.Duration }`
  - `type Chapter struct { Number int; Start time.Duration }`
  - `type VideoTrack struct { PID uint16; Codec bluray.CodingType; Format bluray.VideoFormat; FrameRate bluray.FrameRate; DynamicRange bluray.DynamicRange }` and `func (v VideoTrack) HDR() bool`
  - `type AudioTrack struct { PID uint16; Codec bluray.CodingType; Language string; Channels bluray.AudioFormat; SampleRate bluray.SampleRate }`
  - `type SubtitleTrack struct { PID uint16; Codec bluray.CodingType; Language string }`
  - `type RankInfo struct { Score float64; IsMain, Ambiguous, Filtered bool; DuplicateOf string; Reasons []string }`. Same fields and order as `rank.Info`.
  - unexported `func scanTitles(ctx context.Context, fsys fs.FS) ([]*Title, *bluray.DiscMeta, error)`

- [ ] **Step 1: Write the failing tests**

`zenvik_test.go`:
```go
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
```

`scan_internal_test.go`:
```go
package zenvik

import (
	"context"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/testdisc"
)

type countingFS struct {
	fs.FS
	mu    sync.Mutex
	opens map[string]int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.mu.Lock()
	c.opens[name]++
	c.mu.Unlock()
	return c.FS.Open(name)
}

func TestScanProbesEachClipOnce(t *testing.T) {
	var segs []testdisc.Segment
	for i := 1; i <= 20; i++ {
		segs = append(segs, testdisc.Segment{Clip: fmt.Sprintf("%05d", i), Length: 5 * time.Minute})
	}
	d := &testdisc.Disc{Playlists: map[string]*bluray.Playlist{}, ClipData: map[string][]byte{}}
	for n := 1; n <= 300; n++ {
		k := n % 20
		rotated := append(append([]testdisc.Segment{}, segs[k:]...), segs[:k]...)
		d.Playlists[fmt.Sprintf("%05d", n)] = testdisc.SimplePlaylist(rotated...)
	}
	d.AddClipsFor()
	cfs := &countingFS{FS: d.MapFS(), opens: map[string]int{}}
	titles, _, err := scanTitles(context.Background(), cfs)
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 300 {
		t.Fatalf("titles = %d", len(titles))
	}
	for _, s := range segs {
		if n := cfs.opens["BDMV/STREAM/"+s.Clip+".m2ts"]; n != 1 {
			t.Errorf("%s opened %d times, want 1", s.Clip, n)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test .`
Expected: FAIL to compile (`undefined: zenvik.Open`, `undefined: scanTitles`, ...).

- [ ] **Step 3: Implement errors and types**

`errors.go`:
```go
package zenvik

import (
	"errors"

	"github.com/chad3814/zenvik/internal/source"
)

// Errors returned by Open; check them with errors.Is.
var (
	// ErrUnsupportedSource: the path is not a Blu-ray ISO image or BDMV folder.
	ErrUnsupportedSource = source.ErrUnsupported
	// ErrEncrypted: every usable title on the disc is AACS-encrypted.
	ErrEncrypted = errors.New("zenvik: disc is encrypted; zenvik only reads unencrypted discs")
	// ErrNoTitles: the disc has no playlists.
	ErrNoTitles = errors.New("zenvik: no playlists found")
)
```

`title.go`:
```go
package zenvik

import (
	"time"

	"github.com/chad3814/zenvik/bluray"
)

// Title is one playlist on the disc.
type Title struct {
	ID        string // playlist number, e.g. "00800"
	Duration  time.Duration
	Size      int64 // bytes of the referenced stream files, each distinct clip counted once
	Clips     []Clip
	Chapters  []Chapter
	Video     []VideoTrack
	Audio     []AudioTrack
	Subtitles []SubtitleTrack
	Angles    int // 1 for single-angle titles
	Encrypted bool
	Rank      RankInfo
}

// Clip is one play item: a clip and its IN/OUT times on the clip's clock.
type Clip struct {
	ID      string // "00001" → BDMV/STREAM/00001.m2ts
	In, Out time.Duration
}

// Chapter is a chapter start relative to the beginning of the title.
type Chapter struct {
	Number int // from 1
	Start  time.Duration
}

// VideoTrack is a video stream.
type VideoTrack struct {
	PID          uint16
	Codec        bluray.CodingType
	Format       bluray.VideoFormat
	FrameRate    bluray.FrameRate
	DynamicRange bluray.DynamicRange
}

// HDR reports whether the track uses a high dynamic range format.
func (v VideoTrack) HDR() bool { return v.DynamicRange != 0 }

// AudioTrack is an audio stream.
type AudioTrack struct {
	PID        uint16
	Codec      bluray.CodingType
	Language   string // ISO 639-2
	Channels   bluray.AudioFormat
	SampleRate bluray.SampleRate
}

// SubtitleTrack is a subtitle stream.
type SubtitleTrack struct {
	PID      uint16
	Codec    bluray.CodingType
	Language string // ISO 639-2
}

// RankInfo explains where a title placed in main-feature detection.
type RankInfo struct {
	Score       float64
	IsMain      bool
	Ambiguous   bool // on the main title: a runner-up scored within the ambiguity margin
	Filtered    bool
	DuplicateOf string
	Reasons     []string
}
```

`disc.go`:
```go
package zenvik

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/source"
)

// SourceKind says what kind of path a disc was opened from.
type SourceKind = source.Kind

// Source kinds.
const (
	ISO     = source.ISO
	BDMVDir = source.BDMVDir
)

// Disc is an opened Blu-ray disc image or BDMV folder.
type Disc struct {
	Path   string           // the path passed to Open
	Kind   SourceKind       //
	Label  string           // UDF volume identifier, or the folder's name
	Meta   *bluray.DiscMeta // disc library metadata (title, language); nil if absent
	Titles []*Title         // ranked: the main title, if any, is Titles[0]
	src    *source.Source
}

// Open reads the disc at path (an ISO image, a folder containing BDMV, or a
// BDMV folder), ranks its titles and detects encryption.
func Open(ctx context.Context, path string) (*Disc, error) {
	src, err := source.Open(path)
	if err != nil {
		return nil, err
	}
	titles, meta, err := scanTitles(ctx, src.FS)
	if err != nil {
		src.Close()
		return nil, err
	}
	return &Disc{Path: path, Kind: src.Kind, Label: src.Label, Meta: meta, Titles: titles, src: src}, nil
}

// Main returns the main title, or nil if no title qualifies.
func (d *Disc) Main() *Title {
	for _, t := range d.Titles {
		if t.Rank.IsMain {
			return t
		}
	}
	return nil
}

// Title returns the title with playlist ID id, e.g. "00800".
func (d *Disc) Title(id string) (*Title, error) {
	for _, t := range d.Titles {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("zenvik: no title %q on this disc: %w", id, fs.ErrNotExist)
}

// Close releases the disc. It is safe to call more than once.
func (d *Disc) Close() error {
	if d.src == nil {
		return nil
	}
	err := d.src.Close()
	d.src = nil
	return err
}
```

- [ ] **Step 4: Implement scanning**

`scan.go`:
```go
package zenvik

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/rank"
)

// defaultMinDuration filters titles shorter than this (spec section 4).
const defaultMinDuration = 2 * time.Minute

type playlistFile struct{ id, name string }

// scanTitles reads the BDMV tree in fsys and returns its titles ranked
// best-first, plus the disc metadata (nil when absent).
func scanTitles(ctx context.Context, fsys fs.FS) ([]*Title, *bluray.DiscMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	files, err := playlistFiles(fsys)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, ErrNoTitles
	}
	meta := readMeta(fsys)
	title1 := title1Playlists(fsys)
	clips := &clipCache{fsys: fsys, m: map[string]clipInfo{}}
	titles := make([]*Title, 0, len(files))
	cands := make([]rank.Candidate, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		t, c := loadTitle(fsys, f, clips, title1[f.id])
		titles = append(titles, t)
		cands = append(cands, c)
	}
	order, infos := rank.Rank(cands, defaultMinDuration, rank.DefaultWeights)
	ranked := make([]*Title, len(order))
	for k, i := range order {
		titles[i].Rank = RankInfo(infos[i])
		ranked[k] = titles[i]
	}
	if allEncrypted(ranked) {
		return nil, nil, ErrEncrypted
	}
	return ranked, meta, nil
}

// allEncrypted reports whether some title is encrypted and no unfiltered
// title is unencrypted.
func allEncrypted(titles []*Title) bool {
	sawEncrypted := false
	for _, t := range titles {
		if t.Encrypted {
			sawEncrypted = true
			continue
		}
		if !t.Rank.Filtered {
			return false
		}
	}
	return sawEncrypted
}

func playlistFiles(fsys fs.FS) ([]playlistFile, error) {
	ents, err := fs.ReadDir(fsys, "BDMV/PLAYLIST")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []playlistFile
	for _, e := range ents {
		name := e.Name()
		ext := path.Ext(name)
		if e.IsDir() || !strings.EqualFold(ext, ".mpls") {
			continue
		}
		out = append(out, playlistFile{id: strings.TrimSuffix(name, ext), name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

func loadTitle(fsys fs.FS, f playlistFile, clips *clipCache, playedByTitle1 bool) (*Title, rank.Candidate) {
	t := &Title{ID: f.id}
	c := rank.Candidate{ID: f.id, PlayedByTitle1: playedByTitle1}
	b, err := fs.ReadFile(fsys, "BDMV/PLAYLIST/"+f.name)
	if err == nil {
		var p *bluray.Playlist
		if p, err = bluray.ParsePlaylist(b); err == nil {
			fillFromPlaylist(t, p)
		}
	}
	if err != nil {
		c.Problem = "unreadable playlist: " + err.Error()
		return t, c
	}
	seen := map[string]bool{}
	for _, cl := range t.Clips {
		if seen[cl.ID] {
			continue
		}
		seen[cl.ID] = true
		ci := clips.get(cl.ID)
		if ci.problem != "" {
			if c.Problem == "" {
				c.Problem = ci.problem
			}
			continue
		}
		t.Size += ci.size
		t.Encrypted = t.Encrypted || ci.encrypted
	}
	for _, cl := range t.Clips {
		c.Clips = append(c.Clips, rank.Clip{ID: cl.ID, In: cl.In, Out: cl.Out})
	}
	c.Duration = t.Duration
	c.Chapters = len(t.Chapters)
	c.Languages = countLanguages(t)
	c.HasVideo = len(t.Video) > 0
	c.Encrypted = t.Encrypted
	return t, c
}

func fillFromPlaylist(t *Title, p *bluray.Playlist) {
	t.Duration = p.Duration()
	for i, start := range p.Chapters() {
		t.Chapters = append(t.Chapters, Chapter{Number: i + 1, Start: start})
	}
	t.Angles = 1
	for _, it := range p.Items {
		t.Clips = append(t.Clips, Clip{ID: it.ClipID, In: it.In.Duration(), Out: it.Out.Duration()})
		t.Angles = max(t.Angles, len(it.Angles)+1)
	}
	if len(p.Items) == 0 {
		return
	}
	stn := p.Items[0].STN
	for _, s := range stn.Video {
		t.Video = append(t.Video, VideoTrack{PID: s.PID, Codec: s.Coding, Format: s.VideoFormat, FrameRate: s.FrameRate, DynamicRange: s.DynamicRange})
	}
	for _, s := range stn.Audio {
		t.Audio = append(t.Audio, AudioTrack{PID: s.PID, Codec: s.Coding, Language: s.Language, Channels: s.AudioFormat, SampleRate: s.SampleRate})
	}
	for _, s := range stn.PG {
		t.Subtitles = append(t.Subtitles, SubtitleTrack{PID: s.PID, Codec: s.Coding, Language: s.Language})
	}
}

func countLanguages(t *Title) int {
	set := map[string]bool{}
	for _, a := range t.Audio {
		if a.Language != "" {
			set[a.Language] = true
		}
	}
	for _, s := range t.Subtitles {
		if s.Language != "" {
			set[s.Language] = true
		}
	}
	return len(set)
}

type clipInfo struct {
	size      int64
	encrypted bool
	problem   string // non-empty when the stream file is missing, empty or unreadable
}

// clipCache probes each stream file at most once per scan.
type clipCache struct {
	fsys fs.FS
	m    map[string]clipInfo
}

func (c *clipCache) get(id string) clipInfo {
	if ci, ok := c.m[id]; ok {
		return ci
	}
	ci := probeClip(c.fsys, id)
	c.m[id] = ci
	return ci
}

func probeClip(fsys fs.FS, id string) clipInfo {
	name := id + ".m2ts"
	f, err := fsys.Open("BDMV/STREAM/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		return clipInfo{problem: "missing stream file " + name}
	}
	if err != nil {
		return clipInfo{problem: fmt.Sprintf("unreadable stream file %s: %v", name, err)}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return clipInfo{problem: fmt.Sprintf("unreadable stream file %s: %v", name, err)}
	}
	if st.Size() == 0 {
		return clipInfo{problem: "empty stream file " + name}
	}
	enc, err := clipEncrypted(f)
	if err != nil {
		return clipInfo{problem: fmt.Sprintf("unreadable stream file %s: %v", name, err)}
	}
	return clipInfo{size: st.Size(), encrypted: enc}
}

// title1Playlists returns the playlist IDs HDMV title 1 may play, or nil
// when title 1 is missing, BD-J, or its movie objects can't be read.
func title1Playlists(fsys fs.FS) map[string]bool {
	b, err := fs.ReadFile(fsys, "BDMV/index.bdmv")
	if err != nil {
		return nil
	}
	idx, err := bluray.ParseIndex(b)
	if err != nil || len(idx.Titles) == 0 || idx.Titles[0].Type != bluray.ObjectHDMV {
		return nil
	}
	b, err = fs.ReadFile(fsys, "BDMV/MovieObject.bdmv")
	if err != nil {
		return nil
	}
	mo, err := bluray.ParseMovieObjects(b)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, n := range mo.Playlists(int(idx.Titles[0].MovieObjectID)) {
		out[fmt.Sprintf("%05d", n)] = true
	}
	return out
}

// readMeta returns the disc library metadata, or nil if there is none.
func readMeta(fsys fs.FS) *bluray.DiscMeta {
	ents, err := fs.ReadDir(fsys, "BDMV/META/DL")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	name := bluray.PickMetaFile(names)
	if name == "" {
		return nil
	}
	b, err := fs.ReadFile(fsys, "BDMV/META/DL/"+name)
	if err != nil {
		return nil
	}
	m, err := bluray.ParseMeta(b)
	if err != nil {
		return nil
	}
	return m
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./... && go vet ./... && golangci-lint run`
Expected: PASS; 0 lint issues (`clipEncrypted` now has a caller).

- [ ] **Step 6: Commit**

```bash
git add errors.go title.go disc.go scan.go zenvik_test.go scan_internal_test.go
git commit -m "Add zenvik.Open: scan titles, detect encryption, rank the main feature"
```

---

### Task 6: `zenvik info` CLI

**Files:**
- Create: `cmd/zenvik/main.go`, `cmd/zenvik/info.go`, `cmd/zenvik/format.go`
- Test: `cmd/zenvik/main_test.go`, `cmd/zenvik/format_test.go`
- Modify: `go.mod`, `go.sum` (add cobra), `README.md`

**Interfaces:**
- Consumes (Task 5): `zenvik.Open`, `Disc{Path, Kind, Label, Meta, Titles}`, `(*Disc).Main`, `(*Disc).Close`, `Title` and its track types, `RankInfo`, `ErrUnsupportedSource`, `ErrEncrypted`, `ErrNoTitles`. From `bluray`, the enum `String()` methods.
- Consumes (Task 1, tests only): `testdisc.SampleMovie`, `SimplePlaylist`, `Segment`, `ScrambledM2TS`, `AddClipsFor`, `WriteDir`.
- Produces: the `zenvik` binary with `zenvik info <path> [--all] [--json]`; `func run(ctx context.Context, args []string, stdout, stderr io.Writer) int` (package `main`); exit codes per Global Constraints.
- Output contract:
  - **Table.**
    - The first line is the disc name (meta title, else label), then `(<kind>: <path>)`.
    - The column header is `ID DURATION SIZE CH VIDEO AUDIO SUBS NOTES`, with a leading unnamed column holding `★` for the main title.
    - Filtered titles are hidden unless `--all`; a footer says how many were hidden.
    - If there's no main title, print `No title qualifies as the main feature.` to stdout.
    - An ambiguous main title prints `warning: the main title is a close call (close second: ...)` to stderr.
  - **JSON** is the CLI-owned view defined in `info.go`. It always lists every title.

- [ ] **Step 1: Add the cobra dependency**

Run: `go get github.com/spf13/cobra@latest && go mod tidy`
Expected: `go.mod` gains a `require github.com/spf13/cobra v1.x.y` line, and `go.sum` is created. (`go mod tidy` drops cobra again until code imports it. If that happens, re-run `go get` after Step 4, before testing.)

- [ ] **Step 2: Write the failing tests**

`cmd/zenvik/main_test.go`:
```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func writeDisc(t *testing.T, d *testdisc.Disc) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "SAMPLE_MOVIE")
	if err := d.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func runCLI(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func lineWith(out, s string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func TestInfoTable(t *testing.T) {
	code, out, errOut := runCLI("info", writeDisc(t, testdisc.SampleMovie()))
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if !strings.HasPrefix(out, "Sample Movie  (BDMV folder: ") {
		t.Errorf("header = %q", strings.SplitN(out, "\n", 2)[0])
	}
	main := lineWith(out, "00800")
	for _, want := range []string{"★", "1:40:00", "120.0 KiB", "20", "H.264/AVC 1080p", "eng,fra", "eng"} {
		if !strings.Contains(main, want) {
			t.Errorf("main line %q lacks %q", main, want)
		}
	}
	if !strings.HasPrefix(main, "★") {
		t.Errorf("main line should start with ★: %q", main)
	}
	if dup := lineWith(out, "00801"); !strings.Contains(dup, "duplicate of 00800") || strings.Contains(dup, "★") {
		t.Errorf("duplicate line = %q", dup)
	}
	if strings.Contains(out, "00099") {
		t.Error("filtered title shown without --all")
	}
	if !strings.Contains(out, "1 filtered title(s) hidden; use --all to show them.") {
		t.Errorf("missing hidden footer in:\n%s", out)
	}
}

func TestInfoAll(t *testing.T) {
	code, out, _ := runCLI("info", "--all", writeDisc(t, testdisc.SampleMovie()))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if l := lineWith(out, "00099"); !strings.Contains(l, "shorter than 2m0s") {
		t.Errorf("filtered line = %q", l)
	}
	if strings.Contains(out, "hidden") {
		t.Error("footer shown with --all")
	}
}

func TestInfoJSON(t *testing.T) {
	code, out, _ := runCLI("info", "--json", writeDisc(t, testdisc.SampleMovie()))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var got jsonDisc
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Main != "00800" || got.Kind != "BDMV folder" || got.Label != "SAMPLE_MOVIE" || got.Title != "Sample Movie" {
		t.Errorf("disc = %+v", got)
	}
	if len(got.Titles) != 4 {
		t.Fatalf("titles = %d, want 4", len(got.Titles))
	}
	m := got.Titles[0]
	if m.ID != "00800" || !m.Rank.Main || m.DurationSeconds != 6000 || len(m.Chapters) != 20 ||
		m.Video[0].Codec != "H.264/AVC" || m.Audio[0].Language != "eng" || m.SizeBytes != 20*6144 {
		t.Errorf("main = %+v", m)
	}
	last := got.Titles[3]
	if last.ID != "00099" || !last.Rank.Filtered {
		t.Errorf("filtered title = %+v", last)
	}
	if !strings.Contains(out, `"chapters": [`) || strings.Contains(out, `"reasons": null`) {
		t.Errorf("JSON should use [] rather than null for empty lists:\n%s", out)
	}
}

func TestInfoAmbiguousWarning(t *testing.T) {
	d := testdisc.SampleMovie()
	d.Titles, d.MovieObjects = nil, nil // no title-1 signal
	var segs []testdisc.Segment
	for i := 201; i <= 220; i++ {
		segs = append(segs, testdisc.Segment{Clip: fmt.Sprintf("%05d", i), Length: 297 * time.Second})
	}
	d.Playlists["00802"] = testdisc.SimplePlaylist(segs...)
	d.AddClipsFor()
	code, out, errOut := runCLI("info", writeDisc(t, d))
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(errOut, "warning: the main title is a close call (close second: 00802") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(lineWith(out, "00800"), "ambiguous") {
		t.Errorf("main line = %q", lineWith(out, "00800"))
	}
}

func TestExitCodes(t *testing.T) {
	good := writeDisc(t, testdisc.SampleMovie())
	enc := testdisc.SampleMovie()
	for id := range enc.Clips {
		enc.ClipData[id] = testdisc.ScrambledM2TS(1)
	}
	encrypted := writeDisc(t, enc)
	dvd := filepath.Join(t.TempDir(), "DVD")
	if err := os.MkdirAll(filepath.Join(dvd, "VIDEO_TS"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		args []string
		want int
	}{
		{[]string{"info", good}, 0},
		{nil, 2},
		{[]string{"bogus"}, 2},
		{[]string{"info"}, 2},
		{[]string{"info", good, good}, 2},
		{[]string{"info", "--bogus", good}, 2},
		{[]string{"info", filepath.Join(good, "missing")}, 1},
		{[]string{"info", dvd}, 3},
		{[]string{"info", encrypted}, 3},
	}
	for _, tt := range tests {
		code, _, errOut := runCLI(tt.args...)
		if code != tt.want {
			t.Errorf("zenvik %v: exit %d, want %d (stderr %q)", tt.args, code, tt.want, errOut)
		}
		if tt.want != 0 && !strings.Contains(errOut, "zenvik: ") {
			t.Errorf("zenvik %v: stderr lacks error message: %q", tt.args, errOut)
		}
	}
}
```

`cmd/zenvik/format_test.go`:
```go
package main

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := map[time.Duration]string{
		0:                             "0:00:00",
		150 * time.Second:             "0:02:30",
		100 * time.Minute:             "1:40:00",
		59*time.Second + 600*time.Millisecond: "0:01:00",
		25*time.Hour + 3*time.Second:  "25:00:03",
	}
	for in, want := range tests {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := map[int64]string{
		0:       "0 B",
		1023:    "1023 B",
		1024:    "1.0 KiB",
		122880:  "120.0 KiB",
		5 << 30: "5.0 GiB",
		3 << 40: "3.0 TiB",
		5 << 50: "5120.0 TiB",
	}
	for in, want := range tests {
		if got := formatSize(in); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestJoinLanguages(t *testing.T) {
	if got := joinLanguages([]string{"eng", "fra", "eng", ""}); got != "eng,fra,und" {
		t.Errorf("got %q", got)
	}
	if got := joinLanguages(nil); got != "-" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./cmd/zenvik/`
Expected: FAIL to compile (`undefined: run`, `undefined: jsonDisc`, ...).

- [ ] **Step 4: Implement**

`cmd/zenvik/main.go`:
```go
// Command zenvik inspects and remuxes unencrypted Blu-ray disc images and
// BDMV folders.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik"
)

// usageError marks errors caused by how the command was invoked (exit 2).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes the CLI and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	fmt.Fprintf(stderr, "zenvik: %v\n", err)
	return exitCode(err)
}

// exitCode maps an error to the exit codes in the design spec.
func exitCode(err error) int {
	var ue usageError
	switch {
	case errors.As(err, &ue):
		return 2
	case errors.Is(err, zenvik.ErrUnsupportedSource), errors.Is(err, zenvik.ErrEncrypted), errors.Is(err, zenvik.ErrNoTitles):
		return 3
	}
	return 1
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "zenvik",
		Short:         "Inspect and remux unencrypted Blu-ray disc images",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown command %q", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SetOut(cmd.ErrOrStderr())
			_ = cmd.Help()
			return usageError{errors.New("missing command")}
		},
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return usageError{err} })
	root.AddCommand(newInfoCmd())
	return root
}
```

`cmd/zenvik/info.go`:
```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik"
)

func newInfoCmd() *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "info <path>",
		Short: "List the titles on a Blu-ray ISO image or BDMV folder",
		Long: `List the titles (playlists) on a Blu-ray ISO image or BDMV folder, ranked so
the likely main feature (★) comes first. Duplicates, encrypted titles and
multi-angle titles are noted; very short or unusable titles are hidden
unless --all is given.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError{fmt.Errorf("info needs exactly one path, got %d", len(args))}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := zenvik.Open(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			defer d.Close()
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), d)
			}
			writeTable(cmd.OutOrStdout(), cmd.ErrOrStderr(), d, all)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "also list filtered titles and why they were filtered")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the disc as JSON (always includes every title)")
	return cmd
}

func writeTable(out, errw io.Writer, d *zenvik.Disc, all bool) {
	name := d.Label
	if d.Meta != nil && d.Meta.Title != "" {
		name = d.Meta.Title
	}
	fmt.Fprintf(out, "%s  (%s: %s)\n\n", name, d.Kind, d.Path)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "\tID\tDURATION\tSIZE\tCH\tVIDEO\tAUDIO\tSUBS\tNOTES")
	hidden := 0
	for _, t := range d.Titles {
		if t.Rank.Filtered && !all {
			hidden++
			continue
		}
		mark := ""
		if t.Rank.IsMain {
			mark = "★"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n", mark, t.ID, formatDuration(t.Duration),
			formatSize(t.Size), len(t.Chapters), videoSummary(t), audioLanguages(t), subtitleLanguages(t), notes(t))
	}
	tw.Flush()
	if hidden > 0 {
		fmt.Fprintf(out, "\n%d filtered title(s) hidden; use --all to show them.\n", hidden)
	}
	m := d.Main()
	switch {
	case m == nil:
		fmt.Fprintln(out, "\nNo title qualifies as the main feature.")
	case m.Rank.Ambiguous:
		fmt.Fprintf(errw, "warning: the main title is a close call (%s)\n", closeSecond(m))
	}
}

func notes(t *zenvik.Title) string {
	var n []string
	if t.Rank.DuplicateOf != "" {
		n = append(n, "duplicate of "+t.Rank.DuplicateOf)
	}
	if t.Encrypted {
		n = append(n, "encrypted")
	}
	if t.Angles > 1 {
		n = append(n, fmt.Sprintf("%d angles", t.Angles))
	}
	if t.Rank.Ambiguous {
		n = append(n, "ambiguous")
	}
	if t.Rank.Filtered {
		var why []string
		for _, r := range t.Rank.Reasons {
			if r != "encrypted" {
				why = append(why, r)
			}
		}
		n = append(n, strings.Join(why, "; "))
	}
	return strings.Join(n, ", ")
}

func closeSecond(t *zenvik.Title) string {
	for _, r := range t.Rank.Reasons {
		if strings.HasPrefix(r, "close second") {
			return r
		}
	}
	return "another title scored almost as high"
}

type jsonDisc struct {
	Path     string      `json:"path"`
	Kind     string      `json:"kind"`
	Label    string      `json:"label"`
	Title    string      `json:"title,omitempty"`
	Language string      `json:"language,omitempty"`
	Main     string      `json:"main,omitempty"`
	Titles   []jsonTitle `json:"titles"`
}

type jsonTitle struct {
	ID              string         `json:"id"`
	DurationSeconds float64        `json:"duration_seconds"`
	SizeBytes       int64          `json:"size_bytes"`
	Chapters        []float64      `json:"chapters"` // start times in seconds
	Angles          int            `json:"angles"`
	Encrypted       bool           `json:"encrypted"`
	Video           []jsonVideo    `json:"video"`
	Audio           []jsonAudio    `json:"audio"`
	Subtitles       []jsonSubtitle `json:"subtitles"`
	Rank            jsonRank       `json:"rank"`
}

type jsonVideo struct {
	PID          uint16 `json:"pid"`
	Codec        string `json:"codec"`
	Format       string `json:"format"`
	FrameRate    string `json:"frame_rate"`
	DynamicRange string `json:"dynamic_range"`
}

type jsonAudio struct {
	PID        uint16 `json:"pid"`
	Codec      string `json:"codec"`
	Language   string `json:"language"`
	Channels   string `json:"channels"`
	SampleRate string `json:"sample_rate"`
}

type jsonSubtitle struct {
	PID      uint16 `json:"pid"`
	Codec    string `json:"codec"`
	Language string `json:"language"`
}

type jsonRank struct {
	Score       float64  `json:"score"`
	Main        bool     `json:"main"`
	Ambiguous   bool     `json:"ambiguous"`
	Filtered    bool     `json:"filtered"`
	DuplicateOf string   `json:"duplicate_of,omitempty"`
	Reasons     []string `json:"reasons"`
}

func writeJSON(w io.Writer, d *zenvik.Disc) error {
	out := jsonDisc{Path: d.Path, Kind: d.Kind.String(), Label: d.Label, Titles: make([]jsonTitle, 0, len(d.Titles))}
	if d.Meta != nil {
		out.Title, out.Language = d.Meta.Title, d.Meta.Language
	}
	if m := d.Main(); m != nil {
		out.Main = m.ID
	}
	for _, t := range d.Titles {
		out.Titles = append(out.Titles, toJSONTitle(t))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func toJSONTitle(t *zenvik.Title) jsonTitle {
	jt := jsonTitle{
		ID:              t.ID,
		DurationSeconds: t.Duration.Seconds(),
		SizeBytes:       t.Size,
		Chapters:        make([]float64, 0, len(t.Chapters)),
		Angles:          t.Angles,
		Encrypted:       t.Encrypted,
		Video:           make([]jsonVideo, 0, len(t.Video)),
		Audio:           make([]jsonAudio, 0, len(t.Audio)),
		Subtitles:       make([]jsonSubtitle, 0, len(t.Subtitles)),
		Rank: jsonRank{
			Score:       t.Rank.Score,
			Main:        t.Rank.IsMain,
			Ambiguous:   t.Rank.Ambiguous,
			Filtered:    t.Rank.Filtered,
			DuplicateOf: t.Rank.DuplicateOf,
			Reasons:     append([]string{}, t.Rank.Reasons...),
		},
	}
	for _, c := range t.Chapters {
		jt.Chapters = append(jt.Chapters, c.Start.Seconds())
	}
	for _, v := range t.Video {
		jt.Video = append(jt.Video, jsonVideo{PID: v.PID, Codec: v.Codec.String(), Format: v.Format.String(),
			FrameRate: v.FrameRate.String(), DynamicRange: v.DynamicRange.String()})
	}
	for _, a := range t.Audio {
		jt.Audio = append(jt.Audio, jsonAudio{PID: a.PID, Codec: a.Codec.String(), Language: a.Language,
			Channels: a.Channels.String(), SampleRate: a.SampleRate.String()})
	}
	for _, s := range t.Subtitles {
		jt.Subtitles = append(jt.Subtitles, jsonSubtitle{PID: s.PID, Codec: s.Codec.String(), Language: s.Language})
	}
	return jt
}
```

`cmd/zenvik/format.go`:
```go
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/chad3814/zenvik"
)

// formatDuration renders d as H:MM:SS, rounded to the second.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d:%02d", int(d/time.Hour), int(d/time.Minute)%60, int(d/time.Second)%60)
}

// formatSize renders n bytes with binary units, up to TiB.
func formatSize(n int64) string {
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

func videoSummary(t *zenvik.Title) string {
	if len(t.Video) == 0 {
		return "-"
	}
	v := t.Video[0]
	s := v.Codec.String() + " " + v.Format.String()
	if v.HDR() {
		s += " " + v.DynamicRange.String()
	}
	if extra := len(t.Video) - 1; extra > 0 {
		s += fmt.Sprintf(" +%d", extra)
	}
	return s
}

func audioLanguages(t *zenvik.Title) string {
	langs := make([]string, 0, len(t.Audio))
	for _, a := range t.Audio {
		langs = append(langs, a.Language)
	}
	return joinLanguages(langs)
}

func subtitleLanguages(t *zenvik.Title) string {
	langs := make([]string, 0, len(t.Subtitles))
	for _, s := range t.Subtitles {
		langs = append(langs, s.Language)
	}
	return joinLanguages(langs)
}

// joinLanguages lists distinct languages in order; empty codes become "und".
func joinLanguages(langs []string) string {
	var out []string
	seen := map[string]bool{}
	for _, l := range langs {
		if l == "" {
			l = "und"
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return "-"
	}
	return strings.Join(out, ",")
}
```

Append a usage section to `README.md`:
````markdown

## Usage

```
zenvik info <path>          # list titles; ★ marks the likely main feature
zenvik info --all <path>    # include filtered titles and why they were filtered
zenvik info --json <path>   # machine-readable output
```

`<path>` is an ISO image, a folder containing `BDMV`, or a `BDMV` folder.

Exit codes: 0 success, 1 failure, 2 usage error, 3 unsupported or encrypted source,
4 missing or too-old dependency.
````

- [ ] **Step 5: Run tests to verify they pass**

Run: `go mod tidy && go test -race ./... && go vet ./... && golangci-lint run`
Expected: PASS; 0 lint issues; `go.mod` lists only `github.com/spf13/cobra` as a direct requirement.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum cmd/zenvik README.md
git commit -m "Add zenvik info command"
```

---

### Task 7: Milestone verification

**Files:**
- Modify: none, unless a check fails.

**Interfaces:**
- Consumes: everything.
- Produces: a verified branch ready for the final review.

- [ ] **Step 1: Run the full verification suite**

```bash
test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
go vet ./...
golangci-lint run
go test -race ./...
go test -tags integration ./...
CGO_ENABLED=0 go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
```

Expected: `gofmt -l` prints nothing; every command exits 0.

- [ ] **Step 2: Check the dependency rules**

```bash
go list -deps ./bluray ./udf ./internal/rank | grep -v -e '^[a-z]*$' -e '^[a-z]*/' -e '^github.com/chad3814/zenvik/' || true
grep -A3 '^require' go.mod
go list -deps ./cmd/zenvik | grep -v -e '^[a-z]*$' -e '^[a-z]*/' -e '^github.com/chad3814/zenvik' | sort
```

Expected:
- The first command prints nothing (`bluray`, `udf` and `rank` depend only on the standard library).
- `go.mod`'s direct requirements are only `github.com/spf13/cobra`.
- The CLI's non-standard dependencies are cobra and its own dependencies (`github.com/spf13/pflag`, plus `github.com/inconshreveable/mousetrap` on Windows builds).

- [ ] **Step 3: Smoke-test the binary on a synthetic disc**

```bash
go test ./cmd/zenvik -run 'TestInfoTable|TestInfoJSON' -v
go build -o /tmp/zenvik-m2-smoke ./cmd/zenvik && /tmp/zenvik-m2-smoke --help && /tmp/zenvik-m2-smoke info --help; echo "exit $?"; rm -f /tmp/zenvik-m2-smoke
```

Expected: tests PASS; help text lists the `info` command and its `--all` / `--json` flags; exit 0.

- [ ] **Step 4: Review the branch**

Run: `git log --oneline origin/main..HEAD && git status`
Expected: one commit per task (6 commits, plus the plan commit if it was committed on this branch), a clean tree, and nothing pushed.
