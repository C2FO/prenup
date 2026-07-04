package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v1 example taken from the pre-v2 prenup.example.yaml.
const v1ConfigExample = `
exclude:
  - ".github"
  - "*.yaml"

tasks:
  - name: "Run tests"
    default_selected: true
    command: "go test ./..."
    per_module: true

  - name: "Generate CLI docs"
    default_selected: true
    per_module: true
    command: "make docs"
    working_dir: "{{.repo_root}}"
    output_patterns:
      - "doc/cmd/*.md"
    stage_output: true
`

func TestMigrateV1Preserves(t *testing.T) {
	cfg, out, err := MigrateV1([]byte(v1ConfigExample))
	require.NoError(t, err)

	assert.Equal(t, 2, cfg.Version)
	require.Len(t, cfg.Tasks, 2)
	assert.Equal(t, "Run tests", cfg.Tasks[0].Name)
	assert.True(t, cfg.Tasks[0].PerModule)
	assert.True(t, cfg.Tasks[1].StageOutput)

	assert.True(t, strings.HasPrefix(string(out), "# Prenup configuration."))
	assert.Contains(t, string(out), "version: 2")
	assert.Contains(t, string(out), "Run tests")
}

func TestMigrateThenParseRoundTrips(t *testing.T) {
	_, out, err := MigrateV1([]byte(v1ConfigExample))
	require.NoError(t, err)

	reparsed, err := Parse(out, "migrated")
	require.NoError(t, err)
	require.Len(t, reparsed.Tasks, 2)
	assert.Equal(t, "Run tests", reparsed.Tasks[0].Name)
}
