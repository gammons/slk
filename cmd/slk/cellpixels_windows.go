//go:build windows

package main

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/avatar"
)

// watchCellPixels does nothing on Windows, which has no SIGWINCH.
// The cell size stays the one-shot startup measurement: CSI 16 t when
// the terminal answers, else 8x16. There is no TIOCGWINSZ to re-read,
// and that probe has to run before bubbletea takes stdin.
func watchCellPixels(func(tea.Msg), *avatar.Cache) {
}
