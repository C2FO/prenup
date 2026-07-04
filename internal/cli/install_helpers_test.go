package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c2fo/prenup/internal/hook"
)

// TestInstallModeFromFlags is a truth table for the flag → hook.Mode mapping.
// force wins over replace wins over chain wins over abort (default).
func TestInstallModeFromFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		flags installFlags
		want  hook.Mode
	}{
		{name: "no flags → abort (safe default)", flags: installFlags{}, want: hook.ModeAbort},
		{name: "force wins", flags: installFlags{force: true, replace: true, chain: true}, want: hook.ModeForce},
		{name: "replace beats chain", flags: installFlags{replace: true, chain: true}, want: hook.ModeReplace},
		{name: "chain only", flags: installFlags{chain: true}, want: hook.ModeChain},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, installModeFromFlags(tc.flags))
		})
	}
}

// TestResolveInstallBinary covers the precedence rules for picking what to
// embed in the hook script:
//   - --use-path + --binary are mutually exclusive (error)
//   - --use-path alone → literal "prenup" (PATH-resolved at hook run time)
//   - --binary alone → that path verbatim
//   - no flags → falls through to resolveBinary(), which returns something
//     non-empty (the currently running test binary)
func TestResolveInstallBinary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		flags        installFlags
		wantExact    string // when non-empty, require exact match
		wantErr      string
		wantNonEmpty bool
	}{
		{name: "use-path and binary conflict", flags: installFlags{usePath: true, binary: "/x"}, wantErr: "mutually exclusive"},
		{name: "use-path alone", flags: installFlags{usePath: true}, wantExact: "prenup"},
		{name: "explicit binary path", flags: installFlags{binary: "/custom/prenup"}, wantExact: "/custom/prenup"},
		{name: "default falls through to resolveBinary", flags: installFlags{}, wantNonEmpty: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveInstallBinary(tc.flags)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			if tc.wantExact != "" {
				assert.Equal(t, tc.wantExact, got)
				return
			}
			if tc.wantNonEmpty {
				assert.NotEmpty(t, got)
			}
		})
	}
}

// TestReadInstallFlags round-trips every install flag through a fresh cobra
// command to catch drift between the flag registration in newInstallCmd and
// the reader in readInstallFlags.
func TestReadInstallFlags(t *testing.T) {
	t.Parallel()

	cmd := newInstallCmd()
	must := require.New(t)

	must.NoError(cmd.Flags().Set("force", "true"))
	must.NoError(cmd.Flags().Set("replace", "true"))
	must.NoError(cmd.Flags().Set("chain", "true"))
	must.NoError(cmd.Flags().Set("binary", "/opt/prenup"))
	must.NoError(cmd.Flags().Set("use-path", "true"))
	must.NoError(cmd.Flags().Set("non-interactive", "true"))

	got := readInstallFlags(cmd)
	assert.Equal(t, installFlags{
		force:          true,
		replace:        true,
		chain:          true,
		binary:         "/opt/prenup",
		usePath:        true,
		nonInteractive: true,
	}, got)
}

// TestResolveBinaryReturnsAbsolute confirms the default (os.Executable) path
// works in a test binary context. We can't easily pin the exact path since
// `go test` builds a temp binary, but we can check it's non-empty and
// absolute.
func TestResolveBinaryReturnsAbsolute(t *testing.T) {
	t.Parallel()
	got, err := resolveBinary()
	require.NoError(t, err)
	assert.NotEmpty(t, got)
	assert.True(t, filepath.IsAbs(got), "expected absolute path, got %q", got)
}
