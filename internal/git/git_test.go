package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/prenup/internal/lock"
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
	clearGitHookEnv(s.T())
	// Ignore the developer's global/system git config (core.hooksPath,
	// init.templateDir, ...) so every test stays inside its temp repository.
	s.T().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	s.T().Setenv("GIT_CONFIG_NOSYSTEM", "1")
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

// TestStashConcurrentAcrossWorktrees pins that prenup stashes taken from
// several linked worktrees at once (refs/stash is shared by all of them)
// never fail and always restore each worktree's own changes, never another
// worktree's.
func (s *GitTestSuite) TestStashConcurrentAcrossWorktrees() {
	const worktrees, rounds = 6, 25

	runners := []*Runner{s.r}
	dirs := []string{s.repo}
	for i := 1; i < worktrees; i++ {
		wt := filepath.Join(s.T().TempDir(), "wt")
		s.runGit(s.repo, "worktree", "add", "-b", "feat"+string(rune('a'+i)), wt)
		runners = append(runners, New(wt))
		dirs = append(dirs, wt)
	}

	errs := make(chan error, worktrees*rounds*2)
	var wg sync.WaitGroup
	for i := range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want := fmt.Sprintf("changes of worktree %d\n", i)
			path := filepath.Join(dirs[i], "README.md")
			for range rounds {
				if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
					errs <- err
					return
				}
				stash, err := runners[i].Push()
				if err != nil {
					errs <- fmt.Errorf("worktree %d push: %w", i, err)
					continue
				}
				if err := stash.Pop(); err != nil {
					errs <- fmt.Errorf("worktree %d pop: %w", i, err)
					continue
				}
				got, err := os.ReadFile(path) //nolint:gosec // G304: path is under a test temp dir.
				if err != nil {
					errs <- err
					return
				}
				if string(got) != want {
					errs <- fmt.Errorf("worktree %d restored %q, want %q", i, got, want)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	var failures []string
	for err := range errs {
		failures = append(failures, err.Error())
	}
	s.Empty(failures, "concurrent stash push/pop across worktrees must not fail or cross-restore")
}

// TestStashPushNoopOnCleanWorktree pins that the wrapper handles the
// nothing-to-stash case gracefully -- prenup runs on every commit, so
// a panic or error here would block clean-worktree commits.
func (s *GitTestSuite) TestStashLockTimeout() {
	s.writeFile("README.md", "dirty\n")
	_, commonDir, err := GitDirs(s.repo)
	s.Require().NoError(err)
	held, err := lock.AcquireDir(commonDir, stashLockName, 0)
	s.Require().NoError(err)

	r := &Runner{Dir: s.repo, stashLockWait: 100 * time.Millisecond}
	_, err = r.Push()
	s.Require().ErrorIs(err, lock.ErrContended)
	s.Contains(err.Error(), "waiting for another worktree")

	s.Require().NoError(held.Close())
	st, err := r.Push()
	s.Require().NoError(err)

	held, err = lock.AcquireDir(commonDir, stashLockName, 0)
	s.Require().NoError(err)
	s.Require().ErrorIs(st.Pop(), lock.ErrContended)
	s.Require().NoError(held.Close())
	s.Require().NoError(st.Pop())
}

func (s *GitTestSuite) TestStashGitDirsError() {
	r := &Runner{Dir: s.T().TempDir()}
	_, err := r.Push()
	s.Require().Error(err)
	s.Contains(err.Error(), "locating common git dir")
}

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

// TestHooksDir pins that the hooks directory is resolved the way git itself
// resolves it: linked worktrees share the main repository's hooks, and
// core.hooksPath (absolute or relative) wins when set.
func (s *GitTestSuite) TestHooksDir() {
	wt := filepath.Join(s.T().TempDir(), "wt")
	s.runGit(s.repo, "worktree", "add", "-b", "feat", wt)
	customHooks := filepath.Join(s.T().TempDir(), "custom-hooks")

	tests := []struct {
		name      string
		hooksPath string
		dir       string
		want      string
	}{
		{name: "main checkout uses .git/hooks", dir: s.repo, want: filepath.Join(s.repo, ".git", "hooks")},
		{name: "linked worktree shares main repo hooks", dir: wt, want: filepath.Join(s.repo, ".git", "hooks")},
		{name: "absolute core.hooksPath wins in worktree", hooksPath: customHooks, dir: wt, want: customHooks},
		{name: "relative core.hooksPath resolves against checkout root", hooksPath: "relhooks", dir: wt, want: filepath.Join(wt, "relhooks")},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.hooksPath == "" {
				cmd := exec.Command("git", "config", "--unset", "core.hooksPath")
				cmd.Dir = s.repo
				_ = cmd.Run() // exit 5 when unset is expected.
			} else {
				s.runGit(s.repo, "config", "core.hooksPath", tt.hooksPath)
			}

			got, err := HooksDir(tt.dir)
			s.Require().NoError(err)

			wantEval, evalErr := filepath.EvalSymlinks(filepath.Dir(tt.want))
			s.Require().NoError(evalErr)
			gotEval, evalErr := filepath.EvalSymlinks(filepath.Dir(got))
			s.Require().NoError(evalErr)
			s.Equal(filepath.Join(wantEval, filepath.Base(tt.want)), filepath.Join(gotEval, filepath.Base(got)))
		})
	}
}

// TestHooksDirIsExternal pins detection of a core.hooksPath that is shared
// outside the repository, so callers can warn before touching it.
func (s *GitTestSuite) TestHooksDirIsExternal() {
	wt := filepath.Join(s.T().TempDir(), "wt")
	s.runGit(s.repo, "worktree", "add", "-b", "feat", wt)

	tests := []struct {
		name      string
		hooksPath string
		dir       string
		want      bool
	}{
		{name: "default hooks dir is internal", dir: s.repo},
		{name: "linked worktree uses common git dir", dir: wt},
		{name: "relative hooksPath inside checkout is internal", hooksPath: "relhooks", dir: s.repo},
		{name: "absolute hooksPath outside repo is external", hooksPath: filepath.Join(s.T().TempDir(), "shared"), dir: s.repo, want: true},
		{name: "external hooksPath seen from linked worktree", hooksPath: filepath.Join(s.T().TempDir(), "shared2"), dir: wt, want: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			cmd := exec.Command("git", "config", "--unset", "core.hooksPath")
			cmd.Dir = s.repo
			_ = cmd.Run() // exit 5 when unset is expected.
			if tt.hooksPath != "" {
				s.runGit(s.repo, "config", "core.hooksPath", tt.hooksPath)
			}
			hooks, err := HooksDir(tt.dir)
			s.Require().NoError(err)
			got, err := HooksDirIsExternal(tt.dir, hooks)
			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
}

// TestGitDirs pins the per-checkout vs shared git directory split that
// containerized tasks need to mount for linked worktrees.
func (s *GitTestSuite) TestGitDirs() {
	wt := filepath.Join(s.T().TempDir(), "wt")
	s.runGit(s.repo, "worktree", "add", "-b", "feat", wt)
	eval := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		s.Require().NoError(err)
		return r
	}
	common := eval(filepath.Join(s.repo, ".git"))

	gitDir, commonDir, err := GitDirs(s.repo)
	s.Require().NoError(err)
	s.Equal(common, eval(gitDir))
	s.Equal(common, eval(commonDir))

	sub := filepath.Join(s.repo, "sub")
	s.Require().NoError(os.Mkdir(sub, 0o750))
	gitDir, commonDir, err = GitDirs(sub)
	s.Require().NoError(err)
	s.Equal(common, eval(gitDir))
	s.Equal(common, eval(commonDir))

	gitDir, commonDir, err = GitDirs(wt)
	s.Require().NoError(err)
	s.Equal(filepath.Join(common, "worktrees", "wt"), eval(gitDir))
	s.Equal(common, eval(commonDir))

	_, _, err = GitDirs(s.T().TempDir())
	s.Require().Error(err)
}

func (s *GitTestSuite) TestHooksDirIsExternal_NotARepo() {
	_, err := HooksDirIsExternal(s.T().TempDir(), "/x")
	s.Require().Error(err)
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
