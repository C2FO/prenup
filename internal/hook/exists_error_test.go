package hook

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExistsError_Error pins the user-facing message. This is the string a
// user sees when a competing pre-commit hook blocks install; changing the
// wording would break the CLI's `--force`/`--replace`/`--chain` guidance.
func TestExistsError_Error(t *testing.T) {
	t.Parallel()

	err := &ExistsError{Path: "/repo/.git/hooks/pre-commit"}
	msg := err.Error()

	assert.Contains(t, msg, "/repo/.git/hooks/pre-commit")
	assert.Contains(t, msg, "--force")
	assert.Contains(t, msg, "--replace")
	assert.Contains(t, msg, "--chain")
}
