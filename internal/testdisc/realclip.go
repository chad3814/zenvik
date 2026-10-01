package testdisc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/chad3814/zenvik/bluray"
)

// ClipStartTicks is the first presentation timestamp (1.4 s) of FFmpegClip
// output; play items use it as their IN time.
const ClipStartTicks = bluray.Ticks(63000)

// FFmpegClip writes a real M2TS clip (192-byte source packets) with ffmpeg:
// H.264 1080p 23.976 video on PID 0x1011 and AC-3 stereo audio on PID
// 0x1100, tagged "eng" in the stream, lasting seconds.
func FFmpegClip(ctx context.Context, path string, seconds, toneHz int) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=1920x1080:rate=24000/1001:duration=%d", seconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=%d", toneHz, seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "ac3", "-b:a", "192k", "-metadata:s:a:0", "language=eng",
		"-streamid", "0:0x1011", "-streamid", "1:0x1100",
		"-mpegts_m2ts_mode", "1", "-f", "mpegts", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, out)
	}
	return nil
}

// RealMovieDisc builds a two-clip disc from FFmpegClip output generated in
// scratch: playlist 00800 plays clips 00001 and 00002 (seconds each) with
// three chapters, and title 1 plays it. Its STN marks the audio as Japanese,
// unlike the stream's own "eng" tag.
func RealMovieDisc(ctx context.Context, scratch string, seconds int) (*Disc, error) {
	stn := bluray.STN{
		Video: []bluray.Stream{{PID: 0x1011, Coding: bluray.CodingAVC, VideoFormat: 6, FrameRate: 1}},
		Audio: []bluray.Stream{{PID: 0x1100, Coding: bluray.CodingAC3, AudioFormat: 3, SampleRate: 1, Language: "jpn"}},
	}
	in := ClipStartTicks
	out := in + bluray.Ticks(seconds*bluray.TicksPerSecond)
	d := &Disc{
		Titles:       []bluray.IndexTitle{{Object: bluray.Object{Type: bluray.ObjectHDMV, MovieObjectID: 0}}},
		MovieObjects: []bluray.MovieObject{{Commands: []bluray.NavCommand{CmdPlayPL(800)}}},
		Playlists: map[string]*bluray.Playlist{"00800": {
			Version: "0200",
			Items: []bluray.PlayItem{
				{ClipID: "00001", CodecID: "M2TS", In: in, Out: out, STN: stn},
				{ClipID: "00002", CodecID: "M2TS", ConnectionCondition: 1, In: in, Out: out, STN: stn},
			},
			Marks: []bluray.Mark{
				{Type: bluray.MarkEntry, PlayItem: 0, Time: in, PID: 0xFFFF},
				{Type: bluray.MarkEntry, PlayItem: 0, Time: in + (out-in)/2, PID: 0xFFFF},
				{Type: bluray.MarkEntry, PlayItem: 1, Time: in, PID: 0xFFFF},
			},
		}},
		Clips:     map[string]*bluray.Clip{},
		ClipData:  map[string][]byte{},
		MetaTitle: "Real Movie",
	}
	for i, id := range []string{"00001", "00002"} {
		p := filepath.Join(scratch, id+".m2ts")
		if err := FFmpegClip(ctx, p, seconds, 440*(i+1)); err != nil {
			return nil, err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		d.ClipData[id] = b
		d.Clips[id] = StubClip()
	}
	return d, nil
}

// RealMovie writes RealMovieDisc into dir.
func RealMovie(ctx context.Context, dir string, seconds int) error {
	scratch, err := os.MkdirTemp("", "zenvik-clips-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	d, err := RealMovieDisc(ctx, scratch, seconds)
	if err != nil {
		return err
	}
	return d.WriteDir(dir)
}
