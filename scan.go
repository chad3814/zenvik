package zenvik

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/rank"
)

// defaultMinDuration filters titles shorter than this (spec section 4).
const defaultMinDuration = 2 * time.Minute

type playlistFile struct{ id, name string }

// scanTitles reads the BDMV tree in fsys and returns its titles ranked
// best-first, plus the disc metadata (nil when absent).
func scanTitles(ctx context.Context, fsys fs.FS, minDuration time.Duration, w rank.Weights) ([]*Title, *bluray.DiscMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	files, err := playlistFiles(fsys)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, ErrNoTitles
	}
	meta := readMeta(fsys)
	title1 := title1Playlists(fsys)
	clips := &clipCache{fsys: fsys, m: map[string]clipInfo{}}
	titles := make([]*Title, 0, len(files))
	cands := make([]rank.Candidate, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		t, c := loadTitle(fsys, f, clips, title1[f.id])
		titles = append(titles, t)
		cands = append(cands, c)
	}
	order, infos := rank.Rank(cands, minDuration, w)
	ranked := make([]*Title, len(order))
	for k, i := range order {
		titles[i].Rank = RankInfo(infos[i])
		ranked[k] = titles[i]
	}
	if allEncrypted(ranked) {
		return nil, nil, ErrEncrypted
	}
	return ranked, meta, nil
}

// allEncrypted reports whether some title is encrypted and no unfiltered
// title is unencrypted.
func allEncrypted(titles []*Title) bool {
	sawEncrypted := false
	for _, t := range titles {
		if t.Encrypted {
			sawEncrypted = true
			continue
		}
		if !t.Rank.Filtered {
			return false
		}
	}
	return sawEncrypted
}

func playlistFiles(fsys fs.FS) ([]playlistFile, error) {
	ents, err := fs.ReadDir(fsys, "BDMV/PLAYLIST")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []playlistFile
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !isPlaylistName(name) {
			continue
		}
		out = append(out, playlistFile{id: strings.TrimSuffix(name, ".mpls"), name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

// isPlaylistName reports whether name is five ASCII digits followed by
// ".mpls". The strict form skips stray files (such as macOS "._" metadata)
// and keeps playlist IDs unique on case-sensitive filesystems.
func isPlaylistName(name string) bool {
	id, ok := strings.CutSuffix(name, ".mpls")
	if !ok || len(id) != 5 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func loadTitle(fsys fs.FS, f playlistFile, clips *clipCache, playedByTitle1 bool) (*Title, rank.Candidate) {
	t := &Title{ID: f.id, Angles: 1}
	c := rank.Candidate{ID: f.id, PlayedByTitle1: playedByTitle1}
	b, err := fs.ReadFile(fsys, "BDMV/PLAYLIST/"+f.name)
	if err == nil {
		var p *bluray.Playlist
		if p, err = bluray.ParsePlaylist(b); err == nil {
			fillFromPlaylist(t, p)
		}
	}
	if err != nil {
		c.Problem = "unreadable playlist: " + err.Error()
		return t, c
	}
	seen := map[string]bool{}
	for _, cl := range t.Clips {
		if seen[cl.ID] {
			continue
		}
		seen[cl.ID] = true
		ci := clips.get(cl.ID)
		if ci.problem != "" {
			if c.Problem == "" {
				c.Problem = ci.problem
			}
			continue
		}
		t.Size += ci.size
		t.Encrypted = t.Encrypted || ci.encrypted
	}
	for _, cl := range t.Clips {
		c.Clips = append(c.Clips, rank.Clip{ID: cl.ID, In: cl.In, Out: cl.Out})
	}
	c.Duration = t.Duration
	c.Chapters = len(t.Chapters)
	c.Languages = countLanguages(t)
	c.HasVideo = len(t.Video) > 0
	c.HasAudio = len(t.Audio) > 0
	c.Size = t.Size
	c.Encrypted = t.Encrypted
	return t, c
}

func fillFromPlaylist(t *Title, p *bluray.Playlist) {
	t.Duration = p.Duration()
	for i, start := range p.Chapters() {
		t.Chapters = append(t.Chapters, Chapter{Number: i + 1, Start: start})
	}
	for _, it := range p.Items {
		t.Clips = append(t.Clips, Clip{ID: it.ClipID, In: it.In.Duration(), Out: it.Out.Duration()})
		t.Angles = max(t.Angles, len(it.Angles)+1)
	}
	if len(p.Items) == 0 {
		return
	}
	stn := p.Items[0].STN
	for _, s := range stn.Video {
		t.Video = append(t.Video, VideoTrack{PID: s.PID, Codec: s.Coding, Format: s.VideoFormat, FrameRate: s.FrameRate, DynamicRange: s.DynamicRange})
	}
	for _, s := range stn.Audio {
		t.Audio = append(t.Audio, AudioTrack{PID: s.PID, Codec: s.Coding, Language: s.Language, Channels: s.AudioFormat, SampleRate: s.SampleRate})
	}
	for _, s := range stn.PG {
		t.Subtitles = append(t.Subtitles, SubtitleTrack{PID: s.PID, Codec: s.Coding, Language: s.Language})
	}
}

func countLanguages(t *Title) int {
	set := map[string]bool{}
	for _, a := range t.Audio {
		if a.Language != "" {
			set[a.Language] = true
		}
	}
	for _, s := range t.Subtitles {
		if s.Language != "" {
			set[s.Language] = true
		}
	}
	return len(set)
}

type clipInfo struct {
	size      int64
	encrypted bool
	problem   string // non-empty when the stream file is missing, empty or unreadable
}

// clipCache probes each stream file at most once per scan.
type clipCache struct {
	fsys fs.FS
	m    map[string]clipInfo
}

func (c *clipCache) get(id string) clipInfo {
	if ci, ok := c.m[id]; ok {
		return ci
	}
	ci := probeClip(c.fsys, id)
	c.m[id] = ci
	return ci
}

func probeClip(fsys fs.FS, id string) clipInfo {
	name := id + ".m2ts"
	f, err := fsys.Open("BDMV/STREAM/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		return clipInfo{problem: "missing stream file " + name}
	}
	if err != nil {
		return clipInfo{problem: fmt.Sprintf("unreadable stream file %s: %v", name, err)}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return clipInfo{problem: fmt.Sprintf("unreadable stream file %s: %v", name, err)}
	}
	if st.Size() == 0 {
		return clipInfo{problem: "empty stream file " + name}
	}
	enc, err := clipEncrypted(f)
	if err != nil {
		return clipInfo{problem: fmt.Sprintf("unreadable stream file %s: %v", name, err)}
	}
	return clipInfo{size: st.Size(), encrypted: enc}
}

// title1Playlists returns the playlist IDs HDMV title 1 may play, or nil
// when title 1 is missing, BD-J, or its movie objects can't be read.
func title1Playlists(fsys fs.FS) map[string]bool {
	b, err := fs.ReadFile(fsys, "BDMV/index.bdmv")
	if err != nil {
		return nil
	}
	idx, err := bluray.ParseIndex(b)
	if err != nil || len(idx.Titles) == 0 || idx.Titles[0].Type != bluray.ObjectHDMV {
		return nil
	}
	b, err = fs.ReadFile(fsys, "BDMV/MovieObject.bdmv")
	if err != nil {
		return nil
	}
	mo, err := bluray.ParseMovieObjects(b)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, n := range mo.Playlists(int(idx.Titles[0].MovieObjectID)) {
		out[fmt.Sprintf("%05d", n)] = true
	}
	return out
}

// readMeta returns the disc library metadata, or nil if there is none.
func readMeta(fsys fs.FS) *bluray.DiscMeta {
	ents, err := fs.ReadDir(fsys, "BDMV/META/DL")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	name := bluray.PickMetaFile(names)
	if name == "" {
		return nil
	}
	b, err := fs.ReadFile(fsys, "BDMV/META/DL/"+name)
	if err != nil {
		return nil
	}
	m, err := bluray.ParseMeta(b)
	if err != nil {
		return nil
	}
	return m
}
