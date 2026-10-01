package udf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// hostileFS returns an FS whose single physical partition starts at
// sector 0 of an image made of the given sectors.
func hostileFS(sectors ...[]byte) *FS {
	img := make([]byte, len(sectors)*sectorSize)
	for i, s := range sectors {
		copy(img[i*sectorSize:], s)
	}
	return &FS{
		r:     bytes.NewReader(img),
		size:  int64(len(img)),
		parts: []partition{{start: 0, length: uint32(len(sectors))}},
	}
}

func TestAEDChainLoopIsCorrupt(t *testing.T) {
	ad := make([]byte, 8)
	binary.LittleEndian.PutUint32(ad, 3<<30|sectorSize) // continuation at block 0
	aed := make([]byte, 24+len(ad))
	binary.LittleEndian.PutUint32(aed[20:], uint32(len(ad)))
	copy(aed[24:], ad)
	f := hostileFS(makeTag(tagAED, 0, aed))
	e := &entry{fs: f}
	if err := e.addExtents(ad, false, 0, 0); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestIndirectEntryLoopIsCorrupt(t *testing.T) {
	ie := make([]byte, 52)
	binary.LittleEndian.PutUint32(ie[36:], sectorSize) // indirect ICB → block 0, partition 0
	f := hostileFS(makeTag(tagIE, 0, ie))
	if _, err := f.readEntry(0, 0); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestHugeDirectoryIsCorrupt(t *testing.T) {
	e := &entry{fs: hostileFS(make([]byte, sectorSize)), fileType: fileTypeDirectory, size: 1 << 40}
	if _, err := e.readDir(); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
}

func TestReadOutsidePartitionIsCorrupt(t *testing.T) {
	f := hostileFS(make([]byte, sectorSize))
	if err := f.readAt(0, 1, 0, make([]byte, 16)); !errors.Is(err, ErrCorrupt) {
		t.Errorf("err = %v, want ErrCorrupt", err)
	}
	if err := f.readAt(3, 0, 0, make([]byte, 16)); !errors.Is(err, ErrCorrupt) {
		t.Errorf("bad reference err = %v, want ErrCorrupt", err)
	}
}

func TestReadVDSEndsAtUnreadableSector(t *testing.T) {
	// The extent claims three sectors but the image holds two. The
	// descriptors that were read are used; the unreadable third sector
	// only ends the sequence. The zero logical block size then makes
	// mounting fail with ErrUnsupported, not with the read error.
	f := hostileFS(makeTag(tagLVD, 0, make([]byte, 512)), makeTag(tagPD, 1, make([]byte, 512)))
	err := f.readVDS(extentAD{loc: 0, length: 3 * sectorSize})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported from mounting", err)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v must not be the sector read error", err)
	}
}

func TestShortReadMatchesCorruptAndUnexpectedEOF(t *testing.T) {
	err := readFull(bytes.NewReader(make([]byte, 10)), make([]byte, 20), 0)
	if !errors.Is(err, ErrCorrupt) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want ErrCorrupt and io.ErrUnexpectedEOF", err)
	}
}
