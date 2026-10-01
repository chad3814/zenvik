// Package bluray parses Blu-ray Disc navigation files: index.bdmv,
// MovieObject.bdmv, playlists (MPLS), clip information (CLPI), and disc
// library metadata (bdmt_*.xml). It reads byte slices and never touches
// the file system.
package bluray

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrInvalid reports malformed or truncated Blu-ray navigation data.
var ErrInvalid = errors.New("bluray: invalid data")

// reader is a bounds-checked big-endian cursor over a byte slice. Readers
// made with sub share one error slot with their parent, so the first
// out-of-range access anywhere stops all further reads and a parser only
// needs to check err once.
type reader struct {
	b   []byte
	off int
	e   *error
}

func newReader(b []byte) *reader {
	return &reader{b: b, e: new(error)}
}

func (r *reader) err() error { return *r.e }

func (r *reader) fail(format string, args ...any) {
	if *r.e == nil {
		*r.e = fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
}

func (r *reader) need(n int) bool {
	if *r.e != nil {
		return false
	}
	if n < 0 || n > len(r.b)-r.off {
		r.fail("need %d bytes at offset %d of %d", n, r.off, len(r.b))
		return false
	}
	return true
}

func (r *reader) remaining() int { return len(r.b) - r.off }

func (r *reader) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *reader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.BigEndian.Uint16(r.b[r.off:])
	r.off += 2
	return v
}

func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.BigEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v
}

func (r *reader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v
}

func (r *reader) str(n int) string { return string(r.bytes(n)) }

func (r *reader) skip(n int) {
	if r.need(n) {
		r.off += n
	}
}

// seek moves to an absolute offset within the reader's window.
func (r *reader) seek(off int) {
	if *r.e != nil {
		return
	}
	if off < 0 || off > len(r.b) {
		r.fail("offset %d outside %d bytes", off, len(r.b))
		return
	}
	r.off = off
}

// sub consumes the next n bytes and returns a reader limited to them.
func (r *reader) sub(n int) *reader {
	return &reader{b: r.bytes(n), e: r.e}
}

// header reads the 4-byte type indicator and 4-byte version string that
// start every navigation file, failing if the indicator is not magic.
func (r *reader) header(magic string) string {
	m := r.str(4)
	v := r.str(4)
	if *r.e == nil && m != magic {
		r.fail("type indicator %q, want %q", m, magic)
	}
	return v
}
