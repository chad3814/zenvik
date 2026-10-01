package bluray

import "time"

// TicksPerSecond is the rate of the 45 kHz clock used for play item and
// playlist mark times.
const TicksPerSecond = 45000

// Ticks is a timestamp or duration on the 45 kHz clock.
type Ticks uint32

// Duration converts t to a time.Duration.
func (t Ticks) Duration() time.Duration { return ticksDuration(uint64(t)) }

func ticksDuration(t uint64) time.Duration {
	return time.Duration(t/TicksPerSecond)*time.Second +
		time.Duration(t%TicksPerSecond)*time.Second/TicksPerSecond
}
