package dvd

import (
	"encoding/binary"
	"time"
)

// NAV is what zenvik reads from a NAV pack, the first pack of every VOBU.
type NAV struct {
	StartPTM, EndPTM uint32        // PCI vobu_s_ptm and vobu_e_ptm, 90 kHz
	VOBID, CellID    int           // DSI vobu_vob_idn and vobu_c_idn
	CellElapsed      time.Duration // DSI c_eltm: time since the cell's start
}

// ParseNAV reads a 2048-byte NAV pack. It reports false for any other pack.
// A malformed c_eltm reads as zero.
func ParseNAV(p []byte) (NAV, bool) {
	if len(p) != sectorSize || p[0] != 0 || p[1] != 0 || p[2] != 1 || p[3] != 0xBA || p[4]&0xC0 != 0x40 ||
		p[0x0E] != 0 || p[0x0F] != 0 || p[0x10] != 1 || p[0x11] != 0xBB ||
		p[0x26] != 0 || p[0x27] != 0 || p[0x28] != 1 || p[0x29] != 0xBF || p[0x2C] != 0x00 ||
		p[0x400] != 0 || p[0x401] != 0 || p[0x402] != 1 || p[0x403] != 0xBF || p[0x406] != 0x01 {
		return NAV{}, false
	}
	dsi := p[0x407:]
	n := NAV{
		StartPTM: binary.BigEndian.Uint32(p[0x2D+12:]),
		EndPTM:   binary.BigEndian.Uint32(p[0x2D+16:]),
		VOBID:    int(binary.BigEndian.Uint16(dsi[24:])),
		CellID:   int(dsi[27]),
	}
	if t, err := parseTime(dsi[28:32]); err == nil {
		n.CellElapsed = t.Duration()
	}
	return n, true
}
