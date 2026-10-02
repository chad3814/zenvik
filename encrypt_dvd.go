package zenvik

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// vobEncrypted reports whether a VOB looks CSS-encrypted. It samples up to
// 64 packs spread evenly through the file's sectors and reports true if any
// video, audio or private stream 1 PES packet has PES_scrambling_control
// set.
func vobEncrypted(fsys fs.FS, name string, sectors int64) (bool, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()
	ra, ok := f.(io.ReaderAt)
	if !ok {
		return false, fmt.Errorf("zenvik: %s does not support random access", name)
	}
	n := min(sectors, 64)
	buf := make([]byte, 2048)
	for i := int64(0); i < n; i++ {
		s := i * sectors / n
		if _, err := ra.ReadAt(buf, s*2048); err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		if packScrambled(buf) {
			return true, nil
		}
	}
	return false, nil
}

// packScrambled reports whether an MPEG-2 program stream pack holds a
// video (0xE0–0xEF), audio (0xC0–0xDF) or private stream 1 (0xBD) PES
// packet whose PES_scrambling_control bits are set.
func packScrambled(p []byte) bool {
	if len(p) < 14 || p[0] != 0 || p[1] != 0 || p[2] != 1 || p[3] != 0xBA || p[4]&0xC0 != 0x40 {
		return false
	}
	o := 14 + int(p[13]&7)
	for o+9 <= len(p) {
		if p[o] != 0 || p[o+1] != 0 || p[o+2] != 1 {
			return false
		}
		id := p[o+3]
		if id == 0xBD || id >= 0xC0 && id <= 0xEF {
			if p[o+6]&0x30 != 0 {
				return true
			}
		}
		o += 6 + int(binary.BigEndian.Uint16(p[o+4:]))
	}
	return false
}
