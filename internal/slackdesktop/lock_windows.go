//go:build windows

package slackdesktop

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// IsFileLockError checks if the error obtained from a file operation is a lock
// violation.
func IsFileLockError(err error) bool {
	if pathError, ok := errors.AsType[*os.PathError](err); ok {
		if errno, ok := errors.AsType[windows.Errno](pathError); ok {
			return errno == windows.ERROR_SHARING_VIOLATION
		}
	}
	return false
}
