package markdown

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/prenup/internal/runner"
)

// MarkdownSinkTestSuite groups behavioral tests for the markdown sink: the
// agent-orienting preamble, the live event stream, and the structured digest
// emitted on EventRunCompleted. SetupTest gives every method a fresh
// buffer + sink so they can run in any order.
type MarkdownSinkTestSuite struct {
	suite.Suite
	buf  *bytes.Buffer
	sink *Sink
	now  time.Time
}

func (s *MarkdownSinkTestSuite) SetupTest() {
	s.buf = &bytes.Buffer{}
	s.sink = New(s.buf)
	s.now = time.Now()
}

// emitSample drives a representative one-task run through the sink.
// failing controls whether the task ends in TaskStatusFailed (with a
// FAIL stderr line and a populated FailedTasks list on run_completed).
func (s *MarkdownSinkTestSuite) emitSample(failing bool) {
	s.sink.Emit(runner.Event{
		Kind: runner.EventRunStarted, Time: s.now, Version: "v2.0.0",
		Modules: []string{"pkg/foo"}, Tasks: []string{"Run tests"},
		RepoRoot: "/tmp/repo",
	})
	s.sink.Emit(runner.Event{
		Kind: runner.EventTaskStarted, Time: s.now, Task: "Run tests", Module: "pkg/foo",
		Command: "go test ./...", WorkingDir: "/tmp/pkg/foo",
	})
	s.sink.Emit(runner.Event{
		Kind: runner.EventLine, Time: s.now, Task: "Run tests", Module: "pkg/foo",
		Stream: runner.StreamStdout, Text: "ok  pkg/foo",
	})

	status := runner.TaskStatusDone
	exit := 0
	failed := 0
	var failedTasks []string
	if failing {
		s.sink.Emit(runner.Event{
			Kind: runner.EventLine, Time: s.now, Task: "Run tests", Module: "pkg/foo",
			Stream: runner.StreamStderr, Text: "FAIL",
		})
		status = runner.TaskStatusFailed
		exit = 1
		failed = 1
		failedTasks = []string{"Run tests"}
	}
	s.sink.Emit(runner.Event{
		Kind: runner.EventTaskCompleted, Time: s.now, Task: "Run tests",
		Status: status, DurationMs: 42,
	})
	s.sink.Emit(runner.Event{
		Kind: runner.EventRunCompleted, Time: s.now,
		Succeeded: 1 - failed, Failed: failed, ExitCode: exit,
		FailedTasks: failedTasks,
	})
}

// emitVersion renders a tiny stream with a custom version token so the
// version-label cases can exercise the labeling helper without rebuilding
// the whole fixture.
func (s *MarkdownSinkTestSuite) emitVersion(version string) {
	s.sink.Emit(runner.Event{
		Kind: runner.EventRunStarted, Time: s.now, Version: version,
		Modules: []string{"."}, Tasks: []string{"Echo"},
	})
	s.sink.Emit(runner.Event{Kind: runner.EventTaskStarted, Time: s.now, Task: "Echo"})
	s.sink.Emit(runner.Event{
		Kind: runner.EventTaskCompleted, Time: s.now, Task: "Echo",
		Status: runner.TaskStatusDone,
	})
	s.sink.Emit(runner.Event{Kind: runner.EventRunCompleted, Time: s.now, Succeeded: 1})
}

func (s *MarkdownSinkTestSuite) TestSuccessDigestIsQuiet() {
	s.emitSample(false)
	out := s.buf.String()
	s.Contains(out, "Prenup version: v2.0.0")
	s.Contains(out, "## Prenup v2.0.0")
	s.Contains(out, "1 succeeded, 0 failed")
	s.Contains(out, "### Task: Run tests")
	s.Contains(out, "Status: `done`")
	s.NotContains(out, "Next steps")
	// Successful runs deliberately omit the "Failed tasks:" line; its
	// absence is what tells a scanning agent that nothing went wrong.
	s.NotContains(out, "Failed tasks:")
}

func (s *MarkdownSinkTestSuite) TestPreambleIdentifiesTool() {
	s.emitSample(false)
	out := s.buf.String()
	// Cold-start agent must be able to identify the tool, find docs,
	// and discover the JSON output mode from the very first lines.
	s.Contains(out, "[prenup]", "preamble must be tagged so it stands out from streamed task output")
	s.Contains(out, "Git pre-commit hook", "preamble must explain what prenup is")
	s.Contains(out, "Docs:", "preamble must point at human-facing docs")
	s.Contains(out, "--output json", "preamble must advertise the structured output mode")
}

// TestVersionLabel runs both the dev-build and release-version paths
// through the same fixture; the labeling rule is shared between the
// preamble and the digest header, so both must agree.
func (s *MarkdownSinkTestSuite) TestVersionLabel() {
	cases := []struct {
		name          string
		version       string
		wantPreamble  string
		wantHeader    string
		wantQualifier bool // true if the "(development build...)" parenthetical must appear
	}{
		{
			name:          "dev build is qualified",
			version:       "dev",
			wantPreamble:  "Prenup version: dev (development build",
			wantHeader:    "## Prenup dev (development build",
			wantQualifier: true,
		},
		{
			name:          "release tag renders cleanly",
			version:       "v2.3.1",
			wantPreamble:  "Prenup version: v2.3.1\n",
			wantHeader:    "## Prenup v2.3.1\n",
			wantQualifier: false,
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			// SetupTest does not run for child suite.Run cases, so reset by
			// hand to keep the cases independent.
			s.buf = &bytes.Buffer{}
			s.sink = New(s.buf)
			s.emitVersion(tc.version)
			out := s.buf.String()
			s.Contains(out, tc.wantPreamble)
			s.Contains(out, tc.wantHeader)
			if tc.wantQualifier {
				s.Contains(out, "development build")
			} else {
				s.NotContains(out, "development build")
			}
		})
	}
}

func (s *MarkdownSinkTestSuite) TestFailureDigestIncludesTailAndNextSteps() {
	s.emitSample(true)
	out := s.buf.String()
	s.Contains(out, "Status: `failed`")
	s.Contains(out, "FAIL")
	s.Contains(out, "Next steps")
	s.Contains(out, "<details><summary>Output tail</summary>")
	// Failure footer must clarify hook-vs-git attribution and bypass.
	s.Contains(out, "not by `git`", "footer must disambiguate prenup vs git")
	s.Contains(out, "--no-verify", "footer must document the bypass")
	s.Contains(out, ".prenup.yaml", "footer must point at the config")
}

// TestEchoesResolvedCommand keeps the agent-reproducibility guarantee
// honest: an agent reading the failure tail must see the literal shell
// command (and resolved cwd) prenup ran, not just the task name.
func (s *MarkdownSinkTestSuite) TestEchoesResolvedCommand() {
	s.emitSample(true)
	out := s.buf.String()
	s.Contains(out, "[prenup] $ go test ./...", "command must be echoed in the live stream")
	s.Contains(out, "(cwd: /tmp/pkg/foo)", "resolved working directory must be shown")

	tailIdx := strings.Index(out, "<details><summary>Output tail</summary>")
	s.Require().GreaterOrEqual(tailIdx, 0, "output tail block must exist")
	tail := out[tailIdx:]
	s.Contains(tail, "go test ./...", "command must be in the failure tail for reproducibility")
}

// TestFailedTasksSummary keeps the at-a-glance failure index honest:
// the summary line must be followed by a "Failed tasks: ..." line
// listing the failing task names so an agent reading a long digest can
// jump directly to them.
func (s *MarkdownSinkTestSuite) TestFailedTasksSummary() {
	s.emitSample(true)
	out := s.buf.String()
	s.Require().Contains(out, "**Failed tasks:** Run tests")

	summaryIdx := strings.Index(out, "**Summary:**")
	failedIdx := strings.Index(out, "**Failed tasks:**")
	s.Require().GreaterOrEqual(summaryIdx, 0)
	s.Require().Greater(failedIdx, summaryIdx)
	between := out[summaryIdx:failedIdx]
	s.NotContains(between, "### Task:", "Failed tasks: must appear before any per-task section")
}

func TestMarkdownSinkSuite(t *testing.T) {
	suite.Run(t, new(MarkdownSinkTestSuite))
}
