package human

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c2fo/prenup/internal/runner"
)

// TestSummarySinkBoundsLogs verifies the memory guard on the output buffer:
// once more than maxSummaryLogLines EventLine events arrive, only the most
// recent lines are retained and the rendered summary notes how many were
// dropped.
func TestSummarySinkBoundsLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	s := NewSummarySink(&buf)

	const extra = 25
	total := maxSummaryLogLines + extra
	for i := range total {
		s.Emit(runner.Event{Kind: runner.EventLine, Text: fmt.Sprintf("line-%d", i)})
	}

	assert.Len(t, s.logs, maxSummaryLogLines, "retained lines must be capped")
	assert.Equal(t, extra, s.droppedLines, "dropped count must track the overflow")
	// Oldest lines are evicted; the tail is preserved.
	assert.Equal(t, "line-"+strconv.Itoa(total-1), s.logs[len(s.logs)-1])
	assert.Equal(t, "line-"+strconv.Itoa(extra), s.logs[0])

	require.NoError(t, s.Close())
	out := buf.String()
	assert.Contains(t, out, "earlier line(s) omitted")
	assert.Contains(t, out, "line-"+strconv.Itoa(total-1), "most recent line should render")
	assert.NotContains(t, out, "line-0\n", "evicted oldest line should not render")
}

// TestSummarySinkNoTruncationNotice confirms the truncation notice is absent
// when output stays under the cap.
func TestSummarySinkNoTruncationNotice(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	s := NewSummarySink(&buf)
	s.Emit(runner.Event{Kind: runner.EventLine, Text: "only line"})
	require.NoError(t, s.Close())

	out := buf.String()
	assert.Equal(t, 0, s.droppedLines)
	assert.NotContains(t, out, "omitted")
	assert.Contains(t, out, "only line")
}
