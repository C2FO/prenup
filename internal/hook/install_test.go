package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

// HookInstallTestSuite groups behavioral tests for Install/Uninstall.
// Each method gets a fresh fake repo with .git/hooks/ pre-created so
// the code under test has somewhere to write; tests do not need a real
// git repo for these checks.
type HookInstallTestSuite struct {
	suite.Suite
	repo string // absolute path to the temp repo dir
	hook string // absolute path to .git/hooks/pre-commit inside repo
}

func (s *HookInstallTestSuite) SetupTest() {
	dir := s.T().TempDir()
	s.Require().NoError(os.MkdirAll(filepath.Join(dir, ".git", "hooks"), 0o750))
	s.repo = dir
	s.hook = filepath.Join(dir, ".git", "hooks", "pre-commit")
}

// writeHook drops a fake (non-prenup) executable hook script at the
// pre-commit path. Used by tests that exercise the
// existing-hook-handling modes.
func (s *HookInstallTestSuite) writeHook(content string) {
	s.T().Helper()
	//nolint:gosec // G306: simulating executable git hook in tests.
	s.Require().NoError(os.WriteFile(s.hook, []byte(content), 0o750))
}

// readHook reads the managed pre-commit file. Wrapped so the //nolint
// gosec annotation does not have to repeat at every call site.
func (s *HookInstallTestSuite) readHook(path string) string {
	s.T().Helper()
	//nolint:gosec // G304: path is constructed under the test TempDir.
	data, err := os.ReadFile(path)
	s.Require().NoError(err)
	return string(data)
}

func (s *HookInstallTestSuite) TestInstallOnEmptyWritesManagedHook() {
	s.Require().NoError(Install(s.repo, "/usr/local/bin/prenup", ModeAbort))

	body := s.readHook(s.hook)
	s.Contains(body, PrenupMarker)
	s.Contains(body, `"/usr/local/bin/prenup"`)

	info, err := os.Stat(s.hook)
	s.Require().NoError(err)
	s.NotZero(info.Mode()&0o100, "hook must be executable")
}

func (s *HookInstallTestSuite) TestInstallAbortsOnExistingNonPrenupHook() {
	s.writeHook("#!/usr/bin/env bash\necho hi\n")

	err := Install(s.repo, "/usr/local/bin/prenup", ModeAbort)
	var existsErr *ExistsError
	s.Require().ErrorAs(err, &existsErr, "expected ExistsError, got %v", err)
}

func (s *HookInstallTestSuite) TestInstallReplaceBacksUp() {
	original := "#!/usr/bin/env bash\necho original\n"
	s.writeHook(original)

	s.Require().NoError(Install(s.repo, "/usr/local/bin/prenup", ModeReplace))

	s.Equal(original, s.readHook(s.hook+".prenup-backup"))
	s.Contains(s.readHook(s.hook), PrenupMarker)
}

func (s *HookInstallTestSuite) TestInstallChainKeepsOriginalAsLocal() {
	original := "#!/usr/bin/env bash\necho original\n"
	s.writeHook(original)

	s.Require().NoError(Install(s.repo, "/usr/local/bin/prenup", ModeChain))

	s.Equal(original, s.readHook(filepath.Join(s.repo, ".git", "hooks", "pre-commit.local")))

	managed := s.readHook(s.hook)
	s.Contains(managed, PrenupMarker)
	// In chain mode the managed hook must reference the saved-aside
	// local hook so it can delegate to the user's original script.
	s.Contains(managed, "pre-commit.local")
}

// TestInstallReplacesManagedHookEvenInAbortMode pins that ModeAbort's
// "refuse to clobber" behavior does NOT apply when the existing hook
// is itself a prenup-managed hook -- otherwise upgrades and binary
// path changes would be impossible without a separate uninstall step.
func (s *HookInstallTestSuite) TestInstallReplacesManagedHookEvenInAbortMode() {
	s.Require().NoError(Install(s.repo, "/old/prenup", ModeAbort))
	s.Require().NoError(Install(s.repo, "/new/prenup", ModeAbort))

	body := s.readHook(s.hook)
	s.Contains(body, "/new/prenup")
	s.NotContains(body, "/old/prenup")
}

func (s *HookInstallTestSuite) TestUninstallRestoresBackup() {
	original := "#!/usr/bin/env bash\necho original\n"
	s.writeHook(original)
	s.Require().NoError(Install(s.repo, "/usr/local/bin/prenup", ModeReplace))

	s.Require().NoError(Uninstall(s.repo))
	s.Equal(original, s.readHook(s.hook))
	_, err := os.Stat(s.hook + ".prenup-backup")
	s.True(os.IsNotExist(err), "backup must be removed after restore")
}

func (s *HookInstallTestSuite) TestUninstallRefusesUnmanagedHook() {
	s.writeHook("#!/usr/bin/env bash\necho hi\n")

	err := Uninstall(s.repo)
	s.Require().Error(err)
	s.Contains(err.Error(), "not installed by prenup")
}

func (s *HookInstallTestSuite) TestUninstallNoOpWhenMissing() {
	s.Require().NoError(Uninstall(s.repo))
}

func TestHookInstallSuite(t *testing.T) {
	suite.Run(t, new(HookInstallTestSuite))
}
