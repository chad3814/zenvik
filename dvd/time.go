package dvd

import (
	"fmt"
	"time"
)

// FrameRate is the frame rate of a playback time.
type FrameRate int

// Frame rates.
const (
	Rate25 FrameRate = 25 // PAL
	Rate30 FrameRate = 30 // NTSC, 29.97 frames per second
)

// Time is a BCD playback time from an IFO file.
type Time struct {
	Hours, Minutes, Seconds, Frames int
	Rate                            FrameRate // 0 only for an all-zero time
}

// Duration converts t to a time.Duration. NTSC frames last 1001/30000 s.
func (t Time) Duration() time.Duration {
	d := time.Duration(t.Hours)*time.Hour + time.Duration(t.Minutes)*time.Minute + time.Duration(t.Seconds)*time.Second
	switch t.Rate {
	case Rate25:
		d += time.Duration(t.Frames) * 40 * time.Millisecond
	case Rate30:
		d += time.Duration(t.Frames) * 1001 * time.Second / 30000
	}
	return d
}

// NewTime returns the Time for d at rate r, rounding down to a whole frame.
// Durations of 100 hours or more are clamped to 99:59:59.
func NewTime(d time.Duration, r FrameRate) Time {
	if d < 0 {
		d = 0
	}
	if d >= 100*time.Hour {
		d = 100*time.Hour - time.Second
	}
	t := Time{Rate: r}
	t.Hours = int(d / time.Hour)
	d -= time.Duration(t.Hours) * time.Hour
	t.Minutes = int(d / time.Minute)
	d -= time.Duration(t.Minutes) * time.Minute
	t.Seconds = int(d / time.Second)
	d -= time.Duration(t.Seconds) * time.Second
	switch r {
	case Rate25:
		t.Frames = int(d / (40 * time.Millisecond))
	case Rate30:
		t.Frames = int(d * 30000 / (1001 * time.Second))
	}
	return t
}

func parseTime(b []byte) (Time, error) {
	bcd := func(x byte) (int, bool) {
		hi, lo := x>>4, x&0x0F
		return int(hi)*10 + int(lo), hi <= 9 && lo <= 9
	}
	h, ok1 := bcd(b[0])
	m, ok2 := bcd(b[1])
	s, ok3 := bcd(b[2])
	f, ok4 := bcd(b[3] & 0x3F)
	if !ok1 || !ok2 || !ok3 || !ok4 || m > 59 || s > 59 {
		return Time{}, fmt.Errorf("%w: bad BCD time % X", ErrCorrupt, b[:4])
	}
	t := Time{Hours: h, Minutes: m, Seconds: s, Frames: f}
	switch b[3] >> 6 {
	case 1:
		t.Rate = Rate25
	case 3:
		t.Rate = Rate30
	case 0:
		if t != (Time{}) {
			return Time{}, fmt.Errorf("%w: time % X has no frame rate", ErrCorrupt, b[:4])
		}
		return t, nil
	default:
		return Time{}, fmt.Errorf("%w: time % X has an invalid frame rate", ErrCorrupt, b[:4])
	}
	if f >= int(t.Rate) {
		return Time{}, fmt.Errorf("%w: time % X has frame %d at %d fps", ErrCorrupt, b[:4], f, t.Rate)
	}
	return t, nil
}
