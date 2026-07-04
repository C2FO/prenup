package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseValid(t *testing.T) {
	data := []byte(`
version: 2
module_markers:
  - go.mod
exclude:
  - "**/*.yaml"
clean_worktree: false
max_parallelism: 4
output: json
tasks:
  - name: "Tests"
    command: "go test ./..."
    default_selected: true
    per_module: true
    paths: ["**/*.go"]
  - name: "Docs"
    command: "make docs"
    stage_output: true
    output_patterns: ["docs/**/*.md"]
`)
	cfg, err := Parse(data, "/tmp/test.yaml")
	require.NoError(t, err)

	assert.Equal(t, 2, cfg.Version)
	assert.Equal(t, []string{"go.mod"}, cfg.ModuleMarkers)
	assert.Equal(t, 4, cfg.MaxParallelism)
	assert.Equal(t, OutputJSON, cfg.Output)
	require.NotNil(t, cfg.CleanWorktree)
	assert.False(t, *cfg.CleanWorktree)
	assert.False(t, cfg.CleanWorktreeEnabled())

	require.Len(t, cfg.Tasks, 2)
	assert.True(t, cfg.Tasks[0].DefaultSelected)
	assert.True(t, cfg.Tasks[0].PerModule)
	assert.True(t, cfg.Tasks[0].ParallelEnabled(), "per-module task should default parallel=true")
	assert.False(t, cfg.Tasks[1].ParallelEnabled(), "non-per-module task should default parallel=false")
}

func TestParseMissingVersionRejected(t *testing.T) {
	data := []byte(`
tasks:
  - name: foo
    command: "true"
`)
	_, err := Parse(data, "/tmp/test.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing "version: 2"`)
}

func TestValidateRejectsDuplicateTaskName(t *testing.T) {
	data := []byte(`
version: 2
tasks:
  - name: foo
    command: "true"
  - name: foo
    command: "true"
`)
	_, err := Parse(data, "/tmp/test.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate task name")
}

func TestValidateRequiresOutputPatternsForStageOutput(t *testing.T) {
	data := []byte(`
version: 2
tasks:
  - name: foo
    command: "true"
    stage_output: true
`)
	_, err := Parse(data, "/tmp/test.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stage_output: true requires output_patterns")
}

func TestValidateRejectsUnknownOutputMode(t *testing.T) {
	data := []byte(`
version: 2
output: flashy
tasks:
  - name: foo
    command: "true"
`)
	_, err := Parse(data, "/tmp/test.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output mode")
}

func TestFindPrefersYamlOverYml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".prenup.yaml"), []byte("version: 2\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".prenup.yml"), []byte("version: 2\n"), 0o600))

	p, err := Find(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".prenup.yaml"), p)
}

func TestLoadRejectsPathOutsideRepo(t *testing.T) {
	repo := t.TempDir()
	other := t.TempDir()
	cfgPath := filepath.Join(other, ".prenup.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("version: 2\ntasks: [{name: f, command: \"true\"}]\n"), 0o600))

	_, err := Load(cfgPath, repo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the repository")
}

// TestLoadRejectsSymlinkEscapingRepo guards against a symlink that
// lives inside the repo but points at a config file outside the repo.
// A purely lexical check on the user-supplied path would let such a
// file be read; Load must EvalSymlinks both sides before comparing.
func TestLoadRejectsSymlinkEscapingRepo(t *testing.T) {
	repo := t.TempDir()
	other := t.TempDir()

	target := filepath.Join(other, ".prenup.yaml")
	require.NoError(t, os.WriteFile(target, []byte("version: 2\ntasks: [{name: f, command: \"true\"}]\n"), 0o600))

	link := filepath.Join(repo, ".prenup.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported on this filesystem: %v", err)
	}

	_, err := Load(link, repo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the repository")
}

func TestDefaultConfigStashOn(t *testing.T) {
	cfg := DefaultConfig()
	assert.True(t, cfg.CleanWorktreeEnabled())
}
