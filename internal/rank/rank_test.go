package rank

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// seq returns n clips with consecutive five-digit IDs starting at first.
func seq(first, n int, each time.Duration) []Clip {
	out := make([]Clip, n)
	for i := range out {
		out[i] = Clip{ID: fmt.Sprintf("%05d", first+i), In: 0, Out: each}
	}
	return out
}

// movie builds a candidate with video, three languages and one chapter per clip.
func movie(id string, clips []Clip) Candidate {
	var d time.Duration
	for _, c := range clips {
		d += c.Out - c.In
	}
	return Candidate{ID: id, Clips: clips, Duration: d, Chapters: len(clips), Languages: 3, HasVideo: true}
}

func mainOf(t *testing.T, cands []Candidate, order []int, infos []Info) (string, Info) {
	t.Helper()
	if len(order) == 0 || !infos[order[0]].IsMain {
		return "", Info{}
	}
	for k, i := range order {
		if k > 0 && infos[i].IsMain {
			t.Fatalf("second main title %s", cands[i].ID)
		}
	}
	return cands[order[0]].ID, infos[order[0]]
}

func TestRankScenarios(t *testing.T) {
	encrypted := movie("00800", seq(1, 20, 330*time.Second))
	encrypted.Encrypted = true
	title1 := movie("00801", seq(101, 20, 5*time.Minute))
	title1.PlayedByTitle1 = true

	withChapters := func(c Candidate, n int) Candidate { c.Chapters = n; return c }
	encryptedRunnerUp := movie("00801", seq(101, 20, 5*time.Minute))
	encryptedRunnerUp.Encrypted = true

	tests := []struct {
		name      string
		cands     []Candidate
		wantMain  string
		ambiguous bool
	}{
		{"runner-up 9 points behind is not ambiguous", []Candidate{
			movie("00800", seq(1, 20, 330*time.Second)),   // about 112.7
			movie("00801", seq(101, 20, 300*time.Second)), // about 103.6
		}, "00800", false},
		{"runner-up exactly at the margin is ambiguous", []Candidate{
			withChapters(movie("00800", seq(1, 20, 5*time.Minute)), 30),   // 116
			withChapters(movie("00801", seq(101, 20, 5*time.Minute)), 15), // 111
		}, "00800", true},
		{"runner-up just past the margin is not ambiguous", []Candidate{
			withChapters(movie("00800", seq(1, 20, 5*time.Minute)), 30),
			withChapters(movie("00801", seq(101, 20, 5*time.Minute)), 14), // 5.33 behind
		}, "00800", false},
		{"encrypted runner-up does not make the main ambiguous", []Candidate{
			withChapters(movie("00800", seq(1, 20, 5*time.Minute)), 21), // 2 points above the encrypted runner-up
			withChapters(encryptedRunnerUp, 15),
		}, "00800", false},
		{"simple movie", []Candidate{
			movie("00800", seq(1, 20, 330*time.Second)),
			movie("00001", seq(100, 1, 150*time.Second)),
			movie("00002", seq(101, 1, 30*time.Second)),
		}, "00800", false},
		{"three duplicate copies", []Candidate{
			movie("00800", seq(1, 20, 5*time.Minute)),
			movie("00801", seq(1, 20, 5*time.Minute)),
			movie("00802", seq(1, 20, 5*time.Minute)),
			movie("00900", seq(201, 2, 5*time.Minute)),
		}, "00800", false},
		{"99-playlist obfuscation", obfuscated(), "00057", false},
		{"tv episodes with play-all", []Candidate{
			movie("00001", seq(1, 1, 22*time.Minute)),
			movie("00002", seq(2, 1, 22*time.Minute)),
			movie("00003", seq(3, 1, 22*time.Minute)),
			movie("00004", seq(4, 1, 22*time.Minute)),
			movie("00005", seq(1, 4, 22*time.Minute)),
		}, "00005", false},
		{"extras only", []Candidate{
			movie("00001", seq(1, 1, 3*time.Minute)),
			movie("00002", seq(2, 1, 5*time.Minute)),
			movie("00003", seq(3, 1, 8*time.Minute)),
		}, "00003", false},
		{"ambiguous pair", []Candidate{
			movie("00800", seq(1, 20, 330*time.Second)),
			movie("00801", seq(101, 20, 327*time.Second)),
		}, "00800", true},
		{"title 1 breaks the tie", []Candidate{
			movie("00800", seq(1, 20, 5*time.Minute)),
			title1,
		}, "00801", false},
		{"encrypted is never main", []Candidate{
			encrypted,
			movie("00801", seq(101, 20, 5*time.Minute)),
		}, "00801", false},
		{"exact tie goes to lowest ID", []Candidate{
			movie("00801", seq(101, 20, 5*time.Minute)),
			movie("00800", seq(1, 20, 5*time.Minute)),
		}, "00800", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, infos := Rank(tt.cands, 2*time.Minute, DefaultWeights)
			got, info := mainOf(t, tt.cands, order, infos)
			if got != tt.wantMain {
				t.Fatalf("main = %q, want %q (order %v)", got, tt.wantMain, ids(tt.cands, order))
			}
			if info.Ambiguous != tt.ambiguous {
				t.Errorf("Ambiguous = %v, want %v (reasons %q)", info.Ambiguous, tt.ambiguous, info.Reasons)
			}
		})
	}
}

// obfuscated builds 99 playlists: 00057 plays clips 1–20 in order (100
// min); every other playlist plays a rotation of the same clips plus a
// repeat of one clip and five 3-second segments.
func obfuscated() []Candidate {
	real := seq(1, 20, 5*time.Minute)
	var out []Candidate
	for n := 1; n <= 99; n++ {
		id := fmt.Sprintf("%05d", n)
		if n == 57 {
			out = append(out, movie(id, real))
			continue
		}
		k := n % 20
		clips := append(append([]Clip{}, real[k:]...), real[:k]...)
		clips = append(clips, real[0])
		for j := 0; j < 5; j++ {
			clips = append(clips, Clip{ID: fmt.Sprintf("%05d", 900+j), In: 0, Out: 3 * time.Second})
		}
		out = append(out, movie(id, clips))
	}
	return out
}

func ids(cands []Candidate, order []int) []string {
	out := make([]string, len(order))
	for k, i := range order {
		out[k] = cands[i].ID
	}
	return out
}

func TestRankAmbiguityNamesRunnerUp(t *testing.T) {
	cands := []Candidate{
		movie("00800", seq(1, 20, 330*time.Second)),
		movie("00801", seq(101, 20, 327*time.Second)),
	}
	order, infos := Rank(cands, 2*time.Minute, DefaultWeights)
	reasons := strings.Join(infos[order[0]].Reasons, "; ")
	if !strings.Contains(reasons, "close second: 00801") {
		t.Errorf("reasons = %q", reasons)
	}
}

func TestRankFiltersAndOrder(t *testing.T) {
	noVideo := movie("00003", seq(30, 1, 10*time.Minute))
	noVideo.HasVideo = false
	broken := Candidate{ID: "00004", Problem: "unreadable playlist: bad data"}
	cands := []Candidate{
		broken,
		noVideo,
		movie("00002", seq(20, 1, 20*time.Second)),
		movie("00801", seq(1, 20, 5*time.Minute)),
		movie("00800", seq(1, 20, 5*time.Minute)),
		movie("00010", seq(40, 2, 5*time.Minute)),
	}
	order, infos := Rank(cands, 2*time.Minute, DefaultWeights)
	if got, want := ids(cands, order), []string{"00800", "00010", "00801", "00002", "00003", "00004"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	byID := map[string]Info{}
	for i, c := range cands {
		byID[c.ID] = infos[i]
	}
	if in := byID["00002"]; !in.Filtered || !slices.Contains(in.Reasons, "shorter than 2m0s") {
		t.Errorf("00002 = %+v", in)
	}
	if in := byID["00003"]; !in.Filtered || !slices.Contains(in.Reasons, "no video stream") {
		t.Errorf("00003 = %+v", in)
	}
	if in := byID["00004"]; !in.Filtered || !slices.Contains(in.Reasons, "unreadable playlist: bad data") {
		t.Errorf("00004 = %+v", in)
	}
	if in := byID["00801"]; in.DuplicateOf != "00800" || in.Score != 0 || !slices.Contains(in.Reasons, "duplicate of 00800") {
		t.Errorf("00801 = %+v", in)
	}
	if in := byID["00800"]; !in.IsMain || !slices.Contains(in.Reasons, "100% of the longest title") || !slices.Contains(in.Reasons, "20 chapters") {
		t.Errorf("00800 = %+v", in)
	}
}

func TestRankScores(t *testing.T) {
	c := movie("00001", seq(1, 4, 5*time.Minute))
	c.Clips = append(c.Clips, c.Clips[0], Clip{ID: "00009", In: 0, Out: 2 * time.Second})
	c.Duration = 25*time.Minute + 2*time.Second
	c.Chapters = 40
	c.Languages = 12
	c.PlayedByTitle1 = true
	_, infos := Rank([]Candidate{c}, 2*time.Minute, DefaultWeights)
	// 100 (longest) - 40 (repeat) - 30*(1/6) + 30/3 + 2*10 + 15 = 100
	if got := infos[0].Score; got < 99.999 || got > 100.001 {
		t.Errorf("Score = %v, want 100", got)
	}
	for _, want := range []string{"repeats clip 00001", "1 of 6 segments under 5s", "40 chapters", "12 audio/subtitle languages", "played by title 1"} {
		if !slices.Contains(infos[0].Reasons, want) {
			t.Errorf("missing reason %q in %q", want, infos[0].Reasons)
		}
	}
}

func TestRankEncryptedReasonAndOrder(t *testing.T) {
	enc := movie("00800", seq(1, 20, 330*time.Second))
	enc.Encrypted = true
	cands := []Candidate{enc, movie("00801", seq(101, 20, 5*time.Minute))}
	order, infos := Rank(cands, 2*time.Minute, DefaultWeights)
	if got := ids(cands, order); !reflect.DeepEqual(got, []string{"00801", "00800"}) {
		t.Errorf("order = %v", got)
	}
	if !slices.Contains(infos[0].Reasons, "encrypted") || infos[0].IsMain {
		t.Errorf("00800 = %+v", infos[0])
	}
}

func TestRankNoMain(t *testing.T) {
	cands := []Candidate{movie("00002", seq(1, 1, time.Minute)), movie("00001", seq(2, 1, time.Minute))}
	order, infos := Rank(cands, 2*time.Minute, DefaultWeights)
	if got := ids(cands, order); !reflect.DeepEqual(got, []string{"00001", "00002"}) {
		t.Errorf("order = %v", got)
	}
	for _, in := range infos {
		if in.IsMain {
			t.Error("unexpected main title")
		}
	}
	if order, infos := Rank(nil, 2*time.Minute, DefaultWeights); len(order) != 0 || len(infos) != 0 {
		t.Error("empty input should give empty output")
	}
}
