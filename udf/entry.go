package udf

import (
	"fmt"
	"io"
	"math"
	"time"
)

const (
	fileTypeDirectory      = 4
	fileTypeMetadata       = 250
	fileTypeMetadataMirror = 251
)

type extent struct {
	length uint32
	typ    uint8 // 0 recorded, 1 allocated but not recorded, 2 not allocated
	ref    uint16
	block  uint32
}

// entry is a decoded file entry or extended file entry.
type entry struct {
	fs       *FS
	fileType uint8
	size     int64
	modTime  time.Time
	inline   bool   // data is embedded in the entry
	embedded []byte // embedded data when inline
	extents  []extent
}

type entryLayout struct{ header, size, modTime, lenEA, lenAD int }

var (
	feLayout  = entryLayout{header: 176, size: 56, modTime: 84, lenEA: 168, lenAD: 172}
	efeLayout = entryLayout{header: 216, size: 56, modTime: 92, lenEA: 208, lenAD: 212}
)

// readEntry reads the file entry at a logical block, following indirect
// entries.
func (f *FS) readEntry(ref uint16, block uint32) (*entry, error) {
	d := make([]byte, sectorSize)
	for range 8 {
		if err := f.readAt(ref, block, 0, d); err != nil {
			return nil, err
		}
		id, err := expectTag(d, block, tagFE, tagEFE, tagIE)
		if err != nil {
			return nil, err
		}
		switch id {
		case tagIE:
			ad := parseLongAD(d[36:52])
			ref, block = ad.ref, ad.block
		case tagFE:
			return f.decodeEntry(d, ref, feLayout)
		default:
			return f.decodeEntry(d, ref, efeLayout)
		}
	}
	return nil, fmt.Errorf("%w: too many indirect entries", ErrCorrupt)
}

func (f *FS) decodeEntry(d []byte, ref uint16, l entryLayout) (*entry, error) {
	size := le64(d[l.size:])
	if size > math.MaxInt64 {
		return nil, fmt.Errorf("%w: file size %d", ErrCorrupt, size)
	}
	e := &entry{fs: f, fileType: d[16+11], size: int64(size), modTime: decodeTimestamp(d[l.modTime:])}
	start := int64(l.header) + int64(le32(d[l.lenEA:]))
	end := start + int64(le32(d[l.lenAD:]))
	if end > int64(len(d)) {
		return nil, fmt.Errorf("%w: allocation descriptors overrun file entry", ErrCorrupt)
	}
	ads := d[start:end]
	switch adType := le16(d[16+18:]) & 7; adType {
	case 0, 1:
		if err := e.addExtents(ads, adType == 1, ref, 0); err != nil {
			return nil, err
		}
	case 3:
		if e.size > int64(len(ads)) {
			return nil, fmt.Errorf("%w: embedded data shorter than file size", ErrCorrupt)
		}
		e.inline = true
		e.embedded = append([]byte(nil), ads...)
	default:
		return nil, fmt.Errorf("%w: allocation descriptor type %d", ErrUnsupported, adType)
	}
	return e, nil
}

// addExtents appends the extents described by short (8-byte) or long
// (16-byte) allocation descriptors, following continuation extents into
// allocation extent descriptors.
func (e *entry) addExtents(ads []byte, long bool, ref uint16, depth int) error {
	size := 8
	if long {
		size = 16
	}
	for len(ads) >= size {
		raw := le32(ads)
		x := extent{length: raw & 0x3FFFFFFF, typ: uint8(raw >> 30), ref: ref, block: le32(ads[4:])}
		if long {
			x.ref = le16(ads[8:])
		}
		ads = ads[size:]
		if x.length == 0 {
			return nil
		}
		if x.typ != 3 {
			e.extents = append(e.extents, x)
			continue
		}
		if depth >= 16 {
			return fmt.Errorf("%w: allocation extent chain too long", ErrCorrupt)
		}
		d := make([]byte, sectorSize)
		if err := e.fs.readAt(x.ref, x.block, 0, d); err != nil {
			return err
		}
		if _, err := expectTag(d, x.block, tagAED); err != nil {
			return err
		}
		n := int64(le32(d[20:]))
		if 24+n > sectorSize {
			return fmt.Errorf("%w: allocation extent descriptor length %d", ErrCorrupt, n)
		}
		return e.addExtents(d[24:24+n], long, x.ref, depth+1)
	}
	return nil
}

// ReadAt reads file data. Unrecorded extents read as zeros.
func (e *entry) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("udf: negative offset %d", off)
	}
	if off >= e.size {
		return 0, io.EOF
	}
	want := p
	if rem := e.size - off; int64(len(want)) > rem {
		want = want[:rem]
	}
	if e.inline {
		n := copy(want, e.embedded[off:])
		if n < len(p) {
			return n, io.EOF
		}
		return n, nil
	}
	done := 0
	var base int64
	for _, x := range e.extents {
		if done == len(want) {
			break
		}
		l := int64(x.length)
		pos := off + int64(done)
		if pos >= base+l {
			base += l
			continue
		}
		chunk := want[done:]
		if rem := base + l - pos; int64(len(chunk)) > rem {
			chunk = chunk[:rem]
		}
		if x.typ == 0 {
			if err := e.fs.readAt(x.ref, x.block, pos-base, chunk); err != nil {
				return done, err
			}
		} else {
			clear(chunk)
		}
		done += len(chunk)
		base += l
	}
	if done < len(want) {
		return done, fmt.Errorf("%w: file extents shorter than file size", ErrCorrupt)
	}
	if len(want) < len(p) {
		return done, io.EOF
	}
	return done, nil
}
