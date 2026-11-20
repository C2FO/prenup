package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// CommandsIntegrationTestSuite drives runInstall, runPlan, and root Execute
// against a real temp git repo. Each of these needs the same fixture
// (git init + chdir), so a suite keeps setup DRY.
type CommandsIntegrationTestSuite struct {
	suite.Suite
	repo string
}

func (s *CommandsIntegrationTestSuite) SetupTest() {
	clearGitHookEnv(s.T())
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
	// Some dev environments have a global `init.templateDir` or
	// `core.hooksPath` that seeds .git/hooks/ with existing hook scripts
	// on `git init`. Clear the hooks directory so the install-conflict
	// tests can control whether a pre-commit exists.
	hooksDir := filepath.Join(dir, ".git", "hooks")
	entries, err := os.ReadDir(hooksDir)
	if err == nil {
		for _, e := range entries {
			_ = os.Remove(filepath.Join(hooksDir, e.Name()))
		}
	}
	s.repo = dir
	s.T().Chdir(dir)
}

// TestRunInstall_HappyPath verifies the fresh-repo install: no existing
// pre-commit hook, so no conflict resolution is needed and the hook file is
// created.
func (s *CommandsIntegrationTestSuite) TestRunInstall_HappyPath() {
	cmd := newInstallCmd()
	s.Require().NoError(runInstall(cmd))

	info, err := os.Stat(filepath.Join(s.repo, ".git", "hooks", "pre-commit"))
	s.Require().NoError(err)
	s.False(info.IsDir())
	s.NotZero(info.Mode()&0o100, "hook should be executable")
}

// TestInstallUninstall_LinkedWorktree pins that install/uninstall work from a
// linked worktree, where <worktree>/.git is a file, and operate on the hooks
// directory git actually uses (shared with the main repository).
func (s *CommandsIntegrationTestSuite) TestInstallUninstall_LinkedWorktree() {
	// Isolate from the developer's global/system git config (core.hooksPath).
	s.T().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	s.T().Setenv("GIT_CONFIG_NOSYSTEM", "1")

	for _, args := range [][]string{
		{"commit", "--allow-empty", "-m", "init"},
		{"worktree", "add", "-b", "feat", filepath.Join(s.T().TempDir(), "wt")},
	} {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed binary, internal args.
		cmd.Dir = s.repo
		out, err := cmd.CombinedOutput()
		s.Require().NoErrorf(err, "git %s: %s", strings.Join(args, " "), string(out))
	}
	wt := s.worktreePath()
	s.T().Chdir(wt)

	out := captureStdout(s.T(), func() error { return runInstall(newInstallCmd()) })
	hook := filepath.Join(s.repo, ".git", "hooks", "pre-commit")
	s.FileExists(hook)
	s.Contains(out, "hooks")

	uninstall := newUninstallCmd()
	_ = captureStdout(s.T(), func() error { return uninstall.RunE(uninstall, nil) })
	s.NoFileExists(hook)
}

// TestInstall_WarnsOnSharedHooksPath pins the stderr notice for a
// core.hooksPath that lives outside the repository, and its absence otherwise.
func (s *CommandsIntegrationTestSuite) TestInstall_WarnsOnSharedHooksPath() {
	s.T().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	s.T().Setenv("GIT_CONFIG_NOSYSTEM", "1")
	s.T().Chdir(s.repo)

	stderr := captureStderr(s.T(), func() error {
		return runInstall(newInstallCmd())
	})
	s.NotContains(stderr, "core.hooksPath")

	shared := filepath.Join(s.T().TempDir(), "shared-hooks")
	cmd := exec.Command("git", "config", "core.hooksPath", shared) //nolint:gosec // G204: fixed binary, test-controlled path.
	cmd.Dir = s.repo
	out, err := cmd.CombinedOutput()
	s.Require().NoErrorf(err, "git config: %s", string(out))

	stderr = captureStderr(s.T(), func() error {
		return runInstall(newInstallCmd())
	})
	s.Contains(stderr, "core.hooksPath points outside this repository")
	s.FileExists(filepath.Join(shared, "pre-commit"))
}

// worktreePath returns the path of the single linked worktree created by a test.
func (s *CommandsIntegrationTestSuite) worktreePath() string {
	s.T().Helper()
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = s.repo
	out, err := cmd.Output()
	s.Require().NoError(err)
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, p)
		}
	}
	s.Require().Len(paths, 2, "expected main checkout plus one linked worktree")
	return paths[1]
}

// TestInstallWithConflictResolution_NonInteractiveFailsFast pins the
// documented CI/scripting contract: if a hook already exists, --non-interactive
// (or a non-TTY stdin) surfaces the conflict as an error rather than
// hanging on a prompt.
func (s *CommandsIntegrationTestSuite) TestInstallWithConflictResolution_NonInteractiveFailsFast() {
	hooksDir := filepath.Join(s.repo, ".git", "hooks")
	s.Require().NoError(os.MkdirAll(hooksDir, 0o750))
	s.Require().NoError(os.WriteFile(filepath.Join(hooksDir, "pre-commit"),
		[]byte("#!/bin/sh\necho existing\n"), 0o600))

	err := installWithConflictResolution(hooksDir, "/tmp/prenup", installFlags{nonInteractive: true})
	s.Require().Error(err)
	s.Contains(err.Error(), "already exists")
}

// TestInstallWithConflictResolution_ForceReplaces confirms --force bypasses
// the conflict prompt path entirely: hook.Install returns nil (no
// ExistsError), so the conflict-resolution path is a straight passthrough.
func (s *CommandsIntegrationTestSuite) TestInstallWithConflictResolution_ForceReplaces() {
	hooksDir := filepath.Join(s.repo, ".git", "hooks")
	s.Require().NoError(os.MkdirAll(hooksDir, 0o750))
	s.Require().NoError(os.WriteFile(filepath.Join(hooksDir, "pre-commit"),
		[]byte("#!/bin/sh\necho existing\n"), 0o600))

	err := installWithConflictResolution(hooksDir, "/tmp/prenup", installFlags{force: true})
	s.Require().NoError(err)

	data, err := os.ReadFile(filepath.Join(hooksDir, "pre-commit")) //nolint:gosec // G304: test-controlled path inside t.TempDir().
	s.Require().NoError(err)
	s.Contains(string(data), "prenup", "force should replace the existing hook body")
}

// TestRunPlan_NoConfigProducesHelpfulError makes sure `prenup plan` in a
// repo without a .prenup.yaml points the user at what to do.
func (s *CommandsIntegrationTestSuite) TestRunPlan_NoConfigProducesHelpfulError() {
	cmd := newPlanCmd()
	err := runPlan(cmd)
	s.Require().Error(err)
	s.Contains(err.Error(), "no .prenup.yaml found")
}

// TestRunPlan_WithConfigRenders exercises the successful text-render path
// against a minimal in-repo config.
func (s *CommandsIntegrationTestSuite) TestRunPlan_WithConfigRenders() {
	cfg := `version: 1
tasks:
  - name: "Echo"
    command: "echo hi"
    default_selected: true
`
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, ".prenup.yaml"), []byte(cfg), 0o600))

	// Stage a file so change discovery finds something and BuildPlan has
	// modules to fan out over (or a "." fallback).
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, "hello.txt"), []byte("hi"), 0o600))
	add := exec.Command("git", "add", "hello.txt")
	add.Dir = s.repo
	s.Require().NoError(add.Run())

	out := captureStdout(s.T(), func() error {
		return runPlan(newPlanCmd())
	})
	s.Contains(out, "Repository:")
	s.Contains(out, "Echo")
}

// TestRunPlan_JSONMode covers the --output json branch.
func (s *CommandsIntegrationTestSuite) TestRunPlan_JSONMode() {
	cfg := `version: 1
tasks:
  - name: "Echo"
    command: "echo hi"
    default_selected: true
`
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, ".prenup.yaml"), []byte(cfg), 0o600))
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, "hello.txt"), []byte("hi"), 0o600))
	add := exec.Command("git", "add", "hello.txt")
	add.Dir = s.repo
	s.Require().NoError(add.Run())

	cmd := newPlanCmd()
	s.Require().NoError(cmd.Flags().Set("output", "json"))

	out := captureStdout(s.T(), func() error { return runPlan(cmd) })
	s.Contains(out, `"repo_root"`)
	s.Contains(out, `"Echo"`)
}

// TestExecute_HelpReturnsZero uses the Execute entry point with --help to
// exercise the top-level plumbing without triggering any subcommand's
// heavy work. It's an integration smoke test: os.Args → Execute → 0.
func (s *CommandsIntegrationTestSuite) TestExecute_HelpReturnsZero() {
	origArgs := os.Args
	s.T().Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"prenup", "--help"}

	// Execute writes usage to stdout; capture it so the test output stays
	// clean.
	_ = captureStdout(s.T(), func() error {
		s.Equal(0, Execute())
		return nil
	})
}

// TestExecute_MissingConfigPrintsToStderr verifies that Execute surfaces
// subcommand errors on stderr exactly once without dumping usage.
func (s *CommandsIntegrationTestSuite) TestExecute_MissingConfigPrintsToStderr() {
	origArgs := os.Args
	s.T().Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"prenup", "plan"}

	var code int
	stderr := captureStderr(s.T(), func() error {
		code = Execute()
		return nil
	})
	s.Equal(1, code)
	s.Contains(stderr, "no .prenup.yaml found")
	s.Equal(1, strings.Count(stderr, "Error:"))
	s.NotContains(stderr, "Usage:")
}

func TestCommandsIntegrationSuite(t *testing.T) {
	suite.Run(t, new(CommandsIntegrationTestSuite))
}

// clearGitHookEnv unsets the repository-local variables git exports to hooks
// so git commands in the test act only on the repositories the test creates,
// even when the suite itself runs from a pre-commit hook.
func clearGitHookEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE", "GIT_PREFIX", "GIT_COMMON_DIR"} {
		t.Setenv(k, "") // registers restoration of the original value.
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unsetting %s: %v", k, err)
		}
	}
}
