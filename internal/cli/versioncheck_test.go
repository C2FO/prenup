package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMaybeVersionCheckRespectsCancelledContext is a smoke test that the
// version check returns promptly when the parent context is already
// canceled, instead of blocking on the network call until the inner 10s
// timeout fires. This guards against future regressions that would freeze
// `prenup run` for ten seconds when the user has hit Ctrl-C.
func TestMaybeVersionCheckRespectsCancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invocation; the HTTP call should fail immediately

	start := time.Now()
	info := maybeVersionCheck(ctx)
	elapsed := time.Since(start)

	assert.NotEmpty(t, info.version, "should still report the resolved version")
	assert.Empty(t, info.notice, "no update notice when the network call failed")
	// 2s is a very loose ceiling; in practice this returns sub-second.
	assert.Less(t, elapsed, 2*time.Second, "must not block on the 10s inner timeout")
}
