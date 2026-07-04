// Package jsonout is a runner.Sink that writes one JSON object per line.
// The schema is documented in prenup/docs/SCHEMA.md and is intended to be
// consumed by agents and scripting clients.
package jsonout

import (
	"encoding/json"
	"io"
	"sync"

	"github.com/c2fo/prenup/internal/runner"
	"github.com/c2fo/prenup/internal/ui"
)

// Sink writes NDJSON events to w.
type Sink struct {
	mu  sync.Mutex
	enc *json.Encoder
}

// agentHint is the bootstrap NDJSON line emitted before any runner event.
// It carries the minimum self-describing context an agent needs to orient
// itself if it has no prior knowledge of prenup -- what the tool is, how
// to switch to/parse this stream, and what to do on failure. The schema
// version is bumped via ui.AgentHintSchema when the shape changes.
//
// Consumers that don't recognize the type field should skip the line and
// continue parsing as normal; the rest of the stream is unaffected.
type agentHint struct {
	Type           string `json:"type"`
	Schema         string `json:"schema"`
	Tool           string `json:"tool"`
	Description    string `json:"description"`
	Homepage       string `json:"homepage"`
	HookContext    string `json:"hook_context"`
	BypassHint     string `json:"bypass_hint"`
	StreamFormat   string `json:"stream_format"`
	EventTypesNote string `json:"event_types_note"`
}

// New constructs a Sink writing to w. It immediately writes a single
// `agent_hint` line so first-time consumers can identify the stream
// without prior knowledge of prenup.
func New(w io.Writer) *Sink {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	s := &Sink{enc: enc}
	_ = enc.Encode(agentHint{
		Type:         "agent_hint",
		Schema:       ui.AgentHintSchema,
		Tool:         ui.Tool,
		Description:  ui.Description,
		Homepage:     ui.HomepageURL,
		HookContext:  ui.HookContextNote,
		BypassHint:   ui.FailureBypassHint,
		StreamFormat: "ndjson",
		EventTypesNote: "Subsequent lines are runner events: run_started, " +
			"task_started, line, task_completed, run_completed, notice. " +
			"See prenup/docs/SCHEMA.md.",
	})
	return s
}

// Emit writes ev as a single line of JSON.
func (s *Sink) Emit(ev runner.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.enc.Encode(ev)
}

// Close is a no-op; the underlying writer is owned by the caller.
func (s *Sink) Close() error { return nil }
