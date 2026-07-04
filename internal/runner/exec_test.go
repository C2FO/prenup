package runner

import (
	"context"
	"os/exec"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFlattenEnv verifies the deterministic-shape contract: every entry is
// K=V and the set is exactly the input map. We sort before comparing since
// map iteration order isn't stable.
func TestFlattenEnv(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   map[string]string
		want []string
	}{
		{name: "empty map", in: map[string]string{}, want: []string{}},
		{name: "nil map", in: nil, want: []string{}},
		{name: "single entry", in: map[string]string{"X": "1"}, want: []string{"X=1"}},
		{
			name: "multiple entries deterministic",
			in:   map[string]string{"A": "1", "B": "2", "C": "3"},
			want: []string{"A=1", "B=2", "C=3"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := flattenEnv(tc.in)
			sort.Strings(got)
			sort.Strings(tc.want)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestBashExecutor_Run_HappyPath exercises the full pipe/pump/wait flow: a
// short bash command emits stdout and stderr lines, both are streamed to
// onLine with the correct Stream, and Run returns nil on exit 0.
func TestBashExecutor_Run_HappyPath(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available on this platform")
	}

	var (
		mu    sync.Mutex
		lines []struct {
			stream Stream
			text   string
		}
	)
	onLine := func(s Stream, txt string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, struct {
			stream Stream
			text   string
		}{s, txt})
	}

	err := BashExecutor{}.Run(context.Background(),
		`echo hello; echo boom 1>&2`, "", map[string]string{"PRENUP_TEST": "x"}, onLine)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, lines, 2)

	// Order between stdout and stderr isn't guaranteed (separate goroutines),
	// so sort by text before asserting.
	sort.Slice(lines, func(i, j int) bool { return lines[i].text < lines[j].text })
	assert.Equal(t, StreamStderr, lines[0].stream)
	assert.Equal(t, "boom", lines[0].text)
	assert.Equal(t, StreamStdout, lines[1].stream)
	assert.Equal(t, "hello", lines[1].text)
}

// TestBashExecutor_Run_NonZeroExit surfaces the exit status as an error
// prefixed with "exit status N".
func TestBashExecutor_Run_NonZeroExit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available on this platform")
	}

	err := BashExecutor{}.Run(context.Background(), "exit 7", "", nil, func(Stream, string) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit status 7")
}

// TestBashExecutor_Run_ContextCancel_ReturnsPromptly asserts that canceling
// the parent context terminates a long-running command within the grace
// window instead of waiting on it forever. The Cancel/WaitDelay wiring is
// subtle enough that a regression here would show up as a hang under load.
func TestBashExecutor_Run_ContextCancel_ReturnsPromptly(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available on this platform")
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := BashExecutor{GracefulShutdown: 200 * time.Millisecond}.
		Run(ctx, "sleep 30", "", nil, func(Stream, string) {})
	elapsed := time.Since(start)

	require.Error(t, err, "canceled command should return an error")
	assert.Less(t, elapsed, 5*time.Second, "cancel path must not hang for the full sleep")
}

// TestDiscardSink covers the trivial no-op sink's contract.
func TestDiscardSink(t *testing.T) {
	t.Parallel()
	var s DiscardSink
	assert.NotPanics(t, func() { s.Emit(Event{Kind: EventRunStarted}) })
	assert.NoError(t, s.Close())
}
