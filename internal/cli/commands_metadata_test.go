package cli

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandConstructorsSetMetadata asserts that every cobra command
// constructor returns a command with a non-empty Use, Short, and any flags
// the CLI advertises. Metadata regressions (accidentally dropping a flag,
// renaming Use) are caught here without shelling out to the built binary.
func TestCommandConstructorsSetMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		build     func() *cobra.Command
		wantUse   string
		wantFlags []string
	}{
		{
			name:    "root",
			build:   newRootCmd,
			wantUse: "prenup",
			wantFlags: []string{
				"config", "output", "task", "all",
				"no-interactive", "no-clean-worktree", "parallelism", "dry-run",
			},
		},
		{
			name:    "run",
			build:   newRunCmd,
			wantUse: "run",
			wantFlags: []string{
				"config", "output", "task", "all",
				"no-interactive", "no-clean-worktree", "parallelism", "dry-run",
			},
		},
		{name: "plan", build: newPlanCmd, wantUse: "plan", wantFlags: []string{"config", "output", "all"}},
		{
			name:    "install",
			build:   newInstallCmd,
			wantUse: "install",
			wantFlags: []string{
				"force", "replace", "chain", "binary", "use-path", "non-interactive",
			},
		},
		{name: "uninstall", build: newUninstallCmd, wantUse: "uninstall"},
		{name: "init", build: newInitCmd, wantUse: "init", wantFlags: []string{"force"}},
		{name: "version", build: newVersionCmd, wantUse: "version"},
		{name: "config", build: newConfigCmd, wantUse: "config"},
		{name: "config validate", build: newConfigValidateCmd, wantUse: "validate [path]"},
		{name: "config schema", build: newConfigSchemaCmd, wantUse: "schema"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)
			must := require.New(t)

			cmd := tc.build()
			must.NotNil(cmd, "constructor returned nil")
			is.Equal(tc.wantUse, cmd.Use)
			is.NotEmpty(cmd.Short, "Short description should not be empty")

			for _, flag := range tc.wantFlags {
				is.NotNil(cmd.Flags().Lookup(flag), "expected flag %q", flag)
			}
		})
	}
}

// TestRootCommandWiresSubcommands guards against a subcommand being
// dropped from the command tree.
func TestRootCommandWiresSubcommands(t *testing.T) {
	t.Parallel()

	root := newRootCmd()
	got := map[string]bool{}
	for _, c := range root.Commands() {
		got[c.Name()] = true
	}

	for _, want := range []string{"run", "plan", "install", "uninstall", "init", "config", "version"} {
		assert.Truef(t, got[want], "root should expose %q subcommand; got %v", want, got)
	}
	assert.False(t, got["migrate"], "migrate command should have been removed for v0.1.0")
}

// TestConfigCommandWiresSubcommands pins the two `config` children.
func TestConfigCommandWiresSubcommands(t *testing.T) {
	t.Parallel()

	cfg := newConfigCmd()
	got := map[string]bool{}
	for _, c := range cfg.Commands() {
		got[c.Name()] = true
	}
	assert.True(t, got["validate"], "config validate should be registered")
	assert.True(t, got["schema"], "config schema should be registered")
}

// TestExitCodeError_Error covers both branches: with a wrapped err (returns
// the wrapped message) and without one (returns a generic "exit status N").
func TestExitCodeError_Error(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  *exitCodeError
		want string
	}{
		{name: "wrapped error message wins", err: &exitCodeError{code: 42, err: errors.New("boom")}, want: "boom"},
		{name: "no wrapped error falls back to code", err: &exitCodeError{code: 3}, want: "exit status 3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.err.Error())
		})
	}
}
