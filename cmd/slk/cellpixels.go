package main

import (
	"os"
	"strings"
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
//
// measured is false when the size is the 8x16 fallback. Kitty emulated
// placement needs a size the terminal knows, so it is off then.
func startupCellPixels(proto imgpkg.Protocol) (pxW, pxH int, measured bool) {
	if w, h, ok := imgpkg.MeasureCellPixels(int(os.Stdout.Fd())); ok {
		return w, h, true
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
				return w, h, true
			}
		}
	}
	w, h := imgpkg.CellPixels(-1)
	return w, h, false
}

// shouldProbeCellSize says whether startupCellPixels asks the terminal.
func shouldProbeCellSize(proto imgpkg.Protocol, tmux, zellij string, isTerminal bool) bool {
	if proto != imgpkg.ProtoSixel && proto != imgpkg.ProtoKitty {
		return false
	}
	return tmux == "" && zellij == "" && isTerminal
}

// emulationAllowed says whether a terminal that picked emulated
// placement may use it. On Windows WezTerm runs behind ConPTY, where
// direct placements are untested, so there it stays on what it had
// before (sixel) unless kitty_placement = "emulated" asks for it.
func emulationAllowed(goos, cfg string) bool {
	return goos != "windows" || strings.EqualFold(strings.TrimSpace(cfg), "emulated")
}

// emulatedPlacementIfMeasured is the protocol and placement for a
// session that picked kitty emulated placement: kept when the cell size
// was measured. Otherwise emulation is off and the protocol is what such
// a terminal (WezTerm) had before: sixel in auto mode, half-block when
// image_protocol forced kitty.
func emulatedPlacementIfMeasured(measured, autoProtocol bool) (imgpkg.Protocol, imgpkg.KittyPlacement) {
	switch {
	case measured:
		return imgpkg.ProtoKitty, imgpkg.KittyEmulated
	case autoProtocol:
		return imgpkg.ProtoSixel, imgpkg.KittyNative
	default:
		return imgpkg.ProtoHalfBlock, imgpkg.KittyNative
	}
}
