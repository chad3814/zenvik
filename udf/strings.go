package udf

import (
	"fmt"
	"unicode/utf16"
)

// decodeDchars decodes OSTA CS0 d-characters: a compression ID (8 or 254
// for one byte per character, 16 or 255 for UTF-16BE) followed by data.
func decodeDchars(b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	data := b[1:]
	switch b[0] {
	case 8, 254:
		rs := make([]rune, len(data))
		for i, c := range data {
			rs[i] = rune(c)
		}
		return string(rs), nil
	case 16, 255:
		if len(data)%2 != 0 {
			return "", fmt.Errorf("%w: odd-length UTF-16 name", ErrCorrupt)
		}
		u := make([]uint16, len(data)/2)
		for i := range u {
			u[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
		}
		return string(utf16.Decode(u)), nil
	default:
		return "", fmt.Errorf("%w: character compression ID %d", ErrCorrupt, b[0])
	}
}

// decodeDstring decodes a fixed-size dstring field whose last byte holds
// the number of bytes used.
func decodeDstring(field []byte) (string, error) {
	if len(field) == 0 {
		return "", nil
	}
	n := int(field[len(field)-1])
	if n == 0 {
		return "", nil
	}
	if n > len(field)-1 {
		return "", fmt.Errorf("%w: dstring length %d exceeds field", ErrCorrupt, n)
	}
	return decodeDchars(field[:n])
}
