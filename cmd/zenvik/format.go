package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/chad3814/zenvik"
)

// formatDuration renders d as H:MM:SS, rounded to the second.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d:%02d", int(d/time.Hour), int(d/time.Minute)%60, int(d/time.Second)%60)
}

// formatSize renders n bytes with binary units, up to TiB.
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

func videoSummary(t *zenvik.Title) string {
	if len(t.Video) == 0 {
		return "-"
	}
	v := t.Video[0]
	s := v.Codec.String() + " " + v.Format.String()
	if v.HDR() {
		s += " " + v.DynamicRange.String()
	}
	if extra := len(t.Video) - 1; extra > 0 {
		s += fmt.Sprintf(" +%d", extra)
	}
	return s
}

func audioLanguages(t *zenvik.Title) string {
	langs := make([]string, 0, len(t.Audio))
	for _, a := range t.Audio {
		langs = append(langs, a.Language)
	}
	return joinLanguages(langs)
}

func subtitleLanguages(t *zenvik.Title) string {
	langs := make([]string, 0, len(t.Subtitles))
	for _, s := range t.Subtitles {
		langs = append(langs, s.Language)
	}
	return joinLanguages(langs)
}

// joinLanguages lists distinct languages in order; empty codes become "und".
func joinLanguages(langs []string) string {
	var out []string
	seen := map[string]bool{}
	for _, l := range langs {
		if l == "" {
			l = "und"
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return "-"
	}
	return strings.Join(out, ",")
}
