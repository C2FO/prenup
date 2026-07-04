//go:build !unix

package lock

import "os"

// This stub keeps the lock package (and therefore the whole module)
// compiling on non-unix platforms, where flock(2) is unavailable. Prenup's
// task execution already assumes a unix-like environment (tasks run via
// `bash -c`), so the cross-repo serialization guarantee is unix-only by
// design; here Acquire degrades to a no-op lock rather than failing to
// build. The package doc calls out that real advisory locking is unix-only.

// flockNB is a no-op on non-unix platforms: it always "succeeds" so a
// single prenup run proceeds, but it provides no cross-process exclusion.
func flockNB(*os.File) error { return nil }

// funlock is a no-op on non-unix platforms.
func funlock(*os.File) error { return nil }
