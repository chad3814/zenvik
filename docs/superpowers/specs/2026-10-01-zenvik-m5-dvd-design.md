# zenvik M5 — DVD (VIDEO_TS) Design

- **Date:** 2026-10-01
- **Status:** Draft for review
- **Extends:** [zenvik v1 design](2026-10-01-zenvik-v1-design.md). Everything there still applies unless this document says otherwise.

## 1. Purpose and scope

M5 lets zenvik remux **unencrypted DVD-Video** discs to MKV with the same UX as Blu-ray: automatic main-feature detection, every track kept with correct metadata, chapters, config/presets and naming templates.

### Goals

- Inputs: a **DVD ISO image**, a **`VIDEO_TS` folder**, or a folder containing `VIDEO_TS`.
- Rip any title whose content is made of **whole title VOB files** (most movie main features, "play all" titles).
- Detect CSS encryption and report it. Never decrypt.
- `info`, `rip` and `doctor` behave the same for DVDs as for Blu-ray.

### Non-goals (M5)

- **Titles that start or end in the middle of a VOB file.** This includes most TV episodes that share a title set. They are listed and explained but not ripped. That is planned for a later milestone.
- **Pipes or temp copies of video data.** mkvmerge v102 cannot read MPEG-PS from a FIFO (it aborts with `mm_io::seek_x`) or from stdin. M5 therefore only passes real VOB files to mkvmerge.
- Angles other than angle 1, menus, closed captions (line 21), and CSS decryption.

## 2. Architecture

```
zenvik/
├── dvd/                      # NEW public parser: VIDEO_TS.IFO (VMG) and VTS_nn_0.IFO (VTSI)
├── scan_dvd.go               # NEW: build Titles from a parsed DVD
├── rip_dvd.go                # NEW: DVD mkvmerge arguments, track mapping, chapter file
├── internal/source/          # + VideoTSDir kind, VIDEO_TS detection in folders and ISOs
├── internal/mux/             # + DVD argument builder pieces (generic where possible)
└── internal/testdisc/        # + synthetic VIDEO_TS builder and ISO variant
```

- `dvd/` is standalone like `bluray/`. It knows nothing about zenvik and does no I/O beyond `io.ReaderAt` / `fs.FS`.
- The data flow for a DVD is as follows:
  1. `Open` detects the source and reads the IFOs through `fs.FS` (UDF for an ISO, `os.DirFS` for a folder).
  2. It builds the `Titles`, then checks rippability and encryption, then ranks them.
  3. `Rip` mounts the source if it is an ISO, then runs `mkvmerge` on the title's VOB files.
- No new third-party dependencies.

## 3. Library API changes (additive)

```go
const VideoTSDir SourceKind = 3   // String(): "VIDEO_TS folder"

type Title struct {
  // ...existing fields...
  Unsupported string // "" if rippable; otherwise the reason (DVD only)
}

var ErrUnsupportedTitle = errors.New("zenvik: title is not supported") // CLI exit 1
```

- **Title `ID`:** for a DVD, this is the two-digit title number, `"01"`…`"99"`.
- **`Title.Clips`:** lists the VOB files the title covers.
- **`Disc.Meta`:** `nil` for DVDs.
- **`Disc.Label`:** the UDF volume label for an ISO, or the name of the folder that contains `VIDEO_TS`.
- **`Rip`:**
  - It returns `ErrUnsupportedTitle` (with the reason) for a title with `Unsupported != ""`.
  - It returns `ErrEncrypted` for an encrypted title.
  - Both checks happen before any mounting.
- **JSON `kind`:** `"video_ts"` for `VideoTSDir`. A DVD ISO keeps `"iso"`. To tell the formats apart, JSON gains `"format": "bluray" | "dvd"`.

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
- Fuzz targets: `FuzzParseVMG` and `FuzzParseVTS`.

## 5. Scanning (`scan_dvd.go`)

For each TT_SRPT entry, scanning resolves the title set and the title's first PGC, then builds a `Title`:

- **Duration:** the PGC playback time.
- **Chapters:** one per program. Each chapter starts at the cumulative playback time of the cells before the program's first cell. Only angle-1 cells count.
- **Video:** one track from the VTS video attributes.
- **Audio:** the streams the PGC marks as present. Each gets its codec, channels, language and a code-extension label.
- **Subtitles:** the streams the PGC's subpicture control lists, each with language and type.
- **Angles:** from TT_SRPT.
- **Size:** the sum of the sizes of the VOB files in `Clips`.

### 5.1 Whole-file check

- Title VOB sectors are relative to the start of `VTS_nn_1.VOB`, with the title set's VOBs `_1`…`_9` concatenated. File boundaries come from the real file sizes, which must be multiples of 2048. If they are not, the title is unsupported, with reason "VOB size not a multiple of 2048".
- Take the angle-1 cells in play order. A non-angle cell is always included. In an angle block, only the first cell is included.
- The title is **rippable** when all three of these hold:
  1. Every cell starts on the sector after the previous cell's last sector.
  2. The first cell starts exactly at a file boundary.
  3. The last cell ends exactly at the last sector of some file, which may be the same file.
- `Clips` lists the covered files, in order.
- Otherwise, `Unsupported` is set to the first applicable reason:
  - `"cells are not contiguous (not supported yet)"`
  - `"starts or ends mid-file (not supported yet)"`
- A multi-angle title is rippable only if the whole-file check passes with angle 1 alone. That normally fails, because angle blocks interleave.

### 5.2 Encryption

- For each VOB in the title set, scanning samples up to 64 packs, spread evenly through the file.
- If any MPEG-PS PES packet in the samples has `PES_scrambling_control != 0`, the title is encrypted.
- A read error is reported, not treated as unencrypted.

### 5.3 Ranking

- `internal/rank` gets the same candidate fields as Blu-ray: duration, chapter count and track counts.
- Duplicates are titles whose angle-1 cell lists (VTSN, VOB ID, cell ID) are identical. The one with the lower title number is kept.
- Titles with `Unsupported` set are filtered, with their reason. `min_duration` applies.

## 6. Ripping (`rip_dvd.go`)

1. Reject the title if it is unsupported or encrypted.
2. If the source is an ISO, mount it with `internal/mount`. Mount records and doctor leftovers are unchanged. The VOB paths are resolved under the mount point or the folder. File names are matched case-insensitively.
3. Write the chapter file: Matroska chapter XML in a temp file of a few KB, deleted after the rip.
4. Identify the tracks with `mkvmerge -J <first VOB>` and map each track to its IFO stream by MPEG-PS stream ID:
   - AC-3 `0x80+n`
   - DTS `0x88+n`
   - LPCM `0xA0+n`
   - MPEG audio `0xC0+n`
   - subpicture `0x20+n`

   Here `n` is the physical stream number from the PGC control tables.
5. Run `mkvmerge -o <out> --no-chapters --chapters <xml> [per-track options] VTS_nn_k.VOB + … + VTS_nn_m.VOB`, with these per-track options:
   - `--language` from the IFO.
   - `--track-name`:
     - audio, e.g. `AC-3 5.1 English` or `English (Director's Commentary)`
     - subtitles, e.g. `English`, `English (Forced)` or `English (Large)`
   - `--default-track-flag`: the first audio track is default, and no subtitle track is default.
   - Tracks the IFO does not describe are kept, with no language and no name.
6. Progress, atomic output and `--dry-run` work the same as for Blu-ray. For an ISO, the dry-run command shows a mount placeholder.

### 6.1 Subtitle palette

- VobSub tracks need the PGC's 16-color palette.
- If mkvmerge does not carry the palette from the VOB, zenvik supplies it, converted from YCrCb to RGB, using the mkvmerge option that sets VobSub codec private data.
- If no CLI path can set it, subtitles are still kept and the limitation is documented.
- Task 1 decides which of these applies.

## 7. CLI

- `rip --title <id>` is added as an alias of `--playlist`. It accepts Blu-ray playlist numbers and DVD title numbers. Both flags are documented, and passing both is a usage error (exit 2).
- `info` works as follows:
  - The header shows the source kind (`DVD ISO image` or `VIDEO_TS folder`).
  - Unsupported titles are listed with their reason in the notes column.
  - The JSON includes `format` and `unsupported`.
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
   - macOS builds the ISO with `hdiutil makehybrid -udf`.
   - Linux builds it with `genisoimage -udf`.

   Both rips go through the real mount path.
4. Assert:
   - the duration
   - the audio languages and names
   - the chapter count and times, each within one frame
   - that the subtitle track is present
   - the palette, if Task 1 shows it can be set
   - that no mounts are left over

CI installs `dvdauthor`, plus `genisoimage` on Linux.

### 8.3 Verification first

Like M3's MPLS check, the first plan task verifies these points on dvdauthor-built discs, locally with MKVToolNix v102 and in CI with Ubuntu's mkvtoolnix:

1. How `mkvmerge -J` reports MPEG-PS stream IDs.
2. That `+` appending across split VOBs keeps A/V sync and the full duration.
3. VobSub palette handling.
4. That IFO chapter times line up with mkvmerge's timeline.

If any check fails, this spec is updated, or the minimum MKVToolNix (80.0) is raised, before later tasks build on it.

## 9. Rollout (plan tasks)

1. Verify mkvmerge's DVD behavior (§8.3).
2. The `dvd/` IFO parsers.
3. The synthetic VIDEO_TS builder, plus the ISO variant.
4. Source detection.
5. DVD scan: titles, tracks, chapters, the whole-file check, CSS detection and ranking.
6. DVD rip: mounting, mkvmerge arguments, track mapping, the chapter file and the palette.
7. CLI: `--title`, `info` output, JSON `format`/`kind`.
8. Integration tests and CI.
9. Docs and syncing the v1 spec. That covers §1 non-goals, §3 `SourceKind` and §11.

Tasks 2 and 3 can run in parallel. Once Task 5 is done, Tasks 6 and 7 can run in parallel.
