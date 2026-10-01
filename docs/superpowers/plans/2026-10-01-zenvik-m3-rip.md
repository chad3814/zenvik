# zenvik Milestone 3 (Rip) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `disc.Rip(ctx, title, opts)` remuxes a title to MKV with mkvmerge, mounting ISO images when needed, with progress, cancellation and atomic output. `zenvik rip <path>` drives it from the command line.

**Architecture:**
- `internal/mux` finds and version-checks mkvmerge, identifies a playlist (`mkvmerge -J`), builds the argument list, and runs mkvmerge through a JSON options file in `--gui-mode`, parsing progress, warnings and errors.
- `internal/mount` attaches ISO images read-only, with one implementation per OS behind a single `Attach`/`Detach` API.
- `Disc.Rip` in the root package ties these together:
  - It refuses encrypted titles and existing outputs.
  - It mounts if needed, and maps the title's playlist streams to mkvmerge tracks by PID.
  - It writes `<output>.partial` and renames it on success.
  - It always cleans up the partial file and the mount, including on Ctrl-C.
- `cmd/zenvik` gains `rip`, a progress display, exit code 4, and a second Ctrl-C that exits immediately.

**Tech Stack:** Go 1.27; MKVToolNix `mkvmerge` (external program, v80 or newer); macOS `hdiutil`, Linux `udisksctl`, and Windows PowerShell `Mount-DiskImage`; ffmpeg (integration tests only, to make real M2TS clips). No new Go dependencies.

**Spec:** `docs/superpowers/specs/2026-10-01-zenvik-v1-design.md`, sections 3 (`Rip`, `RipOptions`, `Progress`, `RipResult`, errors), 5 (muxing), 6 (mounting), 7 (`zenvik rip`, exit codes, signals), 9 (integration tests), 10 (CI) and 11 (milestone 3).

## Global Constraints

- Module `github.com/chad3814/zenvik`, `go 1.27`. No cgo: `CGO_ENABLED=0 go build ./...` must succeed.
- No new Go dependencies. The only direct requirement stays `github.com/spf13/cobra`.
- `bluray`, `udf`, `internal/rank`, `internal/mux`, `internal/mount` and `internal/naming` import only the standard library, plus each other and the project's own packages where a task says so.
- Never commit copyrighted disc data. Test media is generated: `internal/testdisc` writes the navigation files and ffmpeg writes the clips.
- Errors are sentinel values wrapped with `%w`, checkable with `errors.Is`. Library error messages start with `zenvik:`.
- mkvmerge (spec §5), verbatim:
  - Minimum version: **MKVToolNix 80.0**.
  - Identify with `mkvmerge -J <root>/BDMV/PLAYLIST/<id>.mpls`, and map tracks to STN streams by PID.
  - Pass arguments through a JSON options file, `mkvmerge @opts.json`.
  - Run with `--gui-mode` and parse `#GUI#progress N%`.
  - Write `<output>.partial` and rename it on success. Delete the partial file on failure or cancel.
  - Exit code 1 (warnings) is success, with the warnings in `RipResult.Warnings`. Any other non-zero exit is an error that carries mkvmerge's error lines.
  - Default flags: the first video and first audio tracks are default; no subtitle track is.
- Mounting (spec §6):
  - Mount read-only for the duration of `Rip`, then unmount even after cancellation.
  - After mounting, check that `BDMV/index.bdmv` exists at the mount point.
  - If mounting isn't possible, return `ErrMountUnavailable` with a message suggesting the user mount the image or extract it and pass the folder.
- CLI exit codes (spec §7): `0` success, `1` failure, `2` usage error, `3` unsupported or encrypted source, `4` missing or too-old dependency.
- Signals (spec §7): SIGINT and SIGTERM cancel the context so cleanup (partial-file removal, unmount) runs. A second signal terminates immediately.
- Code passes `gofmt`, `go vet ./...`, `golangci-lint run` (v2.13.2), `go test -race ./...`, and `go test -tags integration ./...` (which needs `mkvmerge` and `ffmpeg` on `PATH`).
- Work in worktree `/Users/chad/Projects/zenvik/worktrees/m3-rip` on branch `feat/m3-rip`.
- Never `git push`.
- Commits are SSH-signed. If signing fails, commit with `git -c commit.gpgsign=false commit ...` and say so in the task report.

## Design Decisions (within the spec's latitude)

1. **Version floor.**
   - Probing this machine's MKVToolNix v102.0 confirmed what the spec relies on:
     - `mkvmerge -J` on an MPLS file lists each track with its PID in `properties.number` (and `stream_id`).
     - It counts the playlist marks as chapters.
     - Muxing the MPLS joins the clips and writes those chapters.
     - Track languages come from the stream, not the MPLS, which is why we pass `--language` ourselves.
   - v80.0 itself isn't available to test. The minimum stays **80**, and CI's Ubuntu MKVToolNix (v82 on Ubuntu 24.04) runs the integration suite as the oldest version we exercise.
2. **Scanning phase.** In `--gui-mode`, mkvmerge first scans the playlist's files (`#GUI#begin_scanning_playlists` … `#GUI#end_scanning_playlists`, with its own 0–100%), then muxes (another 0–100%). `Phase` therefore has four values (`Mounting`, `Scanning`, `Muxing`, `Finalizing`), and `Fraction` rises steadily within each.
3. **Output tracks are the title's STN streams, matched by PID.**
   - Tracks appear in stream-number order: video, then audio, then subtitles, enforced with `--track-order`.
   - An mkvmerge track that isn't in the STN table is skipped, and so is an STN stream that mkvmerge didn't find. Each skip adds a warning.
4. **Track metadata.**
   - Audio and subtitle tracks get `--language` from the STN table. The video language is left as mkvmerge reads it.
   - Only audio tracks get names: `"<codec> <layout>"`, built from mkvmerge's channel count (`Mono`, `Stereo`, `5.1`, `7.1`, `N ch`). For example: `"TrueHD 7.1"`, `"AC-3 Stereo"`.
   - There are no "Commentary" names, because the STN table doesn't mark commentary tracks.
5. **`RipOptions.DryRun` and `RipResult.Command` are added.** The spec's `--dry-run` needs the resolved mkvmerge command without running it. A dry run on an ISO still mounts and unmounts it, because identification needs the files.
6. **mkvmerge is found on `PATH` in M3.** The `mkvmerge_path` config setting arrives with the config work in M4.
7. **Mounting details.**
   - **macOS** mounts into a fresh temp directory (`os.MkdirTemp("", "zenvik-mount-")`) with `-mountpoint`, instead of parsing `-plist` output. The location is known in advance, and the `zenvik-mount-` prefix lets M4's `doctor` find leftover mounts.
   - **Linux** handles desktops that auto-mount new loop devices: if `udisksctl mount` reports `AlreadyMounted`, the existing mount point is read from `udisksctl info`.
   - **Detaching** ignores context cancellation (unmounting must still happen after Ctrl-C) and gives up after one minute. The error then includes the manual command.
8. **Output name in M3** is `<output-dir>/<SafeFileName(Disc.Name())>.mkv`.
   - `Disc.Name()` implements the spec's `{name}` fallback: the bdmt title, otherwise the volume label tidied up (`THE_MATRIX` → `The Matrix`).
   - `--name`, `--year`, `--template` and `--preset` arrive in M4.
9. **What `Rip` accepts.**
   - It refuses encrypted titles (`ErrEncrypted`).
   - Filtered titles (for example, too short) can still be ripped when the caller picks them explicitly, as with `zenvik rip --playlist`. If files are missing, the mkvmerge error comes through.
10. **CLI errors.**
    - A missing or too-old mkvmerge exits `4`.
    - An unavailable mount or an existing output file exits `1`, with a hint (`--overwrite`, or mount the image and pass the folder).
    - An unknown `--playlist` exits `2`.
    - No main title, with no `--playlist`, exits `1` and suggests `--playlist`.
11. **Concurrency.** A `Disc` isn't safe for concurrent `Rip` calls. That's documented, not enforced.

## Review Focus

These are input classes the spec implies but no feature test exercises. Each has a test in the task that owns the code.

1. **Ctrl-C during a rip, from a folder or an ISO.** mkvmerge is killed, `.partial` is removed, the ISO is unmounted, and `Rip` returns `context.Canceled`. Tests:
   - Task 2, `TestMuxCanceled`
   - Task 6, `TestRipCanceled` and `TestRipISOCanceled` (darwin)
2. **An existing output file, or a missing output directory.** `Rip` refuses without `Overwrite`, creates missing parent directories, and with `Overwrite` replaces the file. Tests: Task 6, `TestRipErrorsBeforeMuxing` and `TestRipOverwrite`.
3. **mkvmerge warnings (exit 1) and errors (exit 2).** Warnings come back with a nil error and the output is kept. Errors remove the partial file and include mkvmerge's message, unescaped from GUI mode. Test: Task 2, `TestMuxWarnings` and `TestMuxFailure`.
4. **Paths with spaces, non-ASCII letters or quotes.** These appear in the output directory, the ISO path and the PowerShell command, and they arrive intact. Tests:
   - Task 2, `TestMuxOptionsFilePreservesPaths`
   - Task 3, `TestPSQuote`
   - Task 6, `TestRipDirectory` (output under `out dir/Réal "Movie".mkv`)
5. **mkvmerge missing or too old, or no mount tool.** The user gets a clear error and the right exit code (4 for mkvmerge, 1 for mounting). Tests:
   - Task 1, `TestCheckVersion` and `TestFindNotFound`
   - Task 3, the per-OS tests that mount fails without the tool
   - Task 7, `TestRipExitCodes`

---

## File Map

| File | Responsibility |
|---|---|
| `internal/mux/mkvmerge.go` | Errors, `Version`, `ParseVersion`, `Mkvmerge`, `Find`, `checkVersion`, command construction |
| `internal/mux/identify.go` | `Identification`, `IdentifiedTrack`, `Identify`, JSON parsing |
| `internal/mux/args.go` | `Track`, `Job`, `Args` |
| `internal/mux/run.go` | `Phase`, `Result`, `Mux`: options file, GUI-mode parsing, exit codes, cancellation |
| `internal/mux/fake_test.go` | Fake mkvmerge (test-binary helper process) shared by mux tests |
| `internal/mount/mount.go` | `ErrUnavailable`, `Mount`, `Attach`, `Detach`, injectable `runner` and `lookPath` |
| `internal/mount/parse.go` | Output parsers for udisksctl and PowerShell; `psQuote` |
| `internal/mount/attach_darwin.go`, `attach_linux.go`, `attach_windows.go`, `attach_other.go` | Per-OS attach and detach |
| `internal/naming/naming.go` | `CleanLabel`, `SafeFileName` |
| `name.go` | `(*Disc).Name()` |
| `internal/testdisc/realclip.go` | `FFmpegClip`, `ClipStartTicks`, `RealMovieDisc`, `RealMovie` (integration media) |
| `rip.go` | `Phase`, `Progress`, `RipOptions`, `RipResult`, `(*Disc).Rip`, mounting glue, `mapTracks`, `audioName` |
| `errors.go` (modify) | `ErrMkvmergeNotFound`, `ErrMkvmergeTooOld`, `ErrMuxFailed`, `ErrMountUnavailable`, `ErrOutputExists` |
| `cmd/zenvik/rip.go` | `rip` command, title selection, output path, hints |
| `cmd/zenvik/progress.go` | TTY progress bar or plain-line progress; `isTerminal`; `shellQuote` |
| `cmd/zenvik/main.go` (modify) | Register `rip`, exit code 4, second-signal exit |
| `.github/workflows/ci.yml` (modify) | Install MKVToolNix and ffmpeg; add a Linux integration job |
| `README.md`, `CLAUDE.md` (modify) | Document `rip`, requirements and mount clean-up |

## Dependency Waves (for parallel execution)

- **A:** Task 1 (mux identify and args), Task 3 (mount), Task 4 (naming), Task 5 (real clips). These are independent.
- **B:** Task 2 (mux run), after Task 1.
- **C:** Task 6 (`Rip`), after Tasks 2, 3, 4 and 5.
- **D:** Task 7 (CLI `rip`), after Task 6.
- **E:** Task 8 (CI, docs, verification).

---
### Task 1: mkvmerge discovery, identification and arguments (`internal/mux`)

**Files:**
- Create: `internal/mux/mkvmerge.go`, `internal/mux/identify.go`, `internal/mux/args.go`, `internal/mux/fake_test.go`
- Test: `internal/mux/mux_test.go`

**Interfaces:**
- Consumes: nothing (standard library only).
- Produces (used by Tasks 2 and 6):
  - `var ErrNotFound, ErrTooOld, ErrFailed error`
  - `var MinVersion = Version{Major: 80}`
  - `type Version struct { Major, Minor, Patch int }` with `String()` and `Less(o Version) bool`
  - `func ParseVersion(out string) (Version, error)`
  - `type Mkvmerge struct { Path string; Version Version; pre []string; env []string }`
  - `func Find(ctx context.Context, path string) (*Mkvmerge, error)`: an empty path means search `PATH`
  - unexported `(m *Mkvmerge) checkVersion(ctx) error`, `(m *Mkvmerge) command(ctx, args...) *exec.Cmd`
  - `type Identification struct { Tracks []IdentifiedTrack; Chapters int }`
  - `type IdentifiedTrack struct { ID int; Type, Codec string; PID uint16; Language string; Channels int }`, where `Type` is `"video"`, `"audio"` or `"subtitles"`
  - `func (m *Mkvmerge) Identify(ctx context.Context, input string) (*Identification, error)`
  - `type Track struct { ID int; Type, Language, Name string; Default bool }`
  - `type Job struct { Input, Output string; Tracks []Track }`
  - `func Args(job Job) []string`: mkvmerge arguments without `--gui-mode`
  - test helpers in `fake_test.go`:
    - `TestHelperProcess`, the fake mkvmerge
    - `fake(mode string, extraEnv ...string) *Mkvmerge`
    - fake modes: `version=<text>`, `identify=<json file>`, `mux=<exit code>`, `hang`
    - setting `ZENVIK_FAKE_ARGS_OUT=<file>` records the received (options-file-expanded) arguments as JSON

- [ ] **Step 1: Write the fake mkvmerge and the failing tests**

`internal/mux/fake_test.go`:
```go
package mux

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is not a real test: when ZENVIK_FAKE_MKVMERGE is set,
// the test binary acts as mkvmerge so the package can be tested without
// MKVToolNix and on every OS.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("ZENVIK_FAKE_MKVMERGE")
	if mode == "" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	os.Exit(fakeMkvmerge(mode, args))
}

func fakeMkvmerge(mode string, args []string) int {
	if len(args) == 1 && strings.HasPrefix(args[0], "@") {
		b, err := os.ReadFile(args[0][1:])
		if err != nil {
			fmt.Println("Error: cannot read the options file")
			return 2
		}
		if err := json.Unmarshal(b, &args); err != nil {
			fmt.Println("Error: the options file is not a JSON array")
			return 2
		}
	}
	if p := os.Getenv("ZENVIK_FAKE_ARGS_OUT"); p != "" {
		b, _ := json.Marshal(args)
		_ = os.WriteFile(p, b, 0o644)
	}
	kind, value, _ := strings.Cut(mode, "=")
	switch kind {
	case "version":
		fmt.Println(value)
		return 0
	case "identify":
		b, err := os.ReadFile(value)
		if err != nil {
			return 2
		}
		_, _ = os.Stdout.Write(b)
		return 0
	case "mux", "hang":
		out := ""
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-o" {
				out = args[i+1]
			}
		}
		fmt.Println("mkvmerge v102.0 ('Little Houses') 64-bit")
		fmt.Println("#GUI#begin_scanning_playlists#num_playlists=1#num_files_in_playlists=2")
		fmt.Println("#GUI#progress 50%")
		fmt.Println("#GUI#end_scanning_playlists")
		_ = os.WriteFile(out, []byte("partial mkv"), 0o644)
		fmt.Println("#GUI#progress 25%")
		if kind == "hang" {
			time.Sleep(time.Minute)
			return 0
		}
		fmt.Println("#GUI#progress 100%")
		code, _ := strconv.Atoi(value)
		switch code {
		case 1:
			fmt.Println("Warning: the playlist has a gap")
			fmt.Println(`#GUI#warning A\swarning\cwith\sescapes`)
		case 2:
			fmt.Println("Error: cannot open the playlist")
		}
		return code
	}
	return 3
}

// fake returns an Mkvmerge that runs the test binary as a fake mkvmerge.
func fake(mode string, extraEnv ...string) *Mkvmerge {
	return &Mkvmerge{
		Path:    os.Args[0],
		Version: Version{Major: 102},
		pre:     []string{"-test.run=^TestHelperProcess$", "--"},
		env:     append([]string{"ZENVIK_FAKE_MKVMERGE=" + mode}, extraEnv...),
	}
}
```

`internal/mux/mux_test.go`:
```go
package mux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want Version
	}{
		{"mkvmerge v102.0 ('Little Houses') 64-bit\n", Version{102, 0, 0}},
		{"mkvmerge v80.0 ('Roundabout') 64-bit", Version{80, 0, 0}},
		{"mkvmerge v9.8.0 ('Kuglblids') 64-bit", Version{9, 8, 0}},
	}
	for _, tt := range tests {
		got, err := ParseVersion(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	if _, err := ParseVersion("ffmpeg version 7.1"); err == nil {
		t.Error("ParseVersion accepted non-mkvmerge output")
	}
	if !(Version{79, 9, 9}).Less(MinVersion) || (Version{80, 0, 0}).Less(MinVersion) || MinVersion.String() != "80.0.0" {
		t.Error("Version comparison or String is wrong")
	}
}

func TestCheckVersion(t *testing.T) {
	ctx := context.Background()
	m := fake("version=mkvmerge v102.0 ('Little Houses') 64-bit")
	m.Version = Version{}
	if err := m.checkVersion(ctx); err != nil || m.Version != (Version{102, 0, 0}) {
		t.Errorf("checkVersion = %v, version %v", err, m.Version)
	}
	old := fake("version=mkvmerge v79.0 ('x') 64-bit")
	if err := old.checkVersion(ctx); !errors.Is(err, ErrTooOld) {
		t.Errorf("old mkvmerge: err = %v, want ErrTooOld", err)
	}
}

func TestFindNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Find(context.Background(), ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty PATH: err = %v, want ErrNotFound", err)
	}
	if _, err := Find(context.Background(), filepath.Join(t.TempDir(), "mkvmerge")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file: err = %v, want ErrNotFound", err)
	}
}

const sampleIdentification = `{"chapters":[{"num_entries":3}],"container":{"properties":{"playlist":true},
"recognized":true,"supported":true,"type":"MPEG transport stream"},"errors":[],"tracks":[
{"codec":"AVC/H.264/MPEG-4p10","id":0,"properties":{"language":"und","number":4113,"stream_id":4113},"type":"video"},
{"codec":"TrueHD Atmos","id":1,"properties":{"audio_channels":8,"language":"eng","number":4352,"stream_id":4352},"type":"audio"},
{"codec":"HDMV PGS","id":2,"properties":{"language":"eng","stream_id":4608},"type":"subtitles"}],"warnings":[]}`

func TestIdentify(t *testing.T) {
	file := filepath.Join(t.TempDir(), "id.json")
	if err := os.WriteFile(file, []byte(sampleIdentification), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fake("identify=" + file).Identify(context.Background(), "/disc/BDMV/PLAYLIST/00800.mpls")
	if err != nil {
		t.Fatal(err)
	}
	want := &Identification{
		Chapters: 3,
		Tracks: []IdentifiedTrack{
			{ID: 0, Type: "video", Codec: "AVC/H.264/MPEG-4p10", PID: 0x1011, Language: "und"},
			{ID: 1, Type: "audio", Codec: "TrueHD Atmos", PID: 0x1100, Language: "eng", Channels: 8},
			{ID: 2, Type: "subtitles", Codec: "HDMV PGS", PID: 0x1200, Language: "eng"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Identify =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseIdentificationErrors(t *testing.T) {
	if _, err := parseIdentification([]byte(`{"errors":["The file could not be opened"],"tracks":[]}`)); !errors.Is(err, ErrFailed) {
		t.Errorf("errors array: err = %v, want ErrFailed", err)
	}
	if _, err := parseIdentification([]byte("not json")); !errors.Is(err, ErrFailed) {
		t.Errorf("bad JSON: err = %v, want ErrFailed", err)
	}
}

func TestArgs(t *testing.T) {
	job := Job{
		Input:  "/d/BDMV/PLAYLIST/00800.mpls",
		Output: "/o/x.mkv.partial",
		Tracks: []Track{
			{ID: 0, Type: "video", Default: true},
			{ID: 1, Type: "audio", Language: "jpn", Name: "AC-3 Stereo", Default: true},
			{ID: 3, Type: "audio", Language: "eng", Name: "TrueHD 7.1"},
			{ID: 2, Type: "subtitles", Language: "eng"},
		},
	}
	want := []string{
		"-o", "/o/x.mkv.partial",
		"--default-track-flag", "0:yes",
		"--language", "1:jpn", "--track-name", "1:AC-3 Stereo", "--default-track-flag", "1:yes",
		"--language", "3:eng", "--track-name", "3:TrueHD 7.1", "--default-track-flag", "3:no",
		"--language", "2:eng", "--default-track-flag", "2:no",
		"--video-tracks", "0", "--audio-tracks", "1,3", "--subtitle-tracks", "2",
		"--track-order", "0:0,0:1,0:3,0:2",
		"/d/BDMV/PLAYLIST/00800.mpls",
	}
	if got := Args(job); !reflect.DeepEqual(got, want) {
		t.Errorf("Args =\n%q\nwant\n%q", got, want)
	}
	noSubs := Args(Job{Input: "in.mpls", Output: "out.mkv", Tracks: []Track{{ID: 0, Type: "video", Default: true}}})
	wantNoSubs := []string{"-o", "out.mkv", "--default-track-flag", "0:yes", "--video-tracks", "0", "--no-audio", "--no-subtitles", "--track-order", "0:0", "in.mpls"}
	if !reflect.DeepEqual(noSubs, wantNoSubs) {
		t.Errorf("Args (video only) = %q", noSubs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mux/`
Expected: FAIL to compile (`undefined: ParseVersion`, `undefined: Mkvmerge`, ...).

- [ ] **Step 3: Implement**

`internal/mux/mkvmerge.go`:
```go
// Package mux runs MKVToolNix's mkvmerge to remux Blu-ray playlists into
// Matroska files.
package mux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

// Errors returned by Find, Identify and Mux.
var (
	ErrNotFound = errors.New("zenvik: mkvmerge not found (install MKVToolNix)")
	ErrTooOld   = errors.New("zenvik: mkvmerge is too old")
	ErrFailed   = errors.New("zenvik: mkvmerge failed")
)

// MinVersion is the oldest supported MKVToolNix release.
var MinVersion = Version{Major: 80}

// Version is an MKVToolNix version number.
type Version struct{ Major, Minor, Patch int }

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less reports whether v is older than o.
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

var versionRE = regexp.MustCompile(`mkvmerge v(\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion extracts the version from `mkvmerge --version` output, such
// as "mkvmerge v102.0 ('Little Houses') 64-bit".
func ParseVersion(out string) (Version, error) {
	m := versionRE.FindStringSubmatch(out)
	if m == nil {
		return Version{}, fmt.Errorf("zenvik: unrecognized mkvmerge version output %q", out)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		v.Patch, _ = strconv.Atoi(m[3])
	}
	return v, nil
}

// Mkvmerge is a located, version-checked mkvmerge executable.
type Mkvmerge struct {
	Path    string
	Version Version
	pre     []string // leading arguments (tests run the test binary as a fake)
	env     []string // extra environment (tests)
}

// Find locates mkvmerge at path, or on PATH when path is empty, and checks
// that it is at least MinVersion.
func Find(ctx context.Context, path string) (*Mkvmerge, error) {
	if path == "" {
		p, err := exec.LookPath("mkvmerge")
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
		}
		path = p
	}
	m := &Mkvmerge{Path: path}
	if err := m.checkVersion(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Mkvmerge) checkVersion(ctx context.Context) error {
	out, err := m.command(ctx, "--version").Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNotFound, m.Path)
		}
		return fmt.Errorf("zenvik: running %s --version: %w", m.Path, err)
	}
	v, err := ParseVersion(string(out))
	if err != nil {
		return err
	}
	m.Version = v
	if v.Less(MinVersion) {
		return fmt.Errorf("%w: found v%s, need v%s or newer", ErrTooOld, v, MinVersion)
	}
	return nil
}

func (m *Mkvmerge) command(ctx context.Context, args ...string) *exec.Cmd {
	all := append(append([]string{}, m.pre...), args...)
	cmd := exec.CommandContext(ctx, m.Path, all...)
	if len(m.env) > 0 {
		cmd.Env = append(os.Environ(), m.env...)
	}
	return cmd
}
```

`internal/mux/identify.go`:
```go
package mux

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Identification is what `mkvmerge -J` reports about an input.
type Identification struct {
	Tracks   []IdentifiedTrack
	Chapters int
}

// IdentifiedTrack is one track mkvmerge found in the input.
type IdentifiedTrack struct {
	ID       int    // mkvmerge's track ID in this input
	Type     string // "video", "audio" or "subtitles"
	Codec    string
	PID      uint16 // transport stream PID
	Language string
	Channels int // audio only
}

// Identify runs `mkvmerge -J input`.
func (m *Mkvmerge) Identify(ctx context.Context, input string) (*Identification, error) {
	out, err := m.command(ctx, "-J", input).Output()
	if err != nil {
		return nil, fmt.Errorf("%w: identifying %s: %w", ErrFailed, input, err)
	}
	return parseIdentification(out)
}

func parseIdentification(b []byte) (*Identification, error) {
	var raw struct {
		Errors []string `json:"errors"`
		Tracks []struct {
			ID         int    `json:"id"`
			Type       string `json:"type"`
			Codec      string `json:"codec"`
			Properties struct {
				Number        uint64 `json:"number"`
				StreamID      uint64 `json:"stream_id"`
				Language      string `json:"language"`
				AudioChannels int    `json:"audio_channels"`
			} `json:"properties"`
		} `json:"tracks"`
		Chapters []struct {
			NumEntries int `json:"num_entries"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%w: unreadable identification output: %w", ErrFailed, err)
	}
	if len(raw.Errors) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrFailed, strings.Join(raw.Errors, "; "))
	}
	id := &Identification{}
	for _, t := range raw.Tracks {
		pid := t.Properties.Number
		if pid == 0 {
			pid = t.Properties.StreamID
		}
		id.Tracks = append(id.Tracks, IdentifiedTrack{
			ID: t.ID, Type: t.Type, Codec: t.Codec, PID: uint16(pid),
			Language: t.Properties.Language, Channels: t.Properties.AudioChannels,
		})
	}
	for _, c := range raw.Chapters {
		id.Chapters += c.NumEntries
	}
	return id, nil
}
```

`internal/mux/args.go`:
```go
package mux

import (
	"strconv"
	"strings"
)

// Track is one output track of a mux job.
type Track struct {
	ID       int    // mkvmerge track ID in the input
	Type     string // "video", "audio" or "subtitles"
	Language string // ISO 639-2; empty keeps mkvmerge's value
	Name     string // empty for no name
	Default  bool
}

// Job describes one remux.
type Job struct {
	Input  string  // playlist (.mpls) path
	Output string  // output file path
	Tracks []Track // output tracks, in output order
}

// Args returns mkvmerge's arguments for job (without --gui-mode): output,
// per-track options, the track selection, the track order, then the input.
func Args(job Job) []string {
	args := []string{"-o", job.Output}
	var video, audio, subs, order []string
	for _, t := range job.Tracks {
		id := strconv.Itoa(t.ID)
		switch t.Type {
		case "video":
			video = append(video, id)
		case "audio":
			audio = append(audio, id)
		case "subtitles":
			subs = append(subs, id)
		}
		if t.Language != "" {
			args = append(args, "--language", id+":"+t.Language)
		}
		if t.Name != "" {
			args = append(args, "--track-name", id+":"+t.Name)
		}
		flag := "no"
		if t.Default {
			flag = "yes"
		}
		args = append(args, "--default-track-flag", id+":"+flag)
		order = append(order, "0:"+id)
	}
	args = append(args, selection("--video-tracks", "--no-video", video)...)
	args = append(args, selection("--audio-tracks", "--no-audio", audio)...)
	args = append(args, selection("--subtitle-tracks", "--no-subtitles", subs)...)
	if len(order) > 0 {
		args = append(args, "--track-order", strings.Join(order, ","))
	}
	return append(args, job.Input)
}

func selection(flag, none string, ids []string) []string {
	if len(ids) == 0 {
		return []string{none}
	}
	return []string{flag, strings.Join(ids, ",")}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/mux/ && go vet ./... && golangci-lint run`
Expected: PASS; 0 lint issues. (`fake_test.go`'s `mux` and `hang` modes aren't used until Task 2. Test-only code is fine.)

- [ ] **Step 5: Commit**

```bash
git add internal/mux
git commit -m "Add mkvmerge discovery, identification and argument building"
```

---

### Task 2: Running mkvmerge (`internal/mux`)

**Files:**
- Create: `internal/mux/run.go`
- Test: `internal/mux/run_test.go`

**Interfaces:**
- Consumes (Task 1): `Mkvmerge`, `(*Mkvmerge).command`, `Job`, `Args`, `ErrFailed`, and the test helper `fake`.
- Produces (used by Task 6):
  - `type Phase int`; constants `PhaseScanning Phase = 1`, `PhaseMuxing Phase = 2`
  - `type Result struct { Warnings []string; Args []string }`
  - `func (m *Mkvmerge) Mux(ctx context.Context, job Job, onProgress func(Phase, float64)) (*Result, error)`
- Behavior:
  - Writes `["--gui-mode", Args(job)...]` to a temp JSON options file and runs `mkvmerge @file`. The options file is always removed afterwards.
  - **Progress:**
    - `#GUI#begin_scanning_playlists` switches to `PhaseScanning` and reports 0.
    - `#GUI#end_scanning_playlists` switches to `PhaseMuxing` and reports 0.
    - `#GUI#progress N%` reports N/100 in the current phase.
    - Before any scanning line, the phase is `PhaseMuxing`.
  - **Messages:**
    - `Warning: X` and `#GUI#warning X` lines are warnings.
    - `Error: X` and `#GUI#error X` lines are errors.
    - `#GUI#` message text is unescaped: `\s`→space, `\2`→`"`, `\c`→`:`, `\h`→`#`, `\b`→`[`, `\B`→`]`, `\\`→`\`.
  - **Outcome:**
    - Context cancelled: the process is killed, `job.Output` is removed, and `ctx.Err()` is returned.
    - Exit 0 or 1: a Result with warnings and a nil error.
    - Anything else: `job.Output` is removed, and the error wraps `ErrFailed` with the exit status and the error lines (or stderr if there are none).

- [ ] **Step 1: Write the failing tests**

`internal/mux/run_test.go`:
```go
package mux

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type event struct {
	Phase    Phase
	Fraction float64
}

func job(t *testing.T, out string) Job {
	t.Helper()
	return Job{Input: "/disc/BDMV/PLAYLIST/00800.mpls", Output: out, Tracks: []Track{{ID: 0, Type: "video", Default: true}}}
}

func TestMuxSuccess(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "movie.mkv.partial")
	argsFile := filepath.Join(dir, "args.json")
	var events []event
	res, err := fake("mux=0", "ZENVIK_FAKE_ARGS_OUT="+argsFile).Mux(context.Background(), job(t, out),
		func(p Phase, f float64) { events = append(events, event{p, f}) })
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %q", res.Warnings)
	}
	want := []event{{PhaseScanning, 0}, {PhaseScanning, 0.5}, {PhaseMuxing, 0}, {PhaseMuxing, 0.25}, {PhaseMuxing, 1}}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output missing: %v", err)
	}
	var got []string
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if wantArgs := append([]string{"--gui-mode"}, Args(job(t, out))...); !reflect.DeepEqual(got, wantArgs) || !reflect.DeepEqual(res.Args, wantArgs) {
		t.Errorf("mkvmerge received %q (Result.Args %q), want %q", got, res.Args, wantArgs)
	}
}

func TestMuxWarnings(t *testing.T) {
	out := filepath.Join(t.TempDir(), "movie.mkv.partial")
	res, err := fake("mux=1").Mux(context.Background(), job(t, out), nil)
	if err != nil {
		t.Fatalf("exit 1 should succeed: %v", err)
	}
	if want := []string{"the playlist has a gap", "A warning:with escapes"}; !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("warnings = %q, want %q", res.Warnings, want)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output should be kept on warnings: %v", err)
	}
}

func TestMuxFailure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "movie.mkv.partial")
	_, err := fake("mux=2").Mux(context.Background(), job(t, out), nil)
	if !errors.Is(err, ErrFailed) || !strings.Contains(err.Error(), "cannot open the playlist") || !strings.Contains(err.Error(), "exit status 2") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("output should be removed on failure: %v", err)
	}
}

func TestMuxCanceled(t *testing.T) {
	out := filepath.Join(t.TempDir(), "movie.mkv.partial")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := time.Now()
	_, err := fake("hang").Mux(ctx, job(t, out), func(p Phase, f float64) {
		if p == PhaseMuxing && f > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if time.Since(start) > 20*time.Second {
		t.Errorf("cancellation took %v", time.Since(start))
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("output should be removed on cancel: %v", err)
	}
}

func TestMuxOptionsFilePreservesPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out dir", "ü")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, `Réal "Movie" 'x'.mkv.partial`)
	argsFile := filepath.Join(t.TempDir(), "args.json")
	if _, err := fake("mux=0", "ZENVIK_FAKE_ARGS_OUT="+argsFile).Mux(context.Background(), job(t, out), nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 || got[1] != "-o" || got[2] != out {
		t.Errorf("args = %q, want -o %q", got, out)
	}
}

func TestMuxRemovesOptionsFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	out := filepath.Join(t.TempDir(), "movie.mkv.partial")
	if _, err := fake("mux=0").Mux(context.Background(), job(t, out), nil); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
		t.Errorf("temp dir not cleaned: %v", ents)
	}
}

func TestUnescapeGUI(t *testing.T) {
	if got := unescapeGUI(`a\sb\2c\2\cd\he\bf\Bg\\s`); got != `a b"c":d#e[f]g\s` {
		t.Errorf("unescapeGUI = %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mux/ -run 'Mux|Unescape'`
Expected: FAIL to compile (`undefined: Phase`, `m.Mux undefined`, `undefined: unescapeGUI`).

- [ ] **Step 3: Implement**

`internal/mux/run.go`:
```go
package mux

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Phase is the stage mkvmerge reports progress for.
type Phase int

// Phases reported by Mux.
const (
	PhaseScanning Phase = 1 // scanning the playlist's files
	PhaseMuxing   Phase = 2 // writing the output
)

// Result is the outcome of a successful mux.
type Result struct {
	Warnings []string
	Args     []string // the arguments mkvmerge received
}

var guiUnescaper = strings.NewReplacer(`\s`, " ", `\2`, `"`, `\c`, ":", `\h`, "#", `\b`, "[", `\B`, "]", `\\`, `\`)

// unescapeGUI decodes the escaping mkvmerge applies to --gui-mode messages.
func unescapeGUI(s string) string { return guiUnescaper.Replace(s) }

// Mux runs mkvmerge for job. onProgress, if non-nil, receives each progress
// update. Exit status 1 (warnings) succeeds with the warnings in the
// Result. On failure or cancellation, job.Output is removed.
func (m *Mkvmerge) Mux(ctx context.Context, job Job, onProgress func(Phase, float64)) (*Result, error) {
	args := append([]string{"--gui-mode"}, Args(job)...)
	opts, err := writeOptions(args)
	if err != nil {
		return nil, err
	}
	defer os.Remove(opts)

	cmd := m.command(ctx, "@"+opts)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: starting %s: %w", ErrFailed, m.Path, err)
	}

	phase := PhaseMuxing
	report := func(f float64) {
		if onProgress != nil {
			onProgress(phase, f)
		}
	}
	var warnings, errs []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case strings.HasPrefix(line, "#GUI#begin_scanning_playlists"):
			phase = PhaseScanning
			report(0)
		case strings.HasPrefix(line, "#GUI#end_scanning_playlists"):
			phase = PhaseMuxing
			report(0)
		case strings.HasPrefix(line, "#GUI#progress "):
			if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, "#GUI#progress "), "%")); err == nil {
				report(float64(n) / 100)
			}
		case strings.HasPrefix(line, "#GUI#warning "):
			warnings = append(warnings, unescapeGUI(strings.TrimPrefix(line, "#GUI#warning ")))
		case strings.HasPrefix(line, "Warning: "):
			warnings = append(warnings, strings.TrimPrefix(line, "Warning: "))
		case strings.HasPrefix(line, "#GUI#error "):
			errs = append(errs, unescapeGUI(strings.TrimPrefix(line, "#GUI#error ")))
		case strings.HasPrefix(line, "Error: "):
			errs = append(errs, strings.TrimPrefix(line, "Error: "))
		}
	}
	waitErr := cmd.Wait()

	if ctx.Err() != nil {
		os.Remove(job.Output)
		return nil, ctx.Err()
	}
	code := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if !errors.As(waitErr, &ee) {
			os.Remove(job.Output)
			return nil, fmt.Errorf("%w: %w", ErrFailed, waitErr)
		}
		code = ee.ExitCode()
	}
	if code == 0 || code == 1 {
		return &Result{Warnings: warnings, Args: args}, nil
	}
	os.Remove(job.Output)
	detail := strings.Join(errs, "; ")
	if detail == "" {
		detail = strings.TrimSpace(stderr.String())
	}
	return nil, fmt.Errorf("%w (exit status %d): %s", ErrFailed, code, detail)
}

// writeOptions writes args to a temporary mkvmerge options file.
func writeOptions(args []string) (string, error) {
	f, err := os.CreateTemp("", "zenvik-mkvmerge-*.json")
	if err != nil {
		return "", err
	}
	if err := json.NewEncoder(f).Encode(args); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/mux/ && go vet ./... && golangci-lint run`
Expected: PASS; 0 lint issues.

- [ ] **Step 5: Commit**

```bash
git add internal/mux
git commit -m "Run mkvmerge with progress, warnings, errors and cancellation"
```

---

### Task 3: Read-only image mounting (`internal/mount`)

**Files:**
- Create: `internal/mount/mount.go`, `internal/mount/parse.go`, `internal/mount/attach_darwin.go`, `internal/mount/attach_linux.go`, `internal/mount/attach_windows.go`, `internal/mount/attach_other.go`
- Test: `internal/mount/parse_test.go`, `internal/mount/attach_darwin_test.go`, `internal/mount/attach_linux_test.go`, `internal/mount/attach_windows_test.go`, `internal/mount/integration_darwin_test.go`

**Interfaces:**
- Consumes: `testdisc.SampleMovie`, `(*testdisc.Disc).ISO`, `udfimage.Options` (darwin integration test only).
- Produces (used by Task 6):
  - `var ErrUnavailable = errors.New("zenvik: cannot mount disc images on this system")`
  - `type Mount struct { Dir string }` (plus an unexported detach function)
  - `func Attach(ctx context.Context, image string) (*Mount, error)`
  - `func (m *Mount) Detach(ctx context.Context) error`: ignores cancellation of ctx, times out after one minute, and is safe to call twice
  - unexported test hooks: `var runner func(ctx context.Context, name string, args ...string) ([]byte, error)` and `var lookPath func(string) (string, error)`
- Per-OS commands (Design Decision 7):
  - **darwin** attaches with `hdiutil attach -readonly -nobrowse -noautoopen -noverify -imagekey diskimage-class=CRawDiskImage -mountpoint <tempdir> <image>`. The temp dir is `os.MkdirTemp("", "zenvik-mount-")`. Detach runs `hdiutil detach <dir>`, then `hdiutil detach -force <dir>` if that fails, then removes the dir.
  - **linux** attaches with `udisksctl loop-setup --no-user-interaction -r -f <image>` and parses `Mapped file … as /dev/loopN.`, then runs `udisksctl mount --no-user-interaction -b /dev/loopN` and parses `Mounted /dev/loopN at <dir>`. If mount fails with `AlreadyMounted`, the mount point comes from `udisksctl info -b /dev/loopN` (`MountPoints:` line). Detach runs `udisksctl unmount … -b` then `udisksctl loop-delete … -b`.
  - **windows** runs `powershell.exe -NoProfile -NonInteractive -Command "$ErrorActionPreference='Stop'; (Mount-DiskImage -ImagePath '<image>' -Access ReadOnly -PassThru | Get-Volume).DriveLetter"`. The drive letter `E` becomes `E:\`. Detach runs `Dismount-DiskImage -ImagePath '<image>'`.
  - **other OSes** return `ErrUnavailable`.
  - **A missing tool** returns `ErrUnavailable`. So does a failing attach command, with the command's output in the message.

- [ ] **Step 1: Write the failing tests**

`internal/mount/parse_test.go`:
```go
package mount

import "testing"

func TestParseUdisksLoop(t *testing.T) {
	for in, want := range map[string]string{
		"Mapped file /tmp/a b.iso as /dev/loop7.\n": "/dev/loop7",
		"Mapped file x.iso as /dev/loop12\n":        "/dev/loop12",
	} {
		if got, err := parseUdisksLoop(in); err != nil || got != want {
			t.Errorf("parseUdisksLoop(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseUdisksLoop("Error setting up loop device"); err == nil {
		t.Error("expected an error for unrecognized output")
	}
}

func TestParseUdisksMount(t *testing.T) {
	for in, want := range map[string]string{
		"Mounted /dev/loop7 at /media/chad/MY DISC\n":  "/media/chad/MY DISC",
		"Mounted /dev/loop7 at /run/media/c/DISC.\n":   "/run/media/c/DISC",
	} {
		if got, err := parseUdisksMount(in); err != nil || got != want {
			t.Errorf("parseUdisksMount(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseUdisksMount("nothing"); err == nil {
		t.Error("expected an error for unrecognized output")
	}
}

func TestParseUdisksInfoMountPoint(t *testing.T) {
	out := "/org/freedesktop/UDisks2/block_devices/loop7:\n  org.freedesktop.UDisks2.Filesystem:\n    MountPoints:        /media/chad/MY DISC\n    Size:               123\n"
	if got, err := parseUdisksInfoMountPoint(out); err != nil || got != "/media/chad/MY DISC" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := parseUdisksInfoMountPoint("    MountPoints:\n"); err == nil {
		t.Error("expected an error when not mounted")
	}
}

func TestParseDriveLetter(t *testing.T) {
	if got, err := parseDriveLetter("e\r\n"); err != nil || got != `E:\` {
		t.Errorf("got %q, %v", got, err)
	}
	for _, bad := range []string{"", "EF", "1"} {
		if _, err := parseDriveLetter(bad); err == nil {
			t.Errorf("parseDriveLetter(%q) should fail", bad)
		}
	}
}

func TestPSQuote(t *testing.T) {
	if got := psQuote(`C:\Movies\Bob's "Disc".iso`); got != `'C:\Movies\Bob''s "Disc".iso'` {
		t.Errorf("psQuote = %s", got)
	}
}
```

`internal/mount/attach_darwin_test.go`:
```go
//go:build darwin

package mount

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

func fakeRunner(t *testing.T, results ...error) *[]call {
	t.Helper()
	var calls []call
	oldRun, oldLook := runner, lookPath
	t.Cleanup(func() { runner, lookPath = oldRun, oldLook })
	lookPath = func(string) (string, error) { return "/usr/bin/hdiutil", nil }
	i := 0
	runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, call{name, args})
		var err error
		if i < len(results) {
			err = results[i]
		}
		i++
		if err != nil {
			return []byte("hdiutil: failed"), err
		}
		return nil, nil
	}
	return &calls
}

func TestAttachDarwin(t *testing.T) {
	calls := fakeRunner(t)
	m, err := Attach(context.Background(), "/images/my disc.iso")
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(m.Dir); err != nil || !st.IsDir() || !strings.Contains(m.Dir, "zenvik-mount-") {
		t.Fatalf("mount dir %q: %v", m.Dir, err)
	}
	attach := (*calls)[0]
	want := []string{"attach", "-readonly", "-nobrowse", "-noautoopen", "-noverify", "-imagekey", "diskimage-class=CRawDiskImage", "-mountpoint", m.Dir, "/images/my disc.iso"}
	if attach.name != "hdiutil" || strings.Join(attach.args, "|") != strings.Join(want, "|") {
		t.Errorf("attach call = %v", attach)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // detaching must still work after cancellation
	if err := m.Detach(ctx); err != nil {
		t.Fatal(err)
	}
	if d := (*calls)[1]; d.name != "hdiutil" || strings.Join(d.args, " ") != "detach "+m.Dir {
		t.Errorf("detach call = %v", d)
	}
	if _, err := os.Stat(m.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("mount dir not removed: %v", err)
	}
	if err := m.Detach(context.Background()); err != nil || len(*calls) != 2 {
		t.Errorf("second Detach: %v, calls %d", err, len(*calls))
	}
}

func TestDetachDarwinForces(t *testing.T) {
	calls := fakeRunner(t, nil, errors.New("busy"))
	m, err := Attach(context.Background(), "/images/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := (*calls)[2]; strings.Join(d.args, " ") != "detach -force "+m.Dir {
		t.Errorf("force detach call = %v", d)
	}
}

func TestAttachDarwinFailure(t *testing.T) {
	fakeRunner(t, errors.New("exit status 1"))
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	_, err := Attach(context.Background(), "/images/x.iso")
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "hdiutil: failed") {
		t.Errorf("err = %v", err)
	}
	if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
		t.Errorf("temp mount dir left behind after a failed attach: %v", ents)
	}
}

func TestAttachDarwinNoTool(t *testing.T) {
	fakeRunner(t)
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Attach(context.Background(), "/images/x.iso"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
}
```

`internal/mount/attach_linux_test.go`:
```go
//go:build linux

package mount

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type step struct {
	out string
	err error
}

func fakeRunner(t *testing.T, steps ...step) *[]string {
	t.Helper()
	var calls []string
	oldRun, oldLook := runner, lookPath
	t.Cleanup(func() { runner, lookPath = oldRun, oldLook })
	lookPath = func(string) (string, error) { return "/usr/bin/udisksctl", nil }
	i := 0
	runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		s := step{}
		if i < len(steps) {
			s = steps[i]
		}
		i++
		return []byte(s.out), s.err
	}
	return &calls
}

func TestAttachLinux(t *testing.T) {
	calls := fakeRunner(t,
		step{out: "Mapped file /i/x.iso as /dev/loop9.\n"},
		step{out: "Mounted /dev/loop9 at /media/u/DISC\n"},
		step{}, step{})
	m, err := Attach(context.Background(), "/i/x.iso")
	if err != nil {
		t.Fatal(err)
	}
	if m.Dir != "/media/u/DISC" {
		t.Errorf("Dir = %q", m.Dir)
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"udisksctl loop-setup --no-user-interaction -r -f /i/x.iso",
		"udisksctl mount --no-user-interaction -b /dev/loop9",
		"udisksctl unmount --no-user-interaction -b /dev/loop9",
		"udisksctl loop-delete --no-user-interaction -b /dev/loop9",
	}
	if strings.Join(*calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls =\n%s", strings.Join(*calls, "\n"))
	}
}

func TestAttachLinuxAlreadyMounted(t *testing.T) {
	fakeRunner(t,
		step{out: "Mapped file /i/x.iso as /dev/loop9.\n"},
		step{out: "Error mounting /dev/loop9: GDBus.Error:org.freedesktop.UDisks2.Error.AlreadyMounted: Device /dev/loop9 is already mounted\n", err: errors.New("exit status 1")},
		step{out: "    MountPoints:        /media/u/AUTO\n"})
	m, err := Attach(context.Background(), "/i/x.iso")
	if err != nil || m.Dir != "/media/u/AUTO" {
		t.Errorf("Attach = %+v, %v", m, err)
	}
}

func TestAttachLinuxFailures(t *testing.T) {
	calls := fakeRunner(t,
		step{out: "Mapped file /i/x.iso as /dev/loop9.\n"},
		step{out: "Not authorized to perform operation", err: errors.New("exit status 1")},
		step{})
	if _, err := Attach(context.Background(), "/i/x.iso"); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "Not authorized") {
		t.Errorf("err = %v", err)
	}
	if last := (*calls)[len(*calls)-1]; last != "udisksctl loop-delete --no-user-interaction -b /dev/loop9" {
		t.Errorf("loop device not cleaned up; last call %q", last)
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Attach(context.Background(), "/i/x.iso"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no udisksctl: err = %v", err)
	}
}
```

`internal/mount/attach_windows_test.go`:
```go
//go:build windows

package mount

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAttachWindows(t *testing.T) {
	var calls []string
	oldRun, oldLook := runner, lookPath
	t.Cleanup(func() { runner, lookPath = oldRun, oldLook })
	lookPath = func(string) (string, error) { return `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, nil }
	outputs := []string{"F\r\n", ""}
	runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		out := outputs[0]
		outputs = outputs[1:]
		return []byte(out), nil
	}
	m, err := Attach(context.Background(), `C:\Movies\Bob's.iso`)
	if err != nil || m.Dir != `F:\` {
		t.Fatalf("Attach = %+v, %v", m, err)
	}
	if !strings.Contains(calls[0], `Mount-DiskImage -ImagePath 'C:\Movies\Bob''s.iso' -Access ReadOnly -PassThru`) {
		t.Errorf("mount call = %s", calls[0])
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(calls[1], `Dismount-DiskImage -ImagePath 'C:\Movies\Bob''s.iso'`) {
		t.Errorf("dismount call = %s", calls[1])
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := Attach(context.Background(), `C:\x.iso`); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no powershell: err = %v", err)
	}
}
```

`internal/mount/integration_darwin_test.go`:
```go
//go:build integration && darwin

package mount

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func TestAttachRealImage(t *testing.T) {
	img, err := testdisc.SampleMovie().ISO(udfimage.Options{Revision: 0x0250, Label: "SAMPLE_MOVIE"})
	if err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(t.TempDir(), "sample disc.iso")
	if err := os.WriteFile(iso, img, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Attach(context.Background(), iso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "BDMV", "index.bdmv")); err != nil {
		t.Errorf("index.bdmv not visible at %s: %v", m.Dir, err)
	}
	if err := m.Detach(context.Background()); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("hdiutil", "info").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), iso) {
		t.Errorf("image still attached:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mount/`
Expected: FAIL to compile (`undefined: parseUdisksLoop`, `undefined: Attach`, ...).

- [ ] **Step 3: Implement**

`internal/mount/mount.go`:
```go
// Package mount attaches disc images read-only so external programs (such
// as mkvmerge) can read their files.
package mount

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// ErrUnavailable reports that images cannot be mounted here: the OS tool
// is missing, not permitted, or failed.
var ErrUnavailable = errors.New("zenvik: cannot mount disc images on this system")

const detachTimeout = time.Minute

// Mount is an attached image.
type Mount struct {
	Dir    string // directory where the image's root is visible
	detach func(ctx context.Context) error
}

// Attach mounts image read-only.
func Attach(ctx context.Context, image string) (*Mount, error) { return attach(ctx, image) }

// Detach unmounts the image. It ignores cancellation of ctx so cleanup
// still runs after Ctrl-C, gives up after one minute, and is safe to call
// more than once.
func (m *Mount) Detach(ctx context.Context) error {
	if m.detach == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachTimeout)
	defer cancel()
	err := m.detach(ctx)
	m.detach = nil
	return err
}

// runner runs a command and returns its combined output; tests replace it.
var runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// lookPath finds a tool on PATH; tests replace it.
var lookPath = exec.LookPath
```

`internal/mount/parse.go`:
```go
package mount

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	udisksLoopRE  = regexp.MustCompile(`(?m)Mapped file .* as (/dev/\S+?)\.?\s*$`)
	udisksMountRE = regexp.MustCompile(`(?m)Mounted /dev/\S+ at (.+?)\.?\s*$`)
	udisksInfoRE  = regexp.MustCompile(`(?m)^\s*MountPoints:[ \t]*(\S.*?)\s*$`)
)

func parseUdisksLoop(out string) (string, error) {
	m := udisksLoopRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("unrecognized udisksctl loop-setup output: %q", strings.TrimSpace(out))
	}
	return m[1], nil
}

func parseUdisksMount(out string) (string, error) {
	m := udisksMountRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("unrecognized udisksctl mount output: %q", strings.TrimSpace(out))
	}
	return m[1], nil
}

func parseUdisksInfoMountPoint(out string) (string, error) {
	m := udisksInfoRE.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("udisksctl info shows no mount point")
	}
	return m[1], nil
}

// parseDriveLetter turns PowerShell's DriveLetter output into a root path.
func parseDriveLetter(out string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(out))
	if len(s) != 1 || s[0] < 'A' || s[0] > 'Z' {
		return "", fmt.Errorf("unexpected drive letter %q", strings.TrimSpace(out))
	}
	return s + `:\`, nil
}

// psQuote quotes s as a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
```

`internal/mount/attach_darwin.go`:
```go
//go:build darwin

package mount

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func attach(ctx context.Context, image string) (*Mount, error) {
	if _, err := lookPath("hdiutil"); err != nil {
		return nil, fmt.Errorf("%w: hdiutil not found", ErrUnavailable)
	}
	dir, err := os.MkdirTemp("", "zenvik-mount-")
	if err != nil {
		return nil, err
	}
	out, err := runner(ctx, "hdiutil", "attach", "-readonly", "-nobrowse", "-noautoopen", "-noverify",
		"-imagekey", "diskimage-class=CRawDiskImage", "-mountpoint", dir, image)
	if err != nil {
		os.Remove(dir)
		return nil, fmt.Errorf("%w: hdiutil attach %s: %v: %s", ErrUnavailable, image, err, strings.TrimSpace(string(out)))
	}
	return &Mount{Dir: dir, detach: func(ctx context.Context) error {
		out, err := runner(ctx, "hdiutil", "detach", dir)
		if err != nil {
			out, err = runner(ctx, "hdiutil", "detach", "-force", dir)
		}
		if err != nil {
			return fmt.Errorf("zenvik: hdiutil detach %s: %v: %s (run `hdiutil detach -force %s`)", dir, err, strings.TrimSpace(string(out)), dir)
		}
		return os.Remove(dir)
	}}, nil
}
```

`internal/mount/attach_linux.go`:
```go
//go:build linux

package mount

import (
	"context"
	"fmt"
	"strings"
)

func attach(ctx context.Context, image string) (*Mount, error) {
	if _, err := lookPath("udisksctl"); err != nil {
		return nil, fmt.Errorf("%w: udisksctl not found (install udisks2)", ErrUnavailable)
	}
	out, err := runner(ctx, "udisksctl", "loop-setup", "--no-user-interaction", "-r", "-f", image)
	if err != nil {
		return nil, fmt.Errorf("%w: udisksctl loop-setup: %v: %s", ErrUnavailable, err, strings.TrimSpace(string(out)))
	}
	dev, err := parseUdisksLoop(string(out))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	cleanup := func() {
		_, _ = runner(context.WithoutCancel(ctx), "udisksctl", "loop-delete", "--no-user-interaction", "-b", dev)
	}
	dir, err := mountLoop(ctx, dev)
	if err != nil {
		cleanup()
		return nil, err
	}
	return &Mount{Dir: dir, detach: func(ctx context.Context) error {
		if out, err := runner(ctx, "udisksctl", "unmount", "--no-user-interaction", "-b", dev); err != nil {
			return fmt.Errorf("zenvik: udisksctl unmount %s: %v: %s (run `udisksctl unmount -b %s && udisksctl loop-delete -b %s`)",
				dev, err, strings.TrimSpace(string(out)), dev, dev)
		}
		if out, err := runner(ctx, "udisksctl", "loop-delete", "--no-user-interaction", "-b", dev); err != nil {
			return fmt.Errorf("zenvik: udisksctl loop-delete %s: %v: %s", dev, err, strings.TrimSpace(string(out)))
		}
		return nil
	}}, nil
}

// mountLoop mounts dev, or finds where the desktop already auto-mounted it.
func mountLoop(ctx context.Context, dev string) (string, error) {
	out, err := runner(ctx, "udisksctl", "mount", "--no-user-interaction", "-b", dev)
	if err == nil {
		dir, perr := parseUdisksMount(string(out))
		if perr != nil {
			return "", fmt.Errorf("%w: %v", ErrUnavailable, perr)
		}
		return dir, nil
	}
	if strings.Contains(string(out), "AlreadyMounted") {
		info, ierr := runner(ctx, "udisksctl", "info", "-b", dev)
		if ierr == nil {
			if dir, perr := parseUdisksInfoMountPoint(string(info)); perr == nil {
				return dir, nil
			}
		}
	}
	return "", fmt.Errorf("%w: udisksctl mount %s: %v: %s", ErrUnavailable, dev, err, strings.TrimSpace(string(out)))
}
```

`internal/mount/attach_windows.go`:
```go
//go:build windows

package mount

import (
	"context"
	"fmt"
	"strings"
)

func attach(ctx context.Context, image string) (*Mount, error) {
	ps, err := lookPath("powershell.exe")
	if err != nil {
		return nil, fmt.Errorf("%w: powershell.exe not found", ErrUnavailable)
	}
	run := func(ctx context.Context, script string) ([]byte, error) {
		return runner(ctx, ps, "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; "+script)
	}
	out, err := run(ctx, fmt.Sprintf("(Mount-DiskImage -ImagePath %s -Access ReadOnly -PassThru | Get-Volume).DriveLetter", psQuote(image)))
	dismount := func(ctx context.Context) ([]byte, error) {
		return run(ctx, fmt.Sprintf("Dismount-DiskImage -ImagePath %s | Out-Null", psQuote(image)))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: Mount-DiskImage %s: %v: %s", ErrUnavailable, image, err, strings.TrimSpace(string(out)))
	}
	dir, err := parseDriveLetter(string(out))
	if err != nil {
		_, _ = dismount(context.WithoutCancel(ctx))
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return &Mount{Dir: dir, detach: func(ctx context.Context) error {
		if out, err := dismount(ctx); err != nil {
			return fmt.Errorf("zenvik: Dismount-DiskImage %s: %v: %s", image, err, strings.TrimSpace(string(out)))
		}
		return nil
	}}, nil
}
```

`internal/mount/attach_other.go`:
```go
//go:build !darwin && !linux && !windows

package mount

import (
	"context"
	"fmt"
	"runtime"
)

func attach(ctx context.Context, image string) (*Mount, error) {
	return nil, fmt.Errorf("%w: no image mounting support on %s", ErrUnavailable, runtime.GOOS)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test -race ./internal/mount/ && go test -tags integration -run TestAttachRealImage -v ./internal/mount/
GOOS=linux go vet ./internal/mount/ && GOOS=windows go vet ./internal/mount/ && GOOS=freebsd go vet ./internal/mount/
go vet ./... && golangci-lint run
```
Expected: PASS on macOS, including the real `hdiutil` round trip. The cross-OS `go vet` runs compile the Linux, Windows and other variants. (Their unit tests run in CI's Linux and Windows jobs.)

- [ ] **Step 5: Commit**

```bash
git add internal/mount
git commit -m "Add read-only disc image mounting for macOS, Linux and Windows"
```

---

### Task 4: Disc name and safe file names

**Files:**
- Create: `internal/naming/naming.go`, `name.go`
- Test: `internal/naming/naming_test.go`, `name_test.go`

**Interfaces:**
- Consumes: `zenvik.Disc` (with its `Meta` and `Label` fields), `testdisc.SampleMovie` (tests).
- Produces (used by Task 7; M4 builds templates on them):
  - `func naming.CleanLabel(label string) string`
  - `func naming.SafeFileName(s string) string`
  - `func (d *Disc) Name() string`: the bdmt title if non-blank, else `CleanLabel(Label)`, else `"untitled"`

- [ ] **Step 1: Write the failing tests**

`internal/naming/naming_test.go`:
```go
package naming

import "testing"

func TestCleanLabel(t *testing.T) {
	tests := map[string]string{
		"THE_MATRIX":      "The Matrix",
		"THE_MATRIX_1999": "The Matrix 1999",
		"  big  buck  ":   "Big Buck",
		"ÉCOLE_DES_FANS":  "École Des Fans",
		"":                "",
		"___":             "",
	}
	for in, want := range tests {
		if got := CleanLabel(in); got != want {
			t.Errorf("CleanLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSafeFileName(t *testing.T) {
	tests := map[string]string{
		"Sample Movie":             "Sample Movie",
		`AC/DC: Live <at> "Wembley"`: "AC_DC_ Live _at_ _Wembley_",
		"a\\b|c?d*e":               "a_b_c_d_e",
		"tab\there":                "tab_here",
		"trailing dots...":         "trailing dots",
		"  spaced  ":               "spaced",
		"":                         "untitled",
		"...":                      "untitled",
		"Amélie":                   "Amélie",
	}
	for in, want := range tests {
		if got := SafeFileName(in); got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}
```

`name_test.go`:
```go
package zenvik_test

import (
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestDiscName(t *testing.T) {
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	if got := d.Name(); got != "Sample Movie" {
		t.Errorf("Name with meta = %q", got)
	}

	noMeta := testdisc.SampleMovie()
	noMeta.MetaTitle = ""
	root := filepath.Join(t.TempDir(), "THE_MATRIX_1999")
	if err := noMeta.WriteDir(root); err != nil {
		t.Fatal(err)
	}
	if got := openDisc(t, root).Name(); got != "The Matrix 1999" {
		t.Errorf("Name from label = %q", got)
	}

	blank := filepath.Join(t.TempDir(), "___")
	if err := noMeta.WriteDir(blank); err != nil {
		t.Fatal(err)
	}
	if got := openDisc(t, blank).Name(); got != "untitled" {
		t.Errorf("Name with blank label = %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/naming/ . -run 'CleanLabel|SafeFileName|DiscName'`
Expected: FAIL to compile (`undefined: CleanLabel`, `d.Name undefined`).

- [ ] **Step 3: Implement**

`internal/naming/naming.go`:
```go
// Package naming builds human-readable names and safe file names.
package naming

import (
	"strings"
	"unicode"
)

// CleanLabel turns a volume label into a readable name: underscores become
// spaces and each word is title-cased ("THE_MATRIX" → "The Matrix").
func CleanLabel(label string) string {
	words := strings.FieldsFunc(label, func(r rune) bool { return r == '_' || unicode.IsSpace(r) })
	for i, w := range words {
		rs := []rune(strings.ToLower(w))
		rs[0] = unicode.ToUpper(rs[0])
		words[i] = string(rs)
	}
	return strings.Join(words, " ")
}

// SafeFileName makes s usable as a file name on macOS, Linux and Windows:
// characters invalid on any of them (<>:"/\|?* and control characters) become
// "_", surrounding spaces and trailing dots are removed, and an empty result
// becomes "untitled".
func SafeFileName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if out == "" {
		return "untitled"
	}
	return out
}
```

`name.go`:
```go
package zenvik

import (
	"strings"

	"github.com/chad3814/zenvik/internal/naming"
)

// Name returns a human-readable disc name: the disc library title if
// present, otherwise the volume label tidied up ("THE_MATRIX" → "The
// Matrix"), otherwise "untitled".
func (d *Disc) Name() string {
	if d.Meta != nil {
		if t := strings.TrimSpace(d.Meta.Title); t != "" {
			return t
		}
	}
	if n := naming.CleanLabel(d.Label); n != "" {
		return n
	}
	return "untitled"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/naming/ . && go vet ./... && golangci-lint run`
Expected: PASS; 0 lint issues.

- [ ] **Step 5: Commit**

```bash
git add internal/naming name.go name_test.go
git commit -m "Add disc names and safe file names"
```

---

### Task 5: Real test media from ffmpeg (`internal/testdisc`)

**Files:**
- Create: `internal/testdisc/realclip.go`
- Test: `internal/testdisc/realclip_integration_test.go`

**Interfaces:**
- Consumes: `testdisc.Disc`, `StubClip`, `CmdPlayPL`, the `bluray` types.
- Produces (used by Tasks 6 and 7 integration tests):
  - `const ClipStartTicks = bluray.Ticks(63000)`: the first presentation timestamp of ffmpeg's M2TS output, 1.4 s
  - `func FFmpegClip(ctx context.Context, path string, seconds, toneHz int) error`: writes a 192-byte-packet M2TS with H.264 1080p 23.976 video on PID 0x1011 and AC-3 stereo audio on PID 0x1100, tagged `eng` in the stream
  - `func RealMovieDisc(ctx context.Context, scratch string, seconds int) (*Disc, error)`:
    - two clips, `00001` and `00002`, generated in scratch, each `seconds` long
    - playlist `00800` plays both, with IN at `ClipStartTicks` and OUT at IN + seconds
    - three chapter marks: item 0 start, item 0 midpoint, item 1 start
    - STN: video AVC 1080p (0x1011) and AC-3 stereo audio in `jpn` (0x1100). The language deliberately differs from the stream's `eng` tag, so tests can tell the disc's value won.
    - title 1 plays playlist 800, and `MetaTitle` is `"Real Movie"`
  - `func RealMovie(ctx context.Context, dir string, seconds int) error`: `RealMovieDisc` followed by `WriteDir(dir)`
- Note: a few-second title is shorter than the 2-minute minimum, so ranking filters it. Integration tests pick `00800` explicitly (Design Decision 9).

- [ ] **Step 1: Write the failing integration test**

`internal/testdisc/realclip_integration_test.go`:
```go
//go:build integration

package testdisc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chad3814/zenvik/bluray"
)

func TestRealMovie(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "REAL_MOVIE")
	if err := RealMovie(context.Background(), dir, 3); err != nil {
		t.Fatal(err)
	}
	for _, clip := range []string{"00001", "00002"} {
		b, err := os.ReadFile(filepath.Join(dir, "BDMV", "STREAM", clip+".m2ts"))
		if err != nil {
			t.Fatal(err)
		}
		if len(b) == 0 || len(b)%192 != 0 {
			t.Fatalf("%s: %d bytes is not a whole number of source packets", clip, len(b))
		}
		for p := 0; p < 32 && (p+1)*192 <= len(b); p++ {
			if b[p*192+4] != 0x47 {
				t.Fatalf("%s: packet %d has no sync byte", clip, p)
			}
		}
	}
	mpls, err := os.ReadFile(filepath.Join(dir, "BDMV", "PLAYLIST", "00800.mpls"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := bluray.ParsePlaylist(mpls)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Items[0].In != ClipStartTicks || len(p.Chapters()) != 3 || p.Items[0].STN.Audio[0].Language != "jpn" {
		t.Errorf("playlist = %+v", p)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags integration ./internal/testdisc/ -run TestRealMovie`
Expected: FAIL to compile (`undefined: RealMovie`, `undefined: ClipStartTicks`).

- [ ] **Step 3: Implement**

`internal/testdisc/realclip.go`:
```go
package testdisc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/chad3814/zenvik/bluray"
)

// ClipStartTicks is the first presentation timestamp (1.4 s) of FFmpegClip
// output; play items use it as their IN time.
const ClipStartTicks = bluray.Ticks(63000)

// FFmpegClip writes a real M2TS clip (192-byte source packets) with ffmpeg:
// H.264 1080p 23.976 video on PID 0x1011 and AC-3 stereo audio on PID
// 0x1100, tagged "eng" in the stream, lasting seconds.
func FFmpegClip(ctx context.Context, path string, seconds, toneHz int) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=1920x1080:rate=24000/1001:duration=%d", seconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=%d", toneHz, seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "ac3", "-b:a", "192k", "-metadata:s:a:0", "language=eng",
		"-streamid", "0:0x1011", "-streamid", "1:0x1100",
		"-mpegts_m2ts_mode", "1", "-f", "mpegts", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, out)
	}
	return nil
}

// RealMovieDisc builds a two-clip disc from FFmpegClip output generated in
// scratch: playlist 00800 plays clips 00001 and 00002 (seconds each) with
// three chapters, and title 1 plays it. Its STN marks the audio as Japanese,
// unlike the stream's own "eng" tag.
func RealMovieDisc(ctx context.Context, scratch string, seconds int) (*Disc, error) {
	stn := bluray.STN{
		Video: []bluray.Stream{{PID: 0x1011, Coding: bluray.CodingAVC, VideoFormat: 6, FrameRate: 1}},
		Audio: []bluray.Stream{{PID: 0x1100, Coding: bluray.CodingAC3, AudioFormat: 3, SampleRate: 1, Language: "jpn"}},
	}
	in := ClipStartTicks
	out := in + bluray.Ticks(seconds*bluray.TicksPerSecond)
	d := &Disc{
		Titles:       []bluray.IndexTitle{{Object: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0}}},
		MovieObjects: []bluray.MovieObject{{Commands: []bluray.NavCommand{CmdPlayPL(800)}}},
		Playlists: map[string]*bluray.Playlist{"00800": {
			Version: "0200",
			Items: []bluray.PlayItem{
				{ClipID: "00001", CodecID: "M2TS", In: in, Out: out, STN: stn},
				{ClipID: "00002", CodecID: "M2TS", ConnectionCondition: 1, In: in, Out: out, STN: stn},
			},
			Marks: []bluray.Mark{
				{Type: bluray.MarkEntry, PlayItem: 0, Time: in, PID: 0xFFFF},
				{Type: bluray.MarkEntry, PlayItem: 0, Time: in + (out-in)/2, PID: 0xFFFF},
				{Type: bluray.MarkEntry, PlayItem: 1, Time: in, PID: 0xFFFF},
			},
		}},
		Clips:     map[string]*bluray.Clip{},
		ClipData:  map[string][]byte{},
		MetaTitle: "Real Movie",
	}
	for i, id := range []string{"00001", "00002"} {
		p := filepath.Join(scratch, id+".m2ts")
		if err := FFmpegClip(ctx, p, seconds, 440*(i+1)); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		d.ClipData[id] = b
		d.Clips[id] = StubClip()
	}
	return d, nil
}

// RealMovie writes RealMovieDisc into dir.
func RealMovie(ctx context.Context, dir string, seconds int) error {
	scratch, err := os.MkdirTemp("", "zenvik-clips-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	d, err := RealMovieDisc(ctx, scratch, seconds)
	if err != nil {
		return err
	}
	return d.WriteDir(dir)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags integration ./internal/testdisc/ -run TestRealMovie -v && go test -race ./internal/testdisc/... && go vet ./... && golangci-lint run && golangci-lint run --build-tags integration`
Expected: PASS; 0 lint issues.

- [ ] **Step 5: Commit**

```bash
git add internal/testdisc/realclip.go internal/testdisc/realclip_integration_test.go
git commit -m "Add ffmpeg-generated real test discs for integration tests"
```

---

### Task 6: `Disc.Rip`

**Files:**
- Create: `rip.go`
- Modify: `errors.go` (add the rip errors)
- Test: `rip_test.go` (package `zenvik_test`), `rip_internal_test.go` (package `zenvik`), `rip_integration_test.go` (`//go:build integration`), `rip_iso_integration_test.go` (`//go:build integration && darwin`)

**Interfaces:**
- Consumes:
  - Task 1: `mux.Find`, `mux.Identification`, `mux.IdentifiedTrack`, `mux.Track`, `mux.Job`, `mux.Args`, and the errors
  - Task 2: `(*mux.Mkvmerge).Mux`, `mux.Phase*`, `mux.Result`
  - Task 3: `mount.Attach`, `(*mount.Mount).Detach`, `mount.ErrUnavailable`
  - Task 5 (integration tests): `testdisc.RealMovie`, `testdisc.RealMovieDisc`
  - M2: `Disc` (unexported `src *source.Source` with `Kind`, `Path`), `Title`, `ErrEncrypted`; the test helpers `openDisc`, `writeDisc` and `mustTitle` in `zenvik_test.go`
- Produces (public API; Task 7 and M4 rely on it):
  - `type Phase int`; constants `PhaseMounting`, `PhaseScanning`, `PhaseMuxing`, `PhaseFinalizing` (1–4) with `String()` returning `"mounting"`, `"scanning"`, `"muxing"`, `"finalizing"`
  - `type Progress struct { Phase Phase; Fraction float64; BytesDone, BytesTotal int64 }`
  - `type RipOptions struct { OutputPath string; Overwrite, DryRun bool; OnProgress func(Progress) }`
  - `type RipResult struct { OutputPath string; Duration time.Duration; Warnings []string; Command []string }`
  - `func (d *Disc) Rip(ctx context.Context, t *Title, opts RipOptions) (*RipResult, error)`
  - `var ErrMkvmergeNotFound = mux.ErrNotFound`, `ErrMkvmergeTooOld = mux.ErrTooOld`, `ErrMuxFailed = mux.ErrFailed`, `ErrMountUnavailable = mount.ErrUnavailable`, `ErrOutputExists = errors.New("zenvik: output file already exists")`
  - unexported `mapTracks(t *Title, id *mux.Identification) ([]mux.Track, []string)` and `audioName(a AudioTrack, channels int) string`
- `Rip` order of checks:
  1. A closed disc wraps `fs.ErrClosed`.
  2. A title not in `d.Titles` is an error mentioning "does not belong".
  3. An encrypted title wraps `ErrEncrypted`.
  4. An empty `OutputPath` is an error mentioning `OutputPath`.
  5. An existing output without `Overwrite` (and not a dry run) wraps `ErrOutputExists`.
  6. `mux.Find`.
  7. Mount, if the disc is an ISO.
  8. Identify.
  9. Map tracks.
  10. On a dry run, return here.
  11. `MkdirAll` the parent directory.
  12. Mux to `<OutputPath>.partial`.
  13. Rename (removing an existing target first when `Overwrite`).

  Checks 1–5 need no mkvmerge.

- [ ] **Step 1: Write the failing unit tests**

`rip_internal_test.go`:
```go
package zenvik

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/mux"
)

func TestMapTracks(t *testing.T) {
	title := &Title{
		ID:    "00800",
		Video: []VideoTrack{{PID: 0x1011, Codec: bluray.CodingAVC}},
		Audio: []AudioTrack{
			{PID: 0x1100, Codec: bluray.CodingTrueHD, Language: "eng", Channels: 6},
			{PID: 0x1101, Codec: bluray.CodingAC3, Language: "fra", Channels: 3},
		},
		Subtitles: []SubtitleTrack{
			{PID: 0x1200, Codec: bluray.CodingPG, Language: "eng"},
			{PID: 0x1201, Codec: bluray.CodingPG, Language: "deu"}, // not found by mkvmerge
		},
	}
	id := &mux.Identification{Tracks: []mux.IdentifiedTrack{
		{ID: 0, Type: "video", PID: 0x1011},
		{ID: 1, Type: "audio", PID: 0x1100, Channels: 8},
		{ID: 2, Type: "audio", PID: 0x1101, Channels: 2},
		{ID: 3, Type: "subtitles", PID: 0x1200},
		{ID: 4, Type: "audio", PID: 0x1102, Channels: 2}, // not in the STN table
	}}
	tracks, warnings := mapTracks(title, id)
	want := []mux.Track{
		{ID: 0, Type: "video", Default: true},
		{ID: 1, Type: "audio", Language: "eng", Name: "TrueHD 7.1", Default: true},
		{ID: 2, Type: "audio", Language: "fra", Name: "AC-3 Stereo"},
		{ID: 3, Type: "subtitles", Language: "eng"},
	}
	if !reflect.DeepEqual(tracks, want) {
		t.Errorf("tracks =\n%+v\nwant\n%+v", tracks, want)
	}
	joined := strings.Join(warnings, "\n")
	if len(warnings) != 2 || !strings.Contains(joined, "0x1201") || !strings.Contains(joined, "0x1102") {
		t.Errorf("warnings = %q", warnings)
	}
}

func TestAudioName(t *testing.T) {
	tests := []struct {
		a        AudioTrack
		channels int
		want     string
	}{
		{AudioTrack{Codec: bluray.CodingDTSHDMA}, 6, "DTS-HD MA 5.1"},
		{AudioTrack{Codec: bluray.CodingAC3}, 1, "AC-3 Mono"},
		{AudioTrack{Codec: bluray.CodingLPCM}, 4, "LPCM 4 ch"},
		{AudioTrack{Codec: bluray.CodingTrueHD, Channels: 6}, 0, "TrueHD multi-channel"},
	}
	for _, tt := range tests {
		if got := audioName(tt.a, tt.channels); got != tt.want {
			t.Errorf("audioName(%v, %d) = %q, want %q", tt.a.Codec, tt.channels, got, tt.want)
		}
	}
}

func TestPhaseString(t *testing.T) {
	if PhaseMounting.String() != "mounting" || PhaseScanning.String() != "scanning" ||
		PhaseMuxing.String() != "muxing" || PhaseFinalizing.String() != "finalizing" || Phase(9).String() != "unknown" {
		t.Error("Phase.String mismatch")
	}
}
```

`rip_test.go`:
```go
package zenvik_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestRipErrorsBeforeMuxing(t *testing.T) {
	ctx := context.Background()
	disc := testdisc.SampleMovie()
	disc.ClipData["00030"] = testdisc.ScrambledM2TS(1)
	d := openDisc(t, writeDisc(t, disc))
	main := d.Main()
	out := filepath.Join(t.TempDir(), "movie.mkv")

	if _, err := d.Rip(ctx, mustTitle(t, d, "00010"), zenvik.RipOptions{OutputPath: out}); !errors.Is(err, zenvik.ErrEncrypted) {
		t.Errorf("encrypted title: err = %v", err)
	}
	if _, err := d.Rip(ctx, main, zenvik.RipOptions{}); err == nil || !strings.Contains(err.Error(), "OutputPath") {
		t.Errorf("missing output path: err = %v", err)
	}
	other := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	if _, err := d.Rip(ctx, other.Main(), zenvik.RipOptions{OutputPath: out}); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Errorf("foreign title: err = %v", err)
	}
	if err := os.WriteFile(out, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Rip(ctx, main, zenvik.RipOptions{OutputPath: out}); !errors.Is(err, zenvik.ErrOutputExists) {
		t.Errorf("existing output: err = %v", err)
	}
	if b, _ := os.ReadFile(out); string(b) != "keep me" {
		t.Error("existing output was modified")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Rip(ctx, main, zenvik.RipOptions{OutputPath: out + "2"}); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("closed disc: err = %v", err)
	}
}

func TestRipMissingMkvmerge(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	d := openDisc(t, writeDisc(t, testdisc.SampleMovie()))
	_, err := d.Rip(context.Background(), d.Main(), zenvik.RipOptions{OutputPath: filepath.Join(t.TempDir(), "x.mkv")})
	if !errors.Is(err, zenvik.ErrMkvmergeNotFound) {
		t.Errorf("err = %v, want ErrMkvmergeNotFound", err)
	}
}
```

`rip_integration_test.go`:
```go
//go:build integration

package zenvik_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/testdisc"
)

func realMovie(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "REAL_MOVIE")
	if err := testdisc.RealMovie(context.Background(), dir, 4); err != nil {
		t.Fatal(err)
	}
	return dir
}

func identify(t *testing.T, path string) *mux.Identification {
	t.Helper()
	mk, err := mux.Find(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := mk.Identify(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertRipped(t *testing.T, out string) {
	t.Helper()
	if _, err := os.Stat(out + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("partial file left behind: %v", err)
	}
	id := identify(t, out)
	if len(id.Tracks) != 2 || id.Tracks[0].Type != "video" || id.Tracks[1].Type != "audio" {
		t.Fatalf("tracks = %+v", id.Tracks)
	}
	if id.Tracks[1].Language != "jpn" {
		t.Errorf("audio language = %q, want jpn (from the disc, not the stream's eng)", id.Tracks[1].Language)
	}
	if id.Chapters != 3 {
		t.Errorf("chapters = %d, want 3", id.Chapters)
	}
}

func TestRipDirectory(t *testing.T) {
	d := openDisc(t, realMovie(t))
	title := mustTitle(t, d, "00800")
	out := filepath.Join(t.TempDir(), "out dir", `Réal "Movie".mkv`)
	var events []zenvik.Progress
	res, err := d.Rip(context.Background(), title, zenvik.RipOptions{
		OutputPath: out,
		OnProgress: func(p zenvik.Progress) { events = append(events, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OutputPath != out || res.Duration != title.Duration || len(res.Command) == 0 {
		t.Errorf("result = %+v", res)
	}
	assertRipped(t, out)
	if len(events) == 0 || events[len(events)-1] != (zenvik.Progress{Phase: zenvik.PhaseFinalizing, Fraction: 1, BytesDone: title.Size, BytesTotal: title.Size}) {
		t.Errorf("last event = %+v", events[len(events)-1])
	}
	last := zenvik.Phase(0)
	for _, e := range events {
		if e.Phase < last || e.Fraction < 0 || e.Fraction > 1 || e.BytesTotal != title.Size {
			t.Fatalf("bad progress sequence: %+v", events)
		}
		last = e.Phase
	}
	if !slices.ContainsFunc(events, func(e zenvik.Progress) bool { return e.Phase == zenvik.PhaseMuxing }) {
		t.Error("no muxing progress reported")
	}
}

func TestRipOverwrite(t *testing.T) {
	d := openDisc(t, realMovie(t))
	title := mustTitle(t, d, "00800")
	out := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Rip(context.Background(), title, zenvik.RipOptions{OutputPath: out, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	assertRipped(t, out)
}

func TestRipDryRun(t *testing.T) {
	d := openDisc(t, realMovie(t))
	out := filepath.Join(t.TempDir(), "movie.mkv")
	res, err := d.Rip(context.Background(), mustTitle(t, d, "00800"), zenvik.RipOptions{OutputPath: out, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(res.Command, "--language")
	if i < 0 || res.Command[i+1] != "1:jpn" || res.Command[len(res.Command)-1] != filepath.Join(d.Path, "BDMV", "PLAYLIST", "00800.mpls") {
		t.Errorf("command = %q", res.Command)
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("dry run created the output")
	}
}

func TestRipCanceled(t *testing.T) {
	d := openDisc(t, realMovie(t))
	out := filepath.Join(t.TempDir(), "movie.mkv")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := d.Rip(ctx, mustTitle(t, d, "00800"), zenvik.RipOptions{
		OutputPath: out,
		OnProgress: func(p zenvik.Progress) {
			if p.Phase == zenvik.PhaseMuxing {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	for _, p := range []string{out, out + ".partial"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left behind: %v", p, err)
		}
	}
}
```

`rip_iso_integration_test.go`:
```go
//go:build integration && darwin

package zenvik_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/testdisc"
	"github.com/chad3814/zenvik/internal/testdisc/udfimage"
)

func realISO(t *testing.T) string {
	t.Helper()
	disc, err := testdisc.RealMovieDisc(context.Background(), t.TempDir(), 4)
	if err != nil {
		t.Fatal(err)
	}
	img, err := disc.ISO(udfimage.Options{Revision: 0x0250, Label: "REAL_MOVIE"})
	if err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(t.TempDir(), "real movie.iso")
	if err := os.WriteFile(iso, img, 0o644); err != nil {
		t.Fatal(err)
	}
	return iso
}

func assertDetached(t *testing.T, iso string) {
	t.Helper()
	out, err := exec.Command("hdiutil", "info").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), iso) {
		t.Errorf("image still attached:\n%s", out)
	}
}

func TestRipISO(t *testing.T) {
	iso := realISO(t)
	d := openDisc(t, iso)
	out := filepath.Join(t.TempDir(), "movie.mkv")
	var phases []zenvik.Phase
	if _, err := d.Rip(context.Background(), mustTitle(t, d, "00800"), zenvik.RipOptions{
		OutputPath: out,
		OnProgress: func(p zenvik.Progress) { phases = append(phases, p.Phase) },
	}); err != nil {
		t.Fatal(err)
	}
	assertRipped(t, out)
	if !slices.Contains(phases, zenvik.PhaseMounting) {
		t.Errorf("phases = %v, want mounting", phases)
	}
	assertDetached(t, iso)
}

func TestRipISOCanceled(t *testing.T) {
	iso := realISO(t)
	d := openDisc(t, iso)
	out := filepath.Join(t.TempDir(), "movie.mkv")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := d.Rip(ctx, mustTitle(t, d, "00800"), zenvik.RipOptions{
		OutputPath: out,
		OnProgress: func(p zenvik.Progress) {
			if p.Phase == zenvik.PhaseMuxing {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(out + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Error("partial file left behind")
	}
	assertDetached(t, iso)
}
```

- [ ] **Step 2: Run unit tests to verify they fail**

Run: `go test . -run 'MapTracks|AudioName|PhaseString|Rip'`
Expected: FAIL to compile (`undefined: mapTracks`, `d.Rip undefined`, ...).

- [ ] **Step 3: Implement**

Append to `errors.go`'s variable block (add imports for `internal/mux` and `internal/mount`):
```go
	// ErrMkvmergeNotFound: mkvmerge (MKVToolNix) is not installed or not on PATH.
	ErrMkvmergeNotFound = mux.ErrNotFound
	// ErrMkvmergeTooOld: the installed mkvmerge is older than the supported minimum.
	ErrMkvmergeTooOld = mux.ErrTooOld
	// ErrMuxFailed: mkvmerge reported an error.
	ErrMuxFailed = mux.ErrFailed
	// ErrMountUnavailable: the ISO image could not be mounted on this system.
	ErrMountUnavailable = mount.ErrUnavailable
	// ErrOutputExists: the output file exists and RipOptions.Overwrite is false.
	ErrOutputExists = errors.New("zenvik: output file already exists")
```

`rip.go`:
```go
package zenvik

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/chad3814/zenvik/internal/mount"
	"github.com/chad3814/zenvik/internal/mux"
)

// Phase identifies a stage of Rip.
type Phase int

// Rip phases, in order.
const (
	PhaseMounting   Phase = 1 // attaching an ISO image
	PhaseScanning   Phase = 2 // mkvmerge scanning the playlist's files
	PhaseMuxing     Phase = 3 // mkvmerge writing the output
	PhaseFinalizing Phase = 4 // renaming the finished file
)

func (p Phase) String() string {
	switch p {
	case PhaseMounting:
		return "mounting"
	case PhaseScanning:
		return "scanning"
	case PhaseMuxing:
		return "muxing"
	case PhaseFinalizing:
		return "finalizing"
	}
	return "unknown"
}

// Progress reports how far a Rip has come.
type Progress struct {
	Phase      Phase
	Fraction   float64 // 0..1 within Phase
	BytesDone  int64   // estimate: Fraction × BytesTotal
	BytesTotal int64   // the title's Size
}

// RipOptions control Rip.
type RipOptions struct {
	OutputPath string         // final MKV path; parent directories are created
	Overwrite  bool           // replace an existing output file
	DryRun     bool           // resolve everything and return the mkvmerge command without running it
	OnProgress func(Progress) // optional; called on Rip's goroutine
}

// RipResult describes a finished (or dry-run) Rip.
type RipResult struct {
	OutputPath string
	Duration   time.Duration
	Warnings   []string
	Command    []string // the mkvmerge program and arguments
}

// Rip remuxes title t to an MKV file at opts.OutputPath with mkvmerge,
// mounting the disc image first if needed. The output appears only when
// muxing succeeds: mkvmerge writes "<OutputPath>.partial", which is renamed
// at the end and removed on failure or cancellation. A Disc must not be
// ripped from concurrently.
func (d *Disc) Rip(ctx context.Context, t *Title, opts RipOptions) (res *RipResult, err error) {
	if d.src == nil {
		return nil, fmt.Errorf("zenvik: disc is closed: %w", fs.ErrClosed)
	}
	if !slices.Contains(d.Titles, t) {
		return nil, errors.New("zenvik: title does not belong to this disc")
	}
	if t.Encrypted {
		return nil, fmt.Errorf("%w: title %s", ErrEncrypted, t.ID)
	}
	if opts.OutputPath == "" {
		return nil, errors.New("zenvik: RipOptions.OutputPath is required")
	}
	if !opts.Overwrite && !opts.DryRun {
		if _, err := os.Stat(opts.OutputPath); err == nil {
			return nil, fmt.Errorf("%w: %s", ErrOutputExists, opts.OutputPath)
		}
	}
	mk, err := mux.Find(ctx, "")
	if err != nil {
		return nil, err
	}
	report := func(p Phase, f float64) {
		if opts.OnProgress != nil {
			opts.OnProgress(Progress{Phase: p, Fraction: f, BytesDone: int64(f * float64(t.Size)), BytesTotal: t.Size})
		}
	}

	root, release, err := d.mountRoot(ctx, report)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr := release(); rerr != nil {
			if res != nil {
				res.Warnings = append(res.Warnings, rerr.Error())
			} else if err == nil {
				err = rerr
			}
		}
	}()

	playlist := filepath.Join(root, "BDMV", "PLAYLIST", t.ID+".mpls")
	ident, err := mk.Identify(ctx, playlist)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	tracks, warnings := mapTracks(t, ident)
	if len(tracks) == 0 {
		return nil, fmt.Errorf("%w: mkvmerge found none of title %s's streams", ErrMuxFailed, t.ID)
	}
	partial := opts.OutputPath + ".partial"
	job := mux.Job{Input: playlist, Output: partial, Tracks: tracks}
	command := append([]string{mk.Path}, mux.Args(job)...)
	if opts.DryRun {
		return &RipResult{OutputPath: opts.OutputPath, Duration: t.Duration, Warnings: warnings, Command: command}, nil
	}

	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return nil, err
	}
	muxed, err := mk.Mux(ctx, job, func(p mux.Phase, f float64) {
		if p == mux.PhaseScanning {
			report(PhaseScanning, f)
			return
		}
		report(PhaseMuxing, f)
	})
	if err != nil {
		os.Remove(partial)
		return nil, err
	}
	report(PhaseFinalizing, 0)
	if opts.Overwrite {
		if err := os.Remove(opts.OutputPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			os.Remove(partial)
			return nil, err
		}
	}
	if err := os.Rename(partial, opts.OutputPath); err != nil {
		os.Remove(partial)
		return nil, err
	}
	report(PhaseFinalizing, 1)
	return &RipResult{
		OutputPath: opts.OutputPath,
		Duration:   t.Duration,
		Warnings:   append(warnings, muxed.Warnings...),
		Command:    command,
	}, nil
}

// mountRoot returns the directory that holds BDMV for mkvmerge to read,
// mounting an ISO image if needed, and a function that releases it.
func (d *Disc) mountRoot(ctx context.Context, report func(Phase, float64)) (string, func() error, error) {
	if d.src.Kind != ISO {
		return d.src.Path, func() error { return nil }, nil
	}
	report(PhaseMounting, 0)
	m, err := mount.Attach(ctx, d.src.Path)
	if err != nil {
		return "", nil, fmt.Errorf("%w (mount the image yourself, or extract it, and pass the folder instead)", err)
	}
	release := func() error { return m.Detach(ctx) }
	if _, err := os.Stat(filepath.Join(m.Dir, "BDMV", "index.bdmv")); err != nil {
		_ = release()
		return "", nil, fmt.Errorf("zenvik: mounted %s at %s but found no BDMV/index.bdmv: %w", d.src.Path, m.Dir, err)
	}
	report(PhaseMounting, 1)
	return m.Dir, release, nil
}

// mapTracks matches the title's playlist streams to mkvmerge's tracks by
// PID, in stream-number order (video, audio, subtitles). It sets languages
// from the playlist, names audio tracks, and makes the first video and
// first audio track default. Streams one side has and the other doesn't
// are skipped with a warning.
func mapTracks(t *Title, id *mux.Identification) ([]mux.Track, []string) {
	byPID := map[uint16]mux.IdentifiedTrack{}
	for _, it := range id.Tracks {
		byPID[it.PID] = it
	}
	var tracks []mux.Track
	var warnings []string
	used := map[int]bool{}
	add := func(pid uint16, kind, lang, name string, def bool) {
		it, ok := byPID[pid]
		if !ok || it.Type != kind {
			warnings = append(warnings, fmt.Sprintf("title %s: %s stream PID 0x%04X not found by mkvmerge; skipped", t.ID, kind, pid))
			return
		}
		used[it.ID] = true
		tracks = append(tracks, mux.Track{ID: it.ID, Type: kind, Language: lang, Name: name, Default: def})
	}
	for i, v := range t.Video {
		add(v.PID, "video", "", "", i == 0)
	}
	for i, a := range t.Audio {
		add(a.PID, "audio", a.Language, audioName(a, byPID[a.PID].Channels), i == 0)
	}
	for _, s := range t.Subtitles {
		add(s.PID, "subtitles", s.Language, "", false)
	}
	for _, it := range id.Tracks {
		if !used[it.ID] {
			warnings = append(warnings, fmt.Sprintf("title %s: mkvmerge track %d (%s, PID 0x%04X) is not in the playlist's stream table; skipped", t.ID, it.ID, it.Type, it.PID))
		}
	}
	return tracks, warnings
}

// audioName names an audio track "<codec> <layout>", e.g. "TrueHD 7.1",
// using mkvmerge's channel count when known.
func audioName(a AudioTrack, channels int) string {
	layout := map[int]string{1: "Mono", 2: "Stereo", 6: "5.1", 8: "7.1"}[channels]
	switch {
	case layout != "":
	case channels > 0:
		layout = fmt.Sprintf("%d ch", channels)
	default:
		layout = a.Channels.String()
	}
	return a.Codec.String() + " " + layout
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test -race ./... && go vet ./... && golangci-lint run && golangci-lint run --build-tags integration
go test -tags integration -run 'Rip' -v .
```
Expected: unit and integration tests PASS on macOS, including `TestRipISO` and `TestRipISOCanceled` with real `hdiutil`. If mkvmerge's track languages or chapter counts differ from the expectations, inspect `mkvmerge -J` on the output and fix the code, not the expectation, unless the expectation contradicts the spec. Explain any change in your report.

- [ ] **Step 5: Commit**

```bash
git add errors.go rip.go rip_test.go rip_internal_test.go rip_integration_test.go rip_iso_integration_test.go
git commit -m "Add Disc.Rip: mount, identify, map tracks, mux atomically with progress"
```

---

### Task 7: `zenvik rip` CLI

**Files:**
- Create: `cmd/zenvik/rip.go`, `cmd/zenvik/progress.go`
- Modify: `cmd/zenvik/main.go` (register `rip`, exit code 4, second-signal exit)
- Test: `cmd/zenvik/rip_test.go`, `cmd/zenvik/progress_test.go`, `cmd/zenvik/rip_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes:
  - Task 6: `Disc.Rip`, `RipOptions`, `RipResult`, `Progress`, `Phase`, and the errors
  - Task 4: `Disc.Name`, `naming.SafeFileName`
  - M2: `zenvik.Open`, `Disc.Main`, `Disc.Title`; the CLI helpers `run`, `usageError`, `exitCode`, `formatDuration`, `formatSize`; the test helpers `writeDisc`, `runCLI`, `lineWith`
  - Task 5 (integration): `testdisc.RealMovie`
- Produces: `zenvik rip <path> [-p|--playlist ID] [-o|--output-dir DIR] [--overwrite] [--dry-run]`.
- Output contract:
  - Stdout gets `Ripping <ID> (<H:MM:SS>) → <output path>`.
  - When the title was picked automatically, the next line is `  chosen because: <reasons joined by "; ">`.
  - Then progress: an in-place bar on a TTY, plain lines otherwise.
  - Then `Done: <path>`, or for `--dry-run` the shell-quoted mkvmerge command.
  - Warnings go to stderr as `warning: …`.
  - An ambiguous automatic pick prints `warning: the main title is a close call (...) — pass --playlist to choose` to stderr.
  - Errors get a hint appended:
    - `ErrOutputExists`: `(use --overwrite to replace it)`
    - `ErrMkvmergeNotFound`: `(install MKVToolNix: https://mkvtoolnix.download/)`
  - Exit codes:
    - `4` for `ErrMkvmergeNotFound` and `ErrMkvmergeTooOld`
    - `2` for an unknown `--playlist`
    - `1` when no main title qualifies and no `--playlist` is given (message suggests `--playlist`)

- [ ] **Step 1: Write the failing tests**

`cmd/zenvik/progress_test.go`:
```go
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chad3814/zenvik"
)

func TestProgressPlain(t *testing.T) {
	var b bytes.Buffer
	p := newProgressPrinter(&b, false)
	for _, f := range []float64{0, 0.03, 0.12, 0.15, 0.5, 1} {
		p.update(zenvik.Progress{Phase: zenvik.PhaseMuxing, Fraction: f, BytesTotal: 1000})
	}
	p.update(zenvik.Progress{Phase: zenvik.PhaseFinalizing, Fraction: 1, BytesTotal: 1000})
	p.done()
	want := "muxing 0%\nmuxing 12%\nmuxing 50%\nmuxing 100%\nfinalizing 100%\n"
	if b.String() != want {
		t.Errorf("plain progress =\n%q\nwant\n%q", b.String(), want)
	}
}

func TestProgressTTY(t *testing.T) {
	var b bytes.Buffer
	p := newProgressPrinter(&b, true)
	p.update(zenvik.Progress{Phase: zenvik.PhaseMuxing, Fraction: 0.42, BytesDone: 420 << 20, BytesTotal: 1000 << 20})
	p.done()
	out := b.String()
	if !strings.HasPrefix(out, "\r[########............]  42% muxing") || !strings.Contains(out, "420.0 MiB / 1000.0 MiB") || !strings.HasSuffix(out, "\n") {
		t.Errorf("tty progress = %q", out)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote([]string{"/usr/bin/mkvmerge", "-o", "/out dir/Bob's.mkv", "--language", "1:jpn"})
	want := `/usr/bin/mkvmerge -o '/out dir/Bob'\''s.mkv' --language 1:jpn`
	if got != want {
		t.Errorf("shellQuote = %s\nwant          %s", got, want)
	}
}
```

`cmd/zenvik/rip_test.go`:
```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestRipExitCodes(t *testing.T) {
	good := writeDisc(t, testdisc.SampleMovie())
	enc := testdisc.SampleMovie()
	for id := range enc.Clips {
		enc.ClipData[id] = testdisc.ScrambledM2TS(1)
	}
	encrypted := writeDisc(t, enc)
	short := testdisc.SampleMovie()
	short.Titles, short.MovieObjects = nil, nil
	delete(short.Playlists, "00800")
	delete(short.Playlists, "00801")
	delete(short.Playlists, "00010")
	noMain := writeDisc(t, short)

	existsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(existsDir, "Sample Movie.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want int
		msg  string
	}{
		{"missing path", []string{"rip", filepath.Join(good, "nope")}, 1, ""},
		{"no args", []string{"rip"}, 2, ""},
		{"unknown playlist", []string{"rip", "-p", "12345", good}, 2, "12345"},
		{"encrypted", []string{"rip", encrypted}, 3, "encrypted"},
		{"no main title", []string{"rip", "-o", t.TempDir(), noMain}, 1, "--playlist"},
		{"output exists", []string{"rip", "-o", existsDir, good}, 1, "--overwrite"},
	}
	for _, tt := range tests {
		code, _, errOut := runCLI(tt.args...)
		if code != tt.want || !strings.Contains(errOut, tt.msg) {
			t.Errorf("%s: exit %d (want %d), stderr %q (want containing %q)", tt.name, code, tt.want, errOut, tt.msg)
		}
	}
}

func TestRipMissingMkvmerge(t *testing.T) {
	good := writeDisc(t, testdisc.SampleMovie())
	t.Setenv("PATH", t.TempDir())
	code, _, errOut := runCLI("rip", "-o", t.TempDir(), good)
	if code != 4 || !strings.Contains(errOut, "mkvmerge not found") || !strings.Contains(errOut, "mkvtoolnix.download") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestRipAmbiguousWarning(t *testing.T) {
	d := testdisc.SampleMovie()
	d.Titles, d.MovieObjects = nil, nil
	var segs []testdisc.Segment
	for i := 201; i <= 220; i++ {
		segs = append(segs, testdisc.Segment{Clip: fmt.Sprintf("%05d", i), Length: 297 * time.Second})
	}
	d.Playlists["00802"] = testdisc.SimplePlaylist(segs...)
	d.AddClipsFor()
	t.Setenv("PATH", t.TempDir()) // stop before muxing; the warning comes first
	_, out, errOut := runCLI("rip", "-o", t.TempDir(), writeDisc(t, d))
	if !strings.Contains(errOut, "close call") || !strings.Contains(errOut, "--playlist") {
		t.Errorf("stderr = %q", errOut)
	}
	if !strings.Contains(out, "Ripping 00800") || !strings.Contains(out, "chosen because:") {
		t.Errorf("stdout = %q", out)
	}
}
```

`cmd/zenvik/rip_integration_test.go`:
```go
//go:build integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chad3814/zenvik/internal/testdisc"
)

func TestRipCommand(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "REAL_MOVIE")
	if err := testdisc.RealMovie(context.Background(), disc, 3); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	code, stdout, errOut := runCLI("rip", "-p", "00800", "-o", out, disc)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	want := filepath.Join(out, "Real Movie.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("output missing: %v\nstdout:\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Done: "+want) || !strings.Contains(stdout, "muxing 100%") {
		t.Errorf("stdout = %q", stdout)
	}

	code, stdout, _ = runCLI("rip", "-p", "00800", "-o", t.TempDir(), "--dry-run", disc)
	if code != 0 || !strings.Contains(stdout, "mkvmerge") || !strings.Contains(stdout, "--language 1:jpn") {
		t.Errorf("dry run: exit %d, stdout %q", code, stdout)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/zenvik/`
Expected: FAIL to compile (`undefined: newProgressPrinter`, `undefined: shellQuote`), and rip tests fail with "unknown command".

- [ ] **Step 3: Implement**

`cmd/zenvik/progress.go`:
```go
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chad3814/zenvik"
)

// progressPrinter shows Rip progress: an in-place bar on a terminal, or a
// line per phase change and per 10 percentage points otherwise.
type progressPrinter struct {
	w         io.Writer
	tty       bool
	drawn     bool
	lastPhase zenvik.Phase
	lastPct   int
}

func newProgressPrinter(w io.Writer, tty bool) *progressPrinter {
	return &progressPrinter{w: w, tty: tty, lastPct: -1}
}

func (p *progressPrinter) update(pr zenvik.Progress) {
	pct := int(pr.Fraction*100 + 0.5)
	if p.tty {
		filled := pct / 5
		fmt.Fprintf(p.w, "\r[%s%s] %3d%% %-10s %s / %s", strings.Repeat("#", filled), strings.Repeat(".", 20-filled),
			pct, pr.Phase, formatSize(pr.BytesDone), formatSize(pr.BytesTotal))
		p.drawn = true
		return
	}
	if pr.Phase != p.lastPhase || pct >= p.lastPct+10 || (pct == 100 && p.lastPct != 100) {
		fmt.Fprintf(p.w, "%s %d%%\n", pr.Phase, pct)
		p.lastPhase, p.lastPct = pr.Phase, pct
	}
}

// done ends an in-place bar with a newline.
func (p *progressPrinter) done() {
	if p.tty && p.drawn {
		fmt.Fprintln(p.w)
	}
}

// isTerminal reports whether w is a character device such as a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// shellQuote renders args as a POSIX shell command line.
func shellQuote(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a != "" && !strings.ContainsAny(a, " \t\n'\"\\$`!*?[](){}<>|&;#~") {
			out[i] = a
			continue
		}
		out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(out, " ")
}
```

`cmd/zenvik/rip.go`:
```go
package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chad3814/zenvik"
	"github.com/chad3814/zenvik/internal/naming"
)

func newRipCmd() *cobra.Command {
	var playlist, outDir string
	var overwrite, dryRun bool
	cmd := &cobra.Command{
		Use:   "rip <path>",
		Short: "Remux a title (the main feature by default) to MKV",
		Long: `Remux one title of a Blu-ray ISO image or BDMV folder to an MKV file with
mkvmerge, keeping every video, audio and subtitle track, chapters and
languages. Without --playlist, the main feature is chosen automatically.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return usageError{fmt.Errorf("rip needs exactly one path, got %d", len(args))}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
			d, err := zenvik.Open(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			defer d.Close()
			t, err := pickTitle(d, playlist)
			if err != nil {
				return err
			}
			auto := playlist == ""
			if auto && t.Rank.Ambiguous {
				fmt.Fprintf(stderr, "warning: the main title is a close call (%s) — pass --playlist to choose\n", closeSecond(t))
			}
			out := filepath.Join(outDir, naming.SafeFileName(d.Name())+".mkv")
			fmt.Fprintf(stdout, "Ripping %s (%s) → %s\n", t.ID, formatDuration(t.Duration), out)
			if auto && len(t.Rank.Reasons) > 0 {
				fmt.Fprintf(stdout, "  chosen because: %s\n", strings.Join(t.Rank.Reasons, "; "))
			}
			prog := newProgressPrinter(stdout, isTerminal(stdout))
			res, err := d.Rip(cmd.Context(), t, zenvik.RipOptions{OutputPath: out, Overwrite: overwrite, DryRun: dryRun, OnProgress: prog.update})
			prog.done()
			if err != nil {
				return withHint(err)
			}
			for _, w := range res.Warnings {
				fmt.Fprintf(stderr, "warning: %s\n", w)
			}
			if dryRun {
				fmt.Fprintln(stdout, shellQuote(res.Command))
				return nil
			}
			fmt.Fprintf(stdout, "Done: %s\n", res.OutputPath)
			return nil
		},
	}
	cmd.Flags().StringVarP(&playlist, "playlist", "p", "", "rip this playlist (e.g. 00800) instead of the main feature")
	cmd.Flags().StringVarP(&outDir, "output-dir", "o", ".", "directory for the MKV file")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing output file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the output path and mkvmerge command without ripping")
	return cmd
}

// pickTitle returns the requested playlist, or the main title.
func pickTitle(d *zenvik.Disc, playlist string) (*zenvik.Title, error) {
	if playlist != "" {
		t, err := d.Title(playlist)
		if err != nil {
			return nil, usageError{err}
		}
		return t, nil
	}
	if m := d.Main(); m != nil {
		return m, nil
	}
	return nil, errors.New("no title qualifies as the main feature; run `zenvik info --all` and pass --playlist")
}

// withHint adds advice for errors a user can fix.
func withHint(err error) error {
	switch {
	case errors.Is(err, zenvik.ErrOutputExists):
		return fmt.Errorf("%w (use --overwrite to replace it)", err)
	case errors.Is(err, zenvik.ErrMkvmergeNotFound):
		return fmt.Errorf("%w (install MKVToolNix: https://mkvtoolnix.download/)", err)
	}
	return err
}
```

Modify `cmd/zenvik/main.go`:
1. In `main`, after `signal.NotifyContext`, add a goroutine so a second signal terminates immediately:
   ```go
   	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
   	go func() {
   		<-ctx.Done()
   		stop() // restore default handling: a second Ctrl-C exits at once
   	}()
   ```
2. In `exitCode`, before the default, add:
   ```go
   	case errors.Is(err, zenvik.ErrMkvmergeNotFound), errors.Is(err, zenvik.ErrMkvmergeTooOld):
   		return 4
   ```
3. In `newRootCmd`, add `root.AddCommand(newRipCmd())` after `newInfoCmd()`.

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
go test -race ./cmd/zenvik/ && go test -tags integration -run TestRipCommand -v ./cmd/zenvik/
go vet ./... && golangci-lint run && golangci-lint run --build-tags integration
```
Expected: PASS; 0 lint issues.

- [ ] **Step 5: Commit**

```bash
git add cmd/zenvik
git commit -m "Add zenvik rip command with progress, hints and exit code 4"
```

---

### Task 8: CI, documentation and milestone verification

**Files:**
- Modify: `.github/workflows/ci.yml`, `README.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: everything.
- Produces: CI that runs the integration suite with MKVToolNix and ffmpeg on macOS and Linux; user docs for `rip`; a verified branch.

- [ ] **Step 1: Update CI**

In `.github/workflows/ci.yml`:
- Replace the `integration-macos` job's steps.
- Add an `integration-linux` job.
- The `test` matrix stays as is: its Linux and Windows runs exercise the per-OS mount unit tests.

```yaml
  integration-macos:
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: brew install mkvtoolnix ffmpeg
      - run: mkvmerge --version && ffmpeg -version | head -1
      - run: go test -tags integration ./...

  integration-linux:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: sudo apt-get update && sudo apt-get install -y mkvtoolnix ffmpeg
      - run: mkvmerge --version && ffmpeg -version | head -1
      - run: go test -tags integration ./...
```

- [ ] **Step 2: Update the docs**

Append to the README's `## Usage` block:
````markdown
zenvik rip <path>                     # remux the main feature to ./<disc name>.mkv
zenvik rip -p 00801 -o ~/Movies <path> # a specific playlist, into ~/Movies
zenvik rip --dry-run <path>           # show the output path and mkvmerge command
````

Then add these sections after Usage:
````markdown
## Requirements

- [MKVToolNix](https://mkvtoolnix.download/) 80 or newer (`mkvmerge` on your `PATH`).
- Ripping from an ISO mounts it read-only for the duration of the rip:
  - macOS: `hdiutil` (built in).
  - Linux: `udisksctl` (package `udisks2`). Headless systems may need polkit permission.
  - Windows: PowerShell `Mount-DiskImage` (built in).
  If mounting isn't possible, mount or extract the image yourself and pass the folder.

## Cleaning up after a crash

Press Ctrl-C once to stop a rip cleanly. A second Ctrl-C exits immediately and may leave
the image mounted or a `.partial` file behind. To remove a leftover mount:

- macOS: `hdiutil info`, then `hdiutil detach -force <mount point>` (zenvik mounts under a
  temporary directory named `zenvik-mount-*`).
- Linux: `udisksctl unmount -b /dev/loopN && udisksctl loop-delete -b /dev/loopN`.
- Windows: `Dismount-DiskImage -ImagePath <image>`.
````

In `CLAUDE.md`, change the integration-test line to:
```
- Integration tests (need `mkvmerge` and `ffmpeg`; macOS also exercises `hdiutil`): `go test -tags integration ./...`
```

- [ ] **Step 3: Run the full verification suite**

```bash
test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }
go vet ./...
GOOS=linux go vet ./... && GOOS=windows go vet ./... && GOOS=freebsd go vet ./...
golangci-lint run && golangci-lint run --build-tags integration
go test -race ./...
go test -tags integration ./...
CGO_ENABLED=0 go build ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
grep -A3 '^require' go.mod
```
Expected: `gofmt -l` prints nothing; every command exits 0; `go.mod` still lists only cobra as a direct requirement.

- [ ] **Step 4: Smoke-test the binary**

```bash
go build -o /tmp/zenvik-m3 ./cmd/zenvik
/tmp/zenvik-m3 rip --help
PATH=/nonexistent /tmp/zenvik-m3 rip . ; echo "exit $?"
rm -f /tmp/zenvik-m3
```
Expected: help lists `-p/--playlist`, `-o/--output-dir`, `--overwrite`, `--dry-run`. The second command exits non-zero with a clear error: `.` (the worktree) is not a disc, so expect exit 3 with "unsupported source".

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml README.md CLAUDE.md
git commit -m "Run integration tests with MKVToolNix and ffmpeg in CI; document rip"
```

- [ ] **Step 6: Review the branch**

Run: `git log --oneline origin/main..HEAD && git status`
Expected: the plan commit plus one commit per task (8), a clean tree, and nothing pushed.
