//go:build windows

package slackclient

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on f that other processes see
// (LockFileEx), waiting while another holder has it. A lock is per
// handle, so two opens of the same file in one process exclude each
// other too.
func lockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
}

// unlockFile releases lockFile's lock.
func unlockFile(f *os.File) error {
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
