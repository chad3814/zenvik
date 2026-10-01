// Package udf reads UDF (ECMA-167 / OSTA UDF 1.02–2.60) file systems such
// as Blu-ray disc images, exposing them as a read-only io/fs.FS. It
// supports physical and metadata partitions, short, long and embedded
// allocation descriptors, and allocation extent chains. Sparable and
// virtual partitions are not supported.
package udf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const sectorSize = 2048

// Errors returned by Open and file operations; wrapped errors add detail.
var (
	ErrNotUDF      = errors.New("udf: no UDF volume found")
	ErrCorrupt     = errors.New("udf: corrupt structure")
	ErrUnsupported = errors.New("udf: unsupported feature")
)

// Descriptor tag identifiers (ECMA-167 3/7.2.1 and 4/7.2.1).
const (
	tagPVD  = 1
	tagAVDP = 2
	tagVDP  = 3
	tagPD   = 5
	tagLVD  = 6
	tagTD   = 8
	tagFSD  = 256
	tagFID  = 257
	tagAED  = 258
	tagIE   = 259
	tagFE   = 261
	tagEFE  = 266
)

// anyLocation disables parseTag's tag location check.
const anyLocation = math.MaxUint32

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func le64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }

// crc16 is the ECMA-167 descriptor CRC: polynomial 0x1021, initial 0.
func crc16(b []byte) uint16 {
	var c uint16
	for _, x := range b {
		c ^= uint16(x) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	return c
}

// parseTag validates the descriptor tag at the start of d (checksum,
// location and CRC) and returns the tag identifier. loc is the logical
// block d was read from, or anyLocation.
func parseTag(d []byte, loc uint32) (uint16, error) {
	if len(d) < 16 {
		return 0, fmt.Errorf("%w: descriptor shorter than its tag", ErrCorrupt)
	}
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	if sum != d[4] {
		return 0, fmt.Errorf("%w: tag checksum mismatch (block %d)", ErrCorrupt, loc)
	}
	if got := le32(d[12:]); loc != anyLocation && got != loc {
		return 0, fmt.Errorf("%w: tag location %d read from block %d", ErrCorrupt, got, loc)
	}
	end := 16 + int(le16(d[10:]))
	if end > len(d) {
		return 0, fmt.Errorf("%w: tag CRC length %d exceeds descriptor", ErrCorrupt, end-16)
	}
	if crc16(d[16:end]) != le16(d[8:]) {
		return 0, fmt.Errorf("%w: tag CRC mismatch (block %d)", ErrCorrupt, loc)
	}
	return le16(d), nil
}

// expectTag is parseTag plus a check that the identifier is one of want.
func expectTag(d []byte, loc uint32, want ...uint16) (uint16, error) {
	id, err := parseTag(d, loc)
	if err != nil {
		return 0, err
	}
	for _, w := range want {
		if id == w {
			return id, nil
		}
	}
	return 0, fmt.Errorf("%w: descriptor tag %d at block %d, want %v", ErrCorrupt, id, loc, want)
}
