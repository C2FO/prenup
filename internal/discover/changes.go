// Package discover finds what changed in the working tree and which modules own
// those changes. It also applies the repo-level exclude filter and per-task
// path filters.
package discover

import (
	"fmt"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/c2fo/prenup/internal/git"
)

// ChangedFiles returns deduplicated paths (relative to repo root) that are
// staged, unstaged, or untracked. Paths matching any excludePattern (a
// doublestar glob) are dropped.
func ChangedFiles(r *git.Runner, excludePatterns []string) ([]string, error) {
	staged, err := r.StagedFiles()
	if err != nil {
		return nil, fmt.Errorf("staged files: %w", err)
	}
	unstaged, err := r.UnstagedFiles()
	if err != nil {
		return nil, fmt.Errorf("unstaged files: %w", err)
	}
	untracked, err := r.UntrackedFiles()
	if err != nil {
		return nil, fmt.Errorf("untracked files: %w", err)
	}

	seen := make(map[string]struct{})
	all := make([]string, 0, len(staged)+len(unstaged)+len(untracked))
	for _, src := range [][]string{staged, unstaged, untracked} {
		for _, f := range src {
			if _, ok := seen[f]; ok {
				continue
			}
			seen[f] = struct{}{}
			all = append(all, f)
		}
	}

	if len(excludePatterns) == 0 {
		return all, nil
	}

	kept := make([]string, 0, len(all))
	for _, f := range all {
		if Matches(excludePatterns, f) {
			continue
		}
		kept = append(kept, f)
	}
	return kept, nil
}

// Matches returns true when path matches any doublestar pattern.
//
// Pattern errors are intentionally swallowed: config validation should have
// caught them at load time, and a per-event log here would amount to one
// warning per file per pattern. Use ValidatePatterns at config-load to
// surface bad patterns early.
func Matches(patterns []string, path string) bool {
	for _, p := range patterns {
		if ok, err := doublestar.Match(p, path); err == nil && ok {
			return true
		}
	}
	return false
}

// ValidatePatterns returns the first invalid pattern (if any) along with the
// underlying parser error. Used by config.Validate to fail loudly at load
// time instead of letting Matches silently miss matches.
func ValidatePatterns(patterns []string) error {
	for _, p := range patterns {
		if _, err := doublestar.Match(p, ""); err != nil {
			return fmt.Errorf("invalid pattern %q: %w", p, err)
		}
	}
	return nil
}

// FilterByPaths returns only the files matching include (if non-empty) and
// not matching exclude. An empty include list includes everything.
func FilterByPaths(files, include, exclude []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if len(include) > 0 && !Matches(include, f) {
			continue
		}
		if len(exclude) > 0 && Matches(exclude, f) {
			continue
		}
		out = append(out, f)
	}
	return out
}
