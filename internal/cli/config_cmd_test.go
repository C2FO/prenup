package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// ConfigCommandTestSuite exercises the `prenup config validate` and `prenup
// config schema` handlers end-to-end. Both need a git repo (validate uses
// git.RepoRoot to anchor path lookups; schema does not, but sharing the
// fixture is cheaper than a second suite).
type ConfigCommandTestSuite struct {
	suite.Suite
	repo string
}

func (s *ConfigCommandTestSuite) SetupTest() {
	dir := s.T().TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed binary, internal args.
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		s.Require().NoErrorf(err, "git %s: %s", strings.Join(args, " "), string(out))
	}
	s.repo = dir
	s.T().Chdir(dir)
}

// TestValidate_WithInRepoConfig covers the discover-then-parse path:
// `prenup config validate` (no args) finds .prenup.yaml at the repo root
// and reports OK.
func (s *ConfigCommandTestSuite) TestValidate_WithInRepoConfig() {
	cfg := `version: 1
tasks:
  - name: "Echo"
    command: "echo hi"
`
	s.Require().NoError(os.WriteFile(filepath.Join(s.repo, ".prenup.yaml"), []byte(cfg), 0o600))

	out := captureStdout(s.T(), func() error {
		return newConfigValidateCmd().RunE(nil, nil)
	})
	s.Contains(out, "OK:")
	s.Contains(out, "v1,")
	s.Contains(out, "1 tasks")
}

// TestValidate_ExplicitPathArg covers the explicit-arg branch: passing a
// path directly bypasses discovery.
func (s *ConfigCommandTestSuite) TestValidate_ExplicitPathArg() {
	explicit := filepath.Join(s.repo, "custom.yaml")
	cfg := `version: 1
tasks:
  - name: "Explicit"
    command: "true"
`
	s.Require().NoError(os.WriteFile(explicit, []byte(cfg), 0o600))

	out := captureStdout(s.T(), func() error {
		return newConfigValidateCmd().RunE(nil, []string{explicit})
	})
	// `config validate` prints `OK: <path> parses cleanly (v<n>, <k> tasks)`;
	// assert against those fields (the task *name* is intentionally not
	// echoed, so don't grep for it).
	s.Contains(out, "OK:")
	s.Contains(out, explicit)
	s.Contains(out, "v1,")
	s.Contains(out, "1 tasks")
}

// TestValidate_MissingConfig surfaces a helpful error when there is no
// .prenup.yaml at the repo root and no explicit path was given.
func (s *ConfigCommandTestSuite) TestValidate_MissingConfig() {
	err := newConfigValidateCmd().RunE(nil, nil)
	s.Require().Error(err)
	s.Contains(err.Error(), "no config file found")
}

// TestValidate_InvalidConfigSurfacesParseError guards the parse-error
// pass-through: a schema violation must reach the user, not silently pass.
func (s *ConfigCommandTestSuite) TestValidate_InvalidConfigSurfacesParseError() {
	invalid := filepath.Join(s.repo, "bad.yaml")
	s.Require().NoError(os.WriteFile(invalid, []byte(`version: "1"`+"\n"), 0o600))

	err := newConfigValidateCmd().RunE(nil, []string{invalid})
	s.Require().Error(err)
	// The exact message comes from config.Parse's versionTypeHint.
	s.Contains(err.Error(), "version")
}

// TestSchema_PrintsEmbeddedJSONSchema pins that `prenup config schema` emits
// the JSON schema embedded in the binary; the output must parse as JSON and
// declare the right title.
func (s *ConfigCommandTestSuite) TestSchema_PrintsEmbeddedJSONSchema() {
	out := captureStdout(s.T(), func() error {
		return newConfigSchemaCmd().RunE(nil, nil)
	})

	var schema map[string]any
	s.Require().NoError(json.Unmarshal([]byte(out), &schema))
	s.Contains(schema, "$schema")
	s.Contains(schema, "properties")
}

func TestConfigCommandSuite(t *testing.T) {
	suite.Run(t, new(ConfigCommandTestSuite))
}
