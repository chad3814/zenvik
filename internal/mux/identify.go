package mux

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Identification is what `mkvmerge -J` reports about an input.
type Identification struct {
	Tracks   []IdentifiedTrack
	Chapters int
}

// IdentifiedTrack is one track mkvmerge found in the input.
type IdentifiedTrack struct {
	ID       int    // mkvmerge's track ID in this input
	Type     string // "video", "audio" or "subtitles"
	Codec    string
	PID      uint16 // transport stream PID
	Language string
	Channels int // audio only
}

// Identify runs `mkvmerge -J input`.
func (m *Mkvmerge) Identify(ctx context.Context, input string) (*Identification, error) {
	out, err := m.command(ctx, "-J", input).Output()
	if err != nil {
		return nil, fmt.Errorf("%w: identifying %s: %w", ErrFailed, input, err)
	}
	return parseIdentification(out)
}

func parseIdentification(b []byte) (*Identification, error) {
	var raw struct {
		Errors []string `json:"errors"`
		Tracks []struct {
			ID         int    `json:"id"`
			Type       string `json:"type"`
			Codec      string `json:"codec"`
			Properties struct {
				Number        uint64 `json:"number"`
				StreamID      uint64 `json:"stream_id"`
				Language      string `json:"language"`
				AudioChannels int    `json:"audio_channels"`
			} `json:"properties"`
		} `json:"tracks"`
		Chapters []struct {
			NumEntries int `json:"num_entries"`
		} `json:"chapters"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%w: unreadable identification output: %w", ErrFailed, err)
	}
	if len(raw.Errors) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrFailed, strings.Join(raw.Errors, "; "))
	}
	id := &Identification{}
	for _, t := range raw.Tracks {
		pid := t.Properties.Number
		if pid == 0 {
			pid = t.Properties.StreamID
		}
		id.Tracks = append(id.Tracks, IdentifiedTrack{
			ID: t.ID, Type: t.Type, Codec: t.Codec, PID: uint16(pid),
			Language: t.Properties.Language, Channels: t.Properties.AudioChannels,
		})
	}
	for _, c := range raw.Chapters {
		id.Chapters += c.NumEntries
	}
	return id, nil
}
