package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// GitTestSuite groups behavioral tests for the git wrapper. Every method
// runs against a freshly-initialized repo with one committed file
// (README.md), so tests are independent and can run in any order.
type GitTestSuite struct {
	suite.Suite
	repo string  // absolute path to the temp git repo
	r    *Runner // wrapper under test, scoped to repo
}

func (s *GitTestSuite) SetupTest() {
	dir := s.T().TempDir()
	s.runGit(dir, "init", "-b", "main")
	s.runGit(dir, "config", "user.email", "test@example.com")
	s.runGit(dir, "config", "user.name", "test")
	// Disable GPG signing so commit succeeds in environments where the
	// developer's global git config requires a signing key the test
	// process cannot access.
	s.runGit(dir, "config", "commit.gpgsign", "false")

	s.Require().NoError(os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o600))
	s.runGit(dir, "add", "README.md")
	s.runGit(dir, "commit", "-m", "initial")

	s.repo = dir
	s.r = New(dir)
}

// runGit runs `git args...` in dir and fails the test on non-zero exit.
// Using s.Require() means a setup failure aborts the test immediately
// rather than letting later assertions run against a half-built repo.
func (s *GitTestSuite) runGit(dir string, args ...string) {
	s.T().Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: git binary is fixed in tests.
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	s.Require().NoErrorf(err, "git %s: %s", strings.Join(args, " "), string(out))
}

// writeFile is a small helper used by tests that need to mutate the
// worktree before the assertion phase.
func (s *GitTestSuite) writeFile(name, content string) {
	s.T().Helper()
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, name), []byte(content), 0o600))
}

func (s *GitTestSuite) TestRepoRoot() {
	got, err := RepoRoot(s.repo)
	s.Require().NoError(err)

	// macOS temp dirs live under /var/folders which symlinks to
	// /private/var/folders; resolve both sides before comparing so a
	// symlink-aware mismatch doesn't break this test on darwin.
	wantEval, _ := filepath.EvalSymlinks(s.repo)
	gotEval, _ := filepath.EvalSymlinks(got)
	s.Equal(wantEval, gotEval)
}

func (s *GitTestSuite) TestChangedFileCategories() {
	s.writeFile("staged.txt", "s")
	s.runGit(s.repo, "add", "staged.txt")
	s.writeFile("README.md", "changed\n") // unstaged modification
	s.writeFile("new.txt", "n")           // untracked

	staged, err := s.r.StagedFiles()
	s.Require().NoError(err)
	s.Equal([]string{"staged.txt"}, staged)

	unstaged, err := s.r.UnstagedFiles()
	s.Require().NoError(err)
	s.Equal([]string{"README.md"}, unstaged)

	untracked, err := s.r.UntrackedFiles()
	s.Require().NoError(err)
	s.Equal([]string{"new.txt"}, untracked)

	has, err := s.r.HasUnstagedChanges()
	s.Require().NoError(err)
	s.True(has)
}

// TestStashPushPopRestoresDirtyWorktree pins the round-trip behavior
// prenup relies on: Push() must hide unstaged + untracked changes
// (leaving staged content for the hook to evaluate), and Pop() must
// put both categories back exactly as they were.
func (s *GitTestSuite) TestStashPushPopRestoresDirtyWorktree() {
	s.writeFile("staged.txt", "staged content\n")
	s.runGit(s.repo, "add", "staged.txt")
	s.writeFile("README.md", "unstaged change\n")
	s.writeFile("new.txt", "untracked\n")

	stash, err := s.r.Push()
	s.Require().NoError(err)

	unstaged, err := s.r.UnstagedFiles()
	s.Require().NoError(err)
	s.Empty(unstaged, "unstaged changes should be stashed away")

	untracked, err := s.r.UntrackedFiles()
	s.Require().NoError(err)
	s.Empty(untracked, "untracked files should be stashed away")

	staged, err := s.r.StagedFiles()
	s.Require().NoError(err)
	s.Contains(staged, "staged.txt")

	s.Require().NoError(stash.Pop())

	unstaged, err = s.r.UnstagedFiles()
	s.Require().NoError(err)
	s.Contains(unstaged, "README.md")

	untracked, err = s.r.UntrackedFiles()
	s.Require().NoError(err)
	s.Contains(untracked, "new.txt")
}

// TestStashPushNoopOnCleanWorktree pins that the wrapper handles the
// nothing-to-stash case gracefully -- prenup runs on every commit, so
// a panic or error here would block clean-worktree commits.
func (s *GitTestSuite) TestStashPushNoopOnCleanWorktree() {
	stash, err := s.r.Push()
	s.Require().NoError(err)
	s.Require().NotNil(stash)
	s.NoError(stash.Pop(), "Pop on a no-op stash must be safe")
}

func (s *GitTestSuite) TestAddFiles() {
	s.writeFile("a.txt", "a")
	s.Require().NoError(s.r.Add([]string{"a.txt"}))

	staged, err := s.r.StagedFiles()
	s.Require().NoError(err)
	s.Contains(staged, "a.txt")
}

// TestPorcelainStatusReturnsRenameDestination pins the parsing rule
// for `git status --porcelain=v1` rename entries, which look like
// `R  old -> new`. PorcelainStatus must return only `new` so callers
// like stageGenerated can pass it to `git add` without git treating
// the literal arrow as part of the filename.
func (s *GitTestSuite) TestPorcelainStatusReturnsRenameDestination() {
	// Seed an extra committed file so we have something to rename.
	s.writeFile("original.txt", "hello\n")
	s.runGit(s.repo, "add", "original.txt")
	s.runGit(s.repo, "commit", "-m", "add original")

	// Stage a rename. `git mv` records both the deletion of the old
	// path and the addition of the new one; with rename detection
	// (which `--porcelain=v1` uses by default for staged changes)
	// status reports a single R entry.
	s.runGit(s.repo, "mv", "original.txt", "renamed.txt")

	files, err := s.r.PorcelainStatus()
	s.Require().NoError(err)
	s.Contains(files, "renamed.txt", "rename destination must be returned")
	s.NotContains(files, "original.txt -> renamed.txt", "raw arrow form must not leak through")
	s.NotContains(files, "original.txt", "source path of a rename must not be reported as a separate entry")
}

func (s *GitTestSuite) TestTrackedFilesSnapshot() {
	tracked, err := s.r.TrackedFiles()
	s.Require().NoError(err)
	_, has := tracked["README.md"]
	s.True(has, "the seed README must show up in TrackedFiles")
}

func TestGitSuite(t *testing.T) {
	suite.Run(t, new(GitTestSuite))
}
