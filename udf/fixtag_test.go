package udf_test

import "encoding/binary"

// fixTag recomputes the CRC and checksum of the descriptor tag at the
// start of d after a test edits the descriptor body.
func fixTag(d []byte) {
	n := int(binary.LittleEndian.Uint16(d[10:]))
	var c uint16
	for _, x := range d[16 : 16+n] {
		c ^= uint16(x) << 8
		for range 8 {
			if c&0x8000 != 0 {
				c = c<<1 ^ 0x1021
			} else {
				c <<= 1
			}
		}
	}
	binary.LittleEndian.PutUint16(d[8:], c)
	var sum byte
	for i := 0; i < 16; i++ {
		if i != 4 {
			sum += d[i]
		}
	}
	d[4] = sum
}
