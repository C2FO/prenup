package git

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVersion pins the diagnostic contract of Runner.Version: it must
// return a non-empty string that starts with "git version" (the git CLI
// output format). Skipped if the git binary isn't on PATH so the test
// still passes in minimal build environments.
func TestVersion(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available on this platform")
	}

	got, err := New("").Version()
	require.NoError(t, err)
	assert.Contains(t, got, "git version")
}
