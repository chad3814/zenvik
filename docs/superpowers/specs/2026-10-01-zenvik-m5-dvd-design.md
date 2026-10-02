# zenvik M5 — DVD (VIDEO_TS) Design

- **Date:** 2026-10-01
- **Status:** Implemented (see section 10 for where the shipped code differs from the reviewed draft)
- **Extends:** [zenvik v1 design](2026-10-01-zenvik-v1-design.md). Everything there still applies unless this document says otherwise.

## 1. Purpose and scope

M5 lets zenvik remux **unencrypted DVD-Video** discs to MKV with the same UX as Blu-ray: automatic main-feature detection, every track kept with correct metadata, chapters, config/presets and naming templates.

### Goals

- Inputs: a **DVD ISO image**, a **`VIDEO_TS` folder**, or a folder containing `VIDEO_TS`.
- Rip any title whose content is made of **whole title VOB files** (most movie main features, "play all" titles).
- Detect CSS encryption and report it. Never decrypt.
- `info`, `rip` and `doctor` behave the same for DVDs as for Blu-ray.

### Non-goals (M5)

- **Titles that start or end in the middle of a VOB file.** This includes most TV episodes that share a title set. They are not ripped: `rip` refuses them, the default `info` listing hides them as filtered, and `info --all` shows each with its reason. These titles are ripped since M6; see the [M6 spec](2026-10-02-zenvik-m6-dvd-cells-design.md).
- **Pipes or temp copies of video data.** mkvmerge v102 cannot read MPEG-PS from a FIFO (it aborts with `mm_io::seek_x`) or from stdin. M5 therefore only passes real VOB files to mkvmerge.
- Angles other than angle 1, menus, closed captions (line 21), and CSS decryption.

## 2. Architecture

```
zenvik/
├── dvd/                      # NEW public parser: VIDEO_TS.IFO (VMG) and VTS_nn_0.IFO (VTSI)
├── scan_dvd.go               # NEW: build Titles from a parsed DVD
├── rip_dvd.go                # NEW: DVD mkvmerge job, track mapping, chapter file
├── encrypt_dvd.go            # NEW: CSS (PES scrambling) detection
├── internal/source/          # + VideoTSDir kind, DVD format, VIDEO_TS detection in folders and ISOs
├── internal/mux/             # + Job.Concat (parenthesized input group) and Job.Extra (more inputs)
├── internal/vobsub/          # NEW: extracts DVD subpictures from title VOBs to a VobSub .idx/.sub
└── internal/testdisc/        # + synthetic VIDEO_TS builder, ISO variant, dvdauthor fixture
```

- `dvd/` is standalone like `bluray/`. It knows nothing about zenvik and does no I/O beyond `io.ReaderAt` / `fs.FS`.
- The data flow for a DVD is as follows:
  1. `Open` detects the source and reads the IFOs through `fs.FS` (UDF for an ISO, `os.DirFS` for a folder).
  2. It builds the `Titles`, then checks rippability and encryption, then ranks them.
  3. `Rip` mounts the source if it is an ISO, extracts the title's subtitles to a temporary VobSub file, then runs `mkvmerge` on the title's VOB files plus that file.
- No new third-party dependencies.

## 3. Library API changes (additive)

```go
const VideoTSDir SourceKind = 3   // String(): "VIDEO_TS folder"

type Format = source.Format       // Bluray or DVD
const (Bluray = source.Bluray; DVD = source.DVD)

type Disc struct {
  // ...existing fields...
  Format Format // Bluray or DVD
}

type Title struct {
  // ...existing fields...
  Unsupported string // "" if rippable; otherwise the reason (DVD only)
}

type VideoTrack    struct { /* ... */ AspectRatio string }  // DVD only: "4:3" or "16:9"
type AudioTrack    struct { /* ... */ Description string }  // DVD only: e.g. "Director's Commentary"
type SubtitleTrack struct { /* ... */ Description string }  // DVD only: e.g. "Forced"

const CodingVobSub bluray.CodingType = 0xFF // Codec of DVD subtitle tracks

const PhaseSubtitles Phase = 5 // String(): "extracting subtitles"

var ErrUnsupportedTitle = errors.New("zenvik: title is not supported") // CLI exit 1
```

- **Title `ID`:** for a DVD, this is the two-digit title number, `"01"`…`"99"`. `Disc.Title` also accepts the unpadded numbers `"1"`…`"9"` for a DVD.
- **`Title.Clips`:** lists the VOB files the title covers, by the file names found on the disc (`In` and `Out` are zero).
- **`Disc.Meta`:** `nil` for DVDs.
- **`Disc.Label`:** the UDF volume label for an ISO, or the name of the folder that contains `VIDEO_TS`.
- **`Rip`:**
  - It returns `ErrUnsupportedTitle` (with the reason) for a title with `Unsupported != ""`. The CLI exits 1 for it.
  - It returns `ErrEncrypted` for an encrypted title. The CLI exits 3.
  - Both checks happen before any mounting.
- **`Open`:** returns `ErrEncrypted` only when every title is encrypted, as for Blu-ray.
- **JSON `kind`:** `"video_ts"` for `VideoTSDir`. A DVD ISO keeps `"iso"`. To tell the formats apart, JSON gains `"format": "bluray" | "dvd"`. The JSON of a DVD title also carries `unsupported`, `aspect_ratio` and `description` when present (see section 7).

## 4. The `dvd` package

### 4.1 VMG (`VIDEO_TS.IFO`)

- The magic is `DVDVIDEO-VMG`.
- The package parses the title search pointer table (TT_SRPT). Each entry gives the title number, angle count, chapter (PTT) count, title set number (VTSN) and the title's index within the set (VTS_TTN).

### 4.2 VTSI (`VTS_nn_0.IFO`)

The magic is `DVDVIDEO-VTS`. The package parses:

- **Video attributes:**
  - coding: MPEG-1 or MPEG-2
  - standard: NTSC or PAL
  - aspect ratio: 4:3 or 16:9
  - resolution
- **Audio attributes** (up to 8 streams):
  - coding: AC-3, MPEG-1, MPEG-2 ext, LPCM or DTS
  - channel count
  - language code (ISO 639-1, from the IFO)
  - code extension: normal, visually impaired, director's commentary or alternate commentary
- **Subpicture attributes** (up to 32 streams):
  - language code
  - code extension: normal, large, children, closed caption, forced or commentary
- **Part-of-title table (VTS_PTT_SRPT):** maps each title and chapter to its PGC and program number.
- **PGC table (VTS_PGCITI):** for each PGC:
  - playback time, with its frame rate
  - audio stream control (present flag and physical stream number)
  - subpicture stream control (physical stream numbers per display mode)
  - the 16-entry YCrCb palette
  - the program map (the first cell of each program)
  - cell playback info: cell type and block mode (for angles), playback time, first sector, last sector
  - cell position info: VOB ID and cell ID

### 4.3 Errors and robustness

- Structural problems wrap `dvd.ErrCorrupt`. Examples: bad magic, a table offset or entry past the end of the file, a count out of range, a program map that references a missing cell.
- BCD playback times with invalid digits are corrupt.
- The parsers tolerate quirks that real discs have and libdvdread accepts, rather than rejecting the disc: audio and subpicture counts are read as a single byte (at `0x203` and `0x255`), a missing or reserved frame rate reads as 30 fps with an out-of-range frame count clamped, and a part-of-title table that is short or has offsets past its end yields titles with no chapters or fewer chapters.
- The `dvd` package maps IFO ISO 639-1 codes to ISO 639-2/B with `dvd.Language6392`.
- Fuzz targets: `FuzzParseVMG` and `FuzzParseVTS`.

## 5. Scanning (`scan_dvd.go`)

For each TT_SRPT entry, scanning resolves the title set and the title's first PGC, then builds a `Title`:

- **Duration:** the PGC playback time.
- **Chapters:** one per part-of-title (PTT) entry of the title, which names a program. Each chapter starts at the cumulative playback time of the cells before the program's first cell. Only angle-1 cells count.
- **Video:** one track from the VTS video attributes.
- **Audio:** the streams the PGC marks as present. Each gets its codec, channels, language and a code-extension label.
- **Subtitles:** the streams the PGC's subpicture control lists, each with language and type. The stream number is the 16:9 (wide) entry when the video is 16:9 and the 4:3 entry otherwise.
- **Angles:** from TT_SRPT.
- **Size:** the sum of the sectors (times 2048) of the angle-1 cells. This is also set for unsupported titles, and it feeds the decoy filter in section 5.3.

### 5.1 Whole-file check

- Title VOB sectors are relative to the start of `VTS_nn_1.VOB`, with the title set's VOBs `_1`…`_9` concatenated. File boundaries come from the real file sizes, which must be multiples of 2048. If a size is not (or a file is empty), every title in the title set is unsupported, with the reason `<VOB>: size is not a multiple of 2048`, where `<VOB>` is the file name (for example `VTS_01_2.VOB: size is not a multiple of 2048`).
- Take the angle-1 cells in play order. A non-angle cell is always included. In an angle block, only the first cell is included.
- The title is **rippable** when all three of these hold:
  1. Every cell starts on the sector after the previous cell's last sector.
  2. The first cell starts exactly at a file boundary.
  3. The last cell ends exactly at the last sector of some file, which may be the same file.
- `Clips` lists the covered files, in order.
- Otherwise, `Unsupported` is set to the first applicable reason:
  - `"cells are not contiguous (not supported yet)"`
  - `"starts or ends mid-file (not supported yet)"`
- A title whose chapters (PTT entries) point at more than one program chain is unsupported with `"spans several program chains (not supported yet)"`. Scanning checks this first. Only the first PGC's cells are otherwise considered.
- Other reasons: `has no cells`, `title set N has no title M`, `missing VTS_nn_0.IFO`, `missing VTS_nn_1.VOB`, and `unreadable <file>: …` or `<file>: <parse error>` when a file can't be read or parsed. A problem with a title set's files makes every title in the set unsupported.
- A multi-angle title is rippable only if the whole-file check passes with angle 1 alone. That normally fails, because angle blocks interleave.

### 5.2 Encryption

- For each VOB in the title set, scanning samples up to 64 packs, spread evenly through the file.
- If any MPEG-PS PES packet in the samples has `PES_scrambling_control != 0`, the title is encrypted.
- A read error is not treated as unencrypted: the title set's titles become unsupported with `unreadable <VOB>: …`.

### 5.3 Ranking

- `internal/rank` gets the same candidate fields as Blu-ray: duration, chapter count and track counts.
- Duplicates are titles whose angle-1 cells match one for one: each cell's (VTSN, VOB ID, cell ID) key and its duration (the clip `Out`) are identical, in order. The one with the lower title number is kept.
- Titles with `Unsupported` set are filtered, with their reason. `min_duration` applies.
- The decoy-rate filter applies to DVDs as it does to Blu-ray: a title whose data rate (`Size` over `Duration`) is under 1% of the highest rate among the unfiltered titles is filtered as a likely decoy. Candidates with an unknown size are left alone.
- Titles are listed in the same order as Blu-ray: the main title, other scored titles, duplicates, then filtered titles.

## 6. Ripping (`rip_dvd.go`)

1. Reject the title if it is unsupported or encrypted.
2. If the source is an ISO, mount it with `internal/mount`. Mount records and doctor leftovers are unchanged. The VOB paths are resolved under the mount point or the folder. File names are matched case-insensitively.
3. Identify the tracks with `mkvmerge -J <first VOB>` and map each video and audio track to its IFO stream by MPEG-PS stream key (`stream_id<<8 | sub_stream_id` for private stream 1, else `stream_id`):
   - video `0x00E0`
   - MPEG audio `0x00C0+n`
   - AC-3 `0xBD80+n`
   - DTS `0xBD88+n`
   - LPCM `0xBDA0+n`
   - subpicture `0xBD20+n` (not read by mkvmerge; see 6.1)

   Here `n` is the physical stream number from the PGC control tables.
4. Extract the title's subtitles (6.1), if it has any.
5. Write the chapter file: mkvmerge's simple (OGM) text format (`CHAPTERnn=HH:MM:SS.mmm` and `CHAPTERnnNAME=Chapter nn`) in a temp file, deleted after the rip.
6. Run `mkvmerge -o <out> --chapters <chapters.txt> [per-track options] --track-order … --no-chapters ( VTS_nn_k.VOB … VTS_nn_m.VOB ) [per-track options] <subtitles.idx>`. The title VOBs are always one parenthesized group, even a single file (see section 10), and the subtitle file is a second input. Per-track options:
   - `--language` from the IFO, as ISO 639-2/B.
   - `--track-name` is the codec and layout, then the description in parentheses, not a language name: audio `AC-3 5.1` or `AC-3 Stereo (Director's Commentary)`; subtitles carry only the description (`Forced`, `Large`, `Commentary`, …) and have no name when the disc gives none.
   - `--default-track-flag`: the first video track and the first audio track are default, and no subtitle track is default.
   - Tracks the IFO does not describe are kept, with no language and no name.
7. Progress, atomic output and `--dry-run` work the same as for Blu-ray, with one more phase, `PhaseSubtitles` ("extracting subtitles"), reported before scanning. A dry run mounts an ISO, as Blu-ray does, to identify the tracks, but extracts and writes nothing: the command shows the placeholders `<subtitles.idx>` and `<chapters.txt>`.

### 6.1 Subtitles

- mkvmerge v102 does not demux DVD subpictures from VOB files (Task 1), so there is no VobSub track or palette to carry through.
- zenvik extracts them itself (`internal/vobsub`): it copies every private-stream-1 pack of the wanted sub-streams (`0x20+n`) into a temporary `subtitles.sub` and writes `subtitles.idx` with the PGC's palette (YCrCb converted to RGB with the BT.601 studio-range matrix), the video size and one `timestamp`/`filepos` entry per subtitle.
- Timestamps come from each VOBU's NAV pack: the DSI gives the VOB ID, cell ID and cell elapsed time, the PCI gives the VOBU start PTM, and the cell's start in the title comes from the IFO cell list.
- mkvmerge reads the `.idx`/`.sub` pair as a second input. Streams that have no subtitle in this title are skipped, with a warning.
- The temporary directory is removed after the rip, and on failure or cancellation.

## 7. CLI

- `rip --title <id>` (`-t`) is added as an alias of `--playlist` (`-p`). It accepts Blu-ray playlist numbers and DVD title numbers (`3` and `03` both work). Both flags are documented, and passing both is a usage error (exit 2).
- `info` works as follows:
  - The header shows the source kind (`DVD ISO image` or `VIDEO_TS folder`).
  - Unsupported titles are filtered, so `info` hides them unless `--all` is given; with `--all` their reason is in the notes column.
  - The JSON always includes every title, with `format` and, when set, `unsupported`; video adds `aspect_ratio` and audio and subtitles add `description`. DVD subtitle tracks have codec `VobSub`.
- Naming:
  - `{playlist}` renders the title ID.
  - `{label}` renders the volume label or folder name.
  - `{name}` falls back from `--name` to the cleaned label, then `untitled`.
- `doctor` is unchanged.

## 8. Testing

### 8.1 Unit tests

`internal/testdisc` gains a VIDEO_TS builder. It writes real IFO structures and VOB files made of valid 2048-byte MPEG-PS packs, with scrambling bits set on demand. The scenarios are:

- A movie title that covers two whole VOB files.
- A title set holding three episodes. Each episode starts or ends mid-file, so it is unsupported. It also has a "play all" title that covers all the files, which is rippable.
- Extras shorter than `min_duration`.
- A duplicate title.
- A two-angle title.
- An encrypted title set.
- Mixed-case file names, and AppleDouble `._*` files.
- The same disc inside a UDF 1.02 ISO, written by `internal/testdisc/udfimage` (extended for the 1.02 layout if needed).

Each layer gets its own tests:

- The `dvd` parser has table tests for corrupt input and the fuzz targets.
- Source detection, scanning, ranking and DVD argument building run against the synthetic discs, with fake mkvmerge output.
- The CLI has `info`/`rip` tests on DVDs and a JSON `format`/`kind` test.

### 8.2 Integration tests (`-tags integration`, CI integration jobs)

1. Build a real authored DVD:
   - ffmpeg renders a short NTSC MPEG-PS with two AC-3 tracks (eng, fra).
   - `spumux` adds a subtitle stream.
   - `dvdauthor` builds a `VIDEO_TS` with chapters.
2. Split the title VOB at a pack boundary into `VTS_01_1.VOB` and `VTS_01_2.VOB`. The IFO stays valid, because its sector addresses are relative to the concatenated title VOBs.
3. Rip from the folder and from an ISO:
   - The ISO is built by `internal/testdisc/udfimage` (UDF 1.02), not by `hdiutil makehybrid` or `genisoimage`.
   - The ISO rip runs on macOS only, as for Blu-ray. Linux CI does not mount ISOs.

   The folder rip and the ISO rip go through the real mount path where it is available.
4. Assert:
   - the duration
   - the audio languages and names
   - the chapter count and times, each within one frame
   - that the subtitle track is present, and that its first subtitle starts at 2.0 s (within 100 ms)
   - that no mounts are left over

The authored title is 15 s, shorter than `min_duration`, so the tests select title `01` explicitly instead of relying on main-feature detection.

CI installs `dvdauthor` on macOS and Linux.

### 8.3 Verification first

Like M3's MPLS check, the first plan task verifies these points on dvdauthor-built discs, locally with MKVToolNix v102 and in CI with Ubuntu's mkvtoolnix:

1. How `mkvmerge -J` reports MPEG-PS stream IDs.
2. That `+` appending across split VOBs keeps A/V sync and the full duration.
3. VobSub palette handling.
4. That IFO chapter times line up with mkvmerge's timeline.

Results are in `docs/superpowers/notes/2026-10-01-m5-mkvmerge-dvd.md`. Checks 2 and 3 failed as assumed here and the design changed (section 10). The minimum MKVToolNix is still 80.0, but the DVD behavior was verified only on v102.

## 9. Rollout (plan tasks)

1. Verify mkvmerge's DVD behavior (§8.3).
2. The `dvd/` IFO parsers.
3. The synthetic VIDEO_TS builder, plus the ISO variant.
4. Source detection.
5. DVD scan: titles, tracks, chapters, the whole-file check, CSS detection and ranking.
6. DVD rip: mounting, mkvmerge arguments (the title VOBs as one `( … )` group), track mapping and the chapter file. There is no palette fallback; subtitles are Task 10.
7. CLI: `--title`, `info` output, JSON `format`/`kind`.
8. Integration tests and CI.
9. Docs and syncing the v1 spec. That covers §1 non-goals, §3 `SourceKind` and §11.
10. (Added after Task 1) Extract DVD subtitles to a VobSub `.idx`/`.sub` pair, because mkvmerge ignores DVD subpictures in VOBs: `internal/vobsub`, `mux.Job.Extra`, and the rip integration. Task 6 maps video and audio only.

Tasks 2 and 3 can run in parallel. Once Task 5 is done, Tasks 6 and 7 can run in parallel.

## 10. Implementation notes

Where the shipped code differs from the draft that was reviewed. The code is the reference.

- **Track names.** They carry the codec, layout and description, not a language name; the language is set on the track. Audio: `AC-3 5.1`, `AC-3 Stereo (Director's Commentary)`. Subtitles: `Forced`, `Large`, …, or no name. The layout comes from what mkvmerge reports. The first video and first audio tracks are default.
- **Chapter file.** It uses mkvmerge's simple OGM text format, not Matroska XML. Chapters are named `Chapter 01`, ….
- **Chapters come from the PTT table**, one per part-of-title entry, not from every program in the program map.
- **New public API.** `Disc.Format` (with `Format`, `Bluray`, `DVD`), `Title.Unsupported`, `VideoTrack.AspectRatio`, `AudioTrack.Description`, `SubtitleTrack.Description`, `CodingVobSub`, `PhaseSubtitles` and `ErrUnsupportedTitle`. `Disc.Title` accepts unpadded DVD numbers.
- **`rip --title`/`-t`** is an alias of `--playlist`/`-p`; passing both is a usage error.
- **JSON** adds `format`, `unsupported`, `aspect_ratio` and `description`. `kind` is `"video_ts"` for a VIDEO_TS folder; a DVD ISO keeps `"iso"`.
- **Unsupported titles are filtered** (hidden without `info --all`), because the ranker treats the unsupported reason as a problem. `rip` of one exits 1 (`ErrUnsupportedTitle`); an encrypted disc exits 3.
- **Unsupported reasons.** Besides the two in section 5.1 there are `spans several program chains (not supported yet)` and `<VOB>: size is not a multiple of 2048`. The last is more specific than the draft's "VOB size not a multiple of 2048" and applies to every title in the title set. Missing or unreadable files and IFO parse errors also make a set's titles unsupported, with the reason.
- **Title size** is the sum of the angle-1 cell sectors, not of the VOB file sizes, so unsupported titles have a size too. The decoy-rate filter (under 1% of the highest data rate) applies to DVDs.
- **Dry run.** Like Blu-ray, it mounts an ISO (to identify the tracks) and does not show a mount placeholder. The chapter file appears as `<chapters.txt>` and the subtitle file as `<subtitles.idx>`.
- **Integration ISOs** are built by `internal/testdisc/udfimage` (UDF 1.02); `hdiutil makehybrid` and `genisoimage` were not needed. `hdiutil` attaches them on macOS. Linux CI does not mount ISOs, as for Blu-ray. The tests select title `01` explicitly because the 15 s authored title is below `min_duration`.
- **Subtitles.** mkvmerge ignores DVD subpictures inside VOBs. zenvik extracts them to a temporary VobSub `.idx`/`.sub`, with the IFO palette (converted from YCrCb to RGB with the BT.601 studio-range matrix) and NAV-pack-based timestamps (DSI VOB ID, cell ID and cell elapsed time, PCI VOBU start PTM, IFO cell starts), and muxes that file as a second input. The dry run shows `<subtitles.idx>`. This replaces the palette fallback of the draft's section 6.1.
- **Title VOBs are passed as mkvmerge's `( a.VOB b.VOB )` group,** even for one file. mkvmerge chains sibling VOBs on its own (`VTS_01_1.VOB` alone is already the whole title), so `+` duplicated content (22.8 s instead of 15 s in Task 1's measurement). A single parenthesized file turns auto-chaining off.
- **Parser tolerance.** The IFO parsers accept real-disc quirks as libdvdread does (single-byte stream counts, a missing frame rate reads as 30 fps, clamped frame counts, short PTT tables). Only structural problems and invalid BCD digits are corrupt. It adds `sh` to `scr` and `mo` to `rum` in the language map.
- **Progress.** `PhaseSubtitles` ("extracting subtitles") runs before `PhaseScanning`, so a DVD rip with subtitles reads the title once before muxing.
- **Authored fixture (Task 8).** Integration tests select title 01 explicitly; the first subtitle is asserted at 2.0 s.
