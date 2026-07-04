// Package git is a thin wrapper around the `git` command-line tool. It exposes
// only the operations prenup needs and keeps the wrapper easy to fake in tests.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes git commands against a specific working directory. A zero
// Runner uses the current working directory.
type Runner struct {
	Dir string
}

// New returns a Runner rooted at dir. If dir is empty, RepoRoot() callers will
// still resolve the repository relative to the current working directory.
func New(dir string) *Runner {
	return &Runner{Dir: dir}
}

// run executes git with the given args and returns stdout. stderr is captured
// and folded into the returned error on failure.
func (r *Runner) run(args ...string) (string, error) {
	// args originate from internal callers building well-formed git command
	// lines; the binary is hardcoded to "git".
	cmd := exec.Command("git", args...) //nolint:gosec // G204: git binary is fixed; args are constructed internally.
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

// RepoRoot returns the absolute path of the git repository containing dir (or
// the current working directory when dir is empty).
func RepoRoot(dir string) (string, error) {
	r := &Runner{Dir: dir}
	out, err := r.run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// StagedFiles returns paths, relative to the repo root, that have staged
// changes vs HEAD.
func (r *Runner) StagedFiles() ([]string, error) {
	out, err := r.run("diff", "--name-only", "--cached", "HEAD")
	if err != nil {
		// Empty repo (no HEAD) is not an error; treat as "everything staged is new".
		if strings.Contains(err.Error(), "unknown revision") || strings.Contains(err.Error(), "bad revision") {
			out, err = r.run("diff", "--name-only", "--cached")
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return splitLines(out), nil
}

// UnstagedFiles returns paths, relative to the repo root, with tracked but
// unstaged modifications.
func (r *Runner) UnstagedFiles() ([]string, error) {
	out, err := r.run("diff", "--name-only")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// UntrackedFiles returns paths, relative to the repo root, that are not
// tracked by git and not ignored by .gitignore.
func (r *Runner) UntrackedFiles() ([]string, error) {
	out, err := r.run("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// HasUnstagedChanges returns true when the worktree has unstaged modifications
// or untracked, non-ignored files. Used by stash-and-restore to decide whether
// any stashing is necessary.
func (r *Runner) HasUnstagedChanges() (bool, error) {
	unstaged, err := r.UnstagedFiles()
	if err != nil {
		return false, err
	}
	if len(unstaged) > 0 {
		return true, nil
	}
	untracked, err := r.UntrackedFiles()
	if err != nil {
		return false, err
	}
	return len(untracked) > 0, nil
}

// TrackedFiles returns the set of files currently tracked by git, used as a
// baseline snapshot before running tasks that may generate new files.
func (r *Runner) TrackedFiles() (map[string]struct{}, error) {
	out, err := r.run("ls-files")
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{})
	for _, line := range splitLines(out) {
		set[line] = struct{}{}
	}
	return set, nil
}

// PorcelainStatus returns the parsed output of
// `git status -s --porcelain=v1 --untracked-files=all` as a set of paths. Only
// the path portion is returned; status codes are discarded.
//
// `--untracked-files=all` is required: by default git collapses untracked
// directories to `dir/`, which would cause stage_output's per-file pattern
// matching to miss anything generated under a brand-new directory.
func (r *Runner) PorcelainStatus() ([]string, error) {
	out, err := r.run("status", "-s", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range splitLines(out) {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		// Rename and copy entries (status codes R / C) format the path
		// as `old -> new`. Take only the destination path so callers
		// can `git add` it without git treating the literal arrow as
		// part of the filename.
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+len(" -> "):]
		}
		files = append(files, path)
	}
	return files, nil
}

// Add stages the named files. A nil or empty slice is a no-op.
func (r *Runner) Add(files []string) error {
	if len(files) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, files...)
	if _, err := r.run(args...); err != nil {
		return err
	}
	return nil
}

// Version returns the `git --version` string, primarily for diagnostics.
func (r *Runner) Version() (string, error) {
	out, err := r.run("--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// splitLines splits s on newlines, trims carriage returns, and drops empties.
func splitLines(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// ErrNoRepo is returned when git cannot find a repository rooted at the given
// directory.
var ErrNoRepo = errors.New("not a git repository")
