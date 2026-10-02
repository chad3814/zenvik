package zenvik

import (
	"time"

	"github.com/chad3814/zenvik/bluray"
	"github.com/chad3814/zenvik/internal/vobsub"
)

// Title is one playlist (Blu-ray) or title (DVD) on the disc.
type Title struct {
	ID        string // playlist number, e.g. "00800", or DVD title number, e.g. "01"
	Duration  time.Duration
	Size      int64 // bytes of the referenced stream files (Blu-ray: distinct clips counted once; DVD: the kept angle-1 cells' sectors)
	Clips     []Clip
	Chapters  []Chapter
	Video     []VideoTrack
	Audio     []AudioTrack
	Subtitles []SubtitleTrack
	Angles    int // 1 for single-angle titles
	Encrypted bool
	// Unsupported is empty when the title can be ripped; otherwise it says
	// why not (DVD titles that span several program chains, for example).
	Unsupported string
	// RipMethod says how a DVD title is ripped: "files" (whole title VOBs),
	// "cut" (one ascending run cut from its VOBs) or "copy" (cells copied in
	// play order to a temporary file). It is "" for Blu-ray titles.
	RipMethod string
	// SkippedCells lists short out-of-order cells at a DVD title's start or
	// end that are left out of the rip.
	SkippedCells []SkippedCell
	Rank         RankInfo

	dvd *dvdInfo // DVD only: what ripping needs beyond the public fields
}

// SkippedCell is a DVD cell left out of a title's rip.
type SkippedCell struct {
	Cell                    int // 1-based cell number in the title's PGC
	Duration                time.Duration
	FirstSector, LastSector uint32
}

// Clip is one play item: a clip and its IN/OUT times on the clip's clock.
// For a DVD title, ID is a title VOB file name (as found on the disc) and
// In/Out are zero.
type Clip struct {
	ID      string // "00001" → BDMV/STREAM/00001.m2ts
	In, Out time.Duration
}

// Chapter is a chapter start relative to the beginning of the title.
type Chapter struct {
	Number int // from 1
	Start  time.Duration
}

// VideoTrack is a video stream.
type VideoTrack struct {
	PID          uint16
	Codec        bluray.CodingType
	Format       bluray.VideoFormat
	FrameRate    bluray.FrameRate
	DynamicRange bluray.DynamicRange
	AspectRatio  string // DVD only: "4:3" or "16:9"
}

// HDR reports whether the track uses a high dynamic range format.
func (v VideoTrack) HDR() bool { return v.DynamicRange != 0 }

// AudioTrack is an audio stream.
type AudioTrack struct {
	PID         uint16
	Codec       bluray.CodingType
	Language    string // ISO 639-2
	Channels    bluray.AudioFormat
	SampleRate  bluray.SampleRate
	Description string // DVD only: e.g. "Director's Commentary"
}

// SubtitleTrack is a subtitle stream.
type SubtitleTrack struct {
	PID         uint16
	Codec       bluray.CodingType
	Language    string // ISO 639-2
	Description string // DVD only: e.g. "Forced"
}

// RankInfo explains where a title placed in main-feature detection.
type RankInfo struct {
	Score       float64
	IsMain      bool
	Ambiguous   bool // on the main title: a runner-up scored within the ambiguity margin
	Filtered    bool
	DuplicateOf string
	Reasons     []string
}

// CodingVobSub is the Codec of DVD subtitle tracks (VobSub bitmaps). It is
// not a Blu-ray stream coding type, so bluray.CodingType.String reports
// it as "0xFF".
const CodingVobSub bluray.CodingType = 0xFF

// dvdInfo is what ripping a DVD title needs beyond its public fields.
type dvdInfo struct {
	cells         []vobsub.Cell     // kept angle-1 cells and their title-timeline start times
	palette       [16]uint32        // the PGC's subpicture palette
	width, height int               // video size
	subLang       map[uint16]string // subtitle PID → ISO 639-1 code from the IFO
	method        string            // RipMethod
	ranges        []sectorRange     // kept angle-1 cells' sectors, in play order
	files         []vobFile         // the title set's title VOBs
	vobus         []uint32          // the title set's VOBU starts
}
