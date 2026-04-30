package image

import "strconv"

// CellPixels returns the (width, height) of a terminal cell in pixels.
// It honors $COLORTERM_CELL_WIDTH/$COLORTERM_CELL_HEIGHT, then attempts
// TIOCGWINSZ on the given fd (unix only), then falls back to (8, 16).
//
// fd is typically int(os.Stdout.Fd()). Pass -1 to skip the ioctl path.
// On Windows the ioctl path is unavailable; only the env override and
// fallback apply.
func CellPixels(fd int) (pxW, pxH int) {
	if w, ok := atoi(getenv("COLORTERM_CELL_WIDTH")); ok {
		if h, ok := atoi(getenv("COLORTERM_CELL_HEIGHT")); ok {
			return w, h
		}
	}
	if fd >= 0 {
		if w, h, ok := winsizePixels(fd); ok {
			return w, h
		}
	}
	return 8, 16
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
