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

// Job describes one remux.
type Job struct {
	Input  string  // playlist (.mpls) path
	Output string  // output file path
	Tracks []Track // output tracks, in output order
}

// Args returns mkvmerge's arguments for job (without --gui-mode): output,
// per-track options, the track selection, the track order, then the input.
func Args(job Job) []string {
	args := []string{"-o", job.Output}
	var video, audio, subs, order []string
	for _, t := range job.Tracks {
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
		order = append(order, "0:"+id)
	}
	args = append(args, selection("--video-tracks", "--no-video", video)...)
	args = append(args, selection("--audio-tracks", "--no-audio", audio)...)
	args = append(args, selection("--subtitle-tracks", "--no-subtitles", subs)...)
	if len(order) > 0 {
		args = append(args, "--track-order", strings.Join(order, ","))
	}
	return append(args, job.Input)
}

func selection(flag, none string, ids []string) []string {
	if len(ids) == 0 {
		return []string{none}
	}
	return []string{flag, strings.Join(ids, ",")}
}
