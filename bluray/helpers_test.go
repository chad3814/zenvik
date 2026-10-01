package bluray

import "encoding/binary"

func be16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

func zeros(n int) []byte { return make([]byte, n) }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
