//go:build !windows

package image

import "golang.org/x/sys/unix"

// winsizePixels queries the terminal's pixel dimensions via TIOCGWINSZ.
// Returns (0, 0, false) if the ioctl fails or the terminal does not
// report pixel sizes.
func winsizePixels(fd int) (pxW, pxH int, ok bool) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, false
	}
	if ws.Xpixel <= 0 || ws.Ypixel <= 0 || ws.Col <= 0 || ws.Row <= 0 {
		return 0, 0, false
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row), true
}
