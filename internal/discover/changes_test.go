package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/c2fo/prenup/internal/git"
)

// TestValidatePatterns is a truth table for the pattern validator. Errors
// only bubble up for malformed doublestar syntax; unmatched-but-valid
// patterns are fine.
func TestValidatePatterns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		patterns []string
		wantErr  string
	}{
		{name: "nil", patterns: nil},
		{name: "empty", patterns: []string{}},
		{name: "valid patterns", patterns: []string{"**/*.go", "vendor/**", "!*.md"}},
		{name: "malformed bracket", patterns: []string{"[unclosed"}, wantErr: "invalid pattern"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePatterns(tc.patterns)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// ChangedFilesTestSuite exercises ChangedFiles against a real git repo.
// The three cases share a non-trivial setup (git init + seed commit), so a
// suite pays for itself.
type ChangedFilesTestSuite struct {
	suite.Suite
	repo   string
	runner *git.Runner
}

func (s *ChangedFilesTestSuite) SetupTest() {
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
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o600))
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed binary, internal args.
		cmd.Dir = dir
		s.Require().NoError(cmd.Run())
	}
	s.repo = dir
	s.runner = git.New(dir)
}

// TestReturnsUntrackedAndStaged: an untracked file plus a staged file both
// appear, deduplicated.
func (s *ChangedFilesTestSuite) TestReturnsUntrackedAndStaged() {
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, "a.go"), []byte("package a\n"), 0o600))
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, "b.go"), []byte("package b\n"), 0o600))
	cmd := exec.Command("git", "add", "a.go")
	cmd.Dir = s.repo
	s.Require().NoError(cmd.Run())

	got, err := ChangedFiles(s.runner, nil)
	s.Require().NoError(err)
	s.ElementsMatch([]string{"a.go", "b.go"}, got)
}

// TestExcludePatternDropsFiles: exclude patterns filter out matching paths.
func (s *ChangedFilesTestSuite) TestExcludePatternDropsFiles() {
	s.Require().NoError(os.MkdirAll(filepath.Join(s.repo, "vendor"), 0o750))
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, "vendor", "dep.go"), []byte("package v\n"), 0o600))
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, "keep.go"), []byte("package k\n"), 0o600))

	got, err := ChangedFiles(s.runner, []string{"vendor/**"})
	s.Require().NoError(err)
	s.NotContains(got, "vendor/dep.go", "vendor/** should be excluded")
	s.Contains(got, "keep.go")
}

// TestEmptyRepoReturnsEmpty: a clean worktree returns an empty slice, no error.
func (s *ChangedFilesTestSuite) TestEmptyRepoReturnsEmpty() {
	got, err := ChangedFiles(s.runner, nil)
	s.Require().NoError(err)
	s.Empty(got)
}

func TestChangedFilesSuite(t *testing.T) {
	suite.Run(t, new(ChangedFilesTestSuite))
}
