//go:build !unix && !windows

package image

import "time"

// pollProbe stub for platforms with neither poll(2) nor a Windows
// console (probe_unix.go, probe_windows.go). Reporting failure keeps the
// caller on half-block and the 8x16 cell guess.
//
// Returns (false, 0, "unsupported_platform") unconditionally.
func pollProbe(fd int, timeout time.Duration, scan func([]byte) (bool, bool)) (bool, int, string) {
	_ = fd
	_ = timeout
	_ = scan
	return false, 0, "unsupported_platform"
}
