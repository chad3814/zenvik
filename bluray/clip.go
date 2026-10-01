package bluray

// Program is one program sequence of a clip.
type Program struct {
	SPNStart uint32
	PMTPID   uint16
	Streams  []Stream
}

// Clip is a parsed CLPI (clip information) file.
type Clip struct {
	Version         string
	StreamType      uint8
	ApplicationType uint8
	TSRecordingRate uint32
	SourcePackets   uint32
	Programs        []Program
}

// Size returns the clip's M2TS size in bytes (192-byte source packets).
func (c *Clip) Size() int64 { return int64(c.SourcePackets) * 192 }

// ParseClip parses the contents of a BDMV/CLIPINF/*.clpi file.
func ParseClip(b []byte) (*Clip, error) {
	r := newReader(b)
	c := &Clip{Version: r.header("HDMV")}
	r.skip(4) // sequence info start address
	progStart := r.u32()

	r.seek(40) // ClipInfo follows the fixed-size header
	cr := r.sub(int(r.u32()))
	cr.skip(2)
	c.StreamType = cr.u8()
	c.ApplicationType = cr.u8()
	cr.skip(4) // reserved, is_ATC_delta
	c.TSRecordingRate = cr.u32()
	c.SourcePackets = cr.u32()

	r.seek(int(progStart))
	pr := r.sub(int(r.u32()))
	pr.skip(1)
	n := int(pr.u8())
	for i := 0; i < n && pr.err() == nil; i++ {
		p := Program{SPNStart: pr.u32(), PMTPID: pr.u16()}
		ns := int(pr.u8())
		pr.skip(1) // number of groups
		for j := 0; j < ns && pr.err() == nil; j++ {
			s := Stream{PID: pr.u16()}
			parseAttributes(pr, &s, true)
			p.Streams = append(p.Streams, s)
		}
		c.Programs = append(c.Programs, p)
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	return c, nil
}
