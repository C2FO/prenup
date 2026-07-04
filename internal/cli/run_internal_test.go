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

	"github.com/c2fo/prenup/internal/config"
	"github.com/c2fo/prenup/internal/git"
)

// RunDiscoverTestSuite groups tests for discoverChangesAndModules.
// They share a non-trivial fixture (a freshly-initialized git repo
// with a single seed commit), so a SetupTest pays for itself here.
//
// Other run_internal helpers (coalescePar, resolveSelection,
// loadRunConfig) have no shared fixture and remain flat tests below.
type RunDiscoverTestSuite struct {
	suite.Suite
	repo string // absolute path to the temp git repo
	cfg  config.Config
}

func (s *RunDiscoverTestSuite) SetupTest() {
	dir := s.T().TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		// Disable GPG signing so the seed commit succeeds in
		// environments where the dev's global git requires a key.
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed git binary, internal args.
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		s.Require().NoErrorf(err, "git %s: %s", strings.Join(args, " "), string(out))
	}
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o600))
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed git binary, internal args.
		cmd.Dir = dir
		s.Require().NoError(cmd.Run())
	}
	s.repo = dir
	s.cfg = config.DefaultConfig()
}

// TestNoChanges returns done with a friendly message when the worktree is clean.
func (s *RunDiscoverTestSuite) TestNoChanges() {
	out, err := discoverChangesAndModules(git.New(s.repo), s.repo, s.cfg, false)
	s.Require().NoError(err)
	s.True(out.done)
	s.Contains(out.message, "No relevant files changed")
}

// TestAllFlagSynthesizesDot pins that --all forces a "." module even
// against a clean tree, so an operator can re-run all tasks at will.
func (s *RunDiscoverTestSuite) TestAllFlagSynthesizesDot() {
	out, err := discoverChangesAndModules(git.New(s.repo), s.repo, s.cfg, true)
	s.Require().NoError(err)
	s.False(out.done)
	s.Equal([]string{"."}, out.modules)
}

// TestUntrackedFileCountsAsChange pins that an untracked file is
// treated as a real change (not just modifications), and that without
// module markers the runner falls back to a single "." module.
func (s *RunDiscoverTestSuite) TestUntrackedFileCountsAsChange() {
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, "added.go"), []byte("package x\n"), 0o600))

	out, err := discoverChangesAndModules(git.New(s.repo), s.repo, s.cfg, false)
	s.Require().NoError(err)
	s.False(out.done)
	s.Contains(out.changedFiles, "added.go")
	s.Equal([]string{"."}, out.modules)
}

func TestRunDiscoverSuite(t *testing.T) {
	suite.Run(t, new(RunDiscoverTestSuite))
}

// --- standalone tests below: pure helpers with no shared fixture, so
// a suite would be ceremony without payoff. They stay flat and use
// assert.New / require.New per the testify house style.

// TestCoalescePar verifies the precedence rule: an explicit --parallelism
// flag wins over the config value, and 0 falls through to the config.
func TestCoalescePar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		flag int
		cfg  int
		want int
	}{
		{"flag wins when set", 4, 8, 4},
		{"flag zero falls back to cfg", 0, 8, 8},
		{"both zero stays zero", 0, 0, 0},
		{"negative flag treated as unset", -1, 8, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, coalescePar(tc.flag, tc.cfg))
		})
	}
}

// TestResolveSelectionExplicitTaskFlags returns exactly the requested set
// regardless of mode.
func TestResolveSelectionExplicitTaskFlags(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	opts := runOptions{taskNames: []string{"lint", "test"}}
	cfg := config.DefaultConfig()
	out, err := resolveSelection(opts, config.OutputJSON, []string{"."}, cfg, versionInfo{})
	must.NoError(err)
	is.False(out.done)
	is.Equal(map[string]bool{"lint": true, "test": true}, out.selected)
}

// TestResolveSelectionNonHumanReturnsNil exercises the fast-path: in
// machine-readable modes there is no interactive picker, so the runner
// should fall back to default_selected (signaled by selected == nil).
func TestResolveSelectionNonHumanReturnsNil(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	opts := runOptions{}
	cfg := config.DefaultConfig()
	out, err := resolveSelection(opts, config.OutputJSON, []string{"."}, cfg, versionInfo{})
	must.NoError(err)
	is.False(out.done)
	is.Nil(out.selected)
}

// TestResolveSelectionNoInteractiveReturnsNil ensures --no-interactive on the
// human path also short-circuits to "use defaults".
func TestResolveSelectionNoInteractiveReturnsNil(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	opts := runOptions{noInteractive: true}
	cfg := config.DefaultConfig()
	out, err := resolveSelection(opts, config.OutputHuman, []string{"."}, cfg, versionInfo{})
	must.NoError(err)
	is.False(out.done)
	is.Nil(out.selected)
}

// TestLoadRunConfigMissing surfaces a useful error when no config exists.
func TestLoadRunConfigMissing(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	_, err := loadRunConfig(t.TempDir(), "")
	must.Error(err)
	is.Contains(err.Error(), "no .prenup.yaml found")
	is.Contains(err.Error(), "prenup init")
}

// TestLoadRunConfigExplicitPath honors an absolute path argument and ignores
// repo-root discovery.
func TestLoadRunConfigExplicitPath(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "custom.yaml")
	must.NoError(os.WriteFile(path, []byte("version: 1\ntasks:\n  - name: t\n    command: \"true\"\n"), 0o600))

	cfg, err := loadRunConfig(dir, path)
	must.NoError(err)
	is.Equal(1, cfg.Version)
	is.Len(cfg.Tasks, 1)
}
