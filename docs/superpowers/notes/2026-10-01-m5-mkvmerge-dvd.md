# M5: mkvmerge DVD behavior (verified 2026-10-01)

**Gate result: both original assumptions were false; the controller ruled on them (below).**

## Conclusions (ruled)

- Subtitles: mkvmerge ignores DVD subpictures in VOBs -> zenvik extracts VobSub (Task 10).
- Multiple VOBs: use `( a b )`, never `+`; mkvmerge auto-chains sibling VOBs.
- Each title VOB starts with a NAV pack; the extractor's timestamps rely on NAV packs at VOBU starts. `TestAuthorDVDAndMkvmerge` pins all of this.

- Tools: mkvmerge v102.0 ('Little Houses') 64-bit, ffmpeg 9.0.2, dvdauthor/spumux 0.7.2 (Homebrew 0.7.2_4), macOS (Darwin, arm64).
- Fixture: `AuthorDVD(ctx, dir, 15)`; VTS_01_1.VOB is 562 sectors in total before the split (split at the NAV pack nearest the middle: VTS_01_1.VOB = 1,150,976 bytes, VTS_01_2.VOB = 1,136,640 bytes). `ffprobe` sees all five streams in the spumux output (video, 2x ac3, `dvd_subtitle`). A raw scan of the VOB finds the private-stream-1 sub-stream 0x20 packet (the subtitle SPU) at sector 188 of VTS_01_1.VOB. Sub-streams 0x80 and 0x81 appear 86 times each.

## (a) `mkvmerge -J VTS_01_1.VOB` tracks (type, stream_id, sub_stream_id)

Observed: `["audio 189 128" "audio 189 129" "video 224 0"]`.

Expected: those three plus `"subtitles 189 32"`.

- Video is stream_id 224 and AC-3 is stream_id 189 with sub_stream_id 128+n, as assumed.
- **mkvmerge v102 reports no subtitle track for a VOB.** I reproduced this with the concatenated VOB, with VTS_01_2.VOB alone, and with a second fixture of 6 separate SPUs (2 colours, 1 s each, spread over 12 s). `mkvmerge --identify` lists only Track IDs 0 to 2 (video, audio, audio). The PS reader apparently does not demux DVD subpictures (the 0x20 to 0x3F sub-streams). The subtitle data is present in the VOB, but mkvmerge ignores it. The subtitle stream cannot be passed through to the MKV by mkvmerge, so it never arrives as a VobSub track.

## (b) `VTS_01_1.VOB + VTS_01_2.VOB` muxed duration

Observed for a 15 s title: **22.808 s**, 3 tracks (expected 15 +/- 0.25 s and 4 tracks).

Cause: **mkvmerge automatically chains sibling VOBs.** `mkvmerge -J VTS_01_1.VOB` reports `container.properties.other_file = [".../VTS_01_2.VOB"]`, and a bare `VTS_01_1.VOB` is already 15.02 s long. Appending VTS_01_2.VOB with `+` then adds the second half again (15.02 + 7.79 = 22.81 s). It also logs "invalid data which were skipped before timestamp 00:00:15.020" for the audio tracks.

Measured durations:

| mkvmerge input | duration | tracks |
|---|---|---|
| `VTS_01_1.VOB` | 15.02 s | 3 |
| `VTS_01_2.VOB` | 7.788 s | 3 |
| concatenated single file (`cat` of both) | 15.02 s | 3 |
| `VTS_01_1.VOB + VTS_01_2.VOB` | 22.808 s | 3 |
| `( VTS_01_1.VOB )` | 7.813 s (sibling chaining off) | 3 |
| `( VTS_01_1.VOB VTS_01_2.VOB )` | 15.02 s | 3 |

Candidate mechanisms that give the full, correct duration: pass the whole VOB list inside one pair of parentheses, `( a.VOB b.VOB )`, or give mkvmerge only the first VOB and let it auto-chain. Use the parenthesized form, because it works whether or not the sibling has the same name pattern, and a single parenthesized file turns auto-chaining off. These have not been used in the mux code yet.

## (c) VobSub codec private data after muxing

Not observable: the muxed MKV has no subtitle track at all (see (a)), so there is no codec private data to read. Conclusion: **no palette** (not because mkvmerge writes none, but because mkvmerge writes no subtitle track).

The video track's codec private data is the MPEG-2 sequence header (`000001b3...`), unrelated to the palette.

## Consequence for Task 6/9

Subtitle tracks cannot come from mkvmerge reading the VOBs: either the subtitle SPUs must be demuxed and muxed as a separate VobSub (`.idx` plus `.sub`, which mkvmerge does support, and which needs the DVD palette from the IFO) or DVD subtitles must be dropped and documented as a limitation. The multi-VOB mux must also use `( a.VOB b.VOB )` instead of `a.VOB + b.VOB`. This needs a decision before Task 6/9 proceeds.
