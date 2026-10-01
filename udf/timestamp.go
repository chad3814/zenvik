package udf

import "time"

// decodeTimestamp decodes a 12-byte ECMA-167 timestamp. Local-time stamps
// carry a UTC offset in minutes (-2047 means unspecified, read as UTC).
// An all-zero timestamp decodes to the zero time.
func decodeTimestamp(b []byte) time.Time {
	month := b[4]
	if month == 0 {
		return time.Time{}
	}
	loc := time.UTC
	tz := le16(b)
	if tz>>12 == 1 {
		off := int(tz & 0x0FFF)
		if off&0x0800 != 0 {
			off -= 0x1000
		}
		if off != -2047 {
			loc = time.FixedZone("", off*60)
		}
	}
	ns := int(b[9])*10_000_000 + int(b[10])*100_000 + int(b[11])*1000
	return time.Date(int(int16(le16(b[2:]))), time.Month(month), int(b[5]),
		int(b[6]), int(b[7]), int(b[8]), ns, loc)
}
