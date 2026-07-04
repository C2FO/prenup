package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TestScanForFile is a truth-table for the helper that answers "does any of
// these names exist at the repo root?".
func TestScanForFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte("all:\n"), 0o600))

	cases := []struct {
		name  string
		names []string
		want  bool
	}{
		{name: "exact match found", names: []string{"Makefile"}, want: true},
		{name: "no match", names: []string{"nope.yml"}, want: false},
		{name: "second candidate matches", names: []string{"missing", "Makefile"}, want: true},
		{name: "empty candidate list", names: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, scanForFile(dir, tc.names...))
		})
	}
}

// TestScanForGoMod covers presence/absence and the pruning rules (hidden
// dirs, vendor/, node_modules/). Rather than a table we just build a couple
// of directory shapes since the layout is the interesting part.
func TestScanForGoMod(t *testing.T) {
	t.Parallel()

	t.Run("finds nested go.mod", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		nested := filepath.Join(dir, "pkg", "sub")
		require.NoError(t, os.MkdirAll(nested, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module x\n"), 0o600))
		assert.True(t, scanForGoMod(dir))
	})

	t.Run("returns false when only go.mod under vendor/", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		vend := filepath.Join(dir, "vendor", "example.com", "y")
		require.NoError(t, os.MkdirAll(vend, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(vend, "go.mod"), []byte("module y\n"), 0o600))
		assert.False(t, scanForGoMod(dir), "vendor/ should be pruned")
	})

	t.Run("returns false in empty dir", func(t *testing.T) {
		t.Parallel()
		assert.False(t, scanForGoMod(t.TempDir()))
	})
}

// InitCommandTestSuite exercises runInit end-to-end. Each test needs a
// freshly-initialized git repo and a chdir into it (because `prenup init`
// calls git.RepoRoot("") which is CWD-relative). A suite with SetupTest
// covers both cheaply.
type InitCommandTestSuite struct {
	suite.Suite
	repo string
}

func (s *InitCommandTestSuite) SetupTest() {
	dir := s.T().TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed binary, internal args.
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		s.Require().NoErrorf(err, "git %s: %s", strings.Join(args, " "), string(out))
	}
	s.repo = dir
	s.T().Chdir(dir)
}

// TestRunInit_ScaffoldsHeader_and_Version verifies the two public contracts
// the fresh-init output must always satisfy: (1) the discoverability header
// linking to the project, and (2) the correct schema version.
func (s *InitCommandTestSuite) TestRunInit_ScaffoldsHeader_and_Version() {
	cmd := newInitCmd()
	s.Require().NoError(runInit(cmd))

	data, err := os.ReadFile(filepath.Join(s.repo, ".prenup.yaml"))
	s.Require().NoError(err)
	got := string(data)

	s.Contains(got, "https://github.com/c2fo/prenup",
		"scaffolded config must advertise the project URL")
	s.Contains(got, "version: 1",
		"scaffolded config must declare the current schema version")
	s.Contains(got, "tasks:")
}

// TestRunInit_TailorsToRepoContents demonstrates that init responds to what
// it finds in the repo: a go.mod triggers a "Run tests" task, and a
// .golangci.yml triggers a lint task.
func (s *InitCommandTestSuite) TestRunInit_TailorsToRepoContents() {
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, "go.mod"), []byte("module example.com/x\n"), 0o600))
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, ".golangci.yml"), []byte("run: {}\n"), 0o600))

	cmd := newInitCmd()
	s.Require().NoError(runInit(cmd))

	data, err := os.ReadFile(filepath.Join(s.repo, ".prenup.yaml"))
	s.Require().NoError(err)
	got := string(data)

	s.Contains(got, "Run tests")
	s.Contains(got, "go test ./...")
	s.Contains(got, "Run golangci-lint")
	s.Contains(got, "golangci-lint run")
}

// TestRunInit_ExampleTask_WhenRepoIsEmpty covers the fallback branch: when
// there's no go.mod / Makefile, init still produces a valid file with a
// placeholder task.
func (s *InitCommandTestSuite) TestRunInit_ExampleTask_WhenRepoIsEmpty() {
	cmd := newInitCmd()
	s.Require().NoError(runInit(cmd))

	data, err := os.ReadFile(filepath.Join(s.repo, ".prenup.yaml"))
	s.Require().NoError(err)
	s.Contains(string(data), "Example task")
}

// TestRunInit_RefusesToOverwrite_WithoutForce guards the safety rule: a
// second init against a repo that already has .prenup.yaml must fail unless
// --force is set.
func (s *InitCommandTestSuite) TestRunInit_RefusesToOverwrite_WithoutForce() {
	first := newInitCmd()
	s.Require().NoError(runInit(first))

	second := newInitCmd()
	err := runInit(second)
	s.Require().Error(err)
	s.Contains(err.Error(), "already exists")
	s.Contains(err.Error(), "--force")
}

// TestRunInit_ForceOverwrites verifies --force lets a re-run replace the
// prior scaffold.
func (s *InitCommandTestSuite) TestRunInit_ForceOverwrites() {
	first := newInitCmd()
	s.Require().NoError(runInit(first))

	second := newInitCmd()
	s.Require().NoError(second.Flags().Set("force", "true"))
	s.Require().NoError(runInit(second))
}

func TestInitCommandSuite(t *testing.T) {
	suite.Run(t, new(InitCommandTestSuite))
}
