// internal/ui/view_messages.go
//
// Messages-region renderer for App.View (Phase 6g).
//
// The messages region is the second-from-left column (rail,
// sidebar, MESSAGES, thread). It has two top-level branches
// depending on a.view:
//
//	ViewThreads  -> threads-list panel (no compose, no typing
//	                line). Whole bordered panel is cached on
//	                threadsView.Version + layout key.
//	ViewChannels -> message pane + typing row + compose box, with
//	                a split-cache pattern: bordered top region
//	                (messages + top edge + sides only, no bottom
//	                edge) cached on messagepane.Version only;
//	                bottom region (typing + compose + bottom
//	                edge + sides) re-rendered fresh each frame.
//	                The two stack into a continuous bordered
//	                panel because BorderBottom(false) on the top
//	                + BorderTop(false) on the bottom lines up the
//	                border glyphs.
//
// PERF (see Phase 2g render-cache discussion + the split-rendering
// note in the channels branch): caching the entire bordered panel
// on a key mixing compose.Version was the dominant per-keystroke
// cost at large terminal sizes. Compose dirty()s on every
// keystroke, which would invalidate the entire bordered+exact-
// sized panel string and force 5-7 full O(height x width)
// ansi-aware rescans over a region that hadn't actually changed.
// The split fixes that: only the small typing+compose bottom
// region gets re-rendered per keystroke.
//
// Side effects (run unconditionally, even when previewActive
// suppresses the visible render):
//
//   - a.messagepane.SetFocused(msgFocused) -- must run BEFORE
//     the cache hit-check (SetFocused bumps Version on a flip
//     and the cache key includes Version).
//   - a.compose.SetWidth(msgWidth - 2) -- affects future compose
//     renders regardless of whether messages is visible right
//     now (preview overlay can close at any time).
//
// renderMessagesRegion returns "" when preview is active (the
// caller substitutes the preview overlay panel instead). The
// caller must guard against the empty-string return to avoid
// pushing a zero-width sentinel into the JoinHorizontal panels
// list.
package ui

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// renderMessagesRegion returns the composed messages-region
// panel string. Runs setup side effects unconditionally; returns
// "" when previewActive so the caller can substitute the preview
// overlay in its place.
func (a *App) renderMessagesRegion(frame panelLayoutFrame, themeVer int64, previewActive bool) string {
	contentHeight := frame.ContentHeight
	msgWidth := frame.MsgWidth
	msgBorder := frame.MsgBorder

	msgFocused := a.focusedPanel == PanelMessages && a.mode != ModeInsert
	// Push focus into the messages pane so the selected-message "▌"
	// border dims when unfocused. MUST happen BEFORE the cache
	// hit-check (the cache key includes Version, which SetFocused
	// bumps on a flip).
	a.messagepane.SetFocused(msgFocused)
	composeFocused := a.mode == ModeInsert && a.focusedPanel != PanelThread
	// Mix the view-mode bit into the layout key so a Channels<->
	// Threads switch invalidates the cached output (the cache is
	// otherwise indistinguishable across views at the same
	// focus/mode/theme).
	//
	// The focused-window id is mixed in at bit 32 (Phase 3): per-
	// window models have INDEPENDENT version counters, so after a
	// pointer-swap focus change the cache could otherwise serve the
	// previous window's frame on a (version, dims, key) collision.
	// Bit budget: bits 1-2 focus/view, themeVer from bit 3 (bumped
	// only on theme Apply — reaching the composeHeight bits at 16
	// would take 2^13 theme switches, and bit 32 would take 2^29),
	// composeHeight bits 16+ (terminal rows, < 2^10), window id
	// bits 32+ (wintree.LeafID increments per split; tiny).
	msgLayoutKey := int64(a.focusedWin)<<32 |
		themeVer<<3 |
		boolToInt(a.view == ViewThreads)<<2 |
		boolToInt(msgFocused)<<1
	a.compose.SetWidth(msgWidth - 2)

	if previewActive {
		// Preview owns the messages+thread region. Side effects
		// above still ran; visible render is suppressed.
		return ""
	}
	if a.view == ViewThreads {
		return a.renderThreadsViewPanel(msgWidth, msgBorder, contentHeight, msgFocused, msgLayoutKey)
	}
	if a.view == ViewActivity {
		return a.renderActivityViewPanel(msgWidth, msgBorder, contentHeight, msgFocused, msgLayoutKey)
	}
	return a.renderChannelMessagesPanel(msgWidth, msgBorder, contentHeight, msgFocused, composeFocused, msgLayoutKey)
}

// renderThreadsViewPanel handles the a.view == ViewThreads
// branch: a single bordered panel containing the threads list,
// no compose / typing row. Whole panel is cached on
// threadsView.Version.
//
// SetUserNames and SetSelfUserID MUST run BEFORE snapshotting
// threadsView.Version: both are equality-checked in the model,
// so identical input is a no-op, but reading Version() before
// the Set calls would mean the cache key reflects a pre-update
// version and the stored output would later miss its own key.
// (This was a real regression once -- see the verbose comment
// preserved verbatim below.)
//
// Channel names are NOT pushed here. They're fanned out from
// SetChannels when the channel list changes, which is rare
// relative to render frequency, so we keep that allocation off
// this hot path.
func (a *App) renderThreadsViewPanel(msgWidth, msgBorder, contentHeight int, msgFocused bool, msgLayoutKey int64) string {
	a.threadsView.SetUserNames(a.userNames)
	a.threadsView.SetSelfUserID(a.currentUserID)
	tvVersion := a.threadsView.Version()
	c := &a.renderCache.msgPanel
	if c.hit(tvVersion, msgWidth, contentHeight, msgLayoutKey) {
		return c.output
	}
	msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
	if msgFocused {
		msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
	}
	msgContentHeight := contentHeight - 2
	a.layout.SetMsgHeight(msgContentHeight)
	if msgContentHeight < 3 {
		msgContentHeight = 3
	}
	tvView := a.threadsView.View(msgContentHeight, msgWidth-2)
	tvView = messages.ReapplyBgAfterResets(tvView, messages.BgANSI())
	out := exactSize(
		msgBorderStyle.Render(tvView),
		msgWidth+msgBorder, contentHeight,
	)
	c.store(out, tvVersion, msgWidth, contentHeight, msgLayoutKey)
	return out
}

// renderActivityViewPanel handles the a.view == ViewActivity branch:
// a single bordered panel containing the Activity feed, no compose /
// typing row. Mirrors renderThreadsViewPanel but caches on its own
// panelCache slot (renderCache.activityPanel) keyed on
// activityView.Version.
//
// SetUserNames and SetSelfUserID MUST run BEFORE snapshotting
// activityView.Version (both are equality-checked no-ops in the model,
// so identical input is free, but reading Version() first would key the
// cache on a pre-update version). Channel names are fanned out from
// SetChannels (rare) and not pushed on this hot path.
func (a *App) renderActivityViewPanel(msgWidth, msgBorder, contentHeight int, msgFocused bool, msgLayoutKey int64) string {
	a.activityView.SetUserNames(a.userNames)
	a.activityView.SetSelfUserID(a.currentUserID)
	avVersion := a.activityView.Version()
	c := &a.renderCache.activityPanel
	if c.hit(avVersion, msgWidth, contentHeight, msgLayoutKey) {
		return c.output
	}
	msgBorderStyle := styles.UnfocusedBorder.Width(msgWidth)
	if msgFocused {
		msgBorderStyle = styles.FocusedBorder.Width(msgWidth)
	}
	msgContentHeight := contentHeight - 2
	a.layout.SetMsgHeight(msgContentHeight)
	if msgContentHeight < 3 {
		msgContentHeight = 3
	}
	avView := a.activityView.View(msgContentHeight, msgWidth-2)
	avView = messages.ReapplyBgAfterResets(avView, messages.BgANSI())
	out := exactSize(
		msgBorderStyle.Render(avView),
		msgWidth+msgBorder, contentHeight,
	)
	c.store(out, avVersion, msgWidth, contentHeight, msgLayoutKey)
	return out
}

// renderChannelMessagesPanel handles the a.view == ViewChannels
// branch: messages pane + typing row + compose box, with the
// split-cache pattern described in the file header.
//
// Border lipgloss/v2 quirk (preserved verbatim from the original
// inline comments): calling .BorderBottom(false) on a style that
// has BorderStyle() set disables ALL borders unless the other
// three sides are explicitly enabled with
// .BorderTop(true).BorderLeft(true).BorderRight(true). Without
// these the entire panel renders without any border at all.
func (a *App) renderChannelMessagesPanel(msgWidth, msgBorder, contentHeight int, msgFocused, composeFocused bool, msgLayoutKey int64) string {
	composeView := a.compose.View(msgWidth-2, composeFocused)
	// Inline pickers stack above the compose box. They're
	// mutually exclusive in compose.Update; emoji wins if
	// somehow both are.
	if pickerView := a.compose.EmojiPickerView(msgWidth - 2); pickerView != "" {
		composeView = pickerView + "\n" + composeView
	} else if mentionView := a.compose.MentionPickerView(msgWidth - 2); mentionView != "" {
		composeView = mentionView + "\n" + composeView
	} else if channelView := a.compose.ChannelPickerView(msgWidth - 2); channelView != "" {
		composeView = channelView + "\n" + composeView
	}
	// Background-colored spacer line above the compose box
	// (replaces MarginTop which produced unstyled/black margin
	// cells).
	composeSpacer := lipgloss.NewStyle().Background(styles.Background).Width(msgWidth - 2).Render("")
	composeView = composeSpacer + "\n" + composeView
	composeHeight := lipgloss.Height(composeView)
	// Always reserve one row above the compose box for the
	// typing indicator. When nobody is typing we render a blank
	// background-colored spacer in that row so the messages-pane
	// height stays constant -- otherwise a transient typing line
	// would shrink the messages area by one row, producing a
	// spurious "more below" indicator and a visible scroll jump.
	typingLine := a.renderTypingLine()
	if typingLine == "" {
		typingLine = lipgloss.NewStyle().
			Background(styles.Background).
			Width(msgWidth - 2).
			Render("")
	}
	typingHeight := 1
	bottomHeight := composeHeight + typingHeight
	msgContentHeight := contentHeight - 2 - bottomHeight
	a.layout.SetMsgHeight(msgContentHeight)
	if msgContentHeight < 3 {
		msgContentHeight = 3
	}

	// Cached top region: messages + top edge + side edges. The cache
	// stores the SELECTION-FREE bordered output (see ViewBare +
	// ApplySelectionToBordered below): selection mutations no longer
	// bump messagepane.Version, so a mouse-drag's per-cell extends do
	// not invalidate this cache. The selection is overlaid as a cheap
	// post-pass against the cached output.
	topPanelVersion := a.messagepane.Version()
	topLayoutKey := msgLayoutKey | int64(composeHeight)<<16
	topHeight := msgContentHeight + 1 // +1 for top border edge
	topBordered := a.renderMessagesTop(msgWidth, msgBorder, topHeight, msgContentHeight, topPanelVersion, topLayoutKey, msgFocused)
	topBordered = a.messagepane.ApplySelectionToBordered(topBordered, 1, 1)

	// Fresh bottom region: typing line + compose, with bottom
	// edge. Same lipgloss/v2 quirk applies.
	bottomBorderStyle := styles.UnfocusedBorder.Width(msgWidth).
		BorderTop(false).BorderLeft(true).BorderRight(true).BorderBottom(true)
	if msgFocused {
		bottomBorderStyle = styles.FocusedBorder.Width(msgWidth).
			BorderTop(false).BorderLeft(true).BorderRight(true).BorderBottom(true)
	}
	bottomInner := lipgloss.JoinVertical(lipgloss.Left, typingLine, composeView)
	bottomInner = messages.ReapplyBgAfterResets(bottomInner, messages.BgANSI())
	bottomBordered := exactSize(
		bottomBorderStyle.Render(bottomInner),
		msgWidth+msgBorder, bottomHeight+1, // +1 for bottom border edge
	)

	return topBordered + "\n" + bottomBordered
}

// renderMessagesTop is the cached top region of the channel
// messages panel (messages content + top border edge + side
// edges, no bottom edge). Cache-key triple: messagepane.Version,
// (msgWidth, topHeight) dimensions, topLayoutKey (which mixes
// composeHeight in so a compose-height flip invalidates correctly).
func (a *App) renderMessagesTop(msgWidth, msgBorder, topHeight, msgContentHeight int, topPanelVersion, topLayoutKey int64, msgFocused bool) string {
	c := &a.renderCache.msgTop
	if c.hit(topPanelVersion, msgWidth, topHeight, topLayoutKey) {
		return c.output
	}
	// ViewBare (not View) so the cached output is selection-free; the
	// caller composes ApplySelectionToBordered on top of the cache return,
	// keeping the bordered re-render off the mouse-drag hot path.
	//
	// PERF: every line of msgView is built to exactly msgWidth-2 display
	// cells (the messages model pads chrome, per-entry lines, spacer,
	// scroll indicators, and the loading hint), so we assemble the pane
	// border by concatenation rather than letting lipgloss re-measure all
	// ~N visible lines grapheme-by-grapheme every frame -- the dominant
	// scroll-frame cost on a wide terminal. borderedTopPane is gated by
	// TestBorderedTopPane_MatchesLipgloss for visual equivalence with the
	// old `padPaneToSize(borderStyle.Render(msgView), ...)` path.
	msgView := a.messagepane.ViewBare(msgContentHeight, msgWidth-2)
	msgView = messages.ReapplyBgAfterResets(msgView, messages.BgANSI())
	out := borderedTopPane(
		msgView,
		msgWidth-2, msgWidth+msgBorder, topHeight, msgFocused, styles.Background,
	)
	c.store(out, topPanelVersion, msgWidth, topHeight, topLayoutKey)
	return out
}
