// internal/ui/view_thread.go
//
// Thread-region renderer for App.View (Phase 6h).
//
// The thread region is the rightmost column when threadVisible
// and threadWidth > 0. Same split-render pattern as the channel
// messages panel: bordered top region (replies + sides + top
// edge) cached on threadPanel.Version; bottom region (compose +
// sides + bottom edge) rendered fresh each frame so threadCompose
// keystrokes don't invalidate the (much larger) replies render.
//
// SetFocused MUST run BEFORE the cache hit-check (it bumps
// Version via dirty()). a.threadCompose.SetWidth must run
// regardless of cache hit so future renders see the right width.
//
// Caller is responsible for the visibility gate
// (a.threadVisible && threadWidth > 0 && !previewActive); this
// helper assumes the thread is visible.
package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// renderThreadRegion returns the composed thread-region panel
// string (top cached region + fresh bottom region, joined with
// a newline).
func (a *App) renderThreadRegion(frame panelLayoutFrame, themeVer int64) string {
	contentHeight := frame.ContentHeight
	threadWidth := frame.ThreadWidth
	threadBorder := frame.ThreadBorder

	threadFocused := a.focusedPanel == PanelThread && a.mode != ModeInsert
	// Push focus into the thread panel so the selected-reply "▌"
	// border dims when unfocused. MUST happen BEFORE the
	// panelCache hit-check (the cache key includes Version, which
	// SetFocused bumps via dirty()).
	a.threadPanel.SetFocused(threadFocused)
	threadComposeFocused := a.mode == ModeInsert && a.focusedPanel == PanelThread
	threadLayoutKey := themeVer<<2 | boolToInt(threadFocused)<<1 | boolToInt(threadComposeFocused)
	a.threadCompose.SetWidth(threadWidth - 2)

	threadComposeView := a.threadCompose.View(threadWidth-2, threadComposeFocused)
	if pickerView := a.threadCompose.EmojiPickerView(threadWidth - 2); pickerView != "" {
		threadComposeView = pickerView + "\n" + threadComposeView
	} else if mentionView := a.threadCompose.MentionPickerView(threadWidth - 2); mentionView != "" {
		threadComposeView = mentionView + "\n" + threadComposeView
	} else if channelView := a.threadCompose.ChannelPickerView(threadWidth - 2); channelView != "" {
		threadComposeView = channelView + "\n" + threadComposeView
	}
	threadComposeSpacer := lipgloss.NewStyle().Background(styles.Background).Width(threadWidth - 2).Render("")
	threadComposeView = threadComposeSpacer + "\n" + threadComposeView
	// Unlike the channel pane's typing row, no row is reserved for
	// this: it appears only while an app is working on this thread.
	if statusLine := a.renderThreadBotStatusLine(threadWidth - 2); statusLine != "" {
		threadComposeView = statusLine + "\n" + threadComposeView
	}
	threadComposeHeight := lipgloss.Height(threadComposeView)
	threadContentHeight := contentHeight - 2 - threadComposeHeight
	a.layout.SetThreadHeight(threadContentHeight)
	if threadContentHeight < 3 {
		threadContentHeight = 3
	}
	// Row layout within the pane: 0 top border, 1..threadContentHeight
	// replies, then the compose region through to the bottom border at
	// contentHeight-1 (no typing row is reserved in the thread pane).
	// Recorded after the minimum clamp and in PanelAt's paneY frame
	// (top border subtracted), mirroring the channel pane's compose
	// top.
	a.layout.SetThreadComposeTop(threadContentHeight)

	// Cached top region.
	threadTopVersion := a.threadPanel.Version()
	threadTopLayoutKey := threadLayoutKey | int64(threadComposeHeight)<<16
	threadTopHeight := threadContentHeight + 1 // +1 top border edge
	threadTopBordered := a.renderThreadTop(
		threadWidth, threadBorder, threadTopHeight, threadContentHeight,
		threadTopVersion, threadTopLayoutKey, threadFocused,
	)

	// Fresh bottom region.
	bottomBorderStyle := styles.UnfocusedBorder.Width(threadWidth).
		BorderTop(false).BorderLeft(true).BorderRight(true).BorderBottom(true)
	if threadFocused {
		bottomBorderStyle = styles.FocusedBorder.Width(threadWidth).
			BorderTop(false).BorderLeft(true).BorderRight(true).BorderBottom(true)
	}
	threadBottomInner := messages.ReapplyBgAfterResets(threadComposeView, messages.BgANSI())
	threadBottomBordered := exactSize(
		bottomBorderStyle.Render(threadBottomInner),
		threadWidth+threadBorder, threadComposeHeight+1, // +1 bottom border edge
	)

	return threadTopBordered + "\n" + threadBottomBordered
}

// renderThreadBotStatusLine is the open thread's app statuses, each as
// "<app name> <status>" the way Slack draws them, cut to one row of
// width cells. "" when no app is working on the thread.
func (a *App) renderThreadBotStatusLine(width int) string {
	if !a.typing.Enabled() {
		return ""
	}
	statuses := a.typing.ThreadBotStatuses(a.threadPanel.ChannelID(), a.threadPanel.ThreadTS())
	if len(statuses) == 0 {
		return ""
	}
	parts := make([]string, 0, len(statuses))
	for _, s := range statuses {
		// Fields: the app's line breaks would otherwise be rows.
		parts = append(parts, a.userNameFor(s.botUserID)+" "+strings.Join(strings.Fields(s.text), " "))
	}
	style := styles.TypingIndicator
	text := ansi.Truncate(strings.Join(parts, " · "), width-style.GetHorizontalFrameSize(), "…")
	return style.Width(width).Render(text)
}

// renderThreadTop is the cached top region of the thread panel
// (replies content + top border edge + side edges, no bottom
// edge). Cache-key triple: threadPanel.Version, (threadWidth,
// threadTopHeight) dimensions, threadTopLayoutKey (which mixes
// threadComposeHeight in so a compose-height flip invalidates).
//
// See lipgloss/v2 quirk note on the message-pane top region.
func (a *App) renderThreadTop(threadWidth, threadBorder, threadTopHeight, threadContentHeight int, threadTopVersion, threadTopLayoutKey int64, threadFocused bool) string {
	c := &a.renderCache.thread
	if c.hit(threadTopVersion, threadWidth, threadTopHeight, threadTopLayoutKey) {
		return c.output
	}
	topBorderStyle := styles.UnfocusedBorder.Width(threadWidth).
		BorderTop(true).BorderLeft(true).BorderRight(true).BorderBottom(false)
	if threadFocused {
		topBorderStyle = styles.FocusedBorder.Width(threadWidth).
			BorderTop(true).BorderLeft(true).BorderRight(true).BorderBottom(false)
	}
	threadView := a.threadPanel.View(threadContentHeight, threadWidth-2)
	threadView = messages.ReapplyBgAfterResets(threadView, messages.BgANSI())
	out := exactSize(
		topBorderStyle.Render(threadView),
		threadWidth+threadBorder, threadTopHeight,
	)
	c.store(out, threadTopVersion, threadWidth, threadTopHeight, threadTopLayoutKey)
	return out
}
