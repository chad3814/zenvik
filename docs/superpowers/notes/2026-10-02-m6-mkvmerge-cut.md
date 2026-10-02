# M6: mkvmerge cutting and copying DVD titles (verified 2026-10-02)

**Gate result: still BLOCKED, on one assertion.** Under the controller's margin ruling, every cut check passes except one: blue's container duration is 6.042 s against the IFO's 6 s, outside the ±34 ms tolerance. Blue's video track is exact (180 frames, 6.005 s). The extra length is audio that mkvmerge lays down back to back across the VOB ID boundaries (see "Remaining failure").

- Tools: mkvmerge v102.0 ('Little Houses') 64-bit, ffmpeg 9.0.2, dvdauthor/spumux 0.7.2, OS macOS 27.0 (26A428).
- Fixture: `testdisc.AuthorEpisodesDVD`.
- Test: `TestEpisodeCutting` in `internal/testdisc/episodes_integration_test.go`, run with `-tags integration`. Every value below is the same on every run.

## What the test asserts and what was only observed

The test asserts:
- **(1)** For each episode cut:
  - The container duration (`mkvmerge -J` `container.properties.duration`) is within 34 ms of the IFO PGC time.
  - The first frame has that episode's colour, and so does the frame 0.5 s before the end.
  - The drift guard is not tripped.
- **(3)** After the green cut with a group-timeline chapter file and a VobSub `.idx`:
  - There are 2 chapters, at 0 and at cell 1's IFO time, each within 34 ms.
  - The first subtitle is at 1.0 s ± 100 ms.
  - The drift guard is not tripped.
- **(4)** The output exists under the exact `-o` name.
- **(5)** The out-of-order temp VOB (blue then red) is within 68 ms of the summed IFO times, with blue first and red last.

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

## Results under the ruling

| episode | start₀ | end₀ | m_s | m_e | cut | VOB ID changes | guard | duration (IFO 6 s) | first / last |
|---|---|---|---|---|---|---|---|---|---|
| red | 0.506666666 s | 6.517333333 s | 253.333333 ms | 302.633333 ms | 0.253333333 – 6.2147 s | 1 | not tripped | 6.022 s ✓ | [254 0 0] / [254 0 0] ✓ |
| green | 6.517333333 s | 12.528 s | 300.3 ms | 302.633333 ms | 6.217033333 – 12.225366667 s | 2 | not tripped | 6.032 s ✓ | [1 128 1] / [1 128 1] ✓ |
| blue | 12.528 s | 18.538666666 s | 300.3 ms | 302.633333 ms | 12.2277 – 18.236033333 s | 3 | not tripped | **6.042 s ✗** (+42 ms, tolerance 34 ms) | [0 0 255] / [0 0 255] ✓ |

The other checks under the ruling:
- **(3)** The green cut puts the chapters at **[5 ms, 3.005 s]** and the first subtitle at **1.005 s**. Pass.
- **(4)** The output is written under the given name. Pass.
- **(5)** The copy is 12.032 s (want 12 s), blue first and red last. Pass.

### Remaining failure (observed by hand, not asserted)

The ruling cuts the video exactly. Each of the three cuts has a 180-frame video track of 6.005 s and holds only its own colour. The failing number is the container duration, which runs to the end of the audio:

| cut | video timestamps (ms) | audio timestamps (ms) | audio track duration | container |
|---|---|---|---|---|
| red | 1 … 6006.37 | 7 … 6023 | 6.016 s | 6.022 s |
| green | 1 … 6006.37 | 17 … 6033 | 6.016 s | 6.032 s |
| blue | 1 … 6006.37 | 27 … 6043 | 6.016 s | 6.042 s |

- Each segment's AC-3 is 10 ms longer than its video, and mkvmerge appends each segment's audio straight after the previous segment's audio.
- So on the group timeline the audio falls about 10 ms further behind the video at each VOB ID change: audio starts 7, 17 and 27 ms after the video in the three cuts.
- In blue's cut the audio starts 27 ms after the video and runs 6.016 s. It ends at about 6.043 s, past the video's 6.006 s, so the container is 6.042 s.

This is not a cut error. It does show two things:
- A container-duration check against the IFO time picks up audio drift that builds up per VOB ID.
- mkvmerge's group mux leaves the audio behind the video by about (audio length − video length) for each preceding VOB ID. Here that is 27 ms at blue.

The tolerance was not loosened. Two options are left for the controller, neither applied:
- Assert on the video track's duration (the `DURATION` tag or frame timestamps), since the cut is a video-keyframe cut.
- Make the fixture's audio match its video length more closely. AC-3 can only get within one 32 ms frame.

## Conclusion for Tasks 4/6

**Task 6 (copy):** as planned. Concatenate the run's sectors into the temp VOB and mux it as a `( … )` group. Out-of-order runs come out as one continuous timeline.

**Task 4 (cut):** implement exactly the ruled algorithm:
1. start₀ = Σ(`VOBU_E_PTM − VOBU_S_PTM`) over every NAV before R0. end₀ = start₀ + the same sum over the run's NAVs (R0 ≤ sector ≤ R1).
2. m_s = min(last VOBU before R0, first VOBU of the run) / 2, or 0 if start₀ = 0. m_e = (the run's last VOBU) / 2.
3. Fail with an error if (VOB ID changes among NAVs from the group start to R1) × 10 ms ≥ min(m_s if start₀ > 0, m_e).
4. Otherwise pass `--split parts:<start₀ − m_s>-<end₀ − m_e>` with a single range, which keeps the given output name.
5. Write chapters and VobSub timestamps on the group timeline (from start₀). mkvmerge shifts them by the keyframe it actually cut at.

On this fixture that algorithm gives frame-exact video for every episode. The one open item is how Task 4's tests should treat the audio drift above, which is up to about 10 ms per earlier VOB ID here.
