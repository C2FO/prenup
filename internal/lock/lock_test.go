package lock

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeRepo creates a directory containing an empty .git/ subdir, mimicking
// the layout Acquire expects. Returns the repo root.
func fakeRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o750))
	return repo
}

func TestAcquireReleaseRoundTrip(t *testing.T) {
	t.Parallel()
	repo := fakeRepo(t)

	l, err := Acquire(repo)
	require.NoError(t, err)
	require.NotNil(t, l)
	require.FileExists(t, l.Path())
	require.Equal(t, filepath.Join(repo, ".git", LockFileName), l.Path())

	require.NoError(t, l.Close())
	// Second Close is a no-op, must not panic or error.
	require.NoError(t, l.Close())
}

func TestAcquireContendedReturnsErrContended(t *testing.T) {
	t.Parallel()
	repo := fakeRepo(t)

	first, err := Acquire(repo)
	require.NoError(t, err)
	defer func() { _ = first.Close() }()

	second, err := Acquire(repo)
	require.Nil(t, second)
	require.ErrorIs(t, err, ErrContended)
}

func TestAcquireAfterReleaseSucceeds(t *testing.T) {
	t.Parallel()
	repo := fakeRepo(t)

	first, err := Acquire(repo)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := Acquire(repo)
	require.NoError(t, err)
	require.NotNil(t, second)
	require.NoError(t, second.Close())
}

func TestAcquireCreatesGitDirIfMissing(t *testing.T) {
	t.Parallel()
	repo := t.TempDir() // no .git/ at all

	l, err := Acquire(repo)
	require.NoError(t, err)
	defer func() { _ = l.Close() }()

	info, statErr := os.Stat(filepath.Join(repo, ".git"))
	require.NoError(t, statErr)
	require.True(t, info.IsDir())
}

func TestAcquireConcurrent(t *testing.T) {
	t.Parallel()
	repo := fakeRepo(t)

	const goroutines = 16
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
		contended int
		other     []error
	)
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			l, err := Acquire(repo)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
				// Hold briefly so others observe contention.
				_ = l.Close()
			case errors.Is(err, ErrContended):
				contended++
			default:
				other = append(other, err)
			}
		}()
	}
	wg.Wait()

	require.Empty(t, other, "no unexpected errors")
	require.GreaterOrEqual(t, successes, 1, "at least one acquirer must win")
	require.Equal(t, goroutines, successes+contended, "every goroutine accounted for")
}
