package bluray

import (
	"testing"
	"time"
)

func TestTicksDuration(t *testing.T) {
	tests := []struct {
		in   Ticks
		want time.Duration
	}{
		{0, 0},
		{45000, time.Second},
		{22500, 500 * time.Millisecond},
		{45000 * 3600, time.Hour},
		{0xFFFFFFFF, 95443*time.Second + 717666666},
	}
	for _, tt := range tests {
		if got := tt.in.Duration(); got != tt.want {
			t.Errorf("Ticks(%d).Duration() = %v, want %v", tt.in, got, tt.want)
		}
	}
}
