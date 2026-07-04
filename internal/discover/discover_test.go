package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatches(t *testing.T) {
	patterns := []string{"**/*.go", "docs/*.md"}
	assert.True(t, Matches(patterns, "pkg/foo/bar.go"))
	assert.True(t, Matches(patterns, "docs/readme.md"))
	assert.False(t, Matches(patterns, "docs/sub/readme.md"))
	assert.False(t, Matches(patterns, "README.txt"))
}

func TestFilterByPaths(t *testing.T) {
	files := []string{"a.go", "a_test.go", "docs/x.md", "README.txt"}
	got := FilterByPaths(files, []string{"**/*.go"}, []string{"**/*_test.go"})
	assert.Equal(t, []string{"a.go"}, got)
}

func TestFilterByPathsEmptyIncludeMeansAll(t *testing.T) {
	files := []string{"a.go", "README.md"}
	assert.Equal(t, files, FilterByPaths(files, nil, nil))
}

func TestModulesFindsNearestAncestor(t *testing.T) {
	root := t.TempDir()
	// Layout:
	//   <root>/go.mod                  (root module)
	//   <root>/services/auth/go.mod    (nested module)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module r\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "services", "auth"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "services", "auth", "go.mod"), []byte("module a\n"), 0o600))

	files := []string{
		"services/auth/handler.go",
		"services/auth/internal/db/db.go",
		"pkg/other/file.go",
	}
	mods := Modules(root, files, []string{"go.mod"})
	assert.Equal(t, []string{".", "services/auth"}, mods)
}

func TestModulesSkipsFilesWithNoMarker(t *testing.T) {
	root := t.TempDir()
	// No go.mod anywhere.
	mods := Modules(root, []string{"pkg/foo/bar.go"}, []string{"go.mod"})
	assert.Empty(t, mods)
}
