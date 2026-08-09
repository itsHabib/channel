//go:build unix

package store

import (
	"os"
	"syscall"
)

// lock takes an exclusive advisory flock on f, blocking until acquired.
// Advisory is enough: every writer goes through appendLine, and readers
// never lock.
func lock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func unlock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
