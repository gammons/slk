package main

import (
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/debuglog"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui"
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

// watchCellPixels keeps the cached cell size in pixels honest while the
// program runs, re-pointing the image encoders when the terminal starts
// reporting a different one.
//
// The one-shot startup read races terminals that resolve their font
// scale asynchronously: foot on a fractional-scaled output loads its
// fonts at the legacy wl_output scale first (Hyprland rounds 1.25 up to
// 2), writes that cell size to TIOCSWINSZ, and only settles at the
// fractional scale -- a second font load and a second TIOCSWINSZ -- tens
// of milliseconds later. slk's read reliably lands in between (it runs
// ~40ms after spawn), and every sixel image is encoded against the
// cached size, so the stale 2x measurement drew avatars and inline
// images at twice their reserved cell footprint.
//
// The settle lands as a real resize (the same window fits fewer, larger
// cells, so rows and columns change and the kernel raises SIGWINCH),
// which this watcher re-measures on. A short early ticker also covers
// rewrites that only move pixels (rows and columns unchanged, which the
// kernel does not signal) and a settle that lands before the first
// signal. After the settle window it is SIGWINCH-only: a later change
// comes from the user, and resizes signal.
//
// send is tea.Program.Send; it is safe from this goroutine, and the
// CellMetricsChangedMsg it delivers drops the render caches that baked
// the old pixels in on the Update goroutine.
func watchCellPixels(send func(tea.Msg), avatars *avatar.Cache) {
	fd := int(os.Stdout.Fd())
	sig := make(chan os.Signal, 8)
	signal.Notify(sig, syscall.SIGWINCH)
	defer signal.Stop(sig)

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	settled := time.After(3 * time.Second)

	for {
		select {
		case <-sig:
		case <-ticker.C:
		case <-settled:
			// The terminal settled: from here on, only a real
			// resize (or font zoom, which resizes) changes the
			// reported size, and both raise SIGWINCH.
			ticker.Stop()
			settled = nil
			continue
		}
		w, h, ok := imgpkg.MeasureCellPixels(fd)
		if ok {
			applyCellPixels(send, avatars, w, h)
		}
	}
}

// applyCellPixels re-points the image encoders when the terminal now
// reports a different cell size than the encoders work against, and
// invalidates the caches that sized renders against the old one.
// Returns whether anything changed. Consecutive calls with the same
// size are no-ops, so a SIGWINCH burst applies once.
func applyCellPixels(send func(tea.Msg), avatars *avatar.Cache, w, h int) bool {
	cw, ch := imgpkg.MeasuredCellPixels()
	if w == cw && h == ch {
		return false
	}
	debuglog.ImgRender("cell pixels changed: %dx%d -> %dx%d; re-pointing image encoders and dropping image caches", cw, ch, w, h)
	imgpkg.SetCellPixels(w, h)
	if avatars != nil {
		avatars.Invalidate()
	}
	// CellMetricsChangedMsg first: the caches drop before the next
	// frame renders. tea.ClearScreen() is a Msg the event loop answers
	// with a whole-screen redraw, which restores the text the erase
	// blanks under the old, oversized pixels. Never re-deliver a
	// captured WindowSizeMsg here: it races the real resize the cell
	// change usually arrives with, and the resize arm would accept the
	// stale dimensions as the terminal's, leaving the app laid out for
	// a screen that no longer matches.
	send(ui.CellMetricsChangedMsg{CellW: w, CellH: h})
	send(tea.ClearScreen())
	return true
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
