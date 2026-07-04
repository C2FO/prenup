package human

import (
	"fmt"
	"io"
	"sync"

	"github.com/charmbracelet/lipgloss"

	"github.com/c2fo/prenup/internal/runner"
)

// maxSummaryLogLines caps how many output lines SummarySink retains for the
// post-run "Prenup output" block. A noisy task (verbose test suite, chatty
// linter) could otherwise grow s.logs without bound during a single hook
// run. When the cap is exceeded we keep the most recent lines -- the tail is
// where failures and stack traces surface -- and note how many were dropped.
const maxSummaryLogLines = 5000

// SummarySink collects events and prints a human-readable post-run summary to
// stdout after the alt-screen UI exits. Used in combination with the
// RunnerSink TUI to preserve visibility in clients that hide the alt-screen.
type SummarySink struct {
	mu           sync.Mutex
	w            io.Writer
	version      string
	notice       string
	logs         []string
	droppedLines int
	taskOrder    []string
	seenTask     map[string]bool
	statuses     map[string]runner.TaskStatus
	durations    map[string]int64
	notes        map[string]string
	summary      runner.Event
}

// NewSummarySink returns a SummarySink writing to w.
func NewSummarySink(w io.Writer) *SummarySink {
	return &SummarySink{
		w:         w,
		seenTask:  make(map[string]bool),
		statuses:  make(map[string]runner.TaskStatus),
		durations: make(map[string]int64),
		notes:     make(map[string]string),
	}
}

// Emit records event data for later printing.
func (s *SummarySink) Emit(ev runner.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Kind {
	case runner.EventRunStarted:
		s.version = ev.Version
		s.notice = ev.Message
	case runner.EventLine:
		s.appendLog(ev.Text)
	case runner.EventTaskStarted:
		if !s.seenTask[ev.Task] {
			s.seenTask[ev.Task] = true
			s.taskOrder = append(s.taskOrder, ev.Task)
		}
	case runner.EventTaskCompleted:
		if !s.seenTask[ev.Task] {
			s.seenTask[ev.Task] = true
			s.taskOrder = append(s.taskOrder, ev.Task)
		}
		s.statuses[ev.Task] = ev.Status
		s.durations[ev.Task] += ev.DurationMs
		if ev.Message != "" {
			s.notes[ev.Task] = ev.Message
		}
	case runner.EventRunCompleted:
		s.summary = ev
	}
}

// appendLog records an output line, enforcing maxSummaryLogLines by
// dropping the oldest line once the cap is reached. Caller holds s.mu.
func (s *SummarySink) appendLog(line string) {
	if len(s.logs) >= maxSummaryLogLines {
		s.logs = s.logs[1:]
		s.droppedLines++
	}
	s.logs = append(s.logs, line)
}

// Close flushes the summary to the writer.
func (s *SummarySink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.render()
	return nil
}

// writef writes to s.w, intentionally discarding errors (summary prints to
// stdout; a failing writer is rare and would fail every subsequent call).
func (s *SummarySink) writef(format string, a ...any) {
	_, _ = fmt.Fprintf(s.w, format, a...)
}

func (s *SummarySink) writeln(a ...any) {
	_, _ = fmt.Fprintln(s.w, a...)
}

func (s *SummarySink) render() {
	s.writef("\nprenup %s\n", s.version)
	if s.notice != "" {
		s.writef("%s\n", s.notice)
	}
	s.writef("\nPrenup output:\n----------------\n")
	if s.droppedLines > 0 {
		s.writef("... (%d earlier line(s) omitted to bound memory) ...\n", s.droppedLines)
	}
	for _, line := range s.logs {
		s.writeln(line)
	}

	s.writef("\nTask Summary:\n------------\n")
	for _, name := range s.taskOrder {
		symbol := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Render("v")
		switch s.statuses[name] {
		case runner.TaskStatusFailed:
			symbol = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render("x")
		case runner.TaskStatusSkipped:
			symbol = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render("-")
		}
		extra := ""
		if note := s.notes[name]; note != "" {
			extra = " -- " + note
		}
		s.writef("%s %s  (%dms)%s\n", symbol, name, s.durations[name], extra)
	}
	s.writef("\nSummary: %d succeeded, %d failed\n", s.summary.Succeeded, s.summary.Failed)
}
