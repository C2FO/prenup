package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c2fo/prenup/internal/git"
)

// newStageRepo creates a fresh git repo with one committed README so we have
// a known "tracked before" baseline to compare against.
func newStageRepo(t *testing.T) (string, *git.Runner) {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-b", "main")
	mustGit(t, dir, "config", "user.email", "test@example.com")
	mustGit(t, dir, "config", "user.name", "test")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o600))
	mustGit(t, dir, "add", "README.md")
	mustGit(t, dir, "commit", "-m", "initial")
	return dir, git.New(dir)
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: git binary is fixed in tests.
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), string(out))
}

// TestStageGeneratedSkipsPreexistingFiles guarantees that stage_output never
// promotes a file that was already tracked before the task ran -- otherwise
// users' in-progress edits to existing generated files would be silently
// added to the index.
func TestStageGeneratedSkipsPreexistingFiles(t *testing.T) {
	t.Parallel()
	dir, r := newStageRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gen.txt"), []byte("v1\n"), 0o600))
	mustGit(t, dir, "add", "gen.txt")
	mustGit(t, dir, "commit", "-m", "add gen")

	before, err := r.TrackedFiles()
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "gen.txt"), []byte("v2 unstaged\n"), 0o600))

	require.NoError(t, stageGenerated(r, []string{"gen.txt"}, before, nil))

	staged, err := r.StagedFiles()
	require.NoError(t, err)
	assert.Empty(t, staged, "pre-existing file should not be auto-staged")
}

// TestStageGeneratedAddsNewMatchingFiles is the happy path: a brand-new file
// matching the patterns is staged.
func TestStageGeneratedAddsNewMatchingFiles(t *testing.T) {
	t.Parallel()
	dir, r := newStageRepo(t)
	before, err := r.TrackedFiles()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "out.txt"), []byte("new\n"), 0o600))

	require.NoError(t, stageGenerated(r, []string{"out.txt"}, before, nil))

	staged, err := r.StagedFiles()
	require.NoError(t, err)
	assert.Equal(t, []string{"out.txt"}, staged)
}

// TestStageGeneratedRespectsModuleScope ensures a per-module task that ran
// in module A cannot accidentally stage files generated in module B even
// when both happen to match the patterns.
func TestStageGeneratedRespectsModuleScope(t *testing.T) {
	t.Parallel()
	dir, r := newStageRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "b"), 0o750))
	before, err := r.TrackedFiles()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "out.txt"), []byte("a\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b", "out.txt"), []byte("b\n"), 0o600))

	require.NoError(t, stageGenerated(r, []string{"**/out.txt"}, before, []string{"a"}))

	staged, err := r.StagedFiles()
	require.NoError(t, err)
	assert.Equal(t, []string{"a/out.txt"}, staged)
}

// TestStageGeneratedDotModuleStagesEverywhere covers the "wildcard" module
// scope (e.g. non per-module task synthesized to ".").
func TestStageGeneratedDotModuleStagesEverywhere(t *testing.T) {
	t.Parallel()
	dir, r := newStageRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "x"), 0o750))
	before, err := r.TrackedFiles()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "top.txt"), []byte("t\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x", "deep.txt"), []byte("d\n"), 0o600))

	require.NoError(t, stageGenerated(r, []string{"**/*.txt"}, before, []string{"."}))

	staged, err := r.StagedFiles()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"top.txt", "x/deep.txt"}, staged)
}
