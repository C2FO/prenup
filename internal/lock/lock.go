// Package lock provides a per-repository advisory lock so two concurrent
// `prenup run` invocations cannot race on the worktree.
//
// Some Git GUIs and IDE integrations invoke `git commit` more than once in
// quick succession; without coordination, two prenup runs can both stash,
// run tasks, and pop in interleaved order, corrupting `stage_output` and
// the user's stash list. The lock guarantees serialization at the repo
// scope.
//
// The implementation uses an OS-level advisory lock (flock(2) on Unix) on a
// file under .git/. Advisory locks are automatically released when the
// holding process dies, so there is no stale-PID file to clean up after a
// crash or kill -9.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockFileName is the basename of the lock file inside .git/.
const LockFileName = "prenup.lock"

// ErrContended is returned by Acquire when another prenup run is already
// holding the lock. Callers should surface a friendly message and exit
// with a non-zero status rather than block.
var ErrContended = errors.New("another prenup run is already in progress for this repository")

// Lock is a held advisory lock on a repository. Release with Close.
type Lock struct {
	path string
	f    *os.File
}

// Acquire takes a non-blocking exclusive advisory lock on
// repoRoot/.git/prenup.lock. It returns ErrContended when another process
// already holds the lock, and any other error if the file system itself is
// the problem.
//
// The returned Lock owns the file descriptor; the caller must call Close
// (typically via defer) to release the lock and remove the file.
func Acquire(repoRoot string) (*Lock, error) {
	gitDir := filepath.Join(repoRoot, ".git")
	// Repos with separate gitdirs (worktrees, submodules) write a `.git`
	// regular file pointing at the real dir; resolve it so the lock lives
	// alongside the repo's actual git metadata. If `.git` doesn't exist
	// yet, MkdirAll below will create it -- callers are responsible for
	// having pointed us at a real repo root.
	if info, err := os.Stat(gitDir); err == nil && !info.IsDir() {
		if resolved, ok := resolveGitFile(gitDir); ok {
			gitDir = resolved
		}
	}
	if err := os.MkdirAll(gitDir, 0o750); err != nil {
		return nil, fmt.Errorf("ensuring lock directory: %w", err)
	}
	path := filepath.Join(gitDir, LockFileName)
	// O_CREATE so first-run users don't need a pre-existing lock file.
	// 0600: only the user running prenup needs to read/write it.
	// G304 suppressed: path = repoRoot/.git/prenup.lock; basename is fixed.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("opening lock file %s: %w", path, err)
	}
	if err := flockNB(f); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrContended
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return &Lock{path: path, f: f}, nil
}

// Close releases the advisory lock and closes the underlying file. It is
// safe to call Close more than once; the second call is a no-op. Close does
// not delete the lock file -- the inode is harmless when unlocked, and
// keeping it avoids a TOCTOU race where another process opens the file
// between our unlink and unlock.
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	// Best-effort unlock; the kernel also releases when the fd closes.
	_ = funlock(l.f)
	err := l.f.Close()
	l.f = nil
	return err
}

// Path returns the absolute path of the lock file. Useful for diagnostics.
func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// resolveGitFile reads a `.git` regular file (used by worktrees and
// submodules) and returns the directory it points at.
func resolveGitFile(gitFile string) (string, bool) {
	data, err := os.ReadFile(gitFile) //nolint:gosec // G304: callers pass a fixed .git path under repoRoot.
	if err != nil {
		return "", false
	}
	const prefix = "gitdir:"
	for _, line := range splitLines(string(data)) {
		if len(line) > len(prefix) && line[:len(prefix)] == prefix {
			return trimSpace(line[len(prefix):]), true
		}
	}
	return "", false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpace(s[start]) {
		start++
	}
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}
