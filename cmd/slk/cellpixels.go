package main

import (
	"os"
	"time"

	"golang.org/x/term"

	"github.com/gammons/slk/internal/debuglog"
	imgpkg "github.com/gammons/slk/internal/image"
)

// startupCellPixels is the terminal's cell size in pixels for image
// encoding: $COLORTERM_CELL_WIDTH/HEIGHT or TIOCGWINSZ when they know,
// else the terminal's own answer to CSI 16 t, else 8x16. The query is
// what gives a size on Windows, which has no TIOCGWINSZ, and it matters
// there: Windows Terminal's default cell is 10x20, so encoding for 8x16
// draws every sixel image a fifth too small for the rows it was given.
//
// Only asked when images are drawn in pixels (sixel or kitty; half-block
// is sized in cells), only on a terminal, and never inside a
// multiplexer, which may answer for itself or not at all. Must run
// before bubbletea takes over stdin.
func startupCellPixels(proto imgpkg.Protocol) (int, int) {
	if w, h, ok := imgpkg.MeasureCellPixels(int(os.Stdout.Fd())); ok {
		return w, h
	}
	if shouldProbeCellSize(proto, os.Getenv("TMUX"), os.Getenv("ZELLIJ"), term.IsTerminal(int(os.Stdin.Fd()))) {
		if state, err := term.MakeRaw(int(os.Stdin.Fd())); err != nil {
			debuglog.ImgRender("cell size probe skipped: cannot enter raw mode: %v", err)
		} else {
			w, h, ok := imgpkg.ProbeCellSize(os.Stdout, os.Stdin, 200*time.Millisecond)
			if rerr := term.Restore(int(os.Stdin.Fd()), state); rerr != nil {
				debuglog.ImgRender("term restore after cell size probe: %v", rerr)
			}
			if ok {
				debuglog.ImgRender("CellPixels: cell_w=%d cell_h=%d source=csi_16t", w, h)
				return w, h
			}
		}
	}
	return imgpkg.CellPixels(-1)
}

// shouldProbeCellSize says whether startupCellPixels asks the terminal.
func shouldProbeCellSize(proto imgpkg.Protocol, tmux, zellij string, isTerminal bool) bool {
	if proto != imgpkg.ProtoSixel && proto != imgpkg.ProtoKitty {
		return false
	}
	return tmux == "" && zellij == "" && isTerminal
}
