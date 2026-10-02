package zenvik

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/chad3814/zenvik/internal/mux"
	"github.com/chad3814/zenvik/internal/source"
	"github.com/chad3814/zenvik/internal/vobsub"
)

// chapterPlaceholder stands in for the chapter file in a dry run, which
// writes nothing.
const chapterPlaceholder = "<chapters.txt>"

// subtitlePlaceholder stands in for the extracted VobSub .idx in a dry
// run, which extracts nothing.
const subtitlePlaceholder = "<subtitles.idx>"

// dvdJob builds the mux job for DVD title t from the files under root.
// The returned cleanup removes the chapter file and the extracted
// subtitles; call it after muxing. report receives subtitle-extraction
// progress.
func dvdJob(ctx context.Context, mk *mux.Mkvmerge, root string, t *Title, output string, dryRun bool, report func(Phase, float64)) (mux.Job, []string, func(), error) {
	none := func() {}
	fsys := os.DirFS(root)
	dir := source.FindName(fsys, ".", "VIDEO_TS", true)
	if dir == "" {
		return mux.Job{}, nil, none, fmt.Errorf("zenvik: no VIDEO_TS folder under %s", root)
	}
	var inputs []string
	for _, c := range t.Clips {
		n := source.FindName(fsys, dir, c.ID, false)
		if n == "" {
			return mux.Job{}, nil, none, fmt.Errorf("zenvik: %s is missing from %s", c.ID, filepath.Join(root, dir))
		}
		inputs = append(inputs, filepath.Join(root, dir, n))
	}
	if len(inputs) == 0 {
		return mux.Job{}, nil, none, fmt.Errorf("%w: title %s has no VOB files", ErrMuxFailed, t.ID)
	}
	ident, err := mk.Identify(ctx, inputs[0])
	if err != nil {
		if ctx.Err() != nil {
			return mux.Job{}, nil, none, ctx.Err()
		}
		return mux.Job{}, nil, none, err
	}
	tracks, warnings := mapDVDTracks(t, ident)
	warnings = append(warnings, ident.Warnings...)
	if len(tracks) == 0 {
		return mux.Job{}, nil, none, fmt.Errorf("%w: mkvmerge found no tracks in title %s", ErrMuxFailed, t.ID)
	}
	job := mux.Job{Concat: inputs, Output: output, Tracks: tracks}
	cleanup := none
	if len(t.Subtitles) > 0 && t.dvd != nil {
		if dryRun {
			job.Extra = []mux.Input{{Path: subtitlePlaceholder, Tracks: subtitleTracks(t.Subtitles)}}
		} else {
			dir, err := os.MkdirTemp("", "zenvik-subtitles-")
			if err != nil {
				return mux.Job{}, nil, none, err
			}
			removeDir := func() { os.RemoveAll(dir) }
			streams := make([]vobsub.Stream, len(t.Subtitles))
			byID := map[int]SubtitleTrack{}
			for i, s := range t.Subtitles {
				id := int(s.PID - 0xBD20)
				streams[i] = vobsub.Stream{ID: id, Language: t.dvd.subLang[s.PID]}
				byID[id] = s
			}
			report(PhaseSubtitles, 0)
			res, err := vobsub.Extract(ctx, vobsub.Params{
				VOBs: inputs, Cells: t.dvd.cells, Streams: streams, Palette: t.dvd.palette,
				Width: t.dvd.width, Height: t.dvd.height, Dir: dir,
				OnProgress: func(done, total int64) {
					if total > 0 {
						report(PhaseSubtitles, float64(done)/float64(total))
					}
				},
			})
			if err != nil {
				removeDir()
				if ctx.Err() != nil {
					return mux.Job{}, nil, none, ctx.Err()
				}
				return mux.Job{}, nil, none, err
			}
			var kept []SubtitleTrack
			for _, s := range res.Streams {
				kept = append(kept, byID[s.ID])
			}
			for _, s := range t.Subtitles {
				if !slices.ContainsFunc(kept, func(k SubtitleTrack) bool { return k.PID == s.PID }) {
					warnings = append(warnings, fmt.Sprintf("title %s: subtitle stream 0x%04X has no subtitles in this title; skipped", t.ID, s.PID))
				}
			}
			if len(kept) > 0 {
				job.Extra = []mux.Input{{Path: res.IDX, Tracks: subtitleTracks(kept)}}
			}
			cleanup = removeDir
		}
	}
	if len(t.Chapters) > 0 {
		job.ChapterFile = chapterPlaceholder
		if !dryRun {
			p, err := writeChapterFile(t.Chapters)
			if err != nil {
				cleanup()
				return mux.Job{}, nil, none, err
			}
			job.ChapterFile = p
			prev := cleanup
			cleanup = func() { os.Remove(p); prev() }
		}
	}
	return job, warnings, cleanup, nil
}

// mapDVDTracks matches the title's video and audio tracks to mkvmerge's by
// MPEG-PS stream key (stream_id<<8 | sub_stream_id, or stream_id alone):
// video first, then audio in IFO order, then any track the IFO doesn't
// describe, kept without a language or name. The first video and audio
// tracks are default. Subtitles are not here: mkvmerge does not read them
// from VOBs, so they come from an extracted VobSub file.
func mapDVDTracks(t *Title, id *mux.Identification) ([]mux.Track, []string) {
	key := func(it mux.IdentifiedTrack) uint16 {
		if it.SubStreamID != 0 {
			return it.StreamID<<8 | it.SubStreamID
		}
		return it.StreamID
	}
	byKey := map[uint16]mux.IdentifiedTrack{}
	for _, it := range id.Tracks {
		byKey[key(it)] = it
	}
	var tracks []mux.Track
	var warnings []string
	used := map[int]bool{}
	haveVideo, haveAudio := false, false
	add := func(pid uint16, kind, lang, name string) {
		it, ok := byKey[pid]
		if !ok || it.Type != kind {
			warnings = append(warnings, fmt.Sprintf("title %s: %s stream 0x%04X not found by mkvmerge; skipped", t.ID, kind, pid))
			return
		}
		if used[it.ID] {
			return
		}
		used[it.ID] = true
		if norm, ok := normalizeLanguage(lang); ok {
			lang = norm
		} else {
			lang = ""
		}
		def := false
		switch kind {
		case "video":
			def, haveVideo = !haveVideo, true
		case "audio":
			def, haveAudio = !haveAudio, true
		}
		tracks = append(tracks, mux.Track{ID: it.ID, Type: kind, Language: lang, Name: name, Default: def})
	}
	for _, v := range t.Video {
		add(v.PID, "video", "", "")
	}
	for _, a := range t.Audio {
		name := audioName(a, byKey[a.PID].Channels)
		if a.Description != "" {
			name += " (" + a.Description + ")"
		}
		add(a.PID, "audio", a.Language, name)
	}
	for _, it := range id.Tracks {
		if used[it.ID] {
			continue
		}
		used[it.ID] = true
		if it.Type == "subtitles" {
			warnings = append(warnings, fmt.Sprintf("title %s: mkvmerge track %d (subtitles) skipped; DVD subtitles come from the extracted VobSub file", t.ID, it.ID))
			continue
		}
		tracks = append(tracks, mux.Track{ID: it.ID, Type: it.Type})
		warnings = append(warnings, fmt.Sprintf("title %s: mkvmerge track %d (%s) is not described by the IFO; kept without a language", t.ID, it.ID, it.Type))
	}
	return tracks, warnings
}

// writeChapterFile writes chapters in mkvmerge's simple (OGM) chapter
// format to a temporary file and returns its path.
func writeChapterFile(chapters []Chapter) (string, error) {
	f, err := os.CreateTemp("", "zenvik-chapters-*.txt")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, c := range chapters {
		fmt.Fprintf(&b, "CHAPTER%02d=%s\nCHAPTER%02dNAME=Chapter %02d\n", i+1, ogmTime(c.Start), i+1, i+1)
	}
	_, werr := f.WriteString(b.String())
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(f.Name())
		if werr != nil {
			return "", werr
		}
		return "", cerr
	}
	return f.Name(), nil
}

// ogmTime formats d as HH:MM:SS.mmm.
func ogmTime(d time.Duration) string {
	ms := d.Milliseconds()
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, ms%1000)
}

// subtitleTracks are the mux tracks of a VobSub input whose streams are
// subs, in .idx order: track i is subs[i]. None is default.
func subtitleTracks(subs []SubtitleTrack) []mux.Track {
	out := make([]mux.Track, len(subs))
	for i, s := range subs {
		lang, _ := normalizeLanguage(s.Language)
		out[i] = mux.Track{ID: i, Type: "subtitles", Language: lang, Name: s.Description}
	}
	return out
}
