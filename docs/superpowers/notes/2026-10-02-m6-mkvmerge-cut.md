# M6: mkvmerge cutting and copying DVD titles (verified 2026-10-02)

**Gate result: BLOCKED.** Checks (1), (2) and (3) fail as written. Checks (4) and (5) pass. The cause is one finding: mkvmerge snaps `--split parts:` ranges to keyframes, and cut times taken from summed VOBU durations run a few milliseconds later than mkvmerge's own timeline. Each boundary therefore snaps one GOP late.

- Tools: mkvmerge v102.0 ('Little Houses') 64-bit, ffmpeg 9.0.2, dvdauthor/spumux 0.7.2, OS macOS 27.0 (26A428).
- Fixture: `testdisc.AuthorEpisodesDVD`. Test: `TestEpisodeCutting` in `internal/testdisc/episodes_integration_test.go` (`-tags integration`). The results below are the same on every run.

## What the test asserts and what was only observed

The test asserts:
- (1) The muxed duration of each episode cut is within 34 ms of the IFO PGC time. The first frame and the frame 0.5 s before the end have that episode's colour.
- (3) After cutting green with a group-timeline chapter file and a VobSub `.idx`, there are 2 chapters, at 0 and at cell 1's IFO time, each within 34 ms. The first subtitle is at 1.0 s ± 100 ms.
- (4) The output exists under the exact name given to `-o`.
- (5) The out-of-order temp VOB (blue then red) has a duration within 68 ms of the sum of the two IFO times. Its first frame is blue and its last frame is red.

The test does not assert (2). It only logs the VOB IDs. Everything in the "Investigation" section was observed by hand with mkvmerge, ffprobe and ffmpeg on the same fixture. None of it is asserted.

## Fixture layout (observed)

- One title set, `VTS_01_1.VOB` only, 442 sectors, 31 NAV packs, VOB IDs 1, 2, 3, 4 in that order.
- PGC 1 (black): 1 cell, sectors 0–12, VOB ID 1, IFO 0.5005 s (15 frames). It has 1 VOBU of 45600 ticks (0.5067 s).
- PGCs 2–4 (red, green, blue): 2 cells each, IFO 3 s + 3 s = 6 s.
  - red: sectors 13–77 and 78–155
  - green: sectors 156–220 and 221–298
  - blue: sectors 299–363 and 364–441
- Each episode has 10 VOBUs. Nine last 54054 ticks (0.6006 s, 18 frames) and the last one lasts 54474 ticks, which is 420 ticks (4.67 ms) more. The VOBU sum per episode is 540960 ticks (6.0107 s), but the video is 180 frames = 540540 ticks (6.006 s).
- Every VOB ID starts at PTS 48003, so the PTS resets at each VOB ID.
- DSI `VOB_V_S_PTM`/`VOB_V_E_PTM` are 48003/588963 for every episode VOB. They carry the same padded end, so they don't give the unpadded video end either.

## (1) Cut accuracy: FAIL

| episode | sectors | cut (summed VOBU) | muxed duration | IFO | first / last frame |
|---|---|---|---|---|---|
| red | 13–155 | 0.506666666 s – 6.517333333 s | 6.630 s | 6 s | red / **green** [1 128 1] |
| green | 156–298 | 6.517333333 s – 12.528 s | 6.008 s | 6 s | green / **blue** [0 0 255] |
| blue | 299–441 | 12.528 s – 18.538666666 s | **5.442 s** | 6 s | blue / blue |

Frame-by-frame content of each cut (observed):
- red: 180 red frames plus 18 green frames (one GOP of green).
- green: 162 green frames plus 18 blue frames. The first GOP of green is missing and the first GOP of blue is included. The duration is still "correct" at 6.008 s, so a duration check alone does not catch this.
- blue: 162 blue frames. The first GOP is missing.

## (2) PTS resets: VOB IDs 1, 2, 3, 4 seen in order. Offsets stayed correct: no, by a few ms per VOB ID

mkvmerge muxes the `( VTS_01_1.VOB )` group into one continuous 18.56 s timeline with no gaps across the PTS resets. Its video keyframes at the VOB ID boundaries, from ffprobe on a full mux, are:
- black: 0.005 s
- red: 0.506 s
- green: 6.512 s
- blue: 12.518 s

The summed-VOBU group times for the same points are 0, 0.5067, 6.5173 and 12.5280 s. So the summed-VOBU timeline runs ahead of mkvmerge's:
- about +0.7 ms at red
- +5.3 ms at green
- +10 ms at blue

The lead grows by about 4.67 ms per episode, which is the padding on the last VOBU of each VOB ID. mkvmerge appears to join segments at the end of the last video frame, not at the NAV `VOBU_E_PTM`.

## (3) Chapters and VobSub on the group timeline: FAIL, as a result of (1)

- The chapter file gave 6.517 and 9.517 s on the group timeline. After the cut the chapters are at 0 and 2.405 s (want 0 and 3 s).
- The `.idx` timestamps were 1.505, 7.516 and 13.527 s. The first subtitle after the cut is at 0.404 s (want 1.0 s).
- Both are exactly 0.6 s (one GOP) early. mkvmerge shifts chapters and subtitles by the cut's real start (the keyframe it snapped to, 7.112 s), not by the requested start. So chapters and VobSub do follow the video correctly. The only error is the snapped start.

## (4) Output naming: PASS

A single `parts:` range writes exactly the name given to `-o`, with no `-001` suffix.

## (5) Copy: PASS

- The out-of-order temp VOB (blue sectors 299–441, then red sectors 13–155) muxes to 12.032 s. The expected value is 12 s (IFO 6 s + 6 s).
- The first frame is blue and the last frame is red.

## Investigation (observed only, not asserted)

These are probes of the snapping rule with single `parts:` ranges, counting frames and checking colours:
- **Start:** output begins at the first keyframe at or after the start time.
  - Starts of 6.400–6.512 s begin at green's keyframe (6.512 s).
  - Starts of 6.513 s or later begin one GOP later (7.112 s).
  - Red's keyframe is between 0.506 s and 0.507 s: 0.506 keeps it and 0.507 drops it.
- **End:** output stops before the first keyframe at or after the end time.
  - Ends up to 6.512 s stop before green.
  - Ends from 6.513 s include green's first GOP.
- **Same cuts moved one NTSC frame (33.367 ms) earlier, start and end:** the problem goes away.
  - red, green and blue each come out as exactly 180 frames of their own colour.
  - The durations are 6.022, 6.032 and 6.042 s. Audio starts at 7, 17 and 27 ms.
  - On green, the chapters come out at 5 ms and 3.005 s and the first subtitle at 1.005 s. All are inside the test's tolerances.

These are candidate fixes. **They are not verified in the test and not decided:**
- Bias both cut points earlier by a margin. The margin must be larger than the accumulated drift (about 4.7 ms per earlier VOB ID here) and smaller than the shortest GOP before the boundary.
- Or compute group time the way mkvmerge does: per VOB ID segment, use the video duration (frame count × frame time) instead of the sum of `VOBU_E_PTM − VOBU_S_PTM`.

The margin's drift budget is set by the number of VOB IDs before the cut point. On a real disc with many VOB IDs a fixed one-frame margin may not be enough. The second option needs video frame timing, which the NAV packs alone don't give.

## Conclusion for Tasks 4/6

The copy path (Task 6) and single-range output naming work as the plan assumes. The cut path (Task 4) does not work with cut points taken directly from summed VOBU durations. mkvmerge snaps each end of the range to the next keyframe, and those times run a few ms past the real boundary keyframes. The plan needs a decided correction, such as an early bias or frame-accurate segment timing, before Task 4 is built on it.
