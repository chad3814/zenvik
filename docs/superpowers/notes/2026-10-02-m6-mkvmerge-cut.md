# M6: mkvmerge cutting and copying DVD titles (verified 2026-10-02)

**Gate result: PASS** under the controller's two rulings. The first sets half-VOBU cut margins with a drift guard. The second makes the duration checks measure the video track. Checks (1), (3), (4) and (5) pass, and check (2) holds once the margins are applied.

Fix wave A (2026-10-02) replaced the half-VOBU margins with a drift-budget margin and corrected NTSC IFO times. See the last section.

History:
- The first run, at the raw summed VOBU times, was BLOCKED on (1)–(3).
- The margin ruling fixed the cuts, but blue's *container* duration stayed 42 ms over the IFO time. The cause is audio drift (see "Audio drift across VOB IDs"), not the cut.

- Tools: mkvmerge v102.0 ('Little Houses') 64-bit, ffmpeg 9.0.2, dvdauthor/spumux 0.7.2, OS macOS 27.0 (26A428).
- Fixture: `testdisc.AuthorEpisodesDVD`.
- Test: `TestEpisodeCutting` in `internal/testdisc/episodes_integration_test.go`, run with `-tags integration`. Every value below is the same on every run.

## What the test asserts and what was only observed

The test asserts:
- **(1)** For each episode cut:
  - The **video track** duration (`videoDuration`) is within 34 ms of the IFO PGC time. `videoDuration` takes mkvextract `timestamps_v2` of track 0 and computes last frame − first frame + 1001/30000 s.
  - The first frame has that episode's colour, and so does the frame 0.5 s before the end.
  - The drift guard is not tripped.
- **(3)** After the green cut with a group-timeline chapter file and a VobSub `.idx`:
  - There are 2 chapters, at 0 and at cell 1's IFO time, each within 34 ms.
  - The first subtitle is at 1.0 s ± 100 ms.
  - The drift guard is not tripped.
- **(4)** The output exists under the exact `-o` name.
- **(5)** The out-of-order temp VOB (blue then red) has a video track duration within 34 ms of the two IFO times summed, with blue first and red last.

The container duration and the audio start offset (`audioOffset`, the first audio timestamp minus the first video timestamp) are logged as observations and not asserted.

The test does not assert (2); it only logs the VOB IDs. Everything in the "Observed by hand" sections was measured with mkvmerge, mkvextract, ffprobe and ffmpeg on the same fixture, and none of it is asserted.

## Fixture layout (observed)

- One title set, `VTS_01_1.VOB` only: 442 sectors, 31 NAV packs, VOB IDs 1, 2, 3, 4 in that order.
- PGC 1 (black): 1 cell, sectors 0–12, VOB ID 1, IFO 0.5005 s (15 frames). It has a single VOBU of 45600 ticks (0.5067 s).
- PGCs 2–4 (red, green, blue): 2 cells each, IFO 3 s + 3 s = 6 s.
  - red: sectors 13–77 and 78–155
  - green: sectors 156–220 and 221–298
  - blue: sectors 299–363 and 364–441
- Each episode has 10 VOBUs:
  - Nine are 54054 ticks (0.6006 s, 18 frames).
  - The last is 54474 ticks, which is **420 ticks (4.67 ms) of padding**.
  - The VOBU sum is 540960 ticks (6.0107 s), but the video is 180 frames = 540540 ticks (6.006 s).
- Every VOB ID starts at PTS 48003, so the PTS resets at each VOB ID.
- DSI `VOB_V_S_PTM` and `VOB_V_E_PTM` are 48003 and 588963 for every episode VOB. They carry the same padded end.
- Each episode's AC-3 is 188 frames = 6.016 s. ffmpeg rounds 6 s up to whole 32 ms frames, so the audio is 10 ms longer than the 6.006 s of video.

## Original run: cut at the summed VOBU times (BLOCKED)

The first version of the test cut at start = start₀ and end = end₀, the summed VOBU durations, with no margin.

| episode | sectors | cut | duration | IFO | first / last frame |
|---|---|---|---|---|---|
| red | 13–155 | 0.506666666 – 6.517333333 s | 6.630 s | 6 s | red / **green** [1 128 1] |
| green | 156–298 | 6.517333333 – 12.528 s | 6.008 s | 6 s | green / **blue** [0 0 255] |
| blue | 299–441 | 12.528 – 18.538666666 s | **5.442 s** | 6 s | blue / blue |

What the cuts contained:
- red: 180 red frames plus 18 green frames.
- green: 162 green frames plus 18 blue frames. Its duration looks right even though the content is wrong.
- blue: 162 blue frames.

Chapter and subtitle results for green:
- The chapters came out at [0, 2.405 s] and the first subtitle at 0.404 s.
- Both are exactly one GOP (0.6 s) early, because the start snapped one GOP late.
- mkvmerge shifts chapters and subtitles by the real start, the keyframe it snapped to, and not by the requested start.

### Observed by hand: mkvmerge's timeline and keyframe snapping

**(2) PTS resets.** mkvmerge muxes the `( VTS_01_1.VOB )` group into one continuous 18.56 s timeline. Its video keyframes at the boundaries are:
- black: 0.005 s
- red: 0.506 s
- green: 6.512 s
- blue: 12.518 s

The summed-VOBU times for the same points are 0, 0.5067, 6.5173 and 12.528 s. So summed-VOBU time runs ahead of mkvmerge's by about **+0.7 ms, +5.3 ms and +10 ms** at the red, green and blue boundaries. The lead grows by about 4.67 ms per VOB ID, which is the last-VOBU padding: mkvmerge joins segments at the end of the last video frame, not at `VOBU_E_PTM`.

**Snapping.** With a single `parts:` range:
- **Start:** the output begins at the first keyframe at or after the start time. Starts from 6.400 to 6.512 s begin at green's keyframe; 6.513 s begins one GOP later.
- **End:** the output stops before the first keyframe at or after the end time. Ends up to 6.512 s exclude green; 6.513 s includes a green GOP.

Any cut time that lands even 1 ms after its boundary keyframe therefore loses or gains a whole GOP.

## The ruling (controller, 2026-10-02)

For a run of sectors R0..R1 on the group timeline:

- **Cut times:**
  - start₀ = the summed VOBU durations (`VOBU_E_PTM − VOBU_S_PTM`) of every NAV before R0.
  - end₀ = start₀ + the summed durations of the run's VOBUs.
  - start = start₀ − m_s and end = end₀ − m_e.
- **Start margin:** m_s = half the shorter of the last VOBU before R0 and the first VOBU of the run. If there is no VOBU before R0 (start₀ = 0), then m_s = 0 and start = 0.
- **End margin:** m_e = half the duration of the run's last VOBU.
- **Drift guard:** count the VOB ID changes among the NAV packs from the group start up to R1. If changes × 10 ms ≥ min of the margins that apply (m_s only when start₀ > 0, and always m_e), the cut can't be trusted and the rip fails with an error.

In the test these are `planCut` and `cutPlan.driftTripped`.

## Results under the first ruling (container duration asserted then)

| episode | start₀ | end₀ | m_s | m_e | cut | VOB ID changes | guard | duration (IFO 6 s) | first / last |
|---|---|---|---|---|---|---|---|---|---|
| red | 0.506666666 s | 6.517333333 s | 253.333333 ms | 302.633333 ms | 0.253333333 – 6.2147 s | 1 | not tripped | 6.022 s ✓ | [254 0 0] / [254 0 0] ✓ |
| green | 6.517333333 s | 12.528 s | 300.3 ms | 302.633333 ms | 6.217033333 – 12.225366667 s | 2 | not tripped | 6.032 s ✓ | [1 128 1] / [1 128 1] ✓ |
| blue | 12.528 s | 18.538666666 s | 300.3 ms | 302.633333 ms | 12.2277 – 18.236033333 s | 3 | not tripped | **6.042 s ✗** (+42 ms, tolerance 34 ms) | [0 0 255] / [0 0 255] ✓ |

The other checks under the ruling:
- **(3)** The green cut puts the chapters at **[5 ms, 3.005 s]** and the first subtitle at **1.005 s**. Pass.
- **(4)** The output is written under the given name. Pass.
- **(5)** The copy is 12.032 s (want 12 s), blue first and red last. Pass.

### Failure under the first ruling: container duration (observed by hand; resolved by the second ruling)

The ruling cuts the video exactly. Each of the three cuts has a 180-frame video track of 6.005 s and holds only its own colour. The failing number is the container duration, which runs to the end of the audio:

| cut | video timestamps (ms; the last value is the end-time line) | audio timestamps (ms) | audio track duration | container |
|---|---|---|---|---|
| red | 1 … 6006.37 | 7 … 6023 | 6.016 s | 6.022 s |
| green | 1 … 6006.37 | 17 … 6033 | 6.016 s | 6.032 s |
| blue | 1 … 6006.37 | 27 … 6043 | 6.016 s | 6.042 s |

- Each segment's AC-3 is 10 ms longer than its video, and mkvmerge appends each segment's audio straight after the previous segment's audio.
- So on the group timeline the audio falls about 10 ms further behind the video at each VOB ID change: the first audio timestamps are 7, 17 and 27 ms in the three cuts, while the first video timestamp is 1 ms. That is 6, 16 and 26 ms after the video.
- In blue's cut the audio starts at 27 ms and runs 6.016 s. It ends at about 6.043 s, past the video's 6.006 s, so the container is 6.042 s.

This is not a cut error. It does show two things:
- A container-duration check against the IFO time picks up audio drift that builds up per VOB ID.
- mkvmerge's group mux leaves the audio behind the video by about (audio length − video length) for each preceding VOB ID. Here that is 26 ms at blue.

The tolerance was not loosened. The controller resolved this with the second ruling: assert the video track's duration.

## Second ruling: duration checks use the video track (controller, 2026-10-02)

- The algorithm under test is the cut, and the cut is frame-exact on video.
- The container duration also includes the fixture's audio overhang and the way mkvmerge appends audio after audio at VOB ID changes. That appending happens in any multi-VOB-ID group, including the M5 whole-file path, and is not specific to cutting.
- So (1) and (5) assert the video track's duration within ±1 frame of the IFO time. All other assertions and tolerances are unchanged.

### Results under both rulings (asserted unless marked "observed")

| run | video duration | IFO | colours | observed: container | observed: audio starts after video |
|---|---|---|---|---|---|
| red | 6.005366666 s ✓ | 6 s | red / red ✓ | 6.022 s | 6 ms |
| green | 6.005366666 s ✓ | 6 s | green / green ✓ | 6.032 s | 16 ms |
| blue | 6.005366666 s ✓ | 6 s | blue / blue ✓ | 6.042 s | 26 ms |
| copy (blue + red) | 12.012366666 s ✓ | 12 s | blue / red ✓ | 12.032 s | −5 ms |

The other results:
- Cut times, margins and VOB ID changes are as in the table above. The drift guard is not tripped.
- (3): chapters at [5 ms, 3.005 s] and first subtitle at 1.005 s ✓.
- (4): ✓.

`TestEpisodeCutting` PASSES.

### Audio drift across VOB IDs (observed, not asserted)

- **Size:** about +10 ms of audio lag for each earlier VOB ID in the group.
  - The audio starts 6, 16 and 26 ms after the video in the red, green and blue cuts, with 1, 2 and 3 VOB ID changes before them.
  - The absolute first audio timestamps are 7, 17 and 27 ms.
- **Cause:**
  - Each segment's AC-3 is 188 frames = 6.016 s, while its video is 180 frames = 6.006 s. ffmpeg rounds 6 s of audio up to whole 32 ms frames.
  - mkvmerge, muxing a `( … )` group across PTS resets, appends each VOB ID's audio straight after the previous audio. Each segment's 10 ms overhang therefore pushes the next segment's audio later than its video.
- **Effect on duration:** the container duration then runs to the end of the audio, for example 6.042 s for blue.
- **Not a cut-path issue.** It shows up the same way in a whole-group mux. The copy run, which has its own two-segment group, shows the audio 5 ms *before* the video at the start.
- **On real discs:** the drift per VOB ID is set by how much each VOB's audio and video lengths differ (at most about one audio frame). Whether that matters for M5/M6 output is a separate question; it is recorded here only as an observation.

### mkvextract timestamps_v2 detail (observed)

For video track 0, mkvextract writes one more value than there are frames. The final line is the end time of the last frame:
- 181 values for the 180 frames of an episode cut, ending at 6006.366666 after a last frame at 5973.
- 556 values for the 555 frames of the whole-group mux.

`videoDuration` drops that last value before computing last − first + one frame. Without dropping it, the duration counts one frame twice (6.0387 s).

## Conclusion for Tasks 4/6

**Task 6 (copy):** as planned. Concatenate the run's sectors into the temp VOB and mux it as a `( … )` group. Out-of-order runs come out as one continuous timeline.

**Task 4 (cut):** implement exactly the ruled algorithm:
1. start₀ = Σ(`VOBU_E_PTM − VOBU_S_PTM`) over every NAV before R0. end₀ = start₀ + the same sum over the run's NAVs (R0 ≤ sector ≤ R1).
2. m_s = min(last VOBU before R0, first VOBU of the run) / 2, or 0 if start₀ = 0. m_e = (the run's last VOBU) / 2.
3. Fail with an error if (VOB ID changes among NAVs from the group start to R1) × 10 ms ≥ min(m_s if start₀ > 0, m_e).
4. Otherwise pass `--split parts:<start₀ − m_s>-<end₀ − m_e>` with a single range, which keeps the given output name.
5. Write chapters and VobSub timestamps on the group timeline (from start₀). mkvmerge shifts them by the keyframe it actually cut at.

On this fixture that algorithm gives frame-exact video for every episode.

**Integration duration checks (Tasks 4 and 6) must use the video track** (`videoDuration`: mkvextract `timestamps_v2` of track 0, dropping the trailing end-time line, then last − first + one frame), and not the container duration. The container duration includes per-VOB-ID audio drift of about 10 ms per earlier VOB ID on this fixture.

## Real-disc findings and the revised margin rule (fix wave A, 2026-10-02)

The *Swiss Family Robinson (1960)* acceptance run found two defects. The disc is local only and is never committed.

### NTSC IFO times are 30 fps frame counts

- Title 01's cells 1–21 sum to 7573.13 s if their IFO times are read as wall-clock time, but their NAV VOBU durations sum to 7580.71 s. The ratio is exactly 1.001.
- dvdauthor writes `00:00:06:00` for each 180-frame (6.006 s) episode on this fixture.
- So at NTSC a BCD time is a frame count at a nominal 30 fps: `hh:mm:ss:ff` is ((h·3600 + m·60 + s)·30 + f) × 1001/30000 s. `dvd.Time.Duration` and `dvd.NewTime` now use this, and `NewTime` rounds to the nearest frame.
- On this fixture the episodes' IFO time is now 6.006 s, and the video tracks are 6.005 s, so they are closer than before.
- The stray limit of 1.0 s is an IFO time of `00:00:01:00`, which is 1.001 s at NTSC, so zenvik compares cells against 1.001 s.

### Half-VOBU margins break on multi-GOP VOBUs

- The disc's VOBUs hold several GOPs. Cell 22 is one 1.001 s VOBU with keyframes at 0, 0.300, 0.601 and 0.901 s.
- The half-VOBU start margin put the start at 0.8008 s, which snapped to the keyframe at 0.901 s. The output began with 0.1 s of the skipped cell.
- The run's last VOBU (0.667 s) has a keyframe 0.2 s before its end. The end margin snapped there, and the output lost the last 0.2 s of cell 21.
- The drift at the VOB ID changes inside the run was not measurable on this disc. On the authored discs it is 4.67 ms per VOB ID.

### Revised rule (controller ruling, fix wave A)

With frame = 1001/30000 s and 10 ms of drift allowed per VOB ID change:

- **Start.** If start₀ = 0, there is no margin. Otherwise m_s = min(c_s × 10 ms + frame, half the shorter of the VOBU before `R0` and the run's first VOBU). c_s counts the VOB ID changes in the NAV sequence from the group start through `R0`'s VOBU, so a change into `R0`'s VOBU counts.
- **End.** m_e = min(c_e × 10 ms + frame, half the run's last VOBU), where c_e counts the changes from the group start through `R1`.
- **Guard.** The cut fails when c × 10 ms + frame exceeds its half-VOBU bound, for m_s (when start₀ > 0) or m_e.
- Chapters and VobSub are still shifted by start₀.

`TestEpisodeCutting`'s `planCut` applies the same rule. Its results, all passing:

| episode | m_s | m_e | VOB ID changes | video duration (IFO 6.006 s) | colours |
|---|---|---|---|---|---|
| red | 43.37 ms | 43.37 ms | 1 | 6.005367 s | red / red |
| green | 53.37 ms | 53.37 ms | 2 | 6.005367 s | green / green |
| blue | 63.37 ms | 63.37 ms | 3 | 6.005367 s | blue / blue |

- The green cut puts the chapters at [5 ms, 3.008 s]. Cell 1's IFO time is now 3.003 s.
- The first subtitle comes at 1.006 s.
- The copy run (blue + red) is 12.012 s, with blue first and red last.

### Acceptance after the fix

- The cut is `parts:00:00:00.957633334-02:06:21.644100000`.
- The video track is 7580.706 s, matching `duration_seconds` 7580.706 s.
- Frame hashes show the output starts on cell 1's first frame and ends on cell 21's last frame. It has no frames from cell 22 or from after `R1`.
- **Chapters are 2 frames early on open GOPs (open finding).**
  - The run's first VOBU starts at `vobu_s_ptm` 25257, but its I-frame has PTS 31263. That is 6006 ticks: two leading B-frames.
  - mkvmerge cuts at the I-frame. It rebases the video to the first displayed frame (start₀), but it shifts the chapters by the I-frame's time, 66.7 ms later.
  - So all 17 non-zero chapters come out 67–68 ms before the title's chapter starts, which is more than one frame.
  - On the authored discs the GOPs are closed, so this didn't show.
