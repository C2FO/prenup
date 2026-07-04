package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSchemaAssetMatchesEmbedded guards against the two schema copies (the
// embedded one in this package and the public asset under ../../assets)
// drifting apart. They are duplicated so external consumers can fetch the
// canonical $id URL without pulling in the full Go module; the duplication
// is acceptable as long as a CI signal catches divergence immediately.
func TestSchemaAssetMatchesEmbedded(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)
	assetPath := filepath.Join(wd, "..", "..", "assets", "prenup.schema.json")
	asset, err := os.ReadFile(assetPath) //nolint:gosec // G304: path is built from a fixed filename relative to the test wd.
	require.NoError(t, err, "read public asset schema")
	require.Equal(t, string(asset), string(Schema()),
		"assets/prenup.schema.json and internal/config/schema.json must be byte-identical; update both when the schema changes")
}
