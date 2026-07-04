package lock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSplitLines is a truth table for the internal newline splitter. It
// intentionally keeps blank lines (unlike git.splitLines) so callers can
// preserve blank-line semantics if they want; it just splits on '\n'.
func TestSplitLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "single line no newline", in: "hello", want: []string{"hello"}},
		{name: "single line with newline", in: "hello\n", want: []string{"hello"}},
		{name: "two lines", in: "a\nb", want: []string{"a", "b"}},
		{name: "trailing blank line preserved", in: "a\n\n", want: []string{"a", ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, splitLines(tc.in))
		})
	}
}

// TestTrimSpaceAndIsSpace covers both helpers together since trimSpace's
// only interesting behavior is that it delegates to isSpace.
func TestTrimSpaceAndIsSpace(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "no whitespace", in: "abc", want: "abc"},
		{name: "leading and trailing spaces", in: "  abc  ", want: "abc"},
		{name: "tabs and CR", in: "\t\rabc\t\r", want: "abc"},
		{name: "all whitespace", in: " \t\n\r ", want: ""},
		{name: "empty", in: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, trimSpace(tc.in))
		})
	}

	assert.True(t, isSpace(' '))
	assert.True(t, isSpace('\t'))
	assert.True(t, isSpace('\r'))
	assert.True(t, isSpace('\n'))
	assert.False(t, isSpace('a'))
	assert.False(t, isSpace(0))
}

// TestResolveGitFile covers the worktree/submodule support: a .git file
// (regular file, not directory) pointing to a real gitdir should resolve.
func TestResolveGitFile(t *testing.T) {
	t.Parallel()

	t.Run("returns false when file is missing", func(t *testing.T) {
		t.Parallel()
		_, ok := resolveGitFile(filepath.Join(t.TempDir(), "nope"))
		assert.False(t, ok)
	})

	t.Run("returns false when prefix is missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		f := filepath.Join(dir, ".git")
		require.NoError(t, os.WriteFile(f, []byte("something else\n"), 0o600))
		_, ok := resolveGitFile(f)
		assert.False(t, ok)
	})

	t.Run("returns trimmed gitdir when present", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		f := filepath.Join(dir, ".git")
		require.NoError(t, os.WriteFile(f, []byte("gitdir: /some/where\n"), 0o600))
		got, ok := resolveGitFile(f)
		require.True(t, ok)
		assert.Equal(t, "/some/where", got)
	})
}
