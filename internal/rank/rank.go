// Package rank picks the main feature among a disc's playlists: filter
// unusable candidates, mark duplicates, score the rest, and choose the
// best unencrypted one (design spec section 4).
package rank

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Clip is one play item: a clip and its IN/OUT times on the clip's clock.
type Clip struct {
	ID      string
	In, Out time.Duration
}

func (c Clip) length() time.Duration {
	if c.Out < c.In {
		return 0
	}
	return c.Out - c.In
}

// Candidate describes one playlist.
type Candidate struct {
	ID             string // playlist ID, e.g. "00800"
	Clips          []Clip // play items in playback order
	Duration       time.Duration
	Chapters       int
	Languages      int // distinct audio and subtitle languages
	HasVideo       bool
	Encrypted      bool
	PlayedByTitle1 bool
	// Problem, when non-empty, filters the candidate with this reason
	// (for example an unreadable playlist or a missing stream file).
	Problem string
}

// Info is the ranking result for one candidate.
type Info struct {
	Score       float64
	IsMain      bool
	Ambiguous   bool // on the main title: a runner-up scored within the ambiguity margin
	Filtered    bool
	DuplicateOf string
	Reasons     []string
}

// Weights tunes scoring.
type Weights struct {
	Duration        float64       // points for the longest candidate, scaled by relative duration
	RepeatedClip    float64       // penalty when any clip appears more than once
	TinySegments    float64       // penalty scaled by the fraction of items shorter than TinySegment
	TinySegment     time.Duration //
	ChapterCap      int           // chapters beyond this count earn nothing
	ChapterDivisor  float64       // points per chapter = 1 / ChapterDivisor
	LanguageWeight  float64       // points per distinct language
	LanguageCap     int           // languages beyond this count earn nothing
	Title1Bonus     float64       // bonus when HDMV title 1 plays the playlist
	AmbiguityMargin float64       // runner-up within this many points makes the choice ambiguous
}

// DefaultWeights are the initial weights from the design spec.
var DefaultWeights = Weights{
	Duration:        100,
	RepeatedClip:    40,
	TinySegments:    30,
	TinySegment:     5 * time.Second,
	ChapterCap:      30,
	ChapterDivisor:  3,
	LanguageWeight:  2,
	LanguageCap:     10,
	Title1Bonus:     15,
	AmbiguityMargin: 5,
}

// Rank scores cands. infos[i] belongs to cands[i]; order lists candidate
// indexes best-first: the main title, other scored candidates by score
// (ties by ID), duplicates by ID, then filtered candidates by ID.
func Rank(cands []Candidate, minDuration time.Duration, w Weights) (order []int, infos []Info) {
	infos = make([]Info, len(cands))
	filter(cands, infos, minDuration)
	dedupe(cands, infos)
	var longest time.Duration
	for i, c := range cands {
		if group(infos[i]) == 0 && c.Duration > longest {
			longest = c.Duration
		}
	}
	for i, c := range cands {
		if group(infos[i]) == 0 {
			s, why := score(c, longest, w)
			infos[i].Score = s
			infos[i].Reasons = append(infos[i].Reasons, why...)
		}
	}
	order = sortOrder(cands, infos)
	return decide(cands, infos, order, w), infos
}

// group is 0 for scored candidates, 1 for duplicates and 2 for filtered ones.
func group(in Info) int {
	switch {
	case in.Filtered:
		return 2
	case in.DuplicateOf != "":
		return 1
	}
	return 0
}

func filter(cands []Candidate, infos []Info, minDuration time.Duration) {
	for i, c := range cands {
		in := &infos[i]
		switch {
		case c.Problem != "":
			in.Filtered = true
			in.Reasons = append(in.Reasons, c.Problem)
		case c.Duration < minDuration:
			in.Filtered = true
			in.Reasons = append(in.Reasons, fmt.Sprintf("shorter than %s", minDuration))
		case !c.HasVideo:
			in.Filtered = true
			in.Reasons = append(in.Reasons, "no video stream")
		}
		if c.Encrypted {
			in.Reasons = append(in.Reasons, "encrypted")
		}
	}
}

// dedupe marks candidates whose clip sequence (IDs and IN/OUT times)
// matches a candidate with a lower ID.
func dedupe(cands []Candidate, infos []Info) {
	var idx []int
	for i := range cands {
		if !infos[i].Filtered {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool { return cands[idx[a]].ID < cands[idx[b]].ID })
	first := map[string]string{}
	for _, i := range idx {
		k := clipKey(cands[i].Clips)
		if id, ok := first[k]; ok {
			infos[i].DuplicateOf = id
			infos[i].Reasons = append(infos[i].Reasons, "duplicate of "+id)
			continue
		}
		first[k] = cands[i].ID
	}
}

func clipKey(clips []Clip) string {
	var b strings.Builder
	for _, c := range clips {
		fmt.Fprintf(&b, "%s@%d-%d;", c.ID, c.In, c.Out)
	}
	return b.String()
}

func score(c Candidate, longest time.Duration, w Weights) (float64, []string) {
	var s float64
	var why []string
	if longest > 0 {
		frac := float64(c.Duration) / float64(longest)
		s += w.Duration * frac
		why = append(why, fmt.Sprintf("%.0f%% of the longest title", 100*frac))
	}
	if id := repeatedClip(c.Clips); id != "" {
		s -= w.RepeatedClip
		why = append(why, "repeats clip "+id)
	}
	if n := len(c.Clips); n > 0 {
		tiny := 0
		for _, cl := range c.Clips {
			if cl.length() < w.TinySegment {
				tiny++
			}
		}
		if tiny > 0 {
			s -= w.TinySegments * float64(tiny) / float64(n)
			why = append(why, fmt.Sprintf("%d of %d segments under %s", tiny, n, w.TinySegment))
		}
	}
	if c.Chapters > 0 {
		s += float64(min(c.Chapters, w.ChapterCap)) / w.ChapterDivisor
		why = append(why, fmt.Sprintf("%d chapters", c.Chapters))
	}
	if c.Languages > 0 {
		s += w.LanguageWeight * float64(min(c.Languages, w.LanguageCap))
		why = append(why, fmt.Sprintf("%d audio/subtitle languages", c.Languages))
	}
	if c.PlayedByTitle1 {
		s += w.Title1Bonus
		why = append(why, "played by title 1")
	}
	return s, why
}

func repeatedClip(clips []Clip) string {
	seen := map[string]bool{}
	for _, c := range clips {
		if seen[c.ID] {
			return c.ID
		}
		seen[c.ID] = true
	}
	return ""
}

func sortOrder(cands []Candidate, infos []Info) []int {
	order := make([]int, len(cands))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := order[a], order[b]
		gx, gy := group(infos[x]), group(infos[y])
		if gx != gy {
			return gx < gy
		}
		if gx == 0 && infos[x].Score != infos[y].Score {
			return infos[x].Score > infos[y].Score
		}
		return cands[x].ID < cands[y].ID
	})
	return order
}

// decide marks the best unencrypted scored candidate as main, moves it to
// the front of order, and flags ambiguity against the next unencrypted
// scored candidate.
func decide(cands []Candidate, infos []Info, order []int, w Weights) []int {
	pos := -1
	for k, i := range order {
		if group(infos[i]) == 0 && !cands[i].Encrypted {
			pos = k
			break
		}
	}
	if pos < 0 {
		return order
	}
	main := order[pos]
	infos[main].IsMain = true
	for _, i := range order[pos+1:] {
		if group(infos[i]) != 0 {
			break
		}
		if cands[i].Encrypted {
			continue
		}
		if infos[main].Score-infos[i].Score <= w.AmbiguityMargin {
			infos[main].Ambiguous = true
			infos[main].Reasons = append(infos[main].Reasons, fmt.Sprintf("close second: %s (score %.1f vs %.1f)",
				cands[i].ID, infos[i].Score, infos[main].Score))
		}
		break
	}
	out := make([]int, 0, len(order))
	out = append(out, main)
	out = append(out, order[:pos]...)
	return append(out, order[pos+1:]...)
}
