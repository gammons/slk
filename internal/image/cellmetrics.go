package image

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// CellPixels returns the (width, height) of a terminal cell in pixels.
// It honors $COLORTERM_CELL_WIDTH/$COLORTERM_CELL_HEIGHT, then attempts
// TIOCGWINSZ on the given fd, then falls back to (8, 16).
//
// fd is typically int(os.Stdout.Fd()). Pass -1 to skip the ioctl path.
func CellPixels(fd int) (pxW, pxH int) {
	if w, ok := atoi(getenv("COLORTERM_CELL_WIDTH")); ok {
		if h, ok := atoi(getenv("COLORTERM_CELL_HEIGHT")); ok {
			return w, h
		}
	}
	if fd >= 0 {
		if ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); err == nil {
			if ws.Xpixel > 0 && ws.Ypixel > 0 && ws.Col > 0 && ws.Row > 0 {
				return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
			}
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
