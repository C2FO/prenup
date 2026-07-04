package cli

import (
	"runtime/debug"
	"sync"
)

// Version is the release version, set at link time with -X.
// When unset, debug.ReadBuildInfo() is consulted (for `go install ...@v1.2.3`).
var Version = "dev"

// resolvedVersion memoizes the version lookup so repeated calls (UI render,
// version check, summary) don't re-read build info on every access. Tests
// that mutate Version should call ResetVersionCacheForTest first.
var resolvedVersion = sync.OnceValue(func() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
})

// ResolvedVersion returns the string displayed in UIs and used for update checks.
func ResolvedVersion() string { return resolvedVersion() }

// ResetVersionCacheForTest clears the memoized version so tests can swap
// Version and observe the change. Not for production use.
func ResetVersionCacheForTest() {
	resolvedVersion = sync.OnceValue(func() string {
		if Version != "dev" {
			return Version
		}
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		return Version
	})
}
