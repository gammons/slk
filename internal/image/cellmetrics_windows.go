//go:build windows

package image

// winsizePixels is a no-op on Windows; the Windows console does not
// expose pixel-per-cell metrics through a stable API. Callers fall
// back to the (8, 16) default.
func winsizePixels(fd int) (pxW, pxH int, ok bool) {
	return 0, 0, false
}
