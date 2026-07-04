package config

import _ "embed"

//go:embed schema.json
var embeddedSchema []byte

// Schema returns the embedded JSON schema describing the config.
// The bytes are stable across calls; callers must not mutate them.
func Schema() []byte {
	return embeddedSchema
}
