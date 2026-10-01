package main

import (
	"testing"
	"time"

	"github.com/chad3814/zenvik"
)

func TestFormatDuration(t *testing.T) {
	tests := map[time.Duration]string{
		0:                                     "0:00:00",
		150 * time.Second:                     "0:02:30",
		100 * time.Minute:                     "1:40:00",
		59*time.Second + 600*time.Millisecond: "0:01:00",
		25*time.Hour + 3*time.Second:          "25:00:03",
	}
	for in, want := range tests {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := map[int64]string{
		0:       "0 B",
		1023:    "1023 B",
		1024:    "1.0 KiB",
		122880:  "120.0 KiB",
		5 << 30: "5.0 GiB",
		3 << 40: "3.0 TiB",
		5 << 50: "5120.0 TiB",
	}
	for in, want := range tests {
		if got := formatSize(in); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestJoinLanguages(t *testing.T) {
	if got := joinLanguages([]string{"eng", "fra", "eng", ""}); got != "eng,fra,und" {
		t.Errorf("got %q", got)
	}
	if got := joinLanguages(nil); got != "-" {
		t.Errorf("got %q", got)
	}
}

func TestKindName(t *testing.T) {
	for k, want := range map[zenvik.SourceKind]string{zenvik.ISO: "iso", zenvik.BDMVDir: "bdmv", 0: "unknown"} {
		if got := kindName(k); got != want {
			t.Errorf("kindName(%v) = %q, want %q", k, got, want)
		}
	}
}
