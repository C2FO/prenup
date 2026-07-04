package runner

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSink struct {
	events   []Event
	closeErr error
	closed   bool
}

func (r *recordingSink) Emit(ev Event) { r.events = append(r.events, ev) }
func (r *recordingSink) Close() error  { r.closed = true; return r.closeErr }

// TestMultiSinkEmitsToAll verifies the basic fan-out contract.
func TestMultiSinkEmitsToAll(t *testing.T) {
	t.Parallel()
	a, b := &recordingSink{}, &recordingSink{}
	m := NewMultiSink(a, b)
	m.Emit(Event{Kind: EventNotice, Message: "hi"})

	assert.Len(t, a.events, 1)
	assert.Len(t, b.events, 1)
}

// TestMultiSinkCloseAggregatesErrors confirms that when one sink's Close
// fails, the other sinks are still closed (so resources don't leak) and the
// first error is what gets returned to the caller.
func TestMultiSinkCloseAggregatesErrors(t *testing.T) {
	t.Parallel()
	first := &recordingSink{closeErr: errors.New("first failed")}
	second := &recordingSink{}
	third := &recordingSink{closeErr: errors.New("third failed")}

	m := NewMultiSink(first, second, third)
	err := m.Close()

	require.EqualError(t, err, "first failed", "first error must propagate")
	assert.True(t, first.closed)
	assert.True(t, second.closed, "second sink must be closed even if first errored")
	assert.True(t, third.closed, "third sink must be closed regardless of earlier errors")
}

// TestMultiSinkCloseAllSuccess returns nil when every sink closes cleanly.
func TestMultiSinkCloseAllSuccess(t *testing.T) {
	t.Parallel()
	m := NewMultiSink(&recordingSink{}, &recordingSink{})
	assert.NoError(t, m.Close())
}
