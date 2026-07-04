package runner_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/runner"
	"github.com/c2fo/prenup/internal/runner/mocks"
)

// RunnerTestSuite groups end-to-end behavioral tests for the runner: its
// plan-building rules and its event-emission contract during execution.
// Tests are written against the public package (runner_test) so the
// suite exercises the same surface a downstream consumer would.
type RunnerTestSuite struct {
	suite.Suite
}

// recordingSink wraps a mockery-generated Sink mock with a permissive
// catch-all EXPECT() that captures every emitted event into a slice
// the test can assert against. The runner emits many events per run
// (started, line, completed for each task plus run-level bookends);
// strict per-event expectations would be brittle and would obscure
// the actual behavior under test. Tests that care about a specific
// event walk the captured slice instead.
type recordingSink struct {
	mock   *mocks.Sink
	mu     sync.Mutex
	events []runner.Event
}

func (s *RunnerTestSuite) newRecordingSink() *recordingSink {
	rs := &recordingSink{mock: mocks.NewSink(s.T())}
	rs.mock.EXPECT().
		Emit(mock.Anything).
		Run(func(ev runner.Event) {
			rs.mu.Lock()
			defer rs.mu.Unlock()
			rs.events = append(rs.events, ev)
		}).
		Return().
		Maybe()
	rs.mock.EXPECT().Close().Return(nil).Maybe()
	return rs
}

func (rs *recordingSink) snapshot() []runner.Event {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]runner.Event, len(rs.events))
	copy(out, rs.events)
	return out
}

// stubExecutor returns a mockery Executor pre-wired so any Run call
// returns runResult, optionally invoking the runner-supplied onLine
// callback with stdoutLine first. Tests that need different per-call
// behavior wire the EXPECT chain themselves.
func (s *RunnerTestSuite) stubExecutor(stdoutLine string, runResult error) (*mocks.Executor, *atomicCallLog) {
	exec := mocks.NewExecutor(s.T())
	calls := &atomicCallLog{}
	exec.EXPECT().
		Run(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, command, workingDir string, env map[string]string, onLine func(runner.Stream, string)) error {
			calls.add(command, workingDir, env)
			if stdoutLine != "" {
				onLine(runner.StreamStdout, stdoutLine)
			}
			return runResult
		}).
		Maybe()
	return exec, calls
}

// atomicCallLog records executor invocations from concurrent runner
// goroutines without forcing tests to manage their own mutex.
type atomicCallLog struct {
	mu    sync.Mutex
	calls []executorCall
}

type executorCall struct {
	command    string
	workingDir string
	env        map[string]string
}

func (l *atomicCallLog) add(command, workingDir string, env map[string]string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, executorCall{command: command, workingDir: workingDir, env: env})
}

func (l *atomicCallLog) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

func (s *RunnerTestSuite) TestSequentialSuccess() {
	cfg := config.Config{
		Version:       1,
		ModuleMarkers: []string{"go.mod"},
		Tasks: []config.Task{
			{Name: "A", Command: "cmd-a", DefaultSelected: true, PerModule: true},
			{Name: "B", Command: "cmd-b", DefaultSelected: true, PerModule: true},
		},
	}
	plan := runner.BuildPlan(cfg, "/repo", []string{"pkg/foo/x.go"}, []string{"pkg/foo"}, nil)
	s.Require().Len(plan.Tasks, 2)
	s.True(plan.Tasks[0].Selected)

	exec, _ := s.stubExecutor("", nil)
	sink := s.newRecordingSink()
	opts := runner.Options{
		Executor:       exec,
		Sink:           sink.mock,
		Version:        "v2.0.0",
		MaxParallelism: 1,
		CleanWorktree:  false,
	}
	result, err := runner.Run(context.Background(), plan, opts)
	s.Require().NoError(err)
	s.Equal(0, result.ExitCode)
	s.Equal(2, result.Succeeded)
	s.Equal(0, result.Failed)

	events := sink.snapshot()
	s.Require().NotEmpty(events)
	s.Equal(runner.EventRunStarted, events[0].Kind, "first event must be run_started")
	s.Equal(runner.EventRunCompleted, events[len(events)-1].Kind, "last event must be run_completed")
}

// TestFailFastWithinTaskAcrossModules pins two intertwined behaviors:
//  1. when the first module of a per-module task fails, the runner
//     does not try the remaining modules of that task; and
//  2. the synthetic failure-attribution stderr line carries a
//     [prenup] prefix in Text but keeps the unprefixed cause in
//     Message, so a programmatic consumer can distinguish prenup's
//     own attribution from a stderr line the user's command happened
//     to emit.
func (s *RunnerTestSuite) TestFailFastWithinTaskAcrossModules() {
	cfg := config.Config{
		Version:       1,
		ModuleMarkers: []string{"go.mod"},
		Tasks: []config.Task{
			{Name: "Lint", Command: "lint", DefaultSelected: true, PerModule: true},
		},
	}
	modules := []string{"a", "b", "c"}
	plan := runner.BuildPlan(cfg, "/repo", []string{"a/x.go", "b/y.go", "c/z.go"}, modules, nil)
	plan.Tasks[0].Modules = modules

	exec, calls := s.stubExecutor("", errors.New("boom"))
	sink := s.newRecordingSink()
	opts := runner.Options{Executor: exec, Sink: sink.mock, MaxParallelism: 1, CleanWorktree: false}

	result, err := runner.Run(context.Background(), plan, opts)
	s.Require().NoError(err)
	s.Equal(1, result.ExitCode)
	s.Equal(1, result.Failed)
	s.Equal(1, calls.len(), "fail-fast must stop after the first failing module")

	var foundFailureLine bool
	// runner.Event is large (~320B); iterate by index per gocritic's
	// rangeValCopy to avoid copying every event into the loop variable.
	events := sink.snapshot()
	for i := range events {
		ev := &events[i]
		if ev.Kind == runner.EventLine && ev.Stream == runner.StreamStderr && ev.Message == "boom" {
			s.Equal("[prenup] command failed: boom", ev.Text,
				"runner-synthesized failure line must be tagged with [prenup]")
			foundFailureLine = true
			break
		}
	}
	s.True(foundFailureLine, "expected a synthetic failure-attribution line on EventLine/stderr")
}

func (s *RunnerTestSuite) TestBuildPlanAppliesPathFilter() {
	cfg := config.Config{
		Version:       1,
		ModuleMarkers: []string{"go.mod"},
		Tasks: []config.Task{
			{
				Name: "Go tests", Command: "go test", DefaultSelected: true, PerModule: true,
				Paths: []string{"**/*.go"},
			},
			{
				Name: "Docs", Command: "mkdocs", DefaultSelected: true,
				Paths: []string{"docs/**/*.md"},
			},
		},
	}
	plan := runner.BuildPlan(cfg, "/repo", []string{"pkg/foo/x.go", "README.txt"}, []string{"pkg/foo"}, nil)
	s.True(plan.Tasks[0].Selected, "Go tests must be selected when a .go file changed")
	s.False(plan.Tasks[1].Selected, "Docs must be skipped when no .md file changed")
	s.Contains(plan.Tasks[1].SkipReason, "no files match task paths")
}

func (s *RunnerTestSuite) TestBuildPlanSelectionMap() {
	cfg := config.Config{
		Version:       1,
		ModuleMarkers: []string{"go.mod"},
		Tasks: []config.Task{
			{Name: "A", Command: "a", DefaultSelected: true, PerModule: true},
			{Name: "B", Command: "b", DefaultSelected: true, PerModule: true},
		},
	}
	plan := runner.BuildPlan(cfg, "/repo", []string{"m/x.go"}, []string{"m"},
		map[string]bool{"A": true})
	s.True(plan.Tasks[0].Selected, "A must respect explicit selection=true")
	s.False(plan.Tasks[1].Selected, "B must respect explicit absence (no implicit selection)")
}

// TestTemplateExpand is a pure-function check; no executor/sink needed.
// It uses table-driven cases to keep all template variables on display.
func (s *RunnerTestSuite) TestTemplateExpand() {
	cases := []struct {
		name   string
		repo   string
		module string
		input  string
		want   string
	}{
		{
			name:   "all four vars",
			repo:   "/repo",
			module: "services/auth",
			input:  "run {{.module_name}} in {{.module_root}} from {{.repo_root}} path {{.module_path}}",
			want:   "run auth in /repo/services/auth from /repo path services/auth",
		},
		{
			name:   "module_name is the leaf of module_path",
			repo:   "/repo",
			module: "a/b/c/leaf",
			input:  "{{.module_name}}",
			want:   "leaf",
		},
		{
			name:   "no template vars passes through",
			repo:   "/repo",
			module: "x",
			input:  "literal command",
			want:   "literal command",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			v := runner.NewTemplateVars(tc.repo, tc.module)
			s.Equal(tc.want, v.Expand(tc.input))
		})
	}
}

func TestRunnerSuite(t *testing.T) {
	suite.Run(t, new(RunnerTestSuite))
}
