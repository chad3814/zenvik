# zenvik M6 — DVD titles that aren't whole VOB files

- **Date:** 2026-10-02
- **Status:** Draft for review
- **Extends:** [zenvik v1 design](2026-10-01-zenvik-v1-design.md) and [M5 DVD design](2026-10-01-zenvik-m5-dvd-design.md). Everything there still applies unless this document says otherwise.

## 1. Purpose and scope

M5 rips only DVD titles whose angle-1 cells cover whole title VOB files. Every other title is listed as `starts or ends mid-file (not supported yet)` or `cells are not contiguous (not supported yet)`. That excludes most TV episodes, and some movies.

The motivating disc is *Swiss Family Robinson (1960)*. Its 2:06:14 feature (title 01) plays cells 1–21 as one ascending run, from sector 439 to 3,100,664 across `VTS_01_1`–`VTS_01_6.VOB`. The run starts 439 sectors into the first file and ends 878 sectors before the end of the last. It then plays cell 22, a 1-second black cell at sectors 0–438. Because title 01 is unsupported, `info` also picks the 7-minute title 11 as the main feature.

### Goals

- Rip any single-PGC DVD title, angle 1, whatever its cell layout:
  - whole files (unchanged)
  - one ascending run that starts or ends mid-file
  - cells out of order
- Avoid temporary copies of video data whenever the layout allows.
- Keep chapters, audio, subtitles and their timing exact.

### Non-goals

- Angles other than 1.
- Titles that span several PGCs (`spans several program chains (not supported yet)` stays).
- Dropping cells longer than 1 second.

## 2. Classifying a title

Take the title's angle-1 cells in play order (M5 §5.1), then:

1. **Whole files.** If the M5 whole-file check passes, use the M5 path unchanged. `rip_method` is `files`.
2. **Drop short stray cells at the edges.**
   - A cell at the start or end of the title is a *stray* when it breaks sector order with its neighbour. The first cell is a stray if its sectors are not immediately followed by the second cell's. The last cell is a stray if it does not immediately follow the previous one.
   - A stray is dropped if its IFO playback time is at most **1.0 s**. Drops are repeated from each end while the condition holds.
   - Cells in the middle are never dropped.
   - Each drop adds a warning: `title <id>: skipped cell <n> (<seconds> s at sectors <a>–<b>, out of order)`.
   - Chapters whose entry cell was dropped move to the next kept cell, or are removed if none follows. Chapter numbers are renumbered from 1.
   - The title's duration excludes dropped cells.
3. **One ascending run.** If the remaining cells are strictly contiguous (each starts on the sector after the previous one ends), use the **cut path** (§3). `rip_method` is `cut`.
4. **Otherwise**, use the **copy path** (§4). `rip_method` is `copy`.

The two M5 reasons `starts or ends mid-file` and `cells are not contiguous` no longer exist. A title with no cells, several PGCs, encryption, or unreadable files is still unsupported for the existing reasons.

`Title` gains:
- `RipMethod string`, which is `"files"`, `"cut"` or `"copy"` for DVD titles and `""` for Blu-ray;
- `SkippedCells []SkippedCell`, where `SkippedCell` is `{Cell int; Duration time.Duration; FirstSector, LastSector uint32}`.

`Size` counts the kept cells' sectors.

Ranking is unchanged. It now sees long titles such as this disc's title 01, so the main feature is picked correctly.

## 3. The cut path (no temporary copy)

Let the run cover sectors `R0`…`R1`, which lie in title VOBs `VTS_nn_a` … `VTS_nn_b`.

1. **mkvmerge input.** mkvmerge reads `( VTS_nn_a.VOB … VTS_nn_b.VOB )`, the files the run touches, as one group (M5 ruling: never `+`). Its timeline starts at 0 at the group's first byte.
2. **Cut points from the VOBU map.**
   - The `dvd` package parses `VTS_VOBU_ADMAP`, the start sector of every VOBU in the title VOBs.
   - zenvik reads the NAV pack at each VOBU start from the group's first sector up to `R1`. From each PCI it takes `vobu_s_ptm` and `vobu_e_ptm`.
   - **start** = Σ (`e_ptm − s_ptm`) over the VOBUs before `R0`.
   - **end** = start + Σ over the run's VOBUs.
   - A missing or unparsable NAV pack, or a VOBU map that disagrees with the files, is an error. The rip fails with `zenvik: title <id>: cannot compute cut points: …`. It does not silently fall back.
3. **Cutting.** mkvmerge gets `--split parts:<start>-<end>`, with nanosecond-precision timestamps. Cells start on VOBU boundaries, which are I-frames, so both cuts land on keyframes.
4. **Chapters and subtitles** are written on the group timeline (title time + start). The chapter file is OGM text; the subtitles are a VobSub `.idx`. mkvmerge cuts every input and the chapters at the same points.
   - The VobSub extractor reads only the run's sectors.
   - Its cell map holds only the title's kept cells, so packs from other titles get no index entries.
5. **Output name.** If mkvmerge appends a number to the split output, zenvik renames that file to `<output>.partial` before the normal final rename.

## 4. The copy path

1. **Free space.** Before copying, zenvik checks the free space in the output directory. It uses `syscall.Statfs` on Unix and `GetDiskFreeSpaceExW` on Windows, through the standard library `syscall` package with no cgo. Copying needs room for the title's `Size` plus 64 MiB. If there isn't enough, the rip fails with `zenvik: copying title <id> needs <size> in <dir>, only <free> free`.
2. **Copy.** zenvik copies each kept cell's sectors, in play order, from the VOBs (mounted for an ISO) into `.<output file name>.title.vob` beside the output, using 1 MiB reads.
   - Progress is reported as a new phase, `PhaseCopying` (`"copying"`).
   - The copy honours cancellation.
3. **Mux.** mkvmerge reads `( <temp>.title.vob )`. Chapters and VobSub use the title timeline. Subtitles are extracted from the temp file, and the NAV packs there still carry the original VOB and cell IDs.
4. **Cleanup.** The temp file is removed after a successful mux, on any failure, and on cancellation.
5. **Dry run.** A dry run copies nothing and checks no free space. Its command shows the placeholder `<title.vob>`.

## 5. User-visible changes

- **`info`:** previously unsupported DVD titles become regular titles.
  - The notes column lists `skipped <n> short cell(s)` when cells were dropped.
  - The JSON gains `"rip_method"` and `"skipped_cells"` (`[{cell, duration_seconds, first_sector, last_sector}]`).
- **`rip`:**
  - The human output prints `  via: cut` or `  via: copy (5.9 GiB temporary file)` under the `Ripping` line.
  - The `--jsonl` `start` event gains `rip_method`, and `temp_bytes` for the copy path.
  - Each skipped cell is a `warning`.
  - The `copying` phase appears in `progress` events.
- **Library:** `Rip` handles all three methods. `PhaseCopying` is new.

## 6. Testing

### 6.1 Verification first (gate)

On dvdauthor-built discs, with mkvmerge v102 locally and Ubuntu's mkvtoolnix in CI, verify the following:

1. **Cut accuracy.** Build a title set with three episodes, each a distinct solid colour and tone, and cut each one from a `( … )` group using VOBU-summed offsets. Each output must have its episode's length (±1 frame), and its first and last frames must be the episode's colour.
2. **PTS resets.** The offsets must stay correct when the episodes have different VOB IDs, which reset the PTS, before `R0`.
3. **Inputs cut together.** Chapters (`--chapters`) and a VobSub `.idx` on the group timeline must be cut with the video and land at the expected times.
4. **Output naming.** Record what file name mkvmerge writes for a single `parts:` range.
5. **Copy path.** A temp VOB made of cells concatenated out of order must mux to one continuous timeline.

If any check fails, update this spec before building on it.

### 6.2 Unit tests

- The classifier: whole files, edge drops (start, end, both, a middle stray kept), one run, copy.
- Chapter remapping and renumbering.
- The `VTS_VOBU_ADMAP` parser, with corrupt-input tests and a fuzz target.
- Cut-point computation from synthetic NAV packs.
- The free-space check and its failure.
- Temp-file cleanup on success, failure, cancellation and dry run.
- The `rip_method` and `skipped_cells` JSON fields, and the `--jsonl` start event.
- `SampleDVD`'s episodes (03–05) and the two-angle title (08) become rippable (`cut` or `copy`). A new fixture title has a 1 s out-of-order trailing cell.

### 6.3 Integration tests

Run on authored discs, from both a folder and a darwin ISO:
- An episodic title set ripped by the cut path. Check length, colours, chapters and subtitles.
- A title with an out-of-order middle cell, ripped by the copy path.
- A title with a short black trailing cell, ripped by the cut path, emitting the skip warning.

### 6.4 Acceptance (local, not committed)

Rip *Swiss Family Robinson (1960)* title 01. It must use `rip_method` `cut` and skip cell 22 with a warning. Its duration must be 2:06:13 ±1 frame, it must have 19 chapters and all its audio and subtitle tracks, and `info` must pick title 01 as the main feature.

## 7. Rollout (plan tasks)

1. Verify mkvmerge cutting, chapters, subtitles, naming and copy timelines (§6.1).
2. The `dvd` `VTS_VOBU_ADMAP` parser.
3. The classifier, edge drops and chapter remap in the scan; `Title.RipMethod` and `SkippedCells`.
4. The cut path: cut points, `--split parts`, the group-timeline chapters and VobSub, and output naming.
5. The copy path: free space, the copier, `PhaseCopying` and cleanup.
6. CLI and JSON: `rip_method`, `skipped_cells`, the `via:` line, and the `--jsonl` fields.
7. Integration tests, docs and spec sync.

Task 2 can run in parallel with Task 1, and Tasks 4 and 5 can run in parallel.
