//go:build !unix

package store

import "os"

// lock is a no-op off unix: O_APPEND single-write appends stay atomic for
// typical message sizes, and the POC targets darwin/linux. Revisit with
// LockFileEx if Windows ever matters.
func lock(_ *os.File) error {
	return nil
}

func unlock(_ *os.File) {}
