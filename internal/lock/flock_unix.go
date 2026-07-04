//go:build unix

package lock

import (
	"os"
	"syscall"
)

// flockNB attempts a non-blocking exclusive advisory lock. Returns
// syscall.EWOULDBLOCK if another process already holds the lock.
func flockNB(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// funlock releases a previously-acquired advisory lock.
func funlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
