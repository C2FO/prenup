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

// TestInstallWithConflictResolution_NonInteractiveFailsFast pins the
// documented CI/scripting contract: if a hook already exists, --non-interactive
// (or a non-TTY stdin) surfaces the conflict as an error rather than
// hanging on a prompt.
func (s *CommandsIntegrationTestSuite) TestInstallWithConflictResolution_NonInteractiveFailsFast() {
	hooksDir := filepath.Join(s.repo, ".git", "hooks")
	s.Require().NoError(os.MkdirAll(hooksDir, 0o750))
	s.Require().NoError(os.WriteFile(filepath.Join(hooksDir, "pre-commit"),
		[]byte("#!/bin/sh\necho existing\n"), 0o600))

	err := installWithConflictResolution(s.repo, "/tmp/prenup", installFlags{nonInteractive: true})
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

	err := installWithConflictResolution(s.repo, "/tmp/prenup", installFlags{force: true})
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

func TestCommandsIntegrationSuite(t *testing.T) {
	suite.Run(t, new(CommandsIntegrationTestSuite))
}
