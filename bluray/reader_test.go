package bluray

import (
	"errors"
	"testing"
)

func TestReaderReadsBigEndian(t *testing.T) {
	r := newReader(cat([]byte{0x01}, be16(0x0203), be32(0x04050607), []byte("--abc")))
	if got := r.u8(); got != 0x01 {
		t.Errorf("u8 = %#x", got)
	}
	if got := r.u16(); got != 0x0203 {
		t.Errorf("u16 = %#x", got)
	}
	if got := r.u32(); got != 0x04050607 {
		t.Errorf("u32 = %#x", got)
	}
	r.skip(2)
	if got := r.str(3); got != "abc" {
		t.Errorf("str = %q", got)
	}
	if r.remaining() != 0 || r.err() != nil {
		t.Errorf("remaining = %d, err = %v", r.remaining(), r.err())
	}
}

func TestReaderOutOfRangeIsSticky(t *testing.T) {
	r := newReader([]byte{1, 2, 3})
	if got := r.u32(); got != 0 {
		t.Errorf("u32 past end = %d, want 0", got)
	}
	if !errors.Is(r.err(), ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", r.err())
	}
	if got := r.u8(); got != 0 {
		t.Errorf("u8 after error = %d, want 0", got)
	}
}

func TestSubReaderSharesError(t *testing.T) {
	r := newReader([]byte{2, 0xAA, 0xBB, 0xCC})
	s := r.sub(int(r.u8()))
	s.u16()
	s.u8() // past the 2-byte window
	if !errors.Is(r.err(), ErrInvalid) {
		t.Fatalf("parent err = %v, want ErrInvalid", r.err())
	}
}

func TestSubReaderAdvancesParent(t *testing.T) {
	r := newReader([]byte{0xAA, 0xBB, 0xCC})
	r.sub(2)
	if got := r.u8(); got != 0xCC {
		t.Errorf("after sub, u8 = %#x, want 0xCC", got)
	}
}

func TestSeekBounds(t *testing.T) {
	r := newReader(zeros(4))
	r.seek(4)
	if r.err() != nil {
		t.Fatalf("seek to end: %v", r.err())
	}
	r.seek(5)
	if !errors.Is(r.err(), ErrInvalid) {
		t.Errorf("seek past end err = %v", r.err())
	}
}

func TestHeader(t *testing.T) {
	r := newReader([]byte("MPLS0300"))
	if v := r.header("MPLS"); v != "0300" || r.err() != nil {
		t.Errorf("header = %q, %v", v, r.err())
	}
	r = newReader([]byte("HDMV0300"))
	r.header("MPLS")
	if !errors.Is(r.err(), ErrInvalid) {
		t.Errorf("wrong magic err = %v", r.err())
	}
}
