package main

import (
	"encoding/json"
	"io"

	"github.com/chad3814/zenvik"
)

// eventsVersion is the version of the rip --jsonl event schema.
const eventsVersion = 1

// eventWriter writes rip's --jsonl events to w, one JSON object per line.
type eventWriter struct {
	enc     *json.Encoder
	lastPct map[zenvik.Phase]int
}

func newEventWriter(w io.Writer) *eventWriter {
	return &eventWriter{enc: json.NewEncoder(w), lastPct: map[zenvik.Phase]int{}}
}

type startEvent struct {
	Event           string   `json:"event"`
	Version         int      `json:"version"`
	Source          string   `json:"source"`
	Kind            string   `json:"kind"`
	Format          string   `json:"format"`
	Title           string   `json:"title"`
	DurationSeconds float64  `json:"duration_seconds"`
	SizeBytes       int64    `json:"size_bytes"`
	Output          string   `json:"output"`
	RipMethod       string   `json:"rip_method,omitempty"`
	TempBytes       int64    `json:"temp_bytes,omitempty"`
	Auto            bool     `json:"auto"`
	Ambiguous       bool     `json:"ambiguous"`
	Reasons         []string `json:"reasons"`
}

type progressEvent struct {
	Event      string  `json:"event"`
	Phase      string  `json:"phase"`
	Fraction   float64 `json:"fraction"`
	BytesDone  int64   `json:"bytes_done,omitempty"`
	BytesTotal int64   `json:"bytes_total,omitempty"`
}

type messageEvent struct {
	Event   string `json:"event"`
	Message string `json:"message"`
}

type doneEvent struct {
	Event           string  `json:"event"`
	Output          string  `json:"output"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type dryRunEvent struct {
	Event   string   `json:"event"`
	Output  string   `json:"output"`
	Command []string `json:"command"`
}

type errorEvent struct {
	Event    string `json:"event"`
	Message  string `json:"message"`
	ExitCode int    `json:"exit_code"`
	Canceled bool   `json:"canceled,omitempty"`
}

// emit writes one event. Errors writing to the caller's pipe are ignored:
// there is nowhere left to report them.
func (e *eventWriter) emit(v any) { _ = e.enc.Encode(v) }

func (e *eventWriter) start(ev startEvent) {
	ev.Event, ev.Version = "start", eventsVersion
	if ev.Reasons == nil {
		ev.Reasons = []string{}
	}
	e.emit(ev)
}

// progress writes a progress event when the phase's whole percentage
// changes, so a long rip doesn't flood the pipe.
func (e *eventWriter) progress(p zenvik.Progress) {
	pct := int(p.Fraction * 100)
	if last, ok := e.lastPct[p.Phase]; ok && last == pct {
		return
	}
	e.lastPct[p.Phase] = pct
	e.emit(progressEvent{Event: "progress", Phase: p.Phase.String(), Fraction: p.Fraction, BytesDone: p.BytesDone, BytesTotal: p.BytesTotal})
}

func (e *eventWriter) warning(msg string) { e.emit(messageEvent{Event: "warning", Message: msg}) }

func (e *eventWriter) done(output string, seconds float64) {
	e.emit(doneEvent{Event: "done", Output: output, DurationSeconds: seconds})
}

func (e *eventWriter) dryRun(output string, command []string) {
	e.emit(dryRunEvent{Event: "dry_run", Output: output, Command: command})
}

func (e *eventWriter) fail(msg string, code int, canceled bool) {
	e.emit(errorEvent{Event: "error", Message: msg, ExitCode: code, Canceled: canceled})
}
