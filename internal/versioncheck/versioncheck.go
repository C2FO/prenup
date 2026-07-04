// Package versioncheck compares the running prenup binary version to the latest GitHub release.
package versioncheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	repoOwner  = "c2fo"
	repoName   = "prenup"
	tagPrefix  = "v"
	apiTimeout = 10 * time.Second
	// per_page=100 is the GitHub API maximum. We do not paginate: the
	// newest valid semver-tagged release is virtually always in the most
	// recent 100 entries, and prenup would rather report "no release
	// found" than fan out multiple HTTP calls on a pre-commit hook path.
	releasesURL = "https://api.github.com/repos/%s/%s/releases?per_page=100"
)

// Release represents a GitHub release.
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckResult contains the result of a version check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	LatestURL      string
	IsOutdated     bool
	IsAhead        bool // Running unreleased commits ahead of latest release
	Error          error
}

// Check compares the current version against the latest GitHub release for prenup.
// If githubToken is non-empty, it is sent as a Bearer token (needed for private repos).
func Check(ctx context.Context, currentVersion, githubToken string) CheckResult {
	apiURL := fmt.Sprintf(releasesURL, repoOwner, repoName)
	return checkAgainstReleasesAPI(ctx, currentVersion, githubToken, apiURL)
}

func checkAgainstReleasesAPI(ctx context.Context, currentVersion, githubToken, apiURL string) CheckResult {
	result := CheckResult{
		CurrentVersion: currentVersion,
	}

	if currentVersion == "dev" || currentVersion == "" {
		result.Error = errors.New("running development build (version not set)")
		return result
	}

	latest, url, err := fetchLatestPrenupRelease(ctx, githubToken, apiURL)
	if err != nil {
		result.Error = fmt.Errorf("failed to check for updates: %w", err)
		return result
	}

	result.LatestVersion = latest
	result.LatestURL = url

	current := normalizeVersion(currentVersion)
	latestNorm := normalizeVersion(latest)
	currentBase := extractBaseVersion(current)

	if !semver.IsValid(currentBase) || !semver.IsValid(latestNorm) {
		result.Error = fmt.Errorf("invalid version format: current=%q, latest=%q", currentBase, latestNorm)
		return result
	}

	cmp := semver.Compare(currentBase, latestNorm)
	switch {
	case cmp < 0:
		result.IsOutdated = true
	case cmp == 0 && current != currentBase:
		result.IsAhead = true
	case cmp > 0:
		result.IsAhead = true
	}

	return result
}

func fetchLatestPrenupRelease(ctx context.Context, githubToken, apiURL string) (version, url string, err error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	// GitHub's REST API requires a User-Agent; requests without one can be
	// rejected with 403 and are harder to trace in server logs.
	req.Header.Set("User-Agent", repoName)
	if githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+githubToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return "", "", fmt.Errorf("GitHub API rate limit exceeded (%s)",
			formatRateLimitReset(resp.Header.Get("X-RateLimit-Reset")))
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", "", err
	}

	var latestVer string
	var latestURL string
	for _, r := range releases {
		if !strings.HasPrefix(r.TagName, tagPrefix) {
			continue
		}
		ver := strings.TrimPrefix(r.TagName, tagPrefix)
		normalized := normalizeVersion(ver)
		if !semver.IsValid(normalized) {
			continue
		}
		if latestVer == "" || semver.Compare(normalized, normalizeVersion(latestVer)) > 0 {
			latestVer = ver
			latestURL = r.HTMLURL
		}
	}

	if latestVer == "" {
		return "", "", errors.New("no prenup releases found")
	}

	return latestVer, latestURL, nil
}

// formatRateLimitReset renders the GitHub X-RateLimit-Reset header (a Unix
// timestamp in seconds) as both a human-readable UTC time and a duration
// until reset. Falls back to the raw value on parse failure so a
// misformatted header never masks the underlying rate-limit condition.
func formatRateLimitReset(raw string) string {
	secs, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return "reset time unavailable"
	}
	reset := time.Unix(secs, 0).UTC()
	wait := time.Until(reset).Round(time.Second)
	if wait < 0 {
		wait = 0
	}
	return fmt.Sprintf("resets in %s at %s", wait, reset.Format(time.RFC3339))
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func extractBaseVersion(v string) string {
	parts := strings.Split(v, "-")
	if len(parts) >= 3 {
		if len(parts[len(parts)-1]) > 1 && parts[len(parts)-1][0] == 'g' {
			return parts[0]
		}
	}
	return v
}
