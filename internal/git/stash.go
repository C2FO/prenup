package git

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// StashMessagePrefix tags prenup-created stashes so we can identify and pop
// them even if the user creates other stashes in the meantime.
const StashMessagePrefix = "prenup:autostash"

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
	// Use the recorded message to re-locate the stash, since running tasks
	// may have added other stashes in between.
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
