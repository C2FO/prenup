// Package runner executes selected tasks against the changed modules and emits
// a stream of events that the UI layers render (human TUI, markdown digest,
// NDJSON for agents).
package runner

import "time"

// EventKind names the shape of an Event.
type EventKind string

// Event kinds emitted by the runner. Consumed by all UI sinks (human, markdown,
// json) and exposed verbatim on the JSON wire format.
const (
	EventRunStarted    EventKind = "run_started"
	EventTaskStarted   EventKind = "task_started"
	EventLine          EventKind = "line"
	EventTaskCompleted EventKind = "task_completed"
	EventRunCompleted  EventKind = "run_completed"
	EventNotice        EventKind = "notice"
)

// Stream identifies which pipe an output line came from.
type Stream string

// Stream values for EventLine events.
const (
	StreamStdout Stream = "stdout"
	StreamStderr Stream = "stderr"
)

// TaskStatus is the terminal status of a task run.
type TaskStatus string

// TaskStatus values reported on EventTaskCompleted.
const (
	TaskStatusDone    TaskStatus = "done"
	TaskStatusFailed  TaskStatus = "failed"
	TaskStatusSkipped TaskStatus = "skipped"
)

// Event is the unit the runner emits to subscribed UIs.
type Event struct {
	Kind EventKind `json:"type"`
	Time time.Time `json:"time,omitempty"`

	// Identifying fields (populated as relevant to Kind).
	Version string `json:"version,omitempty"`
	Task    string `json:"task,omitempty"`
	Module  string `json:"module,omitempty"`

	// Task-start fields. Carry the resolved (post-template-expansion)
	// command and working directory so a consumer reading a failure
	// transcript has everything it needs to reproduce the run without
	// also reading .prenup.yaml.
	Command    string `json:"command,omitempty"`
	WorkingDir string `json:"working_dir,omitempty"`

	// Line-specific fields.
	Stream Stream `json:"stream,omitempty"`
	Text   string `json:"text,omitempty"`

	// Task-completion fields.
	Status     TaskStatus `json:"status,omitempty"`
	DurationMs int64      `json:"duration_ms,omitempty"`
	Error      string     `json:"error,omitempty"`

	// Run-level fields.
	Modules   []string `json:"modules,omitempty"`
	Tasks     []string `json:"tasks,omitempty"`
	Succeeded int      `json:"succeeded,omitempty"`
	Failed    int      `json:"failed,omitempty"`
	ExitCode  int      `json:"exit_code,omitempty"`

	// RepoRoot is the absolute path of the git repository prenup was
	// invoked from. Set on EventRunStarted so a JSON consumer can anchor
	// every subsequent task's working_dir without inferring it from
	// substring matches.
	RepoRoot string `json:"repo_root,omitempty"`

	// FailedTasks lists the names of tasks that ended in TaskStatusFailed.
	// Set on EventRunCompleted so a consumer with a long log can jump
	// directly to the failures without rescanning every task_completed
	// event.
	FailedTasks []string `json:"failed_tasks,omitempty"`

	// Free-form human-readable message used by notices.
	Message string `json:"message,omitempty"`
}

// Sink consumes events.
//
// Implementations MUST be safe for concurrent Emit calls from multiple
// goroutines: parallel per-module task runs (see runner.runParallel) fan out
// goroutines that each call Emit on the same sink. Sinks that wrap shared
// state (writers, channels, maps) need their own synchronization.
//
// Close is called once, by the orchestrator that owns the sink, after the
// final Run-level event has been delivered. Implementations may use it to
// flush buffered output. Close need not be safe to call concurrently with
// Emit.
type Sink interface {
	Emit(Event)
	Close() error
}

// DiscardSink drops every event. Useful as a default for callers that do not
// supply a Sink, and for tests.
type DiscardSink struct{}

// Emit on a DiscardSink does nothing.
func (DiscardSink) Emit(Event) {}

// Close on a DiscardSink does nothing.
func (DiscardSink) Close() error { return nil }
