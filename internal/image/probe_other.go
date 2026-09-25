//go:build !unix

package image

import "time"

// pollProbe stub for non-unix platforms (Windows). Neither the kitty
// graphics protocol nor sixel is used in any meaningful Windows
// terminal, so reaching this stub means the env-detect was wrong;
// safest behavior is to report failure so the caller downgrades to
// halfblock.
//
// Returns (false, 0, "unsupported_platform") unconditionally.
func pollProbe(fd int, timeout time.Duration, scan func([]byte) (bool, bool)) (bool, int, string) {
	_ = fd
	_ = timeout
	_ = scan
	return false, 0, "unsupported_platform"
}
