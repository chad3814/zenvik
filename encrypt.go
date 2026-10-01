package zenvik

import (
	"errors"
	"io"
)

const (
	sourcePacketSize = 192
	alignedUnitSize  = 6144 // 32 source packets
	tsSyncByte       = 0x47
)

// clipEncrypted reports whether an M2TS stream looks AACS-encrypted. AACS
// encrypts each 6144-byte aligned unit except its first 16 bytes, so in an
// encrypted unit the TS sync byte at offset 4 of every source packet after
// the first is scrambled. Only the first aligned unit is read. Streams with
// fewer than two complete source packets cannot be judged and are reported
// as not encrypted.
func clipEncrypted(r io.Reader) (bool, error) {
	buf := make([]byte, alignedUnitSize)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	for p := 1; p < n/sourcePacketSize; p++ {
		if buf[p*sourcePacketSize+4] != tsSyncByte {
			return true, nil
		}
	}
	return false, nil
}
