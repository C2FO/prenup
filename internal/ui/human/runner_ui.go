package human

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/c2fo/prenup/internal/runner"
)

// EventChannelSink is a runner.Sink that pushes every event onto a buffered
// channel. The runner UI consumes the channel via a tea.Cmd loop, which keeps
// rendering and event processing on the same goroutine and avoids relying on
// the program's internal Send queue for high-throughput line streams.
//
// Concurrency:
//   - Emit may be called from any number of goroutines (parallel per-module
//     task runs) at the same time.
//   - Once Close has been called, every subsequent Emit returns immediately
//     instead of panicking with "send on closed channel" or pinning the
//     producer on a full buffer. The underlying event channel is intentionally
//     never closed, because doing so would race with producers that are still
//     in Emit; instead the consumer loop watches Done() to know when to quit.
//   - Close is safe to call multiple times; only the first call has any
//     effect.
type EventChannelSink struct {
	ch     chan runner.Event
	done   chan struct{}
	closer sync.Once
}

// NewEventChannelSink returns a sink with a generously-sized buffer so the
// runner goroutine rarely blocks waiting for the UI to render.
func NewEventChannelSink() *EventChannelSink {
	return &EventChannelSink{
		ch:   make(chan runner.Event, 1024),
		done: make(chan struct{}),
	}
}

// Emit forwards ev to the consumer channel. After Close has been called the
// event is dropped silently. Late notices from deferred cleanup (stash pop,
// etc.) emitted after EventRunCompleted must not crash the process.
func (s *EventChannelSink) Emit(ev runner.Event) {
	select {
	case <-s.done:
		return
	default:
	}
	select {
	case <-s.done:
	case s.ch <- ev:
	}
}

// Close marks the sink as closed exactly once. In-flight Emit calls observe
// the closed state and drop their event; the consumer loop should select on
// both Channel() and Done() and exit when Done fires.
func (s *EventChannelSink) Close() error {
	s.closer.Do(func() { close(s.done) })
	return nil
}

// Channel exposes the underlying buffered event channel.
//
// The channel is intentionally never closed; consumers must also watch Done()
// to know when to terminate. Closing the channel would race with producer
// goroutines still inside Emit and could panic with "send on closed channel".
func (s *EventChannelSink) Channel() <-chan runner.Event { return s.ch }

// Done returns a channel that is closed when Close is called. Consumers select
// on both Channel() and Done() to terminate cleanly.
func (s *EventChannelSink) Done() <-chan struct{} { return s.done }

// eventMsg wraps a runner event for delivery to the Bubble Tea Update loop.
type eventMsg struct{ ev runner.Event }

// channelClosedMsg signals the event channel was closed (run is fully done).
type channelClosedMsg struct{}

// nextEventCmd returns a tea.Cmd that blocks waiting for one event from ch
// (or for done to fire, signaling shutdown), then re-issues itself in the
// model's Update so the loop continues.
func nextEventCmd(ch <-chan runner.Event, done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-ch:
			return eventMsg{ev: ev}
		case <-done:
			// Drain any remaining buffered events so the final summary is
			// captured before quitting.
			select {
			case ev := <-ch:
				return eventMsg{ev: ev}
			default:
				return channelClosedMsg{}
			}
		}
	}
}

// taskUIState holds the per-task display data.
type taskUIState struct {
	name     string
	status   string // pending, running, done, error, skipped
	modules  map[string]string
	duration int64
	note     string
}

// maxLogLines bounds the in-memory log buffer so streaming many thousands of
// lines does not turn viewport.SetContent into a quadratic operation.
const maxLogLines = 5000

// RunnerModel is the Bubble Tea model that renders runner events live.
type RunnerModel struct {
	events   <-chan runner.Event
	done     <-chan struct{}
	version  string
	notice   string
	order    []string
	tasks    map[string]*taskUIState
	logLines []string
	viewport viewport.Model
	finished bool
}

// NewRunnerModel constructs a RunnerModel that consumes events from sink.
// The model uses both the event channel and the sink's Done channel so it
// can shut down cleanly even when no terminating event arrives.
func NewRunnerModel(sink *EventChannelSink) *RunnerModel {
	vp := viewport.New(80, 20)
	vp.SetContent("Waiting for tasks...\n")
	return &RunnerModel{
		events:   sink.Channel(),
		done:     sink.Done(),
		tasks:    make(map[string]*taskUIState),
		viewport: vp,
	}
}

// Init kicks off the event-pump command. Bubble Tea will continuously
// re-issue this command (via nextEventCmd) as long as events arrive.
func (m *RunnerModel) Init() tea.Cmd { return nextEventCmd(m.events, m.done) }

// Update processes events and key/window messages.
func (m *RunnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s := msg.String(); s == "ctrl+c" || s == "q" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.viewport.Width = msg.Width
		}
		if msg.Height > 0 {
			h := msg.Height - (len(m.tasks) + 8)
			if h < 4 {
				h = 4
			}
			m.viewport.Height = h
		}
	case eventMsg:
		m.applyEvent(msg.ev)
		if msg.ev.Kind == runner.EventRunCompleted {
			m.finished = true
		}
		return m, nextEventCmd(m.events, m.done)
	case channelClosedMsg:
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *RunnerModel) applyEvent(ev runner.Event) {
	switch ev.Kind {
	case runner.EventRunStarted:
		m.version = ev.Version
		m.notice = ev.Message
		for _, name := range ev.Tasks {
			if _, ok := m.tasks[name]; !ok {
				m.tasks[name] = &taskUIState{
					name:    name,
					status:  "pending",
					modules: make(map[string]string),
				}
				m.order = append(m.order, name)
			}
		}
	case runner.EventTaskStarted:
		t := m.ensureTask(ev.Task)
		t.status = "running"
		t.modules[ev.Module] = "running"
		m.appendLog(fmt.Sprintf("> %s (%s)", ev.Task, ev.Module))
	case runner.EventLine:
		line := ev.Text
		if ev.Module != "" {
			line = "[" + ev.Module + "] " + line
		}
		m.appendLog(line)
	case runner.EventTaskCompleted:
		t := m.ensureTask(ev.Task)
		t.duration += ev.DurationMs
		var moduleStatus string
		switch ev.Status {
		case runner.TaskStatusDone:
			t.status = "done"
			moduleStatus = "done"
		case runner.TaskStatusFailed:
			t.status = "error"
			moduleStatus = "error"
		case runner.TaskStatusSkipped:
			t.status = "skipped"
			moduleStatus = "skipped"
			t.note = ev.Message
		}
		// Promote any modules left in "running" to the task's final status so
		// the per-module display reflects completion. Modules that completed
		// successfully but the task as a whole failed remain "running" until
		// here; that's acceptable since fail-fast aborts the rest.
		for mod, st := range t.modules {
			if st == "running" {
				t.modules[mod] = moduleStatus
			}
		}
	case runner.EventNotice:
		m.appendLog("notice: " + ev.Message)
	}
	m.viewport.SetContent(strings.Join(m.logLines, "\n"))
	m.viewport.GotoBottom()
}

// appendLog records line in the bounded log buffer.
func (m *RunnerModel) appendLog(line string) {
	m.logLines = append(m.logLines, line)
	if over := len(m.logLines) - maxLogLines; over > 0 {
		// Drop the oldest 'over' lines. Keep the slice short to avoid the
		// underlying array growing without bound.
		m.logLines = append(m.logLines[:0], m.logLines[over:]...)
	}
}

func (m *RunnerModel) ensureTask(name string) *taskUIState {
	t, ok := m.tasks[name]
	if !ok {
		t = &taskUIState{name: name, status: "pending", modules: make(map[string]string)}
		m.tasks[name] = t
		m.order = append(m.order, name)
	}
	return t
}

// View renders the header, task checklist, and live log viewport.
func (m *RunnerModel) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
	separatorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))

	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Prenup " + m.version))
	sb.WriteString("\n")
	if m.notice != "" {
		sb.WriteString(warnStyle.Render(m.notice))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("Task Checklist:"))
	sb.WriteString("\n\n")

	for _, name := range m.order {
		t := m.tasks[name]
		icon, color := statusIcon(t.status)
		iconStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		fmt.Fprintf(&sb, "  %s  %s", iconStyle.Render(icon), t.name)
		if t.duration > 0 {
			fmt.Fprintf(&sb, "  (%dms)", t.duration)
		}
		if t.note != "" {
			sb.WriteString("  -- " + t.note)
		}
		sb.WriteByte('\n')
		// When a task ran across multiple modules, show their per-module
		// status under the task line so users can see fan-out progress.
		if len(t.modules) > 1 {
			modNames := make([]string, 0, len(t.modules))
			for name := range t.modules {
				if name == "" {
					continue
				}
				modNames = append(modNames, name)
			}
			sort.Strings(modNames)
			for _, mod := range modNames {
				modIcon, modColor := statusIcon(t.modules[mod])
				modStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(modColor))
				fmt.Fprintf(&sb, "      %s  %s\n", modStyle.Render(modIcon), mod)
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(separatorStyle.Render("----------------------------------------"))
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render("Output:"))
	sb.WriteString("\n")
	sb.WriteString(m.viewport.View())
	return sb.String()
}

func statusIcon(status string) (string, string) {
	switch status {
	case "running":
		return "*", "#FFFF00"
	case "done":
		return "v", "#00FF00"
	case "error":
		return "x", "#FF0000"
	case "skipped":
		return "-", "#888888"
	default:
		return "o", "#444444"
	}
}
