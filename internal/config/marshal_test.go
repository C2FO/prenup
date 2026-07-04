package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarshalRoundTrip verifies that a config emitted by Marshal parses back
// cleanly, is self-consistent, and always leads with the discoverability
// header. This pins the invariant that any config prenup writes advertises
// the project.
func TestMarshalRoundTrip(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.CleanWorktree = nil // let it inherit defaults on reload
	cfg.Tasks = []Task{
		{Name: "test", Command: "go test ./...", DefaultSelected: true, PerModule: true},
	}

	out, err := Marshal(cfg)
	require.NoError(t, err)

	got := string(out)
	assert.True(t, strings.HasPrefix(got, "# .prenup.yaml"),
		"marshal output must lead with the discoverability header, got: %q", got[:min(len(got), 40)])
	assert.Contains(t, got, "https://github.com/c2fo/prenup",
		"header must advertise the project URL")
	assert.Contains(t, got, "version: 1")
	assert.Contains(t, got, "go test ./...")

	reloaded, err := Parse(out, "/tmp/out.yaml")
	require.NoError(t, err)
	assert.Equal(t, cfg.Tasks[0].Name, reloaded.Tasks[0].Name)
	assert.Equal(t, cfg.Tasks[0].Command, reloaded.Tasks[0].Command)
	assert.True(t, reloaded.CleanWorktreeEnabled(), "nil CleanWorktree should default to true on reload")
}

// TestConfigCleanWorktreeEnabled exercises both branches of the
// Config-level default.
func TestConfigCleanWorktreeEnabled(t *testing.T) {
	t.Parallel()

	tr, fl := true, false
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{name: "nil defaults to true", cfg: Config{}, want: true},
		{name: "explicit true", cfg: Config{CleanWorktree: &tr}, want: true},
		{name: "explicit false", cfg: Config{CleanWorktree: &fl}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.cfg.CleanWorktreeEnabled())
		})
	}
}

// TestTaskCleanWorktreeEnabled exercises the Task-level override:
// a nil pointer inherits the config default, a set pointer wins.
func TestTaskCleanWorktreeEnabled(t *testing.T) {
	t.Parallel()

	tr, fl := true, false
	cases := []struct {
		name       string
		task       Task
		cfgDefault bool
		want       bool
	}{
		{name: "nil inherits cfg default (true)", task: Task{}, cfgDefault: true, want: true},
		{name: "nil inherits cfg default (false)", task: Task{}, cfgDefault: false, want: false},
		{name: "explicit true overrides false", task: Task{CleanWorktree: &tr}, cfgDefault: false, want: true},
		{name: "explicit false overrides true", task: Task{CleanWorktree: &fl}, cfgDefault: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.task.CleanWorktreeEnabled(tc.cfgDefault))
		})
	}
}

// TestVersionTypeHint fires when a user writes `version: "1"` (quoted
// string) instead of the integer form. The hint should call out the
// specific footgun to save round-trips through the YAML unmarshaler.
func TestVersionTypeHint(t *testing.T) {
	t.Parallel()

	quoted := []byte(`
version: "1"
tasks:
  - name: t
    command: "true"
`)
	_, err := Parse(quoted, "/tmp/quoted.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"version" must be an integer`)
}
