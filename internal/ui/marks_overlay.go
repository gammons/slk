// internal/ui/marks_overlay.go
//
// App-side glue for the marks overlay widget (internal/ui/marks/):
// populating its rows from the merged marks store and the setter for
// the [marks] show_jump_overlay option. The overlay is rendered by
// applyOverlays (view_overlays.go) and driven by handleMarksMode.
package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/marks"
)

// SetShowJumpOverlay toggles whether beginning a jump (') shows the
// marks list. Defaults to on. The option never affects the :marks
// command, which always shows the overlay.
func (a *App) SetShowJumpOverlay(b bool) {
	a.showJumpOverlay = b
}

// marksRows converts the active workspace's marks into overlay rows.
// Only the preview snapshot recorded at mark time is used — no channel
// lookup, no message fetch — so the list renders offline and right
// after a restart. A failed table read still yields the session rows,
// with the error.
func (a *App) marksRows() ([]marks.Row, error) {
	stored, err := a.marks.List(a.activeTeamID)
	rows := make([]marks.Row, 0, len(stored))
	for _, m := range stored {
		rows = append(rows, marks.Row{
			Letter:      m.Letter,
			ChannelName: m.ChannelName,
			AuthorName:  previewText(m.AuthorName),
			Excerpt:     previewText(m.Excerpt),
		})
	}
	return rows, err
}

// openMarksOverlay populates the overlay from the active workspace and
// shows it. Used by the ' chord (when the jump overlay is enabled) and
// by the :marks command. Returns a toast when the saved marks could not
// be read.
func (a *App) openMarksOverlay() tea.Cmd {
	cmd := a.refreshMarksOverlay()
	a.marksOverlay.Open()
	return cmd
}

// refreshMarksOverlay repopulates the overlay in place, keeping it
// open — used after a delete action so the remaining marks stay
// visible. Returns a toast when the saved marks could not be read.
func (a *App) refreshMarksOverlay() tea.Cmd {
	rows, err := a.marksRows()
	a.marksOverlay.SetRows(rows)
	if err != nil {
		return toastWithClear(a, "Failed to load saved marks", 3*time.Second)
	}
	return nil
}

// previewText renders stored snapshot text for one overlay row:
// :shortcode: sequences resolved to glyphs (emoji.Sprint, the same
// helper the sidebar uses), and every run of whitespace collapsed to a
// single space.
//
// The collapse is not cosmetic. Slack message bodies routinely contain
// newlines, and a raw excerpt spilled down the overlay as several lines,
// breaking the one-row-per-mark layout and the box borders with it.
//
// Applied at display time rather than at mark time so marks recorded
// before this existed render correctly too, and so the stored snapshot
// stays the faithful original.
func previewText(s string) string {
	return strings.Join(strings.Fields(emoji.Sprint(s)), " ")
}
