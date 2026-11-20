package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

// HookInstallTestSuite groups behavioral tests for Install/Uninstall.
// Each method gets a fresh hooks directory; tests do not need a real git
// repo because Install/Uninstall operate on an already-resolved hooks dir.
type HookInstallTestSuite struct {
	suite.Suite
	hooksDir string // absolute path to the temp hooks directory
	hook     string // absolute path to pre-commit inside hooksDir
}

func (s *HookInstallTestSuite) SetupTest() {
	s.hooksDir = filepath.Join(s.T().TempDir(), "hooks")
	s.Require().NoError(os.MkdirAll(s.hooksDir, 0o750))
	s.hook = filepath.Join(s.hooksDir, "pre-commit")
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
	s.Require().NoError(Install(s.hooksDir, "/usr/local/bin/prenup", ModeAbort))

	body := s.readHook(s.hook)
	s.Contains(body, PrenupMarker)
	s.Contains(body, `"/usr/local/bin/prenup"`)

	info, err := os.Stat(s.hook)
	s.Require().NoError(err)
	s.NotZero(info.Mode()&0o100, "hook must be executable")
}

// TestInstallCreatesMissingHooksDir covers hooks directories (e.g. a
// core.hooksPath target) that do not exist yet.
func (s *HookInstallTestSuite) TestInstallCreatesMissingHooksDir() {
	missing := filepath.Join(s.T().TempDir(), "not", "yet", "created")

	s.Require().NoError(Install(missing, "/usr/local/bin/prenup", ModeAbort))

	s.Contains(s.readHook(filepath.Join(missing, "pre-commit")), PrenupMarker)
}

func (s *HookInstallTestSuite) TestInstallAbortsOnExistingNonPrenupHook() {
	s.writeHook("#!/usr/bin/env bash\necho hi\n")

	err := Install(s.hooksDir, "/usr/local/bin/prenup", ModeAbort)
	var existsErr *ExistsError
	s.Require().ErrorAs(err, &existsErr, "expected ExistsError, got %v", err)
}

func (s *HookInstallTestSuite) TestInstallReplaceBacksUp() {
	original := "#!/usr/bin/env bash\necho original\n"
	s.writeHook(original)

	s.Require().NoError(Install(s.hooksDir, "/usr/local/bin/prenup", ModeReplace))

	s.Equal(original, s.readHook(s.hook+".prenup-backup"))
	s.Contains(s.readHook(s.hook), PrenupMarker)
}

func (s *HookInstallTestSuite) TestInstallChainKeepsOriginalAsLocal() {
	original := "#!/usr/bin/env bash\necho original\n"
	s.writeHook(original)

	s.Require().NoError(Install(s.hooksDir, "/usr/local/bin/prenup", ModeChain))

	s.Equal(original, s.readHook(filepath.Join(s.hooksDir, "pre-commit.local")))

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
	s.Require().NoError(Install(s.hooksDir, "/old/prenup", ModeAbort))
	s.Require().NoError(Install(s.hooksDir, "/new/prenup", ModeAbort))

	body := s.readHook(s.hook)
	s.Contains(body, "/new/prenup")
	s.NotContains(body, "/old/prenup")
}

func (s *HookInstallTestSuite) TestUninstallRestoresBackup() {
	original := "#!/usr/bin/env bash\necho original\n"
	s.writeHook(original)
	s.Require().NoError(Install(s.hooksDir, "/usr/local/bin/prenup", ModeReplace))

	s.Require().NoError(Uninstall(s.hooksDir))
	s.Equal(original, s.readHook(s.hook))
	_, err := os.Stat(s.hook + ".prenup-backup")
	s.True(os.IsNotExist(err), "backup must be removed after restore")
}

func (s *HookInstallTestSuite) TestUninstallRefusesUnmanagedHook() {
	s.writeHook("#!/usr/bin/env bash\necho hi\n")

	err := Uninstall(s.hooksDir)
	s.Require().Error(err)
	s.Contains(err.Error(), "not installed by prenup")
}

func (s *HookInstallTestSuite) TestUninstallNoOpWhenMissing() {
	s.Require().NoError(Uninstall(s.hooksDir))
}

func TestHookInstallSuite(t *testing.T) {
	suite.Run(t, new(HookInstallTestSuite))
}
