package testdisc

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/chad3814/zenvik/dvd"
)

// EpisodeColors are the solid colours of AuthorEpisodesDVD's titles 2–4.
var EpisodeColors = []string{"red", "green", "blue"}

var episodeTones = []int{440, 660, 880}

// AuthorEpisodesDVD builds a real DVD-Video folder at dir (dir/VIDEO_TS)
// with one title set and four titles, each its own <vob> (so its own VOB
// ID and its own PTS start), all inside VTS_01_1.VOB: title 1 is 0.5 s of
// black with silent AC-3; titles 2–4 are 6 s of solid red, green and blue
// with 440, 660 and 880 Hz AC-3, chapters at 0 and 3 s, and an English
// subtitle from 1 s to 3 s. Each episode therefore starts and ends
// mid-file.
func AuthorEpisodesDVD(ctx context.Context, dir string) error {
	work, err := os.MkdirTemp("", "zenvik-episodes-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	enc := []string{"-target", "ntsc-dvd", "-aspect", "4:3", "-c:a", "ac3", "-b:a", "192k", "-ac", "2"}
	if err := runTool(ctx, work, nil, nil, "ffmpeg", append([]string{"-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=720x480:r=30000/1001:d=0.5",
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "0.5"}, append(enc, "black.mpg")...)...); err != nil {
		return err
	}
	if err := runTool(ctx, work, nil, nil, "ffmpeg", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black@0.0:s=720x480,format=rgba,drawbox=x=200:y=400:w=320:h=40:color=white@1.0:t=fill",
		"-frames:v", "1", "sub.png"); err != nil {
		return err
	}
	spu := `<subpictures format="NTSC"><stream><spu start="00:00:01.00" end="00:00:03.00" image="sub.png" force="no"/></stream></subpictures>`
	if err := os.WriteFile(filepath.Join(work, "sub.xml"), []byte(spu), 0o644); err != nil {
		return err
	}
	for i, color := range EpisodeColors {
		raw := fmt.Sprintf("ep%d.mpg", i+1)
		if err := runTool(ctx, work, nil, nil, "ffmpeg", append([]string{"-loglevel", "error", "-y",
			"-f", "lavfi", "-i", "color=c=" + color + ":s=720x480:r=30000/1001:d=6",
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:sample_rate=48000:duration=6", episodeTones[i])}, append(enc, raw)...)...); err != nil {
			return err
		}
		in, err := os.Open(filepath.Join(work, raw))
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(work, fmt.Sprintf("ep%d_sub.mpg", i+1)))
		if err != nil {
			in.Close()
			return err
		}
		err = runTool(ctx, work, in, out, "spumux", "-s", "0", "sub.xml")
		in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	var dest bytes.Buffer
	if err := xml.EscapeText(&dest, []byte(abs)); err != nil {
		return err
	}
	x := fmt.Sprintf(`<dvdauthor dest="%s">
  <vmgm />
  <titleset>
    <titles>
      <video format="ntsc" aspect="4:3" />
      <audio lang="en" />
      <subpicture lang="en" />
      <pgc><vob file="black.mpg" /></pgc>
      <pgc><vob file="ep1_sub.mpg" chapters="0,3" /></pgc>
      <pgc><vob file="ep2_sub.mpg" chapters="0,3" /></pgc>
      <pgc><vob file="ep3_sub.mpg" chapters="0,3" /></pgc>
    </titles>
  </titleset>
</dvdauthor>
`, dest.String())
	if err := os.WriteFile(filepath.Join(work, "dvd.xml"), []byte(x), 0o644); err != nil {
		return err
	}
	return runTool(ctx, work, nil, nil, "dvdauthor", "-x", "dvd.xml")
}

// FrameColor returns the average RGB of a video's first frame, or of the
// frame 0.5 s before its end when fromEnd is set, using ffmpeg.
func FrameColor(ctx context.Context, path string, fromEnd bool) ([3]byte, error) {
	args := []string{"-v", "error"}
	if fromEnd {
		args = append(args, "-sseof", "-0.5")
	}
	args = append(args, "-i", path, "-frames:v", "1", "-vf", "scale=1:1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-")
	out, err := exec.CommandContext(ctx, "ffmpeg", args...).Output()
	if err != nil {
		return [3]byte{}, fmt.Errorf("testdisc: ffmpeg frame colour of %s: %w", path, err)
	}
	if len(out) < 3 {
		return [3]byte{}, fmt.Errorf("testdisc: no frame from %s", path)
	}
	return [3]byte{out[0], out[1], out[2]}, nil
}

// Dominant names the clearly dominant primary of rgb, "black" when all
// channels are dark, or "" when none dominates.
func Dominant(rgb [3]byte) string {
	r, g, b := int(rgb[0]), int(rgb[1]), int(rgb[2])
	switch {
	case r < 40 && g < 40 && b < 40:
		return "black"
	case r > 90 && r > 2*g && r > 2*b:
		return "red"
	case g > 90 && g > 2*r && g > 2*b:
		return "green"
	case b > 90 && b > 2*r && b > 2*g:
		return "blue"
	}
	return ""
}

// AddMixedEpisodeTitles adds two titles to an AuthorEpisodesDVD folder by
// rewriting its IFOs: title 5 plays episode 1 then the 0.5 s black cell (a
// short trailing stray), and title 6 plays episode 3 then episode 1 (out of
// sector order). The IFOs are re-encoded with VMGFile/VTSFile, which keep
// the title table, attributes, PGCs, chapters and VOBU map that zenvik uses.
func AddMixedEpisodeTitles(dir string) error {
	vd := filepath.Join(dir, "VIDEO_TS")
	vb, err := os.ReadFile(filepath.Join(vd, "VIDEO_TS.IFO"))
	if err != nil {
		return err
	}
	vmg, err := dvd.ParseVMG(vb)
	if err != nil {
		return err
	}
	tb, err := os.ReadFile(filepath.Join(vd, "VTS_01_0.IFO"))
	if err != nil {
		return err
	}
	vts, err := dvd.ParseVTS(tb)
	if err != nil {
		return err
	}
	if len(vts.Titles) < 4 {
		return fmt.Errorf("testdisc: %s is not an AuthorEpisodesDVD folder", dir)
	}
	pgcOf := func(title int) *dvd.PGC { return vts.PGCs[vts.Titles[title-1][0].PGC-1] }
	join := func(parts ...*dvd.PGC) *dvd.PGC {
		p := *parts[0]
		p.Cells, p.Programs = nil, nil
		var total time.Duration
		for _, q := range parts {
			for _, c := range q.Cells {
				p.Cells = append(p.Cells, c)
				p.Programs = append(p.Programs, len(p.Cells))
				total += c.Time.Duration()
			}
		}
		p.Time = dvd.NewTime(total, dvd.Rate30)
		return &p
	}
	for _, parts := range [][]*dvd.PGC{{pgcOf(2), pgcOf(1)}, {pgcOf(4), pgcOf(2)}} {
		p := join(parts...)
		vts.PGCs = append(vts.PGCs, p)
		var ptt []dvd.PartOfTitle
		for k := range p.Programs {
			ptt = append(ptt, dvd.PartOfTitle{PGC: len(vts.PGCs), Program: k + 1})
		}
		vts.Titles = append(vts.Titles, ptt)
		vmg.Titles = append(vmg.Titles, dvd.TitleEntry{Angles: 1, Chapters: len(ptt), TitleSet: 1, TitleSetTitle: len(vts.Titles)})
	}
	if err := os.WriteFile(filepath.Join(vd, "VIDEO_TS.IFO"), VMGFile(vmg), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(vd, "VTS_01_0.IFO"), VTSFile(vts), 0o644)
}
