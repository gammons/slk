//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/avatar"
	imgpkg "github.com/gammons/slk/internal/image"
)

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
