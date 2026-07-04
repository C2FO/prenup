// Package markdown is a runner.Sink that streams a plain-text progress feed
// during execution and emits a final structured markdown digest at the end.
// It is selected automatically when stdout is not a TTY.
package markdown

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/c2fo/prenup/internal/runner"
	"github.com/c2fo/prenup/internal/ui"
)

// Sink renders markdown output.
type Sink struct {
	mu sync.Mutex
	w  io.Writer

	version     string
	notice      string
	modules     []string
	selected    []string
	taskLogs    map[string][]string // task -> accumulated lines (truncated tail kept)
	taskStatus  map[string]runner.TaskStatus
	taskTime    map[string]int64
	taskMessage map[string]string
	taskOrder   []string
	succeeded   int
	failed      int
	exitCode    int
	failedTasks []string
}

// New constructs a markdown Sink writing to w.
func New(w io.Writer) *Sink {
	return &Sink{
		w:           w,
		taskLogs:    make(map[string][]string),
		taskStatus:  make(map[string]runner.TaskStatus),
		taskTime:    make(map[string]int64),
		taskMessage: make(map[string]string),
	}
}

const maxFailureTail = 50

// writef writes a formatted line to s.w, ignoring transient write errors on
// stdout/pipes. A closed/failing writer would also fail every subsequent call,
// so surfacing each individual error adds no value.
func (s *Sink) writef(format string, a ...any) {
	_, _ = fmt.Fprintf(s.w, format, a...)
}

// writeln writes a line (or newline-only string) to s.w, ignoring errors.
func (s *Sink) writeln(a ...any) {
	_, _ = fmt.Fprintln(s.w, a...)
}

// Emit handles an event by streaming a short human line now and accumulating
// state for the final digest.
func (s *Sink) Emit(ev runner.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch ev.Kind {
	case runner.EventRunStarted:
		s.handleRunStarted(ev)
	case runner.EventTaskStarted:
		s.handleTaskStarted(ev)
	case runner.EventLine:
		s.handleLine(ev)
	case runner.EventTaskCompleted:
		s.handleTaskCompleted(ev)
	case runner.EventRunCompleted:
		s.succeeded = ev.Succeeded
		s.failed = ev.Failed
		s.exitCode = ev.ExitCode
		s.failedTasks = ev.FailedTasks
		s.renderDigest()
	case runner.EventNotice:
		s.writef("Notice: %s\n", ev.Message)
	}
}

func (s *Sink) handleRunStarted(ev runner.Event) {
	s.version = ev.Version
	s.notice = ev.Message
	s.modules = ev.Modules
	s.selected = ev.Tasks

	// Self-describing preamble so an agent (or operator) that has never
	// heard of prenup can identify the tool and understand what just
	// happened from a single git-commit transcript. Humans see the TUI
	// instead of this and so do not need it.
	s.writef("[%s] %s\n", ui.Tool, ui.Description)
	s.writef("[%s] Docs: %s\n", ui.Tool, ui.HomepageURL)
	s.writef("[%s] %s\n", ui.Tool, ui.JSONHint)
	s.writeln()

	s.writef("Prenup version: %s\n", ui.VersionLabel(s.version))
	if s.notice != "" {
		s.writef("%s\n", s.notice)
	}
	if len(s.modules) > 0 {
		s.writef("Modules: %s\n", strings.Join(s.modules, ", "))
	}
	s.writef("Tasks:   %s\n\n", strings.Join(s.selected, ", "))
}

func (s *Sink) handleTaskStarted(ev runner.Event) {
	if _, ok := s.taskLogs[ev.Task]; !ok {
		s.taskOrder = append(s.taskOrder, ev.Task)
		s.taskLogs[ev.Task] = nil
	}
	if ev.Module != "" {
		s.writef("> %s (%s)\n", ev.Task, ev.Module)
	} else {
		s.writef("> %s\n", ev.Task)
	}
	// Echo the resolved command so the live stream documents exactly what
	// was about to run, and so the failure-tail digest carries enough to
	// reproduce the invocation without reading .prenup.yaml. Routed
	// through appendLog so it lands in the output_tail on failure.
	if ev.Command != "" {
		echo := "[prenup] $ " + ev.Command
		if ev.WorkingDir != "" {
			echo += "   (cwd: " + ev.WorkingDir + ")"
		}
		s.writeln(echo)
		s.appendLog(ev.Task, echo)
	}
}

// handleLine prefixes [module] in parallel runs so the digest tail is
// unambiguous about which module produced each output line.
func (s *Sink) handleLine(ev runner.Event) {
	line := ev.Text
	if ev.Module != "" && ev.Module != "." {
		line = "[" + ev.Module + "] " + line
	}
	s.writeln(line)
	s.appendLog(ev.Task, line)
}

func (s *Sink) handleTaskCompleted(ev runner.Event) {
	s.taskStatus[ev.Task] = ev.Status
	s.taskTime[ev.Task] = ev.DurationMs
	if ev.Message != "" {
		s.taskMessage[ev.Task] = ev.Message
	}
	symbol := "OK"
	switch ev.Status {
	case runner.TaskStatusFailed:
		symbol = "FAIL"
	case runner.TaskStatusSkipped:
		symbol = "SKIP"
	}
	s.writef("[%s] %s (%dms)\n\n", symbol, ev.Task, ev.DurationMs)
}

// Close flushes any buffered state. The digest is emitted on EventRunCompleted,
// so Close is a no-op unless that event was missed.
func (s *Sink) Close() error { return nil }

func (s *Sink) appendLog(task, line string) {
	if task == "" {
		return
	}
	logs := s.taskLogs[task]
	logs = append(logs, line)
	if len(logs) > maxFailureTail {
		logs = logs[len(logs)-maxFailureTail:]
	}
	s.taskLogs[task] = logs
}

func (s *Sink) renderDigest() {
	s.writeln("---")
	s.writef("## Prenup %s\n\n", ui.VersionLabel(s.version))
	if s.notice != "" {
		s.writef("> %s\n\n", s.notice)
	}
	s.writef("**Summary:** %d succeeded, %d failed (exit %d)\n", s.succeeded, s.failed, s.exitCode)
	// Surface failed task names directly under the summary so an agent
	// scanning a long digest can jump straight to the failures without
	// reading every per-task section.
	if len(s.failedTasks) > 0 {
		s.writef("**Failed tasks:** %s\n", strings.Join(s.failedTasks, ", "))
	}
	s.writeln()

	for _, name := range s.taskOrder {
		status := s.taskStatus[name]
		s.writef("### Task: %s\n", name)
		s.writef("- Status: `%s`\n", status)
		s.writef("- Duration: %dms\n", s.taskTime[name])
		if msg := s.taskMessage[name]; msg != "" {
			s.writef("- Note: %s\n", msg)
		}
		if status == runner.TaskStatusFailed {
			tail := s.taskLogs[name]
			if len(tail) > 0 {
				s.writeln()
				s.writeln("<details><summary>Output tail</summary>")
				s.writeln()
				s.writeln("```")
				for _, line := range tail {
					s.writeln(line)
				}
				s.writeln("```")
				s.writeln("</details>")
			}
		}
		s.writeln()
	}

	if s.failed > 0 {
		s.writeln("### Next steps")
		// Lead with the hook-context line so consumers understand the
		// commit was blocked by prenup, not by `git` itself.
		s.writef("- %s\n", ui.HookContextNote)
		s.writeln("- Reproduce the failing task locally, then re-run `git commit`.")
		s.writeln("- Use `prenup run --task \"<name>\"` to rerun a specific task without committing.")
		s.writef("- %s\n", ui.FailureBypassHint)
	}
}
