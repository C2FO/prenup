package jsonout

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/prenup/internal/runner"
)

// JSONOutTestSuite groups behavioral tests for the NDJSON sink: the
// agent_hint bootstrap line, the per-event line shapes, and the run-level
// failure index. Each method gets a fresh buffer + sink via SetupTest.
type JSONOutTestSuite struct {
	suite.Suite
	buf  *bytes.Buffer
	sink *Sink
	now  time.Time
}

func (s *JSONOutTestSuite) SetupTest() {
	s.buf = &bytes.Buffer{}
	s.sink = New(s.buf)
	s.now = time.Now()
}

// decodeLines splits the stream on newlines and JSON-decodes each
// non-empty line into a generic map. Returns the decoded objects so
// callers can assert by index.
func (s *JSONOutTestSuite) decodeLines() []map[string]any {
	raw := strings.Split(strings.TrimRight(s.buf.String(), "\n"), "\n")
	out := make([]map[string]any, 0, len(raw))
	for _, line := range raw {
		if line == "" {
			continue
		}
		var m map[string]any
		s.Require().NoError(json.Unmarshal([]byte(line), &m), "line must be valid JSON: %q", line)
		out = append(out, m)
	}
	return out
}

func (s *JSONOutTestSuite) TestEmitWritesOneLinePerEvent() {
	s.sink.Emit(runner.Event{
		Kind: runner.EventRunStarted, Time: s.now, Version: "v2.0.0",
		Tasks: []string{"Run tests"}, RepoRoot: "/tmp/repo",
	})
	s.sink.Emit(runner.Event{
		Kind: runner.EventTaskStarted, Time: s.now, Task: "Run tests", Module: "pkg/foo",
		Command: "go test ./...", WorkingDir: "/tmp/pkg/foo",
	})
	s.sink.Emit(runner.Event{
		Kind: runner.EventLine, Time: s.now, Task: "Run tests", Module: "pkg/foo",
		Stream: runner.StreamStdout, Text: "ok",
	})
	s.sink.Emit(runner.Event{
		Kind: runner.EventTaskCompleted, Time: s.now, Task: "Run tests",
		Status: runner.TaskStatusFailed, DurationMs: 12,
	})
	s.sink.Emit(runner.Event{
		Kind: runner.EventRunCompleted, Time: s.now,
		Succeeded: 0, Failed: 1, ExitCode: 1,
		FailedTasks: []string{"Run tests"},
	})

	lines := s.decodeLines()
	// 1 agent_hint bootstrap + 5 runner events.
	s.Require().Len(lines, 6)

	hint := lines[0]
	s.Equal("agent_hint", hint["type"], "first line must be the self-describing bootstrap")
	s.Equal("prenup", hint["tool"])
	s.NotEmpty(hint["description"])
	s.NotEmpty(hint["homepage"])
	s.NotEmpty(hint["schema"])

	first := lines[1]
	s.Equal("run_started", first["type"])
	// run_started must carry repo_root so a JSON consumer can anchor
	// every subsequent task's working_dir without inferring it from
	// substring matches.
	s.Equal("/tmp/repo", first["repo_root"])

	// task_started must carry the resolved command and working_dir so a
	// JSON-only consumer can reproduce the invocation without parsing
	// .prenup.yaml or knowing prenup's template-expansion rules.
	started := lines[2]
	s.Equal("task_started", started["type"])
	s.Equal("go test ./...", started["command"])
	s.Equal("/tmp/pkg/foo", started["working_dir"])

	line := lines[3]
	s.Equal("line", line["type"])
	s.Equal("stdout", line["stream"])
	s.Equal("ok", line["text"])

	// run_completed must carry the failed_tasks list so a JSON consumer
	// has an O(1) failure index without rescanning task_completed events.
	completed := lines[5]
	s.Equal("run_completed", completed["type"])
	failedAny, ok := completed["failed_tasks"].([]any)
	s.Require().True(ok, "failed_tasks must be present and an array")
	s.Require().Len(failedAny, 1)
	s.Equal("Run tests", failedAny[0])
}

// TestAgentHintIsFirstLineEvenWithNoEvents asserts the bootstrap line
// is written eagerly at construction. A consumer that opens the stream
// and immediately blocks on read sees the orienting context before any
// runner activity.
func (s *JSONOutTestSuite) TestAgentHintIsFirstLineEvenWithNoEvents() {
	s.Require().NotEmpty(s.buf.Bytes(), "agent_hint must be emitted on construction")
	first := strings.SplitN(s.buf.String(), "\n", 2)[0]
	var obj map[string]any
	s.Require().NoError(json.Unmarshal([]byte(first), &obj))
	s.Equal("agent_hint", obj["type"])
}

func TestJSONOutSuite(t *testing.T) {
	suite.Run(t, new(JSONOutTestSuite))
}
