// internal/ui/reducer_terminal.go
//
// Terminal-capability reducer for App.Update.
//
// Owns:
//
//	tea.ModeReportMsg - the terminal's answer to a DECRQM mode query.
//
// Bubbletea asks for synchronized output (2026) and unicode core
// (2027) at startup and switches its own renderer on the answers; the
// App only needs to know about 2027. When the terminal reports it,
// bubbletea lays frames out with grapheme widths instead of wcwidth,
// and kitty emulated placement (kitty_emulated.go) must parse each
// frame the same way or it would put images in the wrong columns.
package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/emoji"
)

var reduceTerminalModes reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(tea.ModeReportMsg)
	if !ok {
		return nil, false
	}
	if m.Mode == ansi.ModeUnicodeCore {
		debuglog.ImgRender("terminal mode report: unicode core (2027) value=%d", m.Value)
	}
	// The same condition bubbletea's event loop uses to switch its
	// renderer to ansi.GraphemeWidth.
	if m.Mode == ansi.ModeUnicodeCore &&
		(m.Value == ansi.ModeReset || m.Value == ansi.ModeSet || m.Value == ansi.ModePermanentlySet) {
		a.graphemeWidth = true
		// The terminal now measures emoji clusters as lipgloss does,
		// so the glyph fallback may draw multi-codepoint emoji
		// (emoji.ShouldRenderUnicode). The report arrives after the
		// first frames, so rows cached under the stricter rule are
		// rebuilt.
		if emoji.SetGraphemeClusters(true) {
			a.invalidateAllWinModelCaches()
			a.threadPanel.InvalidateCache()
			a.sidebar.InvalidateCache()
		}
	}
	return nil, true
}
