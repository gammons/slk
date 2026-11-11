// internal/ui/draft_preview.go
//
// The draft preview: the draft in a compose box drawn as it will look
// once sent, in a few rows above the box (ctrl+r; spec
// docs/superpowers/specs/2026-11-09-compose-draft-preview-design.md).
// Its on/off state and the cached body live on compose.Model
// (PreviewOn, PreviewBody); this file decides what the rows say, and
// the two bottom-region renderers (view_messages.go, view_thread.go)
// size and stack them.
package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// draftPreviewRows is how many rows the draft preview may take, its
// title included: a third of the pane's inner height, at least 3, and
// no more than room (what is left above the messages area's 3-row
// minimum). Under 2 rows there is no room for the title and a line, so
// none.
func draftPreviewRows(inner, room int) int {
	n := min(max(3, inner/3), room)
	if n < 2 {
		return 0
	}
	return n
}

// draftPreview returns the draft preview's rows for compose box c, or
// "" when it is off or maxRows is under 2: a title rule, then either
// one dim note or the draft's rendered body, drawn with pane's names.
// Past maxRows the body keeps its end, under a dim "…" row when there
// is room for one besides a line. It wraps where the sent message will:
// the pane's render options add the avatar it would draw for the
// current user, which narrows the body. Every row
// is exactly width cells, even where the body renderer's 20-cell floor
// is wider than the pane.
func (a *App) draftPreview(c *compose.Model, pane contentPane, panel Panel, width, maxRows int, key compose.RenderKey) string {
	if !c.PreviewOn() || maxRows < 2 || width < 1 {
		return ""
	}
	muted := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted)
	title := "── draft preview "
	if w := ansi.StringWidth(title); w < width {
		title += strings.Repeat("─", width-w)
	}
	rows := []string{draftPreviewRow(muted.Render(ansi.Truncate(title, width, "")), width)}
	if note := a.draftPreviewNote(c, panel); note != "" {
		return rows[0] + "\n" + draftPreviewRow(muted.Render("  "+note), width)
	}
	// An avatar can arrive without a pane version change, so whether
	// the body wraps beside one is part of the key.
	opt := pane.DraftRenderOptions(a.currentUserID)
	key.Avatar = opt.Avatar != ""
	body := strings.Split(c.PreviewBody(width, key, func(mrkdwn string, width int) string {
		rendered := messages.RenderMessage(messages.MessageItem{Text: mrkdwn, UserID: a.currentUserID}, width,
			pane.DraftRenderContext(), opt)
		lines := strings.Split(rendered.Content, "\n")
		for i, line := range lines {
			lines[i] = draftPreviewRow("  "+line, width)
		}
		return strings.Join(lines, "\n")
	}), "\n")
	switch {
	case len(body) <= maxRows-1:
	case maxRows == 2:
		body = body[len(body)-1:]
	default:
		body = append([]string{draftPreviewRow(muted.Render("  …"), width)}, body[len(body)-(maxRows-2):]...)
	}
	return strings.Join(append(rows, body...), "\n")
}

// draftPreviewNote is the one dim line the draft preview shows instead
// of a body, or "" for a draft it renders: a slash command (an edit
// never runs one), a caption sent with files (sent as typed, without
// the conversion), or nothing to show.
func (a *App) draftPreviewNote(c *compose.Model, panel Panel) string {
	text := c.Value()
	editing := a.editing.IsActive() && a.editing.Panel() == panel
	if _, _, ok := parseSlashCommand(text); ok && !editing {
		return "slash command, not a message"
	}
	if len(c.Attachments()) > 0 {
		return "caption is sent as typed"
	}
	if strings.TrimSpace(text) == "" {
		return "nothing to preview"
	}
	return ""
}

// draftPreviewRow cuts row to width cells and pads it to width on the
// theme background. Tabs are expanded first, so lipgloss can't widen
// the row after the cut.
func draftPreviewRow(row string, width int) string {
	row = ansi.Truncate(strings.ReplaceAll(row, "\t", "    "), width, "")
	return lipgloss.NewStyle().Background(styles.Background).Width(width).Render(row)
}
