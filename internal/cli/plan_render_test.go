package cli

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/runner"
)

// PlanRenderTestSuite groups tests that share a canonical Plan fixture. The
// two rendering functions produce very different output formats, so a suite
// with a common `plan` field keeps each case focused on the format contract
// rather than fixture setup.
type PlanRenderTestSuite struct {
	suite.Suite
	plan runner.Plan
}

func (s *PlanRenderTestSuite) SetupTest() {
	// A plan with one selected per-module task, one skipped task with a
	// reason, and one selected global task exercises every branch inside
	// both renderers.
	s.plan = runner.Plan{
		RepoRoot: "/repo",
		Modules:  []string{"pkg/a", "pkg/b"},
		Tasks: []runner.PlannedTask{
			{
				Task: config.Task{
					Name:      "Run tests",
					Command:   "go test ./...",
					PerModule: true,
				},
				Selected:      true,
				CleanWorktree: true,
				Parallel:      true,
				Modules:       []string{"pkg/a", "pkg/b"},
			},
			{
				Task: config.Task{
					Name:    "Lint (skipped)",
					Command: "golangci-lint run",
				},
				Selected:   false,
				SkipReason: "no matching files",
				Modules:    []string{""}, // renderPlanJSON normalizes this to nil
			},
			{
				Task: config.Task{
					Name:    "Format check",
					Command: "gofmt -l .",
				},
				Selected: true,
				Modules:  nil,
			},
		},
	}
}

// TestRenderPlanText_ContainsHeaderAndTaskLines pins the human-readable text
// contract: header lines for the repo + modules, then a checklist-style block
// per task.
func (s *PlanRenderTestSuite) TestRenderPlanText_ContainsHeaderAndTaskLines() {
	out := s.captureStdout(func(f *os.File) error {
		return renderPlanText(s.plan, f)
	})

	s.Contains(out, "Repository: /repo")
	s.Contains(out, "Modules:    pkg/a, pkg/b")
	s.Contains(out, "[v] Run tests")
	s.Contains(out, "per_module, modules: pkg/a, pkg/b")
	s.Contains(out, "command: go test ./...")
	s.Contains(out, "[-] Lint (skipped)")
	s.Contains(out, "skipped: no matching files")
	s.Contains(out, "[v] Format check")
}

// TestRenderPlanJSON_ProducesValidObject asserts the JSON contract: valid
// JSON, top-level repo_root + modules + tasks, and the SkipReason /
// per-module modules normalization is honored.
func (s *PlanRenderTestSuite) TestRenderPlanJSON_ProducesValidObject() {
	out := s.captureStdout(func(_ *os.File) error {
		return renderPlanJSON(s.plan)
	})

	var got struct {
		RepoRoot string   `json:"repo_root"`
		Modules  []string `json:"modules"`
		Tasks    []struct {
			Name          string   `json:"name"`
			Selected      bool     `json:"selected"`
			SkipReason    string   `json:"skip_reason,omitempty"`
			PerModule     bool     `json:"per_module"`
			CleanWorktree bool     `json:"clean_worktree"`
			Parallel      bool     `json:"parallel"`
			Modules       []string `json:"modules"`
		} `json:"tasks"`
	}
	s.Require().NoError(json.Unmarshal([]byte(out), &got))

	s.Equal("/repo", got.RepoRoot)
	s.Equal([]string{"pkg/a", "pkg/b"}, got.Modules)
	s.Require().Len(got.Tasks, 3)

	s.Equal("Run tests", got.Tasks[0].Name)
	s.True(got.Tasks[0].Selected)
	s.True(got.Tasks[0].PerModule)
	s.Equal([]string{"pkg/a", "pkg/b"}, got.Tasks[0].Modules)

	s.Equal("Lint (skipped)", got.Tasks[1].Name)
	s.False(got.Tasks[1].Selected)
	s.Equal("no matching files", got.Tasks[1].SkipReason)
	// A single "" entry is a runner sentinel for "no modules"; JSON output
	// normalizes it to a missing/empty array so consumers don't see [""].
	s.Empty(got.Tasks[1].Modules)

	s.Equal("Format check", got.Tasks[2].Name)
	s.True(got.Tasks[2].Selected)
	s.Empty(got.Tasks[2].Modules)
}

// captureStdout redirects os.Stdout across the call so both renderers
// (renderPlanText writes to its *os.File arg; renderPlanJSON writes to
// os.Stdout directly) can be tested with the same helper. The renderer is
// invoked with os.Stdout as its arg during the redirect, so passing that
// pointer to renderPlanText makes both paths converge.
func (s *PlanRenderTestSuite) captureStdout(fn func(*os.File) error) string {
	r, w, err := os.Pipe()
	s.Require().NoError(err)
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	fnErr := fn(w)
	s.Require().NoError(w.Close())

	buf, readErr := io.ReadAll(r)
	s.Require().NoError(readErr)
	s.Require().NoError(fnErr)
	return string(buf)
}

func TestPlanRenderSuite(t *testing.T) {
	suite.Run(t, new(PlanRenderTestSuite))
}

// TestRenderPlanText_EmptyPlan covers the degenerate case: no modules and no
// tasks still produces well-formed headers instead of panicking.
func TestRenderPlanText_EmptyPlan(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	err = renderPlanText(runner.Plan{RepoRoot: "/x"}, w)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	buf, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Contains(t, string(buf), "Repository: /x")
}
