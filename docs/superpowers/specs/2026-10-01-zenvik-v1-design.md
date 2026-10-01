# zenvik v1 — Design

- **Date:** 2026-10-01
- **Status:** Draft for review
- **Repo:** `github.com/chad3814/zenvik` (MIT)

## 1. Purpose

zenvik is an open-source, user-friendly tool for turning Blu-ray disc images into MKV files. It is a friendlier alternative to `makemkvcon` for **already-unencrypted** sources: it finds the main feature automatically, names output files sensibly, and keeps every track with correct metadata.

Audience: the author's personal collection first, structured so it can be published later.

### Goals (v1)

- Inputs: Blu-ray **ISO image** or **BDMV directory**. UHD Blu-ray works as plain Blu-ray.
- Output: lossless **remux** of a selected title to MKV with all video, audio, and subtitle tracks, chapters, correct languages, track names, and default flags.
- Automatic **main-feature detection** with explainable, overridable results.
- **Config file + presets** and **output naming templates**.
- Two deliverables: a Go **library** that does all the work, and a **CLI** built on it. A future GUI uses the same library.

### Non-goals (v1)

- **No decryption** of any kind (CSS, AACS, BD+). Encrypted sources are detected and reported.
- No physical drive access. No MakeMKV dependency.
- No transcoding (the muxer abstraction leaves room for it later).
- No track-selection rules (all tracks are kept).
- No online metadata lookup.
- No BD-J execution. No HDR10+/Dolby Vision special handling. Multi-angle titles rip angle 1 only.
- DVD (`VIDEO_TS`) is milestone 5 and gets its own spec.
- No release packaging (goreleaser) in v1.

## 2. Architecture

A single Go module `github.com/chad3814/zenvik`, built with Go 1.27, with **no cgo**. Muxing is delegated to the external `mkvmerge` executable (MKVToolNix), which is invoked as a separate process. It is not linked, because MKVToolNix has no public library API and is GPL-2.0.

```
zenvik/
├── zenvik.go            # public facade: Open, Disc, Title, Rip, FormatName, options, errors
├── udf/                 # public: UDF reader → io/fs.FS over io.ReaderAt
├── bluray/              # public: index.bdmv, MovieObject.bdmv, MPLS, CLPI, bdmt meta parsers
├── internal/
│   ├── source/          # detect input kind (ISO | BDMV dir); expose fs.FS
│   ├── mount/           # OS mount helpers (build tags: darwin, linux, windows)
│   ├── rank/            # main-feature detection
│   ├── mux/             # Muxer interface + mkvmerge implementation
│   ├── config/          # TOML config + presets
│   ├── naming/          # filename templates
│   └── testdisc/        # synthetic BDMV builder for tests
└── cmd/zenvik/          # CLI
```

- The root package `zenvik` is the only API the CLI and GUI depend on.
- `udf` and `bluray` are public, standalone parsers. They know nothing about zenvik and do no I/O beyond `io.ReaderAt` / `fs.FS`.
- Everything else is `internal/` until its API stabilizes.
- DVD later adds a `dvd/` parser and a new source kind. The facade API does not change.

**Data flow:** `zenvik.Open(path)` → detect source → parse BDMV via `fs.FS` (UDF for ISO, `os.DirFS` for directories) → build `Disc{Titles}` → detect encryption → rank → `disc.Rip(ctx, title, opts)` → mount the ISO if needed → `mkvmerge` → progress events → MKV.

**Third-party Go dependencies:** `github.com/spf13/cobra` (CLI) and `github.com/pelletier/go-toml/v2` (config). Everything else uses the standard library.

## 3. Library API (`package zenvik`)

```go
func Open(ctx context.Context, path string, opts ...OpenOption) (*Disc, error) // e.g. WithMinDuration(d)
func FormatName(tmpl string, d *Disc, t *Title, vars NameVars) (string, error)

type SourceKind int // ISO, BDMVDir (later: VideoTSDir)

type Disc struct {
  Path   string
  Kind   SourceKind
  Label  string       // volume label (ISO) or directory name
  Meta   *DiscMeta    // from BDMV/META/DL/bdmt_*.xml; nil if absent
  Titles []*Title     // ranked; Titles[0] is the best main-feature candidate
}
func (d *Disc) Main() *Title                    // nil if no title qualifies
func (d *Disc) Title(id string) (*Title, error) // by playlist id, e.g. "00800"
func (d *Disc) Rip(ctx context.Context, t *Title, opts RipOptions) (*RipResult, error)
func (d *Disc) Close() error                    // unmounts anything zenvik mounted

type Title struct {
  ID        string        // MPLS number, "00800"
  Duration  time.Duration
  Size      int64         // sum of referenced clip file sizes
  Clips     []Clip        // m2ts name + in/out times
  Chapters  []Chapter
  Video     []VideoTrack  // codec, resolution, frame rate, HDR flag, PID
  Audio     []AudioTrack  // codec, language, channels, PID
  Subtitles []SubtitleTrack // codec, language, PID
  Angles    int
  Encrypted bool
  Rank      RankInfo      // Score, IsMain, Ambiguous, DuplicateOf, Filtered, Reasons []string
}

type RipOptions struct {
  OutputPath   string         // final path; naming is resolved by the caller (e.g. via FormatName)
  MkvmergePath string         // mkvmerge executable; empty searches PATH
  Overwrite    bool
  DryRun       bool           // resolve everything and return the command without running it
  OnProgress   func(Progress) // optional; called from the muxing goroutine
}
type Progress struct {
  Phase      Phase   // Mounting, Scanning, Muxing, Finalizing
  Fraction   float64 // 0..1 within the current phase
  BytesDone  int64
  BytesTotal int64
}
type RipResult struct { OutputPath string; Duration time.Duration; Warnings []string; Command []string }
```

- Ranking runs inside `Open`, so all consumers see the same ordering and reasons.
- Cancellation uses `ctx`: mkvmerge is killed and the partial file deleted.
- Progress is a callback, not a channel, so there is nothing to drain or leak.
- Sentinel errors (checked with `errors.Is`): `ErrUnsupportedSource`, `ErrEncrypted`, `ErrNoTitles`, `ErrMkvmergeNotFound`, `ErrMkvmergeTooOld`, `ErrMountUnavailable`, `ErrOutputExists`.

### Encryption detection

Blu-ray M2TS clips are stored in 6144-byte aligned units of 32 × 192-byte packets. When AACS-encrypted, only the first 16 bytes of each unit are in the clear. `Open` reads the first aligned unit of each referenced clip and checks for the TS sync byte `0x47` at offset 4 of every 192-byte packet. If any check after the first packet fails, the clip and its titles are marked `Encrypted`. A leftover `AACS/` directory is ignored, because decrypted backups often keep it. If every candidate title is encrypted, `Open` returns `ErrEncrypted`.

## 4. Main-feature detection (`internal/rank`)

**Pass 1: filter.** Mark titles `Filtered` (with a reason) if their duration is below `min_duration` (default 2m) or they have no video stream. Encrypted titles stay listed but are flagged.

**Pass 2: dedupe.** Titles with identical clip sequences and identical in/out times are duplicates. The lowest ID is kept; the others get `DuplicateOf`.

**Pass 3: score.** Initial weights live in one `Weights` struct and are tuned against golden tests:

| Signal | Initial weight |
|---|---|
| Duration | `100 × duration / longest candidate duration` |
| Any clip referenced more than once in the playlist | −40 |
| Fraction of play items shorter than 5 s | `−30 × fraction` |
| Chapter count | `+ min(chapters, 30) / 3` |
| Distinct audio + subtitle languages | `+ 2 × min(languages, 10)` |
| Playlist played by HDMV title 1 (`index.bdmv` → `MovieObject.bdmv` → `PlayPL` commands) | +15 |

Ties go to the lowest playlist ID.

**Pass 4: decide.** The top scorer gets `IsMain`. If the best non-duplicate runner-up scores within 5 points, `Ambiguous` is set and the reasons name the runner-up.

**Known limitation:** discs that choose their playlist in BD-J code get no `MovieObject` signal. They rely on the structural penalties alone and will produce most ambiguity warnings.

## 5. Muxing (`internal/mux`)

```go
type Muxer interface {
  Mux(ctx context.Context, job Job, onProgress func(Progress)) error
}
```

The mkvmerge implementation:

1. **Locate:** use `mkvmerge_path` from config, else `PATH`. Parse `mkvmerge --version`. The minimum is **MKVToolNix 80.0**. Milestone 3 starts by verifying MPLS identification and track-ID mapping on 80.0; if that fails, the minimum is raised to the oldest passing version and this spec is updated.
2. **Identify:** `mkvmerge -J <root>/BDMV/PLAYLIST/<id>.mpls` (mkvmerge reads MPLS natively, joins clips, and imports chapters). Map mkvmerge track IDs to `bluray` STN streams by PID. Like muxing, identification accepts exit code 1 (warnings).
3. **Build an options file** (`mkvmerge @opts.json`, a JSON array of arguments) with the output path, explicit `--language` per track from STN data, `--track-name` (e.g. "TrueHD 7.1", "DTS-HD MA 5.1"), and default flags: first video, first audio, no subtitles.
4. **Run** with `--gui-mode` and parse `#GUI#progress N%` lines into `Progress`. Write to `<output>.partial` and rename on success. On failure or cancel, delete the partial file. A non-zero exit returns an error containing mkvmerge's warning and error lines. mkvmerge exit code 1 (warnings) counts as success, and the warnings go into `RipResult.Warnings`.

## 6. Sources and mounting

- **BDMV directory:** accepts the directory containing `BDMV/` or the `BDMV/` directory itself. Uses `os.DirFS`. No mount is needed.
- **ISO, scanning:** the public `udf` package reads the image directly. No mount.
- **ISO, ripping:** mount read-only for the duration of `Rip`:

| OS | Mount | Unmount |
|---|---|---|
| macOS | `hdiutil attach -readonly -nobrowse -noautoopen -noverify -imagekey diskimage-class=CRawDiskImage -mountpoint <temp dir zenvik-mount-*> <iso>` | `hdiutil detach <dir>` (then `-force`) |
| Linux | `udisksctl loop-setup -r -f <iso>` then `udisksctl mount -b <loopdev>` | `udisksctl unmount -b`, then `udisksctl loop-delete -b` |
| Windows | PowerShell `Mount-DiskImage -ImagePath <iso> -PassThru \| Get-Volume` (drive letter) | `Dismount-DiskImage -ImagePath <iso>` |

After mounting, zenvik checks that `BDMV/index.bdmv` exists at the mount point. `Close()` unmounts only what zenvik mounted. If mounting isn't possible, `Rip` returns `ErrMountUnavailable`, and the message suggests extracting or mounting manually and passing the directory instead. If the process is killed hard, a mount may be left behind; this is documented. Mounts are recorded in `$XDG_STATE_HOME/zenvik/mounts` (or the user cache dir; a relative `XDG_STATE_HOME` is ignored, per the XDG spec) until detached, and `zenvik doctor` lists records whose process is gone, with the command to remove each. That command also deletes the record file. Mount tools run with `LC_ALL=C.UTF-8` (not `C`) so output parsing is locale-independent while GLib keeps non-ASCII mount paths intact; glibc falls back to the C locale where `C.UTF-8` is missing. On Windows, a liveness check that gets access-denied treats the process as alive.

### `udf` package

Read-only `fs.FS` over `io.ReaderAt`. Supports UDF 1.02–2.60, including the metadata partition (2.50 and later, used by Blu-ray), multi-extent files, and files over 4 GB. Exposes the volume identifier for `Disc.Label`. ISO 9660 is not supported, because Blu-ray images are UDF.

### `bluray` package

Typed parsers for `index.bdmv` (titles → movie object or BD-J), `MovieObject.bdmv` (navigation commands, enough to extract `PlayPL` targets), MPLS (play items, in/out times, STN stream tables, angles, playlist marks → chapters), CLPI (stream coding info: codec, language, resolution, frame rate, channels), and `META/DL/bdmt_*.xml` (disc title).

## 7. CLI (`cmd/zenvik`)

```
zenvik info <path> [--all] [--json]
zenvik rip  <path> [-p|--playlist ID] [-o|--output-dir DIR] [--name NAME] [--year YYYY]
                   [--template TMPL] [--preset NAME] [--overwrite] [--dry-run]
zenvik doctor
```

- **`info`:** a ranked table with columns ★ (main), ID, duration, size, chapters, video, audio languages, subtitle languages, and notes (duplicate, ambiguous, encrypted, angles). `--all` includes filtered titles with their reasons. `--json` prints the `Disc` model, with `"kind": "iso" | "bdmv"`.
- **`rip`:** rips the main title by default (or `--playlist`). It prints the chosen title and why, then the progress. When the result is ambiguous, it rips the top candidate and warns, naming the alternatives. `--dry-run` prints the resolved output path and the mkvmerge command without running it.
- **`doctor`:** checks that mkvmerge is found and its version, whether ISO mounting is possible on this OS, the config path and whether it parses, and any leftover zenvik mounts.
- **Progress:** an in-place progress bar on a TTY; plain periodic lines otherwise.
- **Signals:** SIGINT/SIGTERM cancel the context, so cleanup (partial-file removal, unmount) runs.
- **Exit codes:** `0` success, `1` failure, `2` usage error, `3` unsupported or encrypted source, `4` missing or too-old dependency.

## 8. Config and naming

**Config location:** `$XDG_CONFIG_HOME/zenvik/config.toml`, falling back to `~/.config/zenvik/config.toml` on macOS and Linux, and `%AppData%\zenvik\config.toml` on Windows. A missing file means built-in defaults. `XDG_CONFIG_HOME` is honored on every OS, including Windows.

```toml
output_dir    = "~/Movies"
template      = "{name}[ ({year})].mkv"
min_duration  = "2m"
mkvmerge_path = ""
preset        = ""

[presets.plex]
output_dir = "/Volumes/Media/Movies"
template   = "{name}[ ({year})]/{name}[ ({year})].mkv"
```

- **Precedence:** CLI flags > selected preset (`--preset`, else `preset`) > top-level config > built-in defaults. Built-in defaults: `output_dir` = current directory, `template` = `{name}[ ({year})].mkv`, `min_duration` = `2m`.
- Presets may override only `output_dir`, `template`, and `min_duration` in v1.
- An unknown preset name or an invalid value is an error (exit 2) that names the key. Unknown config keys are errors too, which catches typos. Durations are strings validated by zenvik, and negative durations are invalid.

**Template variables:**

| Variable | Source |
|---|---|
| `{name}` | `--name`, else the bdmt title, else the cleaned volume label (underscores → spaces, title case: `THE_MATRIX` → `The Matrix`) |
| `{year}` | `--year` only |
| `{label}` | Raw volume label |
| `{playlist}` | Playlist ID |

- `[...]` is an optional group, dropped if any variable inside it is empty. Groups do not nest.
- An unknown variable is an error.
- Characters invalid on any of macOS, Linux, or Windows (`<>:"\|?*` and control characters) are replaced with `_`. `/` in the template creates directories; `/` inside a variable's value is replaced.
- Every rendered path component is cleaned: surrounding spaces and trailing dots are trimmed, Windows reserved base names (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`) get a `_` suffix, and each component is cut to 247 bytes at a rune boundary (keeping the extension) so `<name>.partial` fits in 255 bytes. The final component gets `.mkv` if missing (case-insensitive). Empty, `.` or `..` components and absolute results are errors; a `\` in the template is an invalid character, not a separator.
- Paths are relative to `output_dir`, and `~` is expanded.
- An existing output file causes `ErrOutputExists` unless `--overwrite` is passed.

## 9. Testing

- **`internal/testdisc`:** a builder that writes valid `index.bdmv`, `MovieObject.bdmv`, MPLS, and CLPI files from a Go description. It is the main fixture source; no copyrighted disc data is committed.
- **Parser tests:** builder round-trips, plus hand-built edge cases (empty STN, multi-angle, extension data blocks).
- **Ranking golden tests:** simple movie, 3 duplicate copies of the main feature, 99-playlist obfuscation, TV-episode disc, extras-only disc, and an ambiguous pair. Each asserts the main ID and the `Ambiguous` flag.
- **UDF tests:** a test-only Go writer (`internal/testdisc/udfimage`) builds UDF 1.02 and 2.50 (metadata partition) images in memory, written separately from the reader. A darwin integration test mounts its output with `hdiutil` to check it against an independent implementation. One externally produced UDF 1.02 image, made by `hdiutil makehybrid` through the committed `scripts/gen-udf-fixtures.sh`, is committed under `udf/testdata/`. (`mkudffs` can't fill images, and no available tool fills a UDF 2.50 image with a metadata partition.)
- **Encryption tests:** a generated clean M2TS and a scrambled variant.
- **Naming and config:** table-driven tests covering precedence, optional groups, and sanitization.
- **Integration (`//go:build integration`):** `ffmpeg -f mpegts -mpegts_m2ts_mode 1` generates real clips, `testdisc` writes the BDMV, and real mkvmerge rips it. Asserts duration, track count, languages, chapters, progress events, cancellation cleanup, and `.partial` handling. ISO mount tests run on macOS (`hdiutil`).
- **Known gap:** Linux mounting needs polkit for `udisksctl`, which headless CI runners lack. It is verified manually and this is documented.
- **Optional corpus:** `ZENVIK_CORPUS=/path/to/backups` runs ranking checks against local decrypted backups. Nothing from it is committed.

## 10. Tooling

- `gofmt`, `go vet`, `golangci-lint`, `go test -race ./...`.
- GitHub Actions: lint and unit tests on macOS, Linux, and Windows; integration tests on macOS and Linux with MKVToolNix and ffmpeg installed.

## 11. Milestones

1. **Parsers:** `udf`, `bluray`, `internal/testdisc`.
2. **Scan:** `source`, `Open`, encryption detection, `rank`, `zenvik info`.
3. **Rip:** `mux` (mkvmerge), `mount`, `zenvik rip` with progress and cancellation.
4. **Polish:** `config`, presets, `naming`, `zenvik doctor`.
5. **DVD:** separate spec.
