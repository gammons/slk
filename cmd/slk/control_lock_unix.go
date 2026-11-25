//go:build !windows

package main

import (
	"os"
	"syscall"
)

// lockFile blocks until it holds an exclusive lock on f. The lock goes
// away with the file descriptor, so a crashed slk never leaves it held.
func lockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			return err
		}
	}
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
