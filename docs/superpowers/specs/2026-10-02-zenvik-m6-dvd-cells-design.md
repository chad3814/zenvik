# zenvik M6 — DVD titles that aren't whole VOB files

- **Date:** 2026-10-02
- **Status:** Implemented
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
- Interleaved angle blocks. A title whose kept cells include an interleaved cell (C_PBIT bit 2) is unsupported with `interleaved angle block (not supported yet)`: the other angles' sectors lie inside angle 1's range, so copying or cutting that range would mix every angle into the output. This keeps M5's refusal for the usual authored multi-angle title.
- Titles that span several PGCs (`spans several program chains (not supported yet)` stays).
- Dropping cells longer than 1 second.

## 2. Classifying a title

Take the title's angle-1 cells in play order (M5 §5.1), then:

1. **Whole files.** If the M5 whole-file check passes, use the M5 path unchanged. `rip_method` is `files`.
2. **Drop short stray cells at the edges.**
   - A cell at the start or end of the title is a *stray* when it breaks sector order with its neighbour. The first cell is a stray if its sectors are not immediately followed by the second cell's. The last cell is a stray if it does not immediately follow the previous one.
   - A stray is dropped if the playback time its IFO lists is at most **1.0 s** (`00:00:01:00`): 30 NTSC frames, which last 1.001 s, or 25 PAL frames, 1.0 s. A 31-frame NTSC cell is kept. Drops are repeated from each end while the condition holds.
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

1. **mkvmerge input.** mkvmerge reads `( VTS_nn_a.VOB … VTS_nn_b.VOB )` as one group (M5 ruling: never `+`), from the latest title VOB at or before the run's first file whose first sector starts a VOBU (VTS_nn_1 always does), through the run's last file. Real discs split VOB files at about 1 GB, which can fall inside a VOBU. Its timeline starts at 0 at the group's first byte.
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
5. **Output name.** mkvmerge writes a single `parts:` range under the given name (verified with v102), so no rename is needed.

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
- `SampleDVD`'s episodes (03–05) become rippable (`cut`), while its two-angle title (08), whose angle cells are marked interleaved, stays unsupported (`interleaved angle block (not supported yet)`). `StrayCellDVD` has titles with a 1 s out-of-order cell, one of them copied.

### 6.3 Integration tests

Run on authored discs, from both a folder and a darwin ISO:
- An episodic title set ripped by the cut path. Check length, colours, chapters and subtitles.
- A title with an out-of-order middle cell, ripped by the copy path.
- A title with a short black trailing cell, ripped by the cut path, emitting the skip warning.

### 6.4 Acceptance (local, not committed)

Rip *Swiss Family Robinson (1960)* title 01. It must use `rip_method` `cut` and skip cell 22 with a warning. Its video duration must be the sum of its kept cells (about 7580.7 s, 2:06:21, with NTSC IFO times read as frame counts; see §8) within ±0.1 s. It must have 18 chapters (chapter 19 starts at the skipped cell 22, so §2 removes it), each within one frame of the title's chapter starts, and all its audio and subtitle tracks, and `info` must pick title 01 as the main feature.

## 7. Rollout (plan tasks)

1. Verify mkvmerge cutting, chapters, subtitles, naming and copy timelines (§6.1).
2. The `dvd` `VTS_VOBU_ADMAP` parser.
3. The classifier, edge drops and chapter remap in the scan; `Title.RipMethod` and `SkippedCells`.
4. The cut path: cut points, `--split parts`, the group-timeline chapters and VobSub, and output naming.
5. The copy path: free space, the copier, `PhaseCopying` and cleanup.
6. CLI and JSON: `rip_method`, `skipped_cells`, the `via:` line, and the `--jsonl` fields.
7. Integration tests, docs and spec sync.

Task 2 can run in parallel with Task 1, and Tasks 4 and 5 can run in parallel.

## 8. Implementation notes

These record where the implementation differs from §2–§6 and what was observed while building it. The verification notes are in [docs/superpowers/notes/2026-10-02-m6-mkvmerge-cut.md](../notes/2026-10-02-m6-mkvmerge-cut.md).

- **NTSC IFO times are frame counts (fix wave A).** At NTSC (rate bits 3) the BCD playback time counts frames at a nominal 30 fps, and each frame lasts 1001/30000 s. So `hh:mm:ss:ff` is ((h·3600 + m·60 + s)·30 + f) × 1001/30000 s, and dvdauthor's `00:00:06:00` for a 180-frame episode is 6.006 s. PAL (25 fps) times are exact. `dvd.NewTime` inverts this, rounding to the nearest frame.
  - Evidence: on *Swiss Family Robinson* title 01, cells 1–21 sum to 7573.13 s read as wall-clock time, but their NAV VOBU durations sum to 7580.71 s, exactly ×1.001.
  - Before this fix every NTSC duration and chapter was 0.1 % short: 7.5 s over this 2 h film. Chapters are now placed from the corrected IFO times.
  - The stray limit (§2's 1.0 s) is on the time the IFO lists, `00:00:01:00`: 30 NTSC frames (1.001 s) or 25 PAL frames (1.0 s). zenvik compares the cell's duration against 1.001 s, so a one-second NTSC cell such as cell 22 on this disc is still dropped, and a 31-frame NTSC or 26-frame PAL cell is kept (controller ruling, fix wave A round 2).
- **Cut points (replaces §3 step 2's start and end).** Let start₀ be the summed VOBU durations (`vobu_e_ptm − vobu_s_ptm`) before `R0`, and end₀ be start₀ plus the run's summed VOBU durations. mkvmerge gets start = start₀ − m_s and end = end₀ − m_e. With frame = 1001/30000 s:
  - m_s = min(c_s × 10 ms + frame, half the shorter of the VOBU before the run and the run's first VOBU). c_s counts the VOB ID changes among the NAV packs from the group start through `R0`'s VOBU, so a change into `R0`'s VOBU counts. m_s is 0 when start₀ is 0.
  - m_e = min(c_e × 10 ms + frame, half the run's last VOBU). c_e counts the VOB ID changes from the group start through `R1`.
- **Why the margins exist.** Summed VOBU durations run about 4.7 ms ahead of mkvmerge's timeline per VOB ID on the authored discs, because each VOB's last VOBU is padded past its last video frame. mkvmerge cuts at the first keyframe at or after the time it is given, so a cut time even 1 ms past a boundary keyframe gains or loses a whole GOP. Each point is moved back by the drift bound (10 ms per VOB ID change) plus one frame.
  - Real VOBUs can hold several GOPs. The first rule moved each point back by half a VOBU, and on *Swiss Family Robinson* that snapped to keyframes inside a VOBU. The output began with 0.1 s of the skipped black cell and lost the last 0.2 s of cell 21. The drift budget keeps the margin a few frames wide, short of any earlier keyframe.
- **Drift guard.** The rip fails with `zenvik: title <id>: cannot compute cut points: …` when c × 10 ms + frame exceeds its half-VOBU bound, for m_s (only when start₀ > 0) or m_e. The drift budget must fit inside half a VOBU.
- **Chapters and VobSub on cut titles (fix wave A round 2).** Neither is shifted by the requested cut time.
  - VobSub timestamps are shifted by start₀, the run's true start. mkvmerge rebases the subtitle track, like the video, to the run's first displayed frame.
  - Chapters are shifted by start₀ + gap. mkvmerge moves chapters back by the I-frame it cuts at, not by the first displayed frame.
  - gap = (PTS of the first video PES with a PTS after the NAV pack in the run's first VOBU) − that VOBU's `vobu_s_ptm`. On an open GOP this is the leading B-frames' length. It is 0 if there is no such PES before the next VOBU or if it is negative. A gap longer than the VOBU fails the cut. The `--split` times do not use it.
- **The cut group** starts at the latest VOBU-aligned title VOB at or before the run's first file (§3 step 1), so mkvmerge's timeline starts on a VOBU.
- **Cells outside the VOBs.** A title with a kept cell whose sectors lie outside its title set's VOBs is unsupported, with the reason `cells point outside the title VOBs`. Dropped strays are not checked.
- **Classification order (differs from §2).** Short edge strays are dropped first, and then the whole-file check runs. So kept cells that cover whole VOB files after a drop use the `files` method.
- **Audio lag at VOB ID changes.** In a `( … )` group, mkvmerge appends each VOB ID's audio straight after the previous one's audio. On the authored discs each segment's AC-3 is 10 ms longer than its video, so the audio lags the video by about 10 ms per earlier VOB ID. This is an observation about mkvmerge's group mux (it shows in the whole-file path too), not a cut-path defect. The integration tests therefore check durations on the video track (mkvextract `timestamps_v2` of track 0, dropping its trailing end-time line), not the container duration.
- **Single-part naming.** A single `parts:` range writes the given file name, so §3 step 5's rename is not needed.
- **Integration tests (§6.3).** `testdisc.AddMixedEpisodeTitles` adds two titles to the episodes fixture: title 05 (red episode, then the 0.5 s black cell as a trailing stray) and title 06 (blue episode, then red, out of sector order). Observed with mkvmerge v102:
  - title 03 (cut, green): video 6.005 s (IFO 6 s), green to green, chapters at 5 ms and 3.005 s, first subtitle at 1.005 s; container 6.032 s.
  - title 05 (cut, red, cell 3 skipped): video 6.005 s, red to red, warning `title 05: skipped cell 3 (0.5 s at sectors 0–12, out of order)`; container 6.022 s.
  - title 06 (copy): video 12.012 s (IFO 12 s), blue to red; container 12.032 s.
  - title 04 from a darwin ISO (cut): blue to blue, no mount left.
- **First acceptance run (§6.4), 2026-10-02, mkvmerge v102, before fix wave A.** *Swiss Family Robinson (1960)* title 01:
  - `info` marked 01 with ★, `cut`, with the note `skipped 1 short cell(s)`, 18 chapters, 2:06:13 (IFO times read as wall-clock seconds).
  - The cut was `parts:00:00:00.800800000-02:06:21.373800000`. The video track was 7580.606 s and the container 7580.702 s.
  - The IFO cell times summed to 7573.13 s for cells 1–21, but their NAV packs summed to 7580.71 s. This led to the NTSC time fix above.
  - **Chapters: 18, not 19.** Chapter 19 starts at cell 22, the dropped stray, and no kept cell follows it, so §2 removes it.
  - Chapters placed from the short IFO times drifted early, to about 7.3 s early at chapter 18.
  - The half-VOBU margins snapped inside multi-GOP VOBUs (see "Why the margins exist" above).
- **Acceptance run after fix wave A, 2026-10-02, mkvmerge v102.**
  - `info` marks 01 with ★, `cut`, 2:06:21, 5.9 GiB, 18 chapters, `skipped 1 short cell(s)`. `duration_seconds` is 7580.706 s, the NAV-summed length of cells 1–21. Every title's method and skipped-cell count is the same as before the fix.
  - The `start` event has `"rip_method":"cut"`, `"title":"01"` and `duration_seconds` 7580.706466666. The warning is `title 01: skipped cell 22 (1.0 s at sectors 0–438, out of order)`, and the run ends with `done`. No mounts are left.
  - The cut is `parts:00:00:00.957633334-02:06:21.644100000`: m_s = 10 ms + frame (one VOB ID change, 1→2, into `R0`'s VOBU), and m_e = 30 ms + frame (three changes).
  - The video track is 7580.706 s, 0.1 ms from `duration_seconds`. The container is 7580.727 s.
  - Frame hashes show that the output's first frame is cell 1's first frame. The group's first 30 frames are cell 22's; the output has none of them and none of the frames after `R1`. The output ends on the last frame of cell 21. Cell 1 itself opens with about 0.93 s of black, so a colour check of the first frame can't tell the two cells apart.
  - Tracks: video, audio eng (AC-3 5.1), spa (AC-3 5.1), eng (AC-3 Stereo, Director's Commentary), and English VobSub.
  - **Chapters were 2 frames early on this open-GOP cut.** All 17 non-zero chapters were 67.1–68.0 ms before the title's chapter starts in `info --json`, more than the one-frame tolerance.
    - The run's first VOBU (sector 439) has `vobu_s_ptm` 25257, but its first video PES (the I-frame) has PTS 31263. That is 6006 ticks, two leading B-frames of an open GOP. The stray VOBU at sector 0 has no lead-in.
    - mkvmerge snaps the cut to that I-frame, at 1.068 s on the group timeline. It rebases the video to the first displayed frame (1.001 s, start₀), but it shifts the chapters by the keyframe's 1.068 s.
- **Acceptance run after round 2 (chapters at start₀ + gap, VobSub at start₀), 2026-10-02.**
  - The `info`, the `start`, `warning` and `done` events, the cut, the tracks and the video duration (7580.706 s) are all unchanged. No mounts are left.
  - zenvik computes start₀ = 1.001 s and a gap of 66.733 ms. The chapter file starts at `00:00:01.067`.
  - All 18 output chapters are within 1.3 ms of `info --json` (−1.27 ms to 0).
  - **Subtitles.** The first `.idx` timestamp is `00:01:46:306` on the group timeline. The output's first subtitle block is at 105.305 s, which is idx − start₀. The next two blocks match the same way (111.561 → 110.560 s, 119.719 → 118.718 s).
  - With the round-2 ruling as first written (VobSub also at start₀ + gap), the idx began at `00:01:46:372` and the output's first block was at 105.371 s. That is idx − start₀, 67 ms later than idx − (start₀ + gap). So mkvmerge shifts subtitles by start₀, and only the chapters take the gap.
