package bluray

import "time"

// STN is a play item's stream number table: the streams a player may
// select, in stream-number order. Interactive graphics and secondary
// streams are not decoded.
type STN struct {
	Video []Stream
	Audio []Stream
	PG    []Stream
}

// PlayItem is one clip segment of a playlist.
type PlayItem struct {
	ClipID              string // "00001" → CLIPINF/00001.clpi, STREAM/00001.m2ts
	CodecID             string // "M2TS"
	ConnectionCondition uint8
	STCID               uint8
	In, Out             Ticks
	StillMode           uint8
	Angles              []string // clip IDs of angles 2..n; empty for single-angle items
	STN                 STN
}

// Duration returns the length of the play item.
func (p PlayItem) Duration() time.Duration { return ticksDuration(span(p)) }

func span(p PlayItem) uint64 {
	if p.Out < p.In {
		return 0
	}
	return uint64(p.Out - p.In)
}

// MarkType distinguishes chapter entry marks from link points.
type MarkType uint8

// Mark types.
const (
	MarkEntry MarkType = 1
	MarkLink  MarkType = 2
)

// Mark is a playlist mark.
type Mark struct {
	Type     MarkType
	PlayItem uint16
	Time     Ticks // on the referenced play item's clock
	PID      uint16
	Duration Ticks
}

// Playlist is a parsed MPLS file.
type Playlist struct {
	Version string
	Items   []PlayItem
	Marks   []Mark
}

// Duration returns the summed length of all play items.
func (p *Playlist) Duration() time.Duration {
	var t uint64
	for _, it := range p.Items {
		t += span(it)
	}
	return ticksDuration(t)
}

// Chapters returns the start of each entry mark relative to the start of
// the playlist, ascending and without duplicates. Marks that reference a
// missing play item or fall outside their item's IN/OUT range are ignored.
func (p *Playlist) Chapters() []time.Duration {
	starts := make([]uint64, len(p.Items))
	var t uint64
	for i, it := range p.Items {
		starts[i] = t
		t += span(it)
	}
	var out []time.Duration
	for _, m := range p.Marks {
		if m.Type != MarkEntry || int(m.PlayItem) >= len(p.Items) {
			continue
		}
		it := p.Items[m.PlayItem]
		if m.Time < it.In || m.Time > it.Out {
			continue
		}
		d := ticksDuration(starts[m.PlayItem] + uint64(m.Time-it.In))
		if len(out) > 0 && d <= out[len(out)-1] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// ParsePlaylist parses the contents of a BDMV/PLAYLIST/*.mpls file.
func ParsePlaylist(b []byte) (*Playlist, error) {
	r := newReader(b)
	p := &Playlist{Version: r.header("MPLS")}
	plStart := r.u32()
	markStart := r.u32()

	r.seek(int(plStart))
	pr := r.sub(int(r.u32()))
	pr.skip(2)
	nItems := int(pr.u16())
	pr.skip(2) // number of sub-paths
	for i := 0; i < nItems && pr.err() == nil; i++ {
		p.Items = append(p.Items, parsePlayItem(pr))
	}

	r.seek(int(markStart))
	mr := r.sub(int(r.u32()))
	nMarks := int(mr.u16())
	for i := 0; i < nMarks && mr.err() == nil; i++ {
		mr.skip(1)
		p.Marks = append(p.Marks, Mark{
			Type:     MarkType(mr.u8()),
			PlayItem: mr.u16(),
			Time:     Ticks(mr.u32()),
			PID:      mr.u16(),
			Duration: Ticks(mr.u32()),
		})
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return p, nil
}

func parsePlayItem(r *reader) PlayItem {
	pr := r.sub(int(r.u16()))
	var pi PlayItem
	pi.ClipID = pr.str(5)
	pi.CodecID = pr.str(4)
	flags := pr.u16()
	multiAngle := flags&0x10 != 0
	pi.ConnectionCondition = uint8(flags & 0x0F)
	pi.STCID = pr.u8()
	pi.In = Ticks(pr.u32())
	pi.Out = Ticks(pr.u32())
	if pr.err() == nil && pi.Out < pi.In {
		pr.fail("play item %s: OUT time %d before IN time %d", pi.ClipID, pi.Out, pi.In)
	}
	pr.skip(8) // UO mask table
	pr.skip(1) // random access flag, reserved
	pi.StillMode = pr.u8()
	pr.skip(2) // still time or reserved
	if multiAngle {
		n := int(pr.u8())
		pr.skip(1) // is_different_audios, is_seamless_angle_change
		for i := 1; i < n && pr.err() == nil; i++ {
			pi.Angles = append(pi.Angles, pr.str(5))
			pr.skip(4 + 1) // codec identifier, ref_to_STC_id
		}
	}
	pi.STN = parseSTN(pr)
	return pi
}

func parseSTN(r *reader) STN {
	sr := r.sub(int(r.u16()))
	sr.skip(2)
	nVideo, nAudio, nPG := int(sr.u8()), int(sr.u8()), int(sr.u8())
	sr.skip(4) // IG, secondary audio, secondary video, PiP PG counts
	sr.skip(5)
	read := func(n int) []Stream {
		var out []Stream
		for i := 0; i < n && sr.err() == nil; i++ {
			out = append(out, parseStreamEntry(sr))
		}
		return out
	}
	var stn STN
	stn.Video = read(nVideo)
	stn.Audio = read(nAudio)
	stn.PG = read(nPG)
	return stn
}

// parseStreamEntry reads a stream_entry (which locates the stream's PID)
// followed by its stream_attributes.
func parseStreamEntry(r *reader) Stream {
	var s Stream
	er := r.sub(int(r.u8()))
	switch er.u8() {
	case 1: // stream in the main clip
		s.PID = er.u16()
	case 2, 4: // sub-path, sub-clip entry
		er.skip(2)
		s.PID = er.u16()
	case 3: // sub-path
		er.skip(1)
		s.PID = er.u16()
	}
	parseAttributes(r, &s, false)
	return s
}
