package cli

import (
	"bytes"
	"errors"
	"fmt"
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
	// Guard SilenceErrors / SilenceUsage on root: runRoot owns stderr error
	// formatting, so enabling Cobra's default printer would double-print errors.
	assert.True(t, root.SilenceErrors, "root must set SilenceErrors so runRoot prints errors once")
	assert.True(t, root.SilenceUsage, "root must set SilenceUsage so runtime errors do not dump usage")
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
// the wrapped message) and without one (returns a generic "exit status N"),
// plus Unwrap propagation for errors.Is / errors.As.
func TestExitCodeError_Error(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	cases := []struct {
		name       string
		err        *exitCodeError
		want       string
		wantUnwrap error
	}{
		{name: "wrapped error message wins", err: &exitCodeError{code: 42, err: sentinel}, want: "boom", wantUnwrap: sentinel},
		{name: "no wrapped error falls back to code", err: &exitCodeError{code: 3}, want: "exit status 3", wantUnwrap: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.err.Error())
			if tc.wantUnwrap != nil {
				assert.ErrorIs(t, tc.err, tc.wantUnwrap)
			} else {
				assert.NoError(t, tc.err.Unwrap())
			}
		})
	}
}

// TestExecuteErrorHandling verifies that CLI/RunE errors are written to stderr
// while bare exitCodeError results (already rendered by task sinks) stay silent.
func TestExecuteErrorHandling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		runErr     error
		args       []string
		wantCode   int
		wantStderr string
	}{
		{
			name:       "success writes nothing and exits 0",
			runErr:     nil,
			wantCode:   0,
			wantStderr: "",
		},
		{
			name:       "plain error prints to stderr and exits 1",
			runErr:     errors.New("no .prenup.yaml found"),
			wantCode:   1,
			wantStderr: "Error: no .prenup.yaml found\n",
		},
		{
			name:       "exitCodeError with wrapped error prints message and preserves code",
			runErr:     &exitCodeError{code: 2, err: errors.New("another prenup run is already in progress")},
			wantCode:   2,
			wantStderr: "Error: another prenup run is already in progress\n",
		},
		{
			name:       "wrapped exitCodeError preserves outer context and exit code",
			runErr:     fmt.Errorf("acquiring lock: %w", &exitCodeError{code: 2, err: errors.New("contended")}),
			wantCode:   2,
			wantStderr: "Error: acquiring lock: contended\n",
		},
		{
			name:       "bare exitCodeError without wrapped error stays silent on stderr",
			runErr:     &exitCodeError{code: 1},
			wantCode:   1,
			wantStderr: "",
		},
		{
			name:       "unknown cobra flag prints error and exits 1",
			args:       []string{"--definitely-unknown-flag"},
			wantCode:   1,
			wantStderr: "Error: unknown flag: --definitely-unknown-flag\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			cmd := &cobra.Command{
				Use:           "prenup",
				SilenceUsage:  true,
				SilenceErrors: true,
				RunE: func(_ *cobra.Command, _ []string) error {
					return tc.runErr
				},
			}
			cmd.SetErr(&stderr)
			// Cobra treats nil args as "use os.Args[1:]", which would pick up
			// go test flags; always pass a non-nil slice.
			args := tc.args
			if args == nil {
				args = []string{}
			}
			cmd.SetArgs(args)

			code := runRoot(cmd)
			assert.Equal(t, tc.wantCode, code)
			assert.Equal(t, tc.wantStderr, stderr.String())
		})
	}
}
