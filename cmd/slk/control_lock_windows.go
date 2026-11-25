//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile blocks until it holds an exclusive lock on f. The lock goes
// away with the handle, so a crashed slk never leaves it held.
func lockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
}

func unlockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
