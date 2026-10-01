package mux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Identification is what `mkvmerge -J` reports about an input.
type Identification struct {
	Tracks   []IdentifiedTrack
	Chapters int
	Warnings []string // mkvmerge's identification warnings
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
	cmd := m.command(ctx, "-J", input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return parseIdentification(stdout.Bytes())
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return nil, fmt.Errorf("%w: identifying %s: %w", ErrFailed, input, err)
	}
	if ee.ExitCode() == 1 { // warnings; the JSON is still valid
		return parseIdentification(stdout.Bytes())
	}
	if raw, derr := decodeIdentification(stdout.Bytes()); derr == nil && len(raw.Errors) > 0 {
		return nil, fmt.Errorf("%w: identifying %s: %s", ErrFailed, input, strings.Join(raw.Errors, "; "))
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = err.Error()
	}
	return nil, fmt.Errorf("%w: identifying %s: %s", ErrFailed, input, detail)
}

type rawIdentification struct {
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
	Tracks   []struct {
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

func decodeIdentification(b []byte) (*rawIdentification, error) {
	var raw rawIdentification
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%w: unreadable identification output: %w", ErrFailed, err)
	}
	return &raw, nil
}

func parseIdentification(b []byte) (*Identification, error) {
	raw, err := decodeIdentification(b)
	if err != nil {
		return nil, err
	}
	if len(raw.Errors) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrFailed, strings.Join(raw.Errors, "; "))
	}
	id := &Identification{}
	if len(raw.Warnings) > 0 {
		id.Warnings = raw.Warnings
	}
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
