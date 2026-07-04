package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TestDefaultRunOptions pins the zero value contract: the returned struct is
// the type's zero value, so any future field is opt-in by default.
func TestDefaultRunOptions(t *testing.T) {
	t.Parallel()
	assert.Equal(t, runOptions{}, defaultRunOptions())
}

// TestReadRunOptions round-trips every registered flag through the cobra
// command so a rename or type mismatch between addRunFlags and
// readRunOptions is caught.
func TestReadRunOptions(t *testing.T) {
	t.Parallel()

	cmd := newRunCmd()
	must := require.New(t)

	must.NoError(cmd.Flags().Set("config", "/tmp/x.yaml"))
	must.NoError(cmd.Flags().Set("output", "json"))
	must.NoError(cmd.Flags().Set("task", "lint"))
	must.NoError(cmd.Flags().Set("task", "test"))
	must.NoError(cmd.Flags().Set("all", "true"))
	must.NoError(cmd.Flags().Set("no-interactive", "true"))
	must.NoError(cmd.Flags().Set("no-clean-worktree", "true"))
	must.NoError(cmd.Flags().Set("parallelism", "4"))
	must.NoError(cmd.Flags().Set("dry-run", "true"))

	got := readRunOptions(cmd)
	assert.Equal(t, runOptions{
		configPath:      "/tmp/x.yaml",
		outputMode:      "json",
		taskNames:       []string{"lint", "test"},
		all:             true,
		noInteractive:   true,
		noCleanWorktree: true,
		parallelism:     4,
		dryRun:          true,
	}, got)
}

// TestPrintlnSafe covers both branches: empty input is a no-op, non-empty
// input writes exactly one line.
func TestPrintlnSafe(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty is no-op", in: "", want: ""},
		{name: "non-empty writes line", in: "hello", want: "hello\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureStdout(t, func() error { return printlnSafe(tc.in) })
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestGithubTokenForVersionCheck exercises the env-var precedence:
// PRENUP_GITHUB_TOKEN wins over GITHUB_TOKEN wins over GH_TOKEN.
// Whitespace-only entries are treated as unset.
func TestGithubTokenForVersionCheck(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "prenup wins", env: map[string]string{"PRENUP_GITHUB_TOKEN": "p", "GITHUB_TOKEN": "g", "GH_TOKEN": "h"}, want: "p"},
		{name: "github beats gh", env: map[string]string{"GITHUB_TOKEN": "g", "GH_TOKEN": "h"}, want: "g"},
		{name: "gh only", env: map[string]string{"GH_TOKEN": "h"}, want: "h"},
		{name: "whitespace is unset", env: map[string]string{"PRENUP_GITHUB_TOKEN": "   ", "GITHUB_TOKEN": "g"}, want: "g"},
		{name: "none set", env: nil, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// t.Setenv restores on cleanup automatically; must-clear
			// the other two keys so a leaking value in the process env
			// (unlikely under `go test` but not impossible) can't
			// influence the case.
			for _, k := range []string{"PRENUP_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			assert.Equal(t, tc.want, githubTokenForVersionCheck())
		})
	}
}

// TestResolvedVersionUsesInjectedValue confirms the -X ldflag path: if
// Version has been overridden at link time (or by tests), ResolvedVersion
// returns it. ResetVersionCacheForTest clears the sync.OnceValue so the
// swap actually takes effect.
func TestResolvedVersionUsesInjectedValue(t *testing.T) {
	orig := Version
	t.Cleanup(func() {
		Version = orig
		ResetVersionCacheForTest()
	})

	Version = "v9.8.7"
	ResetVersionCacheForTest()
	assert.Equal(t, "v9.8.7", ResolvedVersion())
}

// TestVersionCommandPrintsResolvedVersion covers newVersionCmd end-to-end
// through cobra's Execute so we exercise the RunE handler wiring too.
func TestVersionCommandPrintsResolvedVersion(t *testing.T) {
	orig := Version
	t.Cleanup(func() {
		Version = orig
		ResetVersionCacheForTest()
	})
	Version = "v1.2.3-test"
	ResetVersionCacheForTest()

	got := captureStdout(t, func() error {
		return newVersionCmd().Execute()
	})
	assert.Contains(t, got, "prenup v1.2.3-test")
}

// AcquireRepoLockTestSuite exercises acquireRepoLock against a real
// initialized git repo. Two cases share the same fixture, so a suite pays
// its keep.
type AcquireRepoLockTestSuite struct {
	suite.Suite
	repo string
}

func (s *AcquireRepoLockTestSuite) SetupTest() {
	dir := s.T().TempDir()
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	s.Require().NoErrorf(err, "git init: %s", string(out))
	s.repo = dir
}

// TestDryRunSkipsLock verifies the documented dry-run behavior: no lock is
// acquired, so a subsequent lock file is not present. The release func must
// still be safe to call.
func (s *AcquireRepoLockTestSuite) TestDryRunSkipsLock() {
	release, err := acquireRepoLock(s.repo, true)
	s.Require().NoError(err)
	s.Require().NotNil(release)
	release() // must not panic

	// dry-run should not create the lock file.
	_, err = os.Stat(filepath.Join(s.repo, ".git", "prenup.lock"))
	s.True(os.IsNotExist(err), "dry-run should not create the lock file, but stat err was %v", err)
}

// TestAcquireAndRelease exercises the happy path: acquire, release, and be
// able to acquire again.
func (s *AcquireRepoLockTestSuite) TestAcquireAndRelease() {
	release, err := acquireRepoLock(s.repo, false)
	s.Require().NoError(err)
	s.Require().NotNil(release)
	release()

	// After release, a second acquire should succeed.
	release2, err := acquireRepoLock(s.repo, false)
	s.Require().NoError(err)
	release2()
}

func TestAcquireRepoLockSuite(t *testing.T) {
	suite.Run(t, new(AcquireRepoLockTestSuite))
}

// captureStdout redirects os.Stdout, invokes fn, and returns whatever fn
// wrote. Panics via t.Fatal on plumbing errors to keep call sites terse.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fnErr := fn()
	require.NoError(t, w.Close())

	buf, readErr := io.ReadAll(r)
	require.NoError(t, readErr)
	require.NoError(t, fnErr)
	return string(buf)
}
