package git

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/c2fo/prenup/internal/lock"
)

// StashMessagePrefix tags prenup-created stashes so we can identify and pop
// them even if the user creates other stashes in the meantime.
const StashMessagePrefix = "prenup:autostash"

const (
	// stashLockName is the lock file, inside the common git directory, that
	// serializes prenup stash operations across all linked worktrees.
	stashLockName = "prenup-stash.lock"
	// stashLockWait bounds how long a stash operation waits for another
	// worktree's prenup to finish its own. Each holder only runs a handful of
	// git commands, so contention clears quickly.
	stashLockWait = 30 * time.Second
)

// lockStash serializes stash operations across every worktree of the
// repository. refs/stash lives in the common git directory and is shared by
// all worktrees, so the per-worktree run lock cannot protect it: without
// this, one worktree's `stash pop stash@{N}` can apply another worktree's
// stash after the indices shift. The returned func releases the lock.
func (r *Runner) lockStash() (func(), error) {
	_, commonDir, err := GitDirs(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("locating common git dir for stash lock: %w", err)
	}
	l, err := lock.AcquireDir(commonDir, stashLockName, stashLockWait)
	if err != nil {
		return nil, fmt.Errorf("waiting for shared stash lock: %w", err)
	}
	return func() { _ = l.Close() }, nil
}

// Stash represents a prenup-managed stash that must be restored.
type Stash struct {
	Ref     string // e.g. "stash@{0}" at the time of creation
	Message string
	runner  *Runner
	active  bool
}

// Push stashes unstaged changes (keeping the index intact) so task execution
// runs against a worktree that matches the pending commit. Returns a Stash
// which the caller must Pop (typically via defer).
//
// If the worktree is clean, Push returns a zero-value Stash whose Pop is a
// no-op. Untracked files are included so they can't leak into task runs.
func (r *Runner) Push() (*Stash, error) {
	unlock, err := r.lockStash()
	if err != nil {
		return nil, err
	}
	defer unlock()

	dirty, err := r.HasUnstagedChanges()
	if err != nil {
		return nil, fmt.Errorf("detecting unstaged changes: %w", err)
	}
	if !dirty {
		return &Stash{runner: r}, nil
	}

	// Combine nanoseconds with a small random suffix so back-to-back stashes
	// (e.g. tests, retries) cannot collide on the same message.
	var nonce [4]byte
	_, _ = rand.Read(nonce[:])
	msg := fmt.Sprintf("%s %d-%s", StashMessagePrefix, time.Now().UnixNano(), hex.EncodeToString(nonce[:]))
	_, err = r.run("stash", "push", "--include-untracked", "--keep-index", "--message", msg)
	if err != nil {
		return nil, fmt.Errorf("creating stash: %w", err)
	}

	ref, err := r.findStashRef(msg)
	if err != nil {
		return nil, err
	}
	return &Stash{Ref: ref, Message: msg, runner: r, active: true}, nil
}

// Pop restores a stash previously created via Push. Safe to call on a zero
// Stash or one that has already been popped.
func (s *Stash) Pop() error {
	if s == nil || !s.active {
		return nil
	}
	unlock, err := s.runner.lockStash()
	if err != nil {
		return err
	}
	defer unlock()

	// Use the recorded message to re-locate the stash, since running tasks
	// may have added other stashes in between. Holding the lock keeps the
	// index found here valid until the pop below.
	ref, err := s.runner.findStashRef(s.Message)
	if err != nil {
		return err
	}
	if ref == "" {
		return fmt.Errorf("prenup stash %q not found when restoring", s.Message)
	}
	if _, err := s.runner.run("stash", "pop", ref); err != nil {
		return fmt.Errorf("restoring stash: %w", err)
	}
	s.active = false
	return nil
}

// findStashRef walks `git stash list` and returns the ref matching msg, or
// the empty string if not found.
func (r *Runner) findStashRef(msg string) (string, error) {
	out, err := r.run("stash", "list")
	if err != nil {
		return "", fmt.Errorf("listing stashes: %w", err)
	}
	for _, line := range splitLines(out) {
		// Format: "stash@{N}: On branch: message"
		if !strings.Contains(line, msg) {
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			return line[:idx], nil
		}
	}
	return "", nil
}
