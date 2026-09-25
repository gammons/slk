//go:build !windows

package slackdesktop

// IsFileLockError checks if the error obtained from a file operation is a lock
// violation. Not yet implemented for non-Windows operating systems.
func IsFileLockError(err error) bool {
	return false
}
