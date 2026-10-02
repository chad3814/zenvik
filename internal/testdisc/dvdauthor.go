package testdisc

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AuthorDVD builds a real, playable DVD-Video folder at dir (dir/VIDEO_TS)
// with ffmpeg, spumux and dvdauthor: one title of seconds (at least 15) of
// NTSC 4:3 MPEG-2 video with two AC-3 stereo tracks (English at 440 Hz,
// French at 660 Hz), one English subtitle stream shown from 2 s to 6 s, and
// chapters requested at 0, 1/3 and 2/3 of the length (dvdauthor moves them
// to the nearest VOBU). The title VOB is then split by SplitTitleVOB.
func AuthorDVD(ctx context.Context, dir string, seconds int) error {
	if seconds < 15 {
		return fmt.Errorf("testdisc: AuthorDVD needs at least 15 seconds, got %d", seconds)
	}
	work, err := os.MkdirTemp("", "zenvik-dvd-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	if err := runTool(ctx, work, nil, nil, "ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=720x480:rate=30000/1001:duration=%d", seconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%d", seconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=660:sample_rate=48000:duration=%d", seconds),
		"-map", "0", "-map", "1", "-map", "2", "-target", "ntsc-dvd", "-aspect", "4:3",
		"-c:a", "ac3", "-b:a", "192k", "-ac", "2", "movie.mpg"); err != nil {
		return err
	}
	if err := runTool(ctx, work, nil, nil, "ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black@0.0:s=720x480,format=rgba,drawbox=x=200:y=400:w=320:h=40:color=white@1.0:t=fill",
		"-frames:v", "1", "sub.png"); err != nil {
		return err
	}
	spu := `<subpictures format="NTSC"><stream>` +
		`<spu start="00:00:02.00" end="00:00:06.00" image="sub.png" force="no"/>` +
		`</stream></subpictures>`
	if err := os.WriteFile(filepath.Join(work, "sub.xml"), []byte(spu), 0o644); err != nil {
		return err
	}
	in, err := os.Open(filepath.Join(work, "movie.mpg"))
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(filepath.Join(work, "movie_sub.mpg"))
	if err != nil {
		return err
	}
	err = runTool(ctx, work, in, out, "spumux", "-s", "0", "sub.xml")
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var dest bytes.Buffer
	if err := xml.EscapeText(&dest, []byte(abs)); err != nil {
		return err
	}
	cfg := fmt.Sprintf(`<dvdauthor dest="%s">
  <vmgm />
  <titleset>
    <titles>
      <video format="ntsc" aspect="4:3" />
      <audio lang="en" />
      <audio lang="fr" />
      <subpicture lang="en" />
      <pgc>
        <vob file="movie_sub.mpg" chapters="0,%d,%d" />
      </pgc>
    </titles>
  </titleset>
</dvdauthor>
`, dest.String(), seconds/3, 2*seconds/3)
	if err := os.WriteFile(filepath.Join(work, "dvd.xml"), []byte(cfg), 0o644); err != nil {
		return err
	}
	if err := runTool(ctx, work, nil, nil, "dvdauthor", "-x", "dvd.xml"); err != nil {
		return err
	}
	return SplitTitleVOB(dir)
}

// SplitTitleVOB splits dir/VIDEO_TS/VTS_01_1.VOB at the NAV pack nearest its
// middle into VTS_01_1.VOB and VTS_01_2.VOB. Cell sector addresses in the
// IFO are relative to the concatenated title VOBs, so the IFO stays valid.
func SplitTitleVOB(dir string) error {
	p := filepath.Join(dir, "VIDEO_TS", "VTS_01_1.VOB")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	n := len(b) / 2048
	best := -1
	for s := 1; s < n; s++ {
		if isNavPack(b[s*2048:]) && (best < 0 || absInt(s-n/2) < absInt(best-n/2)) {
			best = s
		}
	}
	if best < 0 {
		return fmt.Errorf("testdisc: %s has no NAV pack to split at", p)
	}
	if err := os.WriteFile(filepath.Join(dir, "VIDEO_TS", "VTS_01_2.VOB"), b[best*2048:], 0o644); err != nil {
		return err
	}
	return os.WriteFile(p, b[:best*2048], 0o644)
}

// isNavPack reports whether p starts with an MPEG-2 pack header followed
// directly by a system header, which is how every DVD NAV pack begins.
func isNavPack(p []byte) bool {
	return len(p) >= 18 && bytes.Equal(p[0:4], []byte{0, 0, 1, 0xBA}) && bytes.Equal(p[14:18], []byte{0, 0, 1, 0xBB})
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// runTool runs name in dir with VIDEO_FORMAT=NTSC, optionally wiring stdin
// and stdout, and folds the tool's stderr into any error.
func runTool(ctx context.Context, dir string, stdin *os.File, stdout *os.File, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VIDEO_FORMAT=NTSC")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = stdin
	}
	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = &stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("testdisc: %s: %w: %s", name, err, stderr.Bytes())
	}
	return nil
}
