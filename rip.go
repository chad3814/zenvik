package zenvik

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/chad3814/zenvik/internal/mount"
	"github.com/chad3814/zenvik/internal/mux"
)

// Phase identifies a stage of Rip.
type Phase int

// Rip phases, in order.
const (
	PhaseMounting   Phase = 1 // attaching an ISO image
	PhaseScanning   Phase = 2 // mkvmerge scanning the playlist's files
	PhaseMuxing     Phase = 3 // mkvmerge writing the output
	PhaseFinalizing Phase = 4 // renaming the finished file
)

func (p Phase) String() string {
	switch p {
	case PhaseMounting:
		return "mounting"
	case PhaseScanning:
		return "scanning"
	case PhaseMuxing:
		return "muxing"
	case PhaseFinalizing:
		return "finalizing"
	}
	return "unknown"
}

// Progress reports how far a Rip has come.
type Progress struct {
	Phase      Phase
	Fraction   float64 // 0..1 within Phase
	BytesDone  int64   // estimate: Fraction × BytesTotal
	BytesTotal int64   // the title's Size
}

// RipOptions control Rip.
type RipOptions struct {
	OutputPath string         // final MKV path; parent directories are created
	Overwrite  bool           // replace an existing output file
	DryRun     bool           // resolve everything and return the mkvmerge command without running it
	OnProgress func(Progress) // optional; called on Rip's goroutine
}

// RipResult describes a finished (or dry-run) Rip.
type RipResult struct {
	OutputPath string
	Duration   time.Duration
	Warnings   []string
	Command    []string // the mkvmerge program and arguments
}

// Rip remuxes title t to an MKV file at opts.OutputPath with mkvmerge,
// mounting the disc image first if needed. The output appears only when
// muxing succeeds: mkvmerge writes "<OutputPath>.partial", which is renamed
// at the end and removed on failure or cancellation. A Disc must not be
// ripped from concurrently.
func (d *Disc) Rip(ctx context.Context, t *Title, opts RipOptions) (res *RipResult, err error) {
	if d.src == nil {
		return nil, fmt.Errorf("zenvik: disc is closed: %w", fs.ErrClosed)
	}
	if !slices.Contains(d.Titles, t) {
		return nil, errors.New("zenvik: title does not belong to this disc")
	}
	if t.Encrypted {
		return nil, fmt.Errorf("%w: title %s", ErrEncrypted, t.ID)
	}
	if opts.OutputPath == "" {
		return nil, errors.New("zenvik: RipOptions.OutputPath is required")
	}
	if !opts.Overwrite && !opts.DryRun {
		if _, err := os.Stat(opts.OutputPath); err == nil {
			return nil, fmt.Errorf("%w: %s", ErrOutputExists, opts.OutputPath)
		}
	}
	mk, err := mux.Find(ctx, "")
	if err != nil {
		return nil, err
	}
	report := func(p Phase, f float64) {
		if opts.OnProgress != nil {
			opts.OnProgress(Progress{Phase: p, Fraction: f, BytesDone: int64(f * float64(t.Size)), BytesTotal: t.Size})
		}
	}

	root, release, err := d.mountRoot(ctx, report)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr := release(); rerr != nil {
			if res != nil {
				res.Warnings = append(res.Warnings, rerr.Error())
			} else if err == nil {
				err = rerr
			}
		}
	}()

	playlist := filepath.Join(root, "BDMV", "PLAYLIST", t.ID+".mpls")
	ident, err := mk.Identify(ctx, playlist)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	tracks, warnings := mapTracks(t, ident)
	if len(tracks) == 0 {
		return nil, fmt.Errorf("%w: mkvmerge found none of title %s's streams", ErrMuxFailed, t.ID)
	}
	partial := opts.OutputPath + ".partial"
	job := mux.Job{Input: playlist, Output: partial, Tracks: tracks}
	command := append([]string{mk.Path}, mux.Args(job)...)
	if opts.DryRun {
		return &RipResult{OutputPath: opts.OutputPath, Duration: t.Duration, Warnings: warnings, Command: command}, nil
	}

	if err := os.MkdirAll(filepath.Dir(opts.OutputPath), 0o755); err != nil {
		return nil, err
	}
	muxed, err := mk.Mux(ctx, job, func(p mux.Phase, f float64) {
		if p == mux.PhaseScanning {
			report(PhaseScanning, f)
			return
		}
		report(PhaseMuxing, f)
	})
	if err != nil {
		os.Remove(partial)
		return nil, err
	}
	report(PhaseFinalizing, 0)
	if opts.Overwrite {
		if err := os.Remove(opts.OutputPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			os.Remove(partial)
			return nil, err
		}
	}
	if err := os.Rename(partial, opts.OutputPath); err != nil {
		os.Remove(partial)
		return nil, err
	}
	report(PhaseFinalizing, 1)
	return &RipResult{
		OutputPath: opts.OutputPath,
		Duration:   t.Duration,
		Warnings:   append(warnings, muxed.Warnings...),
		Command:    command,
	}, nil
}

// mountRoot returns the directory that holds BDMV for mkvmerge to read,
// mounting an ISO image if needed, and a function that releases it.
func (d *Disc) mountRoot(ctx context.Context, report func(Phase, float64)) (string, func() error, error) {
	if d.src.Kind != ISO {
		return d.src.Path, func() error { return nil }, nil
	}
	report(PhaseMounting, 0)
	m, err := mount.Attach(ctx, d.src.Path)
	if err != nil {
		return "", nil, fmt.Errorf("%w (mount the image yourself, or extract it, and pass the folder instead)", err)
	}
	release := func() error { return m.Detach(ctx) }
	if _, err := os.Stat(filepath.Join(m.Dir, "BDMV", "index.bdmv")); err != nil {
		_ = release()
		return "", nil, fmt.Errorf("zenvik: mounted %s at %s but found no BDMV/index.bdmv: %w", d.src.Path, m.Dir, err)
	}
	report(PhaseMounting, 1)
	return m.Dir, release, nil
}

// mapTracks matches the title's playlist streams to mkvmerge's tracks by
// PID, in stream-number order (video, audio, subtitles). It sets languages
// from the playlist, names audio tracks, and makes the first video and
// first audio track default. Streams one side has and the other doesn't
// are skipped with a warning.
func mapTracks(t *Title, id *mux.Identification) ([]mux.Track, []string) {
	byPID := map[uint16]mux.IdentifiedTrack{}
	for _, it := range id.Tracks {
		byPID[it.PID] = it
	}
	var tracks []mux.Track
	var warnings []string
	used := map[int]bool{}
	add := func(pid uint16, kind, lang, name string, def bool) {
		it, ok := byPID[pid]
		if !ok || it.Type != kind {
			warnings = append(warnings, fmt.Sprintf("title %s: %s stream PID 0x%04X not found by mkvmerge; skipped", t.ID, kind, pid))
			return
		}
		used[it.ID] = true
		tracks = append(tracks, mux.Track{ID: it.ID, Type: kind, Language: lang, Name: name, Default: def})
	}
	for i, v := range t.Video {
		add(v.PID, "video", "", "", i == 0)
	}
	for i, a := range t.Audio {
		add(a.PID, "audio", a.Language, audioName(a, byPID[a.PID].Channels), i == 0)
	}
	for _, s := range t.Subtitles {
		add(s.PID, "subtitles", s.Language, "", false)
	}
	for _, it := range id.Tracks {
		if !used[it.ID] {
			warnings = append(warnings, fmt.Sprintf("title %s: mkvmerge track %d (%s, PID 0x%04X) is not in the playlist's stream table; skipped", t.ID, it.ID, it.Type, it.PID))
		}
	}
	return tracks, warnings
}

// audioName names an audio track "<codec> <layout>", e.g. "TrueHD 7.1",
// using mkvmerge's channel count when known.
func audioName(a AudioTrack, channels int) string {
	layout := map[int]string{1: "Mono", 2: "Stereo", 6: "5.1", 8: "7.1"}[channels]
	switch {
	case layout != "":
	case channels > 0:
		layout = fmt.Sprintf("%d ch", channels)
	default:
		layout = a.Channels.String()
	}
	return a.Codec.String() + " " + layout
}
