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

// Duration converts t to a time.Duration. NTSC times count frames at a
// nominal 30 fps (dvdauthor writes 00:00:06:00 for 180 frames), and each
// frame lasts 1001/30000 s, so 00:00:06:00 is 6.006 s. PAL times are exact.
func (t Time) Duration() time.Duration {
	secs := int64(t.Hours)*3600 + int64(t.Minutes)*60 + int64(t.Seconds)
	switch t.Rate {
	case Rate25:
		return time.Duration(secs)*time.Second + time.Duration(t.Frames)*40*time.Millisecond
	case Rate30:
		// frames × 1001 s / 30000, reduced by 10⁴ so 99:59:59:29 doesn't overflow.
		return time.Duration(secs*30+int64(t.Frames)) * 100100000 / 3
	}
	return time.Duration(secs) * time.Second
}

// NewTime returns the Time for d at rate r, rounded to the nearest frame.
// It inverts Duration. Durations past the last frame of 99:59:59 are
// clamped to that frame.
func NewTime(d time.Duration, r FrameRate) Time {
	// Clamp before multiplying: 101 h is past the last frame of 99:59:59 at
	// either rate (NTSC's is 100.1 h), and d × 3 would overflow near 854 h.
	d = min(max(d, 0), 101*time.Hour)
	t := Time{Rate: r}
	var frames, fps int64
	switch r {
	case Rate25:
		fps = 25
		frames = int64((d + 20*time.Millisecond) / (40 * time.Millisecond))
	case Rate30:
		// (d × 30000 + 1001 s/2) / 1001 s, reduced by 10⁴ so it can't overflow.
		fps = 30
		frames = int64((d*3 + 50050000) / 100100000)
	default:
		s := min(int64(d/time.Second), 100*3600-1)
		t.Hours, t.Minutes, t.Seconds = int(s/3600), int(s/60%60), int(s%60)
		return t
	}
	frames = min(frames, (100*3600)*fps-1)
	t.Hours = int(frames / (fps * 3600))
	t.Minutes = int(frames / (fps * 60) % 60)
	t.Seconds = int(frames / fps % 60)
	t.Frames = int(frames % fps)
	return t
}

// parseTime decodes a 4-byte BCD time. Only invalid digits are an error, and
// the message carries no sentinel: callers wrap it with corrupt. Like
// libdvdread, it tolerates real-disc quirks: a missing or reserved frame rate
// reads as 30 fps and an out-of-range frame count is clamped.
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
		return Time{}, fmt.Errorf("bad BCD time % X", b[:4])
	}
	t := Time{Hours: h, Minutes: m, Seconds: s, Frames: f}
	if b[3]>>6 == 0 && t == (Time{}) {
		return t, nil
	}
	t.Rate = Rate30
	if b[3]>>6 == 1 {
		t.Rate = Rate25
	}
	if f >= int(t.Rate) {
		t.Frames = int(t.Rate) - 1
	}
	return t, nil
}
