// Package human renders the interactive Bubble Tea UIs: task selection and
// runner output. It is selected automatically when stdout is a TTY.
package human

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/c2fo/prenup/internal/config"
)

// ErrCanceled signals the user quit the selection UI; the caller should block
// the commit with a non-zero exit.
var ErrCanceled = errors.New("canceled by user")

// SelectionInput configures the selection UI.
type SelectionInput struct {
	Version     string
	Notice      string // update warning, shown in yellow
	Modules     []string
	Tasks       []config.Task
	DefaultOnly bool // when true, skip the UI and return default_selected tasks
}

// SelectionResult indicates the user's choice.
type SelectionResult struct {
	Selected map[string]bool // task name -> selected
	Skipped  bool            // user hit 's': run nothing, commit proceeds
}

// SelectTasks runs the Bubble Tea selection UI. Returns ErrCanceled when the
// user quits with q/ctrl+c (the caller should block the commit).
func SelectTasks(in SelectionInput) (SelectionResult, error) {
	if in.DefaultOnly {
		sel := map[string]bool{}
		for i := range in.Tasks {
			if in.Tasks[i].DefaultSelected {
				sel[in.Tasks[i].Name] = true
			}
		}
		return SelectionResult{Selected: sel}, nil
	}

	model := newSelectionModel(in)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := prog.Run()
	if err != nil {
		return SelectionResult{}, fmt.Errorf("selection UI: %w", err)
	}
	m := finalModel.(*selectionModel)
	if m.canceled {
		return SelectionResult{}, ErrCanceled
	}
	if m.skipped {
		return SelectionResult{Skipped: true}, nil
	}

	sel := make(map[string]bool, len(m.tasks))
	for _, t := range m.tasks {
		if t.enabled {
			sel[t.name] = true
		}
	}
	return SelectionResult{Selected: sel}, nil
}

type selectableTask struct {
	name    string
	enabled bool
}

func (s selectableTask) Title() string {
	if s.enabled {
		return "[x] " + s.name
	}
	return "[ ] " + s.name
}
func (selectableTask) Description() string   { return "" }
func (s selectableTask) FilterValue() string { return s.name }

type selectionModel struct {
	version  string
	notice   string
	modules  []string
	tasks    []selectableTask
	list     list.Model
	canceled bool
	skipped  bool
}

func newSelectionModel(in SelectionInput) *selectionModel {
	items := make([]list.Item, len(in.Tasks))
	selected := make([]selectableTask, len(in.Tasks))
	for i := range in.Tasks {
		t := &in.Tasks[i]
		selected[i] = selectableTask{name: t.Name, enabled: t.DefaultSelected}
		items[i] = selected[i]
	}
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)

	listHeight := len(in.Tasks) + 4
	l := list.New(items, delegate, 50, listHeight)
	l.Title = "Select Prenup Tasks to Run"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()

	return &selectionModel{
		version: in.Version,
		notice:  in.Notice,
		modules: in.Modules,
		tasks:   selected,
		list:    l,
	}
}

func (m *selectionModel) Init() tea.Cmd { return nil }

func (m *selectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		chrome := 7 + len(m.modules)
		if m.notice != "" {
			chrome++
		}
		h := msg.Height - chrome
		if h < len(m.tasks)+4 {
			h = len(m.tasks) + 4
		}
		m.list.SetSize(msg.Width, h)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.canceled = true
			return m, tea.Quit
		case "s":
			m.skipped = true
			return m, tea.Quit
		case "enter":
			return m, tea.Quit
		case " ":
			idx := m.list.Index()
			if idx >= 0 && idx < len(m.tasks) {
				m.tasks[idx].enabled = !m.tasks[idx].enabled
				items := m.list.Items()
				items[idx] = m.tasks[idx]
				m.list.SetItems(items)
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *selectionModel) View() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00")).
		Render("Prenup Interactive Pre-Commit " + m.version)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))

	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n")
	if m.notice != "" {
		sb.WriteString(warn.Render(m.notice))
		sb.WriteString("\n")
	}
	sb.WriteString("\nChanged modules:\n")
	for _, mod := range m.modules {
		sb.WriteString("  - ")
		sb.WriteString(mod)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(m.list.View())
	sb.WriteString("\nup/down move, SPACE toggle, ENTER confirm, s skip (allow commit), q cancel (block commit)\n")
	return sb.String()
}
