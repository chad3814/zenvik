package mux

import (
	"strconv"
	"strings"
)

// Track is one output track of a mux job.
type Track struct {
	ID       int    // mkvmerge track ID in the input
	Type     string // "video", "audio" or "subtitles"
	Language string // ISO 639-2; empty keeps mkvmerge's value
	Name     string // empty for no name
	Default  bool
}

// Input is a further input file with its own output tracks.
type Input struct {
	Path   string
	Tracks []Track // output tracks from this file, in output order
}

// Job describes one remux.
type Job struct {
	Input       string   // the input file (a playlist, .mpls); ignored when Concat is set
	Concat      []string // files mkvmerge reads as one input, passed as "( a b … )" (DVD title VOBs)
	ChapterFile string   // chapters to use instead of the inputs' own; "" keeps the inputs' chapters
	Output      string   // output file path
	Tracks      []Track  // output tracks, in output order
	Extra       []Input  // more input files, after the main one (DVD subtitles)
}

// Args returns mkvmerge's arguments for job (without --gui-mode): output, the chapter file, the main input's per-track options and track selection, the track order, the main input (a "( … )" group for Concat), then each extra input's per-track options, selection and path.
func Args(job Job) []string {
	args := []string{"-o", job.Output}
	if job.ChapterFile != "" {
		args = append(args, "--chapters", job.ChapterFile)
	}
	args = append(args, fileArgs(job.Tracks)...)
	var order []string
	for _, t := range job.Tracks {
		order = append(order, "0:"+strconv.Itoa(t.ID))
	}
	for n, in := range job.Extra {
		for _, t := range in.Tracks {
			order = append(order, strconv.Itoa(n+1)+":"+strconv.Itoa(t.ID))
		}
	}
	if len(order) > 0 {
		args = append(args, "--track-order", strings.Join(order, ","))
	}
	if job.ChapterFile != "" {
		args = append(args, "--no-chapters")
	}
	if len(job.Concat) > 0 {
		args = append(args, "(")
		args = append(args, job.Concat...)
		args = append(args, ")")
	} else {
		args = append(args, job.Input)
	}
	for _, in := range job.Extra {
		args = append(args, fileArgs(in.Tracks)...)
		args = append(args, in.Path)
	}
	return args
}

// fileArgs returns the per-track options and the track selection that
// precede one input file.
func fileArgs(tracks []Track) []string {
	var args, video, audio, subs []string
	for _, t := range tracks {
		id := strconv.Itoa(t.ID)
		switch t.Type {
		case "video":
			video = append(video, id)
		case "audio":
			audio = append(audio, id)
		case "subtitles":
			subs = append(subs, id)
		}
		if t.Language != "" {
			args = append(args, "--language", id+":"+t.Language)
		}
		if t.Name != "" {
			args = append(args, "--track-name", id+":"+t.Name)
		}
		flag := "no"
		if t.Default {
			flag = "yes"
		}
		args = append(args, "--default-track-flag", id+":"+flag)
	}
	args = append(args, selection("--video-tracks", "--no-video", video)...)
	args = append(args, selection("--audio-tracks", "--no-audio", audio)...)
	return append(args, selection("--subtitle-tracks", "--no-subtitles", subs)...)
}

func selection(flag, none string, ids []string) []string {
	if len(ids) == 0 {
		return []string{none}
	}
	return []string{flag, strings.Join(ids, ",")}
}
