package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildBinary compiles the prenup binary into a temporary location and
// returns the path. The compiled binary is reused across subtests.
func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "prenup")
	// Build with the host go toolchain; the binary path is constructed from
	// the test's TempDir and the go binary name is fixed.
	cmd := exec.Command("go", "build", "-o", out, ".") //nolint:gosec // G204: fixed binary, controlled args.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())
	return out
}

// initRepo creates a fresh git repo rooted at dir, committing the passed
// files on top of an empty initial commit.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	mustRunGit(t, dir, "init", "-b", "main")
	mustRunGit(t, dir, "config", "user.email", "t@example.com")
	mustRunGit(t, dir, "config", "user.name", "t")
	mustRunGit(t, dir, "config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o600))
	mustRunGit(t, dir, "add", "README.md")
	mustRunGit(t, dir, "commit", "-m", "init")
}

// mustRunGit runs `git <args>` in dir, failing the test on error. The git
// binary is fixed; only its arguments vary.
func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: git binary is fixed in tests.
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, string(out))
}

// runPrenup executes the built binary in dir and returns stdout + exit code.
func runPrenup(t *testing.T, binary, dir string, args ...string) (string, int) {
	t.Helper()
	// binary is the path returned by buildBinary in this test process.
	cmd := exec.Command(binary, args...) //nolint:gosec // G204: binary is built locally for the test.
	cmd.Dir = dir
	// Force non-interactive output for deterministic test behavior.
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		ee := &exec.ExitError{}
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("running prenup: %v\nstderr: %s", err, stderr.String())
		}
	}
	if testing.Verbose() {
		t.Logf("stdout:\n%s", stdout.String())
		t.Logf("stderr:\n%s", stderr.String())
	}
	return stdout.String(), exit
}

func TestE2EVersion(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	initRepo(t, dir)

	out, exit := runPrenup(t, bin, dir, "version")
	assert.Equal(t, 0, exit)
	assert.Contains(t, out, "prenup")
}

func TestE2ERunWithNoChanges(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := `version: 1
tasks:
  - name: "Echo"
    command: "echo hello"
    default_selected: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".prenup.yaml"), []byte(cfg), 0o600))
	// Commit the config so the worktree is clean; with no staged/unstaged/
	// untracked files we exercise the "no relevant files changed" path.
	mustRunGit(t, dir, "add", ".prenup.yaml")
	mustRunGit(t, dir, "commit", "-m", "add config")

	out, exit := runPrenup(t, bin, dir, "run")
	assert.Equal(t, 0, exit)
	assert.Contains(t, out, "No relevant files changed")
}

func TestE2ERunPassingTaskMarkdownOutput(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	initRepo(t, dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o600))

	cfg := `version: 1
clean_worktree: false
tasks:
  - name: "Echo"
    command: "echo prenup-ran"
    default_selected: true
    per_module: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".prenup.yaml"), []byte(cfg), 0o600))

	out, exit := runPrenup(t, bin, dir, "run", "--no-clean-worktree")
	assert.Equal(t, 0, exit, "stdout: %s", out)
	assert.Contains(t, out, "prenup-ran")
	assert.Contains(t, out, "1 succeeded, 0 failed")
	assert.Contains(t, out, "## Prenup")
}

func TestE2ERunFailingTaskBlocksCommit(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	initRepo(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o600))

	cfg := `version: 1
clean_worktree: false
tasks:
  - name: "Fail"
    command: "echo bad && exit 1"
    default_selected: true
    per_module: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".prenup.yaml"), []byte(cfg), 0o600))

	out, exit := runPrenup(t, bin, dir, "run", "--no-clean-worktree")
	assert.Equal(t, 1, exit, "expected non-zero exit on failure")
	assert.Contains(t, out, "0 succeeded, 1 failed")
}

func TestE2ERunJSONOutput(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	initRepo(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o600))

	cfg := `version: 1
clean_worktree: false
tasks:
  - name: "Echo"
    command: "echo hello"
    default_selected: true
    per_module: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".prenup.yaml"), []byte(cfg), 0o600))

	out, exit := runPrenup(t, bin, dir, "run", "--output", "json", "--no-clean-worktree")
	assert.Equal(t, 0, exit)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 2)

	// First line must be the agent_hint bootstrap so a cold-start consumer
	// can identify the stream before any runner events arrive.
	var hint map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &hint))
	assert.Equal(t, "agent_hint", hint["type"])
	assert.Equal(t, "prenup", hint["tool"])

	var first map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &first))
	assert.Equal(t, "run_started", first["type"])

	var last map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &last))
	assert.Equal(t, "run_completed", last["type"])
	// exit_code is omitted when zero (omitempty); its absence means success.
	if v, ok := last["exit_code"]; ok {
		f, isFloat := v.(float64)
		require.True(t, isFloat, "exit_code should be a JSON number")
		assert.InDelta(t, 0.0, f, 0.0001)
	}
}

func TestE2EPlanJSON(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	initRepo(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o600))

	cfg := `version: 1
tasks:
  - name: "Echo"
    command: "echo hi"
    default_selected: true
    per_module: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".prenup.yaml"), []byte(cfg), 0o600))

	out, exit := runPrenup(t, bin, dir, "plan", "--output", "json", "--all")
	assert.Equal(t, 0, exit)

	var plan map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &plan))
	assert.NotEmpty(t, plan["tasks"])
}

func TestE2EInstallUninstallRoundTrip(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	initRepo(t, dir)

	// Use --force in case git init created a template hook under
	// core.hooksPath or a default .git/hooks/pre-commit.
	_, exit := runPrenup(t, bin, dir, "install", "--force")
	require.Equal(t, 0, exit)
	data, err := os.ReadFile(filepath.Join(dir, ".git", "hooks", "pre-commit")) //nolint:gosec // G304: path under test TempDir.
	require.NoError(t, err)
	assert.Contains(t, string(data), "prenup-managed-hook")

	_, exit = runPrenup(t, bin, dir, "uninstall")
	require.Equal(t, 0, exit)
	_, err = os.Stat(filepath.Join(dir, ".git", "hooks", "pre-commit"))
	assert.True(t, os.IsNotExist(err))
}

func TestE2EConfigValidate(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()
	initRepo(t, dir)

	cfg := `version: 1
tasks:
  - name: "Echo"
    command: "echo"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".prenup.yaml"), []byte(cfg), 0o600))

	out, exit := runPrenup(t, bin, dir, "config", "validate")
	assert.Equal(t, 0, exit)
	assert.Contains(t, out, "OK:")
}
