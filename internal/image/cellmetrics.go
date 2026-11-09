package image

import (
	"strconv"

	"github.com/gammons/slk/internal/debuglog"
)

// CellPixels returns the (width, height) of a terminal cell in pixels:
// MeasureCellPixels when it knows, else (8, 16).
//
// fd is typically int(os.Stdout.Fd()). Pass -1 to skip the ioctl path.
func CellPixels(fd int) (pxW, pxH int) {
	if w, h, ok := MeasureCellPixels(fd); ok {
		return w, h
	}
	debuglog.ImgRender("CellPixels: cell_w=8 cell_h=16 source=fallback (no env, no ioctl)")
	return 8, 16
}

// MeasureCellPixels returns the cell size from $COLORTERM_CELL_WIDTH and
// $COLORTERM_CELL_HEIGHT, else from TIOCGWINSZ on fd (unix only), and ok
// false when neither knows. On Windows there is no ioctl, so only the
// override applies; the caller then asks the terminal (ProbeCellSize).
func MeasureCellPixels(fd int) (pxW, pxH int, ok bool) {
	if w, ok := atoi(getenv("COLORTERM_CELL_WIDTH")); ok {
		if h, ok := atoi(getenv("COLORTERM_CELL_HEIGHT")); ok {
			debuglog.ImgRender("CellPixels: cell_w=%d cell_h=%d source=env_override", w, h)
			return w, h, true
		}
	}
	if fd >= 0 {
		if w, h, ok := winsizePixels(fd); ok {
			debuglog.ImgRender("CellPixels: cell_w=%d cell_h=%d source=ioctl fd=%d", w, h, fd)
			return w, h, true
		}
	}
	return 0, 0, false
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
