package udf

import (
	"bytes"
	"encoding/binary"
	"errors"
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
