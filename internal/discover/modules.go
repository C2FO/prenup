package discover

import (
	"os"
	"path/filepath"
	"sort"
)

// Modules returns the deduplicated, sorted list of module directories (relative
// to repoRoot) that own the given changed files. A module is defined as the
// nearest ancestor directory of a changed file that contains any of the
// markerFiles (e.g. go.mod).
//
// Files that have no ancestor marker are skipped.
func Modules(repoRoot string, changedFiles, markerFiles []string) []string {
	if len(markerFiles) == 0 {
		markerFiles = []string{"go.mod"}
	}
	seen := make(map[string]struct{})
	for _, f := range changedFiles {
		dir := filepath.Dir(f)
		mod := findModuleRoot(repoRoot, dir, markerFiles)
		if mod == "" {
			continue
		}
		seen[mod] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// findModuleRoot walks up from startDir looking for any markerFile. Returns
// the relative path from repoRoot, or "." if the marker lives at the root.
// Returns the empty string if no marker is found up to the repo root, or if
// startDir lies inside a nested git checkout (any directory below repoRoot
// holding its own .git). Modules inside another checkout belong to that
// checkout, even when the marker sits deeper than the checkout root.
func findModuleRoot(repoRoot, startDir string, markerFiles []string) string {
	if insideNestedCheckout(repoRoot, startDir) {
		return ""
	}
	current := startDir
	for {
		for _, m := range markerFiles {
			candidate := filepath.Join(repoRoot, current, m)
			if _, err := os.Stat(candidate); err == nil {
				return current
			}
		}
		if current == "." || current == string(filepath.Separator) {
			return ""
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// insideNestedCheckout reports whether dir or any ancestor below repoRoot
// holds its own .git.
func insideNestedCheckout(repoRoot, dir string) bool {
	for current := dir; current != "." && current != string(filepath.Separator); {
		if _, err := os.Lstat(filepath.Join(repoRoot, current, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
	return false
}
