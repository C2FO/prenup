package runner

import (
	"github.com/bmatcuk/doublestar/v4"

	"github.com/c2fo/prenup/internal/git"
)

// stageGenerated stages files that newly appeared (vs beforeTracked), match
// any of the doublestar patterns, and (for per-module tasks) live under one
// of the task's modules. Pre-existing unstaged changes are never promoted.
//
// modules may be nil/empty (non per-module tasks) or contain "" / "." to mean
// "scope to the whole repo". A non-empty list of real module paths restricts
// staging to files that live under those modules so a task that ran in module
// A cannot accidentally stage files under module B.
func stageGenerated(gitRunner *git.Runner, patterns []string, beforeTracked map[string]struct{}, modules []string) error {
	if len(patterns) == 0 {
		return nil
	}
	after, err := gitRunner.PorcelainStatus()
	if err != nil {
		return err
	}

	scoped := make([]string, 0, len(modules))
	for _, m := range modules {
		if m == "" || m == "." {
			// Wildcard scope: a single empty entry signals "match anywhere"
			// to underModule below.
			scoped = nil
			break
		}
		scoped = append(scoped, m)
	}

	var toStage []string
	for _, f := range after {
		if _, existed := beforeTracked[f]; existed {
			continue
		}
		if !matchesAny(patterns, f) {
			continue
		}
		if !underModules(f, scoped) {
			continue
		}
		toStage = append(toStage, f)
	}
	return gitRunner.Add(toStage)
}

// underModules returns true when f lives under one of modules. An empty
// modules slice means "match anywhere" (no per-module restriction).
func underModules(f string, modules []string) bool {
	if len(modules) == 0 {
		return true
	}
	for _, m := range modules {
		if hasPathPrefix(f, m) {
			return true
		}
	}
	return false
}

func matchesAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if ok, err := doublestar.Match(p, path); err == nil && ok {
			return true
		}
	}
	return false
}
