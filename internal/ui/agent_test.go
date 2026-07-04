package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVersionLabel is a truth table for the human-readable version formatter.
// The three branches (unknown / dev build / release) all show up verbatim in
// user-facing output and in the agent_hint event, so a wording regression
// here would leak into external contracts.
func TestVersionLabel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version string
		want    string
	}{
		{name: "empty is unknown", version: "", want: "unknown"},
		{name: "dev gets development-build suffix", version: "dev", want: "dev (development build, not a tagged release)"},
		{name: "release passes through", version: "v1.2.3", want: "v1.2.3"},
		{name: "arbitrary token passes through", version: "abc123", want: "abc123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, VersionLabel(tc.version))
		})
	}
}

// TestAgentConstantsExposeStableStrings pins the exported strings that
// consumers may pattern-match against. These aren't secrets or long
// paragraphs of copy, but they are stable API surface as far as JSON
// consumers are concerned.
func TestAgentConstantsExposeStableStrings(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "prenup", Tool)
	assert.Equal(t, "https://github.com/c2fo/prenup", HomepageURL)
	assert.Equal(t, "dev", DevVersion)
	assert.Equal(t, "1", AgentHintSchema)
	assert.NotEmpty(t, Description)
	assert.NotEmpty(t, HookContextNote)
	assert.NotEmpty(t, JSONHint)
	assert.NotEmpty(t, FailureBypassHint)
}
