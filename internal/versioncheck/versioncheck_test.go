package versioncheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractBaseVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "clean version", input: "v1.2.0", expected: "v1.2.0"},
		{name: "git describe with commits after tag", input: "v1.2.0-1-g71f9e6d", expected: "v1.2.0"},
		{name: "git describe with many commits", input: "v1.2.0-15-gabcdef1", expected: "v1.2.0"},
		{name: "actual prerelease (not git describe)", input: "v1.2.0-beta.1", expected: "v1.2.0-beta.1"},
		{name: "actual prerelease rc", input: "v1.2.0-rc1", expected: "v1.2.0-rc1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractBaseVersion(tt.input))
		})
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "already has v prefix", input: "v1.2.3", expected: "v1.2.3"},
		{name: "missing v prefix", input: "1.2.3", expected: "v1.2.3"},
		{name: "with whitespace", input: "  v1.2.3  ", expected: "v1.2.3"},
		{name: "whitespace without v", input: "  1.2.3  ", expected: "v1.2.3"},
		{name: "prerelease version", input: "v1.2.3-beta.1", expected: "v1.2.3-beta.1"},
		{name: "empty string", input: "", expected: "v"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeVersion(tt.input))
		})
	}
}

func TestCheck_DevBuild(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "dev version", version: "dev"},
		{name: "empty version", version: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Check(context.Background(), tt.version, "")

			assert.Equal(t, tt.version, result.CurrentVersion)
			assert.Empty(t, result.LatestVersion)
			assert.Empty(t, result.LatestURL)
			assert.False(t, result.IsOutdated)
			require.Error(t, result.Error)
			assert.Contains(t, result.Error.Error(), "development build")
		})
	}
}

func TestCheck_WithMockServer(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		releases       []Release
		statusCode     int
		wantOutdated   bool
		wantAhead      bool
		wantError      bool
		errorContains  string
	}{
		{
			name:           "current version is latest",
			currentVersion: "v1.2.0",
			releases: []Release{
				{TagName: "v1.2.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.2.0"},
				{TagName: "v1.1.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.1.0"},
			},
			statusCode: http.StatusOK,
		},
		{
			name:           "current version is outdated",
			currentVersion: "v1.1.0",
			releases: []Release{
				{TagName: "v1.2.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.2.0"},
				{TagName: "v1.1.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.1.0"},
			},
			statusCode:   http.StatusOK,
			wantOutdated: true,
		},
		{
			name:           "current version without v prefix is outdated",
			currentVersion: "1.1.0",
			releases: []Release{
				{TagName: "v1.2.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.2.0"},
			},
			statusCode:   http.StatusOK,
			wantOutdated: true,
		},
		{
			name:           "current version is newer than latest",
			currentVersion: "v1.3.0",
			releases: []Release{
				{TagName: "v1.2.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.2.0"},
			},
			statusCode: http.StatusOK,
			wantAhead:  true,
		},
		{
			name:           "git-describe version with commits after latest tag is ahead",
			currentVersion: "v1.2.0-1-g71f9e6d",
			releases: []Release{
				{TagName: "v1.2.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.2.0"},
			},
			statusCode: http.StatusOK,
			wantAhead:  true,
		},
		{
			name:           "git-describe version IS outdated when newer release exists",
			currentVersion: "v1.1.0-5-g1234567",
			releases: []Release{
				{TagName: "v1.2.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.2.0"},
			},
			statusCode:   http.StatusOK,
			wantOutdated: true,
		},
		{
			name:           "patch version comparison",
			currentVersion: "v1.2.0",
			releases: []Release{
				{TagName: "v1.2.1", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.2.1"},
			},
			statusCode:   http.StatusOK,
			wantOutdated: true,
		},
		{
			name:           "API returns error status",
			currentVersion: "v1.2.0",
			statusCode:     http.StatusInternalServerError,
			wantError:      true,
			errorContains:  "status 500",
		},
		{
			name:           "no prenup releases found",
			currentVersion: "v1.2.0",
			releases: []Release{
				{TagName: "rowtater/v1.0.0", HTMLURL: "https://github.com/c2fo/rowtater/releases/tag/v1.0.0"},
			},
			statusCode:    http.StatusOK,
			wantError:     true,
			errorContains: "no prenup releases found",
		},
		{
			name:           "empty releases list",
			currentVersion: "v1.2.0",
			releases:       []Release{},
			statusCode:     http.StatusOK,
			wantError:      true,
			errorContains:  "no prenup releases found",
		},
		{
			name:           "skips other tools to find prenup",
			currentVersion: "v1.1.0",
			releases: []Release{
				{TagName: "releasegen/v1.0.0", HTMLURL: "https://github.com/c2fo/releasegen/releases/tag/v1.0.0"},
				{TagName: "v1.2.0", HTMLURL: "https://github.com/c2fo/prenup/releases/tag/v1.2.0"},
			},
			statusCode:   http.StatusOK,
			wantOutdated: true,
		},
		{
			name:           "selects highest semver regardless of API order",
			currentVersion: "v1.2.0",
			releases: []Release{
				{TagName: "v1.1.0", HTMLURL: "https://example.com/a"},
				{TagName: "v1.3.0", HTMLURL: "https://example.com/b"},
				{TagName: "v1.2.0", HTMLURL: "https://example.com/c"},
			},
			statusCode:   http.StatusOK,
			wantOutdated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "application/vnd.github.v3+json", r.Header.Get("Accept"))
				w.WriteHeader(tt.statusCode)
				if tt.releases != nil {
					_ = json.NewEncoder(w).Encode(tt.releases)
				}
			}))
			defer server.Close()

			result := checkAgainstReleasesAPI(context.Background(), tt.currentVersion, "", server.URL)

			assert.Equal(t, tt.currentVersion, result.CurrentVersion)
			assert.Equal(t, tt.wantOutdated, result.IsOutdated, "IsOutdated mismatch")
			assert.Equal(t, tt.wantAhead, result.IsAhead, "IsAhead mismatch")

			if tt.wantError {
				require.Error(t, result.Error)
				if tt.errorContains != "" {
					assert.Contains(t, result.Error.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, result.Error)
				assert.NotEmpty(t, result.LatestVersion)
				assert.NotEmpty(t, result.LatestURL)
			}
		})
	}
}

func TestCheck_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]Release{{TagName: "v1.0.0"}})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := checkAgainstReleasesAPI(ctx, "v1.0.0", "", server.URL)

	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "context canceled")
}

func TestCheck_WithGitHubToken(t *testing.T) {
	var receivedToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]Release{
			{TagName: "v1.0.0", HTMLURL: "https://example.com"},
		})
	}))
	defer server.Close()

	result := checkAgainstReleasesAPI(context.Background(), "v1.0.0", "my-secret-token", server.URL)

	require.NoError(t, result.Error)
	assert.Equal(t, "Bearer my-secret-token", receivedToken)
}

func TestCheck_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1700000000")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	result := checkAgainstReleasesAPI(context.Background(), "v1.0.0", "", server.URL)

	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "rate limit exceeded")
	assert.Contains(t, result.Error.Error(), "1700000000")
}

func TestCheck_InvalidCurrentVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]Release{
			{TagName: "v1.0.0", HTMLURL: "https://example.com"},
		})
	}))
	defer server.Close()

	result := checkAgainstReleasesAPI(context.Background(), "not-a-version", "", server.URL)

	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "invalid version format")
}
