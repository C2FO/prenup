package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Executor runs a shell command and streams stdout+stderr lines to a callback.
// It is an interface so tests can swap in a fake.
type Executor interface {
	Run(ctx context.Context, command, workingDir string, env map[string]string, onLine func(Stream, string)) error
}

// BashExecutor invokes `bash -c command`.
//
// On context cancellation it sends SIGTERM and gives the child up to
// gracefulShutdown to clean up before SIGKILL. Long-running tools (test
// runners, linters) often want to flush output or remove temp files; the
// default exec.CommandContext behavior of immediate SIGKILL would skip that.
type BashExecutor struct {
	// GracefulShutdown caps how long to wait after cancel before the
	// process is forcefully killed. Zero means use the default of 5s.
	GracefulShutdown time.Duration
}

// gracefulShutdownDefault is the wait between SIGTERM and SIGKILL when
// GracefulShutdown is unset.
const gracefulShutdownDefault = 5 * time.Second

// Run executes command via bash -c. stdout and stderr are read concurrently
// and forwarded to onLine preserving the originating stream.
func (b BashExecutor) Run(ctx context.Context, command, workingDir string, env map[string]string, onLine func(Stream, string)) error {
	cmd := exec.CommandContext(ctx, "bash", "-c", command) //nolint:gosec // command comes from user config
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	cmd.Env = taskEnv(os.Environ(), env)
	// On ctx cancel, ask politely first; SIGKILL after the grace period.
	cmd.Cancel = func() error {
		// SIGTERM may legitimately fail (process already gone); ignore.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return nil
	}
	grace := b.GracefulShutdown
	if grace <= 0 {
		grace = gracefulShutdownDefault
	}
	cmd.WaitDelay = grace

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting command: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go pumpLines(&wg, stdout, StreamStdout, onLine)
	go pumpLines(&wg, stderr, StreamStderr, onLine)
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("exit status %d", exitErr.ExitCode())
		}
		return err
	}
	return nil
}

// pumpLines reads from r line-by-line up to maxScannerLine. If a single line
// exceeds the buffer, the scanner stops; we surface that to the caller as a
// synthetic stderr-style notice on the same stream so users know their
// output was truncated.
func pumpLines(wg *sync.WaitGroup, r io.Reader, stream Stream, onLine func(Stream, string)) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxScannerLine)
	for scanner.Scan() {
		onLine(stream, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		onLine(stream, fmt.Sprintf("[prenup] output truncated: %v (line exceeded %d bytes)", err, maxScannerLine))
	}
}

// maxScannerLine bounds a single line's length. 1 MB is generous for human
// output but small enough to keep memory bounded against pathological tools.
const maxScannerLine = 1024 * 1024

// gitLocalEnvVars are the variables reported by `git rev-parse
// --local-env-vars`. git exports some of them (notably GIT_DIR and
// GIT_INDEX_FILE) to hooks, and githooks(5) says to clear them before running
// git against any other repository or worktree. Left in place they make git
// commands run by a task operate on the committing worktree's metadata, even
// from a nested module directory or a freshly initialized temp repo.
var gitLocalEnvVars = map[string]struct{}{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": {},
	"GIT_CONFIG":                       {},
	"GIT_CONFIG_PARAMETERS":            {},
	"GIT_CONFIG_COUNT":                 {},
	"GIT_OBJECT_DIRECTORY":             {},
	"GIT_DIR":                          {},
	"GIT_WORK_TREE":                    {},
	"GIT_IMPLICIT_WORK_TREE":           {},
	"GIT_GRAFT_FILE":                   {},
	"GIT_INDEX_FILE":                   {},
	"GIT_NO_REPLACE_OBJECTS":           {},
	"GIT_REPLACE_REF_BASE":             {},
	"GIT_PREFIX":                       {},
	"GIT_SHALLOW_FILE":                 {},
	"GIT_COMMON_DIR":                   {},
}

// taskEnv builds a task's environment: environ minus git's repository-local
// variables, followed by the task's own overrides. Overrides come last so
// they win, including when a task deliberately sets one of the scrubbed vars.
func taskEnv(environ []string, overrides map[string]string) []string {
	out := make([]string, 0, len(environ)+len(overrides))
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if _, drop := gitLocalEnvVars[key]; drop {
			continue
		}
		out = append(out, kv)
	}
	return append(out, flattenEnv(overrides)...)
}

func flattenEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
