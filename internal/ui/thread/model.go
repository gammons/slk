package thread

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	emoji "github.com/kyokomi/emoji/v2"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/selection"
	"github.com/gammons/slk/internal/ui/styles"
)

var thickLeftBorder = lipgloss.Border{Left: "▌"}

// viewEntry is a pre-rendered reply, matching the shape used by
// internal/ui/messages.viewEntry: linesNormal is the bordered styled
// content split on "\n"; linesPlain is the column-aligned mirror of
// the UNBORDERED content; contentColOffset is the column where content
// begins inside the BORDERED viewContent (= 1 for replies, which carry
// the thick left border applied during the viewContent build step).
type viewEntry struct {
	linesNormal      []string
	linesPlain       []messages.PlainLine
	height           int
	replyIdx         int
	contentColOffset int
}

// Model represents the thread panel UI component.
// It displays a parent message and its replies with cursor navigation.
type Model struct {
	parent            messages.MessageItem
	replies           []messages.MessageItem
	channelID         string
	threadTS          string
	selected          int
	focused           bool
	avatarFn          messages.AvatarFunc
	userNames         map[string]string
	vp                viewport.Model
	reactionNavActive bool
	reactionNavIndex  int

	// Render cache -- pre-rendered reply entries (unbordered content
	// captured per reply; borders are applied later when assembling
	// viewContent).
	cache         []viewEntry
	cacheWidth    int
	cacheReplyLen int

	// entryOffsets / totalLines mirror the FULLY BORDERED viewContent:
	// entryOffsets[i] is the absolute line index inside viewContent where
	// reply i starts. Inter-reply separators occupy a single line that is
	// NOT inside any entry's [start, start+height) range; selection
	// overlay/extraction skips them naturally.
	entryOffsets []int
	totalLines   int

	// View-level cache -- bordered content ready for viewport
	viewContent       string
	viewSelected      int
	viewWidth         int
	viewHeight        int
	viewCacheValid    bool
	selectedStartLine int
	selectedEndLine   int

	// Mouse selection state. selRange is the user's drag selection.
	// replyIDToIdx maps reply TS -> entry index in m.cache for O(1)
	// anchor resolution; rebuilt on every cache build. lastViewHeight is
	// captured during View() so ScrollHintForDrag knows the reply-area
	// bounds without needing the App to plumb them through.
	selRange       selection.Range
	hasSelection   bool
	replyIDToIdx   map[string]int
	lastViewHeight int
	// chromeHeight is the number of visual rows occupied by the thread's
	// chrome (header + separator + parent message + separator) at the top
	// of View()'s output. It's stored so click / drag handlers can offset
	// pane-local y coordinates into the reply-content coordinate space the
	// rest of the model operates in.
	chromeHeight int

	// unreadBoundaryTS is the Slack timestamp the user has already read up
	// to in this thread. Replies whose TS > unreadBoundaryTS are considered
	// new; a "── new ──" landmark is inserted between the last read reply
	// and the first new one. Empty string disables the landmark. Set via
	// SetUnreadBoundary, typically with the parent channel's last_read_ts
	// at the moment the thread is opened.
	unreadBoundaryTS string

	// version increments on every state change that could alter View() output.
	version int64
}

// New creates an empty thread panel.
func New() *Model {
	return &Model{}
}

// Version returns a counter that increments any time the View() output could
// change. Used by App's panel-output cache.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes or style updates.
func (m *Model) InvalidateCache() {
	m.cache = nil
	m.viewCacheValid = false
	m.dirty()
}

// SetThread populates the thread panel with a parent message and replies.
// The cursor starts at the bottom (newest reply). When the channel/thread
// identity changes (i.e. the user is opening a different thread, not just
// receiving a refresh of the current one), the unread boundary is cleared
// so a fresh boundary can be set by the caller via SetUnreadBoundary.
func (m *Model) SetThread(parent messages.MessageItem, replies []messages.MessageItem, channelID, threadTS string) {
	if channelID != m.channelID || threadTS != m.threadTS {
		m.unreadBoundaryTS = ""
	}
	m.ClearSelection()
	m.parent = parent
	m.replies = replies
	m.channelID = channelID
	m.threadTS = threadTS
	m.selected = 0
	m.InvalidateCache()
}

// SetUnreadBoundary sets the timestamp the user has already read up to in
// this thread. A "── new ──" landmark is rendered between the last reply
// with TS <= boundary and the first reply with TS > boundary. Pass "" to
// clear the boundary. Typically called by the App right after SetThread,
// using the parent channel's last_read_ts as the boundary.
func (m *Model) SetUnreadBoundary(ts string) {
	if m.unreadBoundaryTS == ts {
		return
	}
	m.unreadBoundaryTS = ts
	m.viewCacheValid = false
	m.dirty()
}

// AddReply appends a reply to the thread and scrolls to the bottom.
// We always advance `selected` to the new last index so the incoming
// reply is visible regardless of where the user had scrolled.
func (m *Model) AddReply(msg messages.MessageItem) {
	// Idempotent on TS -- same race-defense rationale as
	// messages.Model.AppendMessage: optimistic add (HTTP response) and
	// WS echo can arrive in either order, and a caller-side dedup map
	// can lose the race if the echo lands first.
	if msg.TS != "" {
		for i := len(m.replies) - 1; i >= 0; i-- {
			if m.replies[i].TS == msg.TS {
				return
			}
		}
	}
	m.replies = append(m.replies, msg)
	m.InvalidateCache()
	m.selected = len(m.replies) - 1
}

// Clear resets all thread state.
func (m *Model) Clear() {
	m.ClearSelection()
	m.parent = messages.MessageItem{}
	m.replies = nil
	m.channelID = ""
	m.threadTS = ""
	m.selected = 0
	m.InvalidateCache()
}

// ThreadTS returns the thread timestamp.
func (m *Model) ThreadTS() string {
	return m.threadTS
}

// ChannelID returns the channel ID this thread belongs to.
func (m *Model) ChannelID() string {
	return m.channelID
}

// IsEmpty returns true if no thread is loaded.
func (m *Model) IsEmpty() bool {
	return m.threadTS == ""
}

// ReplyCount returns the number of replies.
func (m *Model) ReplyCount() int {
	return len(m.replies)
}

// ParentMsg returns the parent message.
func (m *Model) ParentMsg() messages.MessageItem {
	return m.parent
}

// Replies returns the slice of currently-loaded thread replies. Used by
// the App for cross-pane lookups (e.g. resolving an attachment for the
// preview overlay when the click landed inside the thread panel).
func (m *Model) Replies() []messages.MessageItem {
	return m.replies
}

// UpdateMessageInPlace finds a reply by TS and replaces its text,
// marking it edited. Returns true if found.
func (m *Model) UpdateMessageInPlace(ts, newText string) bool {
	for i, r := range m.replies {
		if r.TS == ts {
			m.replies[i].Text = newText
			m.replies[i].IsEdited = true
			m.InvalidateCache()
			return true
		}
	}
	return false
}

// RemoveMessageByTS removes a reply by TS, adjusting the selected
// index so it remains valid. Returns true if found.
func (m *Model) RemoveMessageByTS(ts string) bool {
	for i, r := range m.replies {
		if r.TS == ts {
			m.replies = append(m.replies[:i], m.replies[i+1:]...)
			if i <= m.selected && m.selected > 0 {
				m.selected--
			}
			if m.selected >= len(m.replies) {
				if len(m.replies) == 0 {
					m.selected = 0
				} else {
					m.selected = len(m.replies) - 1
				}
			}
			m.InvalidateCache()
			return true
		}
	}
	return false
}

// UpdateParentInPlace updates the thread parent's text and marks it
// edited if its TS matches. Returns true if updated.
func (m *Model) UpdateParentInPlace(ts, newText string) bool {
	if m.parent.TS != ts {
		return false
	}
	m.parent.Text = newText
	m.parent.IsEdited = true
	m.InvalidateCache()
	return true
}

// SetFocused sets whether the thread panel has focus.
func (m *Model) SetFocused(focused bool) {
	if m.focused != focused {
		m.focused = focused
		m.dirty()
	}
}

// Focused returns whether the thread panel has focus.
func (m *Model) Focused() bool {
	return m.focused
}

// SetAvatarFunc sets the avatar rendering function.
func (m *Model) SetAvatarFunc(fn messages.AvatarFunc) {
	m.avatarFn = fn
}

// SetUserNames sets the user ID -> display name map for mention resolution.
func (m *Model) SetUserNames(names map[string]string) {
	m.userNames = names
	m.InvalidateCache()
}

// SelectedReply returns the currently selected reply, or nil if none.
func (m *Model) SelectedReply() *messages.MessageItem {
	if m.selected < 0 || m.selected >= len(m.replies) {
		return nil
	}
	return &m.replies[m.selected]
}

// MoveUp moves the selection cursor up one reply.
// ScrollUp scrolls the thread viewport up by n lines without changing the
// selected reply.
func (m *Model) ScrollUp(n int) {
	if n > 0 {
		m.vp.ScrollUp(n)
		m.dirty()
	}
}

// ScrollDown scrolls the thread viewport down by n lines without changing the
// selected reply.
func (m *Model) ScrollDown(n int) {
	if n > 0 {
		m.vp.ScrollDown(n)
		m.dirty()
	}
}

func (m *Model) MoveUp() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected > 0 {
		m.selected--
		m.dirty()
	}
}

// MoveDown moves the selection cursor down one reply.
func (m *Model) MoveDown() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected < len(m.replies)-1 {
		m.selected++
		m.dirty()
	}
}

func (m *Model) IsAtBottom() bool {
	return m.selected >= len(m.replies)-1
}

// GoToTop moves the selection to the first reply.
func (m *Model) GoToTop() {
	if m.selected != 0 {
		m.selected = 0
		m.dirty()
	}
}

// GoToBottom moves the selection to the last reply.
func (m *Model) GoToBottom() {
	if len(m.replies) > 0 && m.selected != len(m.replies)-1 {
		m.selected = len(m.replies) - 1
		m.dirty()
	}
}

// EnterReactionNav activates reaction navigation on the selected reply.
func (m *Model) EnterReactionNav() {
	if reply := m.SelectedReply(); reply != nil && len(reply.Reactions) > 0 {
		m.reactionNavActive = true
		m.reactionNavIndex = 0
		m.InvalidateCache()
	}
}

// ExitReactionNav deactivates reaction navigation.
func (m *Model) ExitReactionNav() {
	m.reactionNavActive = false
	m.reactionNavIndex = 0
	m.InvalidateCache()
}

// ReactionNavActive returns whether reaction navigation is active.
func (m *Model) ReactionNavActive() bool {
	return m.reactionNavActive
}

// ReactionNavLeft moves the reaction cursor left with wrapping.
func (m *Model) ReactionNavLeft() {
	reply := m.SelectedReply()
	if reply == nil {
		return
	}
	total := len(reply.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex - 1 + total) % total
	m.InvalidateCache()
}

// ReactionNavRight moves the reaction cursor right with wrapping.
func (m *Model) ReactionNavRight() {
	reply := m.SelectedReply()
	if reply == nil {
		return
	}
	total := len(reply.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex + 1) % total
	m.InvalidateCache()
}

// SelectedReaction returns the currently highlighted reaction emoji name,
// or isPlus=true if the "+" button is highlighted.
func (m *Model) SelectedReaction() (emojiName string, isPlus bool) {
	reply := m.SelectedReply()
	if reply == nil {
		return "", false
	}
	if m.reactionNavIndex >= len(reply.Reactions) {
		return "", true
	}
	return reply.Reactions[m.reactionNavIndex].Emoji, false
}

// ClampReactionNav ensures the reaction nav index is within bounds.
func (m *Model) ClampReactionNav() {
	reply := m.SelectedReply()
	if reply == nil || len(reply.Reactions) == 0 {
		m.ExitReactionNav()
		return
	}
	total := len(reply.Reactions) + 1
	if m.reactionNavIndex >= total {
		m.reactionNavIndex = total - 1
	}
	m.InvalidateCache()
}

// UpdateReaction updates the reaction state for a specific message in the thread.
func (m *Model) UpdateReaction(messageTS, emojiName, userID string, remove bool) {
	for i, reply := range m.replies {
		if reply.TS == messageTS {
			if remove {
				for j, r := range reply.Reactions {
					if r.Emoji == emojiName {
						r.Count--
						if r.Count <= 0 {
							m.replies[i].Reactions = append(reply.Reactions[:j], reply.Reactions[j+1:]...)
						} else {
							r.HasReacted = false
							m.replies[i].Reactions[j] = r
						}
						break
					}
				}
			} else {
				found := false
				for j, r := range reply.Reactions {
					if r.Emoji == emojiName {
						r.Count++
						r.HasReacted = true
						m.replies[i].Reactions[j] = r
						found = true
						break
					}
				}
				if !found {
					m.replies[i].Reactions = append(m.replies[i].Reactions, messages.ReactionItem{
						Emoji:      emojiName,
						Count:      1,
						HasReacted: true,
					})
				}
			}
			m.InvalidateCache()
			if m.reactionNavActive {
				m.ClampReactionNav()
			}
			return
		}
	}
}

// ClickAt handles a mouse click at the given y-coordinate (the pane-local
// y returned by App.panelAt — measured from the panel's top border, so
// y=0..chromeHeight-1 sits inside the chrome (header / separator / parent
// message / separator) and y=chromeHeight onward is reply content). Clicks
// in the chrome are ignored.
func (m *Model) ClickAt(y int) {
	if len(m.replies) == 0 || len(m.cache) == 0 {
		return
	}
	contentY := y - m.chromeHeight
	if contentY < 0 {
		return // click on chrome — ignore
	}
	absoluteY := contentY + m.vp.YOffset()

	currentLine := 0
	for _, e := range m.cache {
		h := e.height
		if h == 0 {
			h = 1
		}
		if absoluteY >= currentLine && absoluteY < currentLine+h {
			if m.selected != e.replyIdx {
				m.selected = e.replyIdx
				m.viewCacheValid = false
				m.dirty()
			}
			return
		}
		currentLine += h
		// Inter-reply separators occupy 1 line in the bordered viewContent
		// but are NOT inside any cache entry. Skip a line between entries
		// so click coordinates stay in sync with viewContent.
		currentLine++
	}
}

// BeginSelectionAt anchors a new selection at the given pane-local
// coordinates (App.panelAt's coordinate system: 0 == panel content top,
// just below the border). Clicks on the chrome (header / separator /
// parent message / separator at pane-local y < chromeHeight) are
// ignored — there's no reply content there to anchor on. The selection
// becomes Active. Out-of-range inputs that don't land on any cache
// entry are silently no-ops.
func (m *Model) BeginSelectionAt(viewportY, x int) {
	if viewportY < m.chromeHeight {
		return
	}
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	m.selRange = selection.Range{Start: a, End: a, Active: true}
	m.hasSelection = true
	m.dirty()
}

// ExtendSelectionAt updates the End anchor of the active selection.
// No-op if BeginSelectionAt was never called or the coordinates fall
// on a non-entry row (inter-reply separator).
func (m *Model) ExtendSelectionAt(viewportY, x int) {
	if !m.hasSelection {
		return
	}
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	m.selRange.End = a
	m.dirty()
}

// EndSelection finalizes the drag, returning the plain-text contents
// of the selection. Returns ok=false when the selection is empty
// (a click without drag).
func (m *Model) EndSelection() (string, bool) {
	if !m.hasSelection {
		return "", false
	}
	m.selRange.Active = false
	if m.selRange.IsEmpty() {
		m.hasSelection = false
		m.selRange = selection.Range{}
		m.dirty()
		return "", false
	}
	text := m.SelectionText()
	m.dirty()
	if text == "" {
		return "", false
	}
	return text, true
}

// ClearSelection removes the current selection, if any.
func (m *Model) ClearSelection() {
	if !m.hasSelection {
		return
	}
	m.hasSelection = false
	m.selRange = selection.Range{}
	m.dirty()
}

// HasSelection reports whether a selection is currently active or
// pinned-on-screen post-drag.
func (m *Model) HasSelection() bool { return m.hasSelection }

// ScrollHintForDrag returns -1 if the cursor is within 1 row of the top
// edge of the reply-content area, +1 if within 1 row of the bottom, else 0.
// The incoming viewportY is pane-local (0 == top of panel content, just
// below the border); we offset by m.chromeHeight so "top edge" is measured
// against the reply content, not the chrome (header / separator / parent
// message / separator). A cursor sitting on the chrome is treated the same
// as the top content row, so an upward drag keeps auto-scrolling toward
// older replies.
func (m *Model) ScrollHintForDrag(viewportY int) int {
	h := m.lastViewHeight
	if h <= 0 {
		return 0
	}
	contentY := viewportY - m.chromeHeight
	if contentY <= 0 {
		return -1
	}
	if contentY >= h-1 {
		return +1
	}
	return 0
}

// absoluteLineAt converts a pane-local y coordinate to an absolute line
// index inside m.viewContent (the bordered content the viewport scrolls
// through). The incoming viewportY is what App.panelAt returns: zero at
// the panel's content top (just below the border), so rows
// 0..chromeHeight-1 are the thread chrome (header / separator / parent /
// separator) and chromeHeight onward is reply content. We strip the chrome
// offset before mapping into viewContent lines, clamping negative
// (in-chrome) values to the first content line. The result is clamped to
// [0, totalLines-1] for out-of-range inputs.
func (m *Model) absoluteLineAt(viewportY int) int {
	contentY := viewportY - m.chromeHeight
	if contentY < 0 {
		contentY = 0
	}
	abs := contentY + m.vp.YOffset()
	if abs < 0 {
		abs = 0
	}
	if m.totalLines > 0 && abs >= m.totalLines {
		abs = m.totalLines - 1
	}
	return abs
}

// anchorAt converts an absolute line + display column into an Anchor.
// `col` is the mouse's display column (relative to the reply area's
// content). We subtract contentColOffset to get the plain column, then
// clamp to plain-line width. Returns ok=false when no entry covers the
// line (inter-reply separator) or when the cache is empty.
func (m *Model) anchorAt(absLine, col int) (selection.Anchor, bool) {
	for i, e := range m.cache {
		start := m.entryOffsets[i]
		end := start + e.height
		if absLine < start || absLine >= end {
			continue
		}
		j := absLine - start
		plainCol := col - e.contentColOffset
		if plainCol < 0 {
			plainCol = 0
		}
		if j < len(e.linesPlain) {
			if w := messages.DisplayWidthOfPlain(e.linesPlain[j]); plainCol > w {
				plainCol = w
			}
		}
		var msgID string
		if e.replyIdx >= 0 && e.replyIdx < len(m.replies) {
			msgID = m.replies[e.replyIdx].TS
		}
		return selection.Anchor{MessageID: msgID, Line: j, Col: plainCol}, true
	}
	return selection.Anchor{}, false
}

// resolveAnchor returns the absolute line + plain col for an Anchor.
// Returns ok=false when the reply is no longer present.
func (m *Model) resolveAnchor(a selection.Anchor) (absLine, col int, ok bool) {
	if a.MessageID == "" {
		return 0, 0, false
	}
	idx, found := m.replyIDToIdx[a.MessageID]
	if !found || idx >= len(m.cache) {
		return 0, 0, false
	}
	e := m.cache[idx]
	if a.Line < 0 || a.Line >= e.height {
		return 0, 0, false
	}
	return m.entryOffsets[idx] + a.Line, a.Col, true
}

// SelectionText extracts the plain-text contents of the current
// selection. Trailing whitespace is trimmed per line; a final trailing
// newline is removed. Multi-rune grapheme clusters are preserved
// intact.
func (m *Model) SelectionText() string {
	if !m.hasSelection || m.selRange.IsEmpty() {
		return ""
	}
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 {
		return ""
	}
	if loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return ""
	}
	var b strings.Builder
	for i, e := range m.cache {
		entryStart := m.entryOffsets[i]
		entryEnd := entryStart + e.height
		if entryEnd <= loLine {
			continue
		}
		if entryStart > hiLine {
			break
		}
		for j, plain := range e.linesPlain {
			absLine := entryStart + j
			if absLine < loLine {
				continue
			}
			if absLine > hiLine {
				break
			}
			from := 0
			to := messages.DisplayWidthOfPlain(plain)
			if absLine == loLine {
				from = loCol
			}
			if absLine == hiLine {
				to = hiCol
			}
			seg := messages.SliceColumns(plain, from, to)
			seg = strings.TrimRight(seg, " ")
			b.WriteString(seg)
			if absLine != hiLine {
				b.WriteByte('\n')
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// applySelectionOverlay returns viewContent with selection-style
// applied to the visible columns of the active selection range.
// Operates on the FULLY-BORDERED viewContent because the viewport
// will slice and render it. Plain columns from linesPlain map to
// display columns by adding the entry's contentColOffset (= 1 for
// reply rows, since the thick left border occupies display column 0).
//
// Inter-reply separator lines are NOT inside any cache entry's
// [start, end) range, so they are skipped naturally.
func (m *Model) applySelectionOverlay(content string) string {
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 || loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return content
	}
	selStyle := styles.SelectionStyle()
	lines := strings.Split(content, "\n")
	for absLine := loLine; absLine <= hiLine && absLine < len(lines); absLine++ {
		entryIdx := -1
		for i := range m.cache {
			start := m.entryOffsets[i]
			if absLine >= start && absLine < start+m.cache[i].height {
				entryIdx = i
				break
			}
		}
		if entryIdx < 0 {
			continue // separator line between replies
		}
		e := m.cache[entryIdx]
		j := absLine - m.entryOffsets[entryIdx]
		if j < 0 || j >= len(e.linesPlain) {
			continue
		}
		plain := e.linesPlain[j]
		styled := lines[absLine]

		from := 0
		to := messages.DisplayWidthOfPlain(plain)
		if absLine == loLine {
			from = loCol
		}
		if absLine == hiLine {
			to = hiCol
		}
		if from < 0 {
			from = 0
		}
		if to > messages.DisplayWidthOfPlain(plain) {
			to = messages.DisplayWidthOfPlain(plain)
		}
		if from >= to {
			continue
		}
		dispFrom := from + e.contentColOffset
		dispTo := to + e.contentColOffset

		styledWidth := ansi.StringWidth(styled)
		if dispFrom >= styledWidth {
			continue
		}
		if dispTo > styledWidth {
			dispTo = styledWidth
		}
		prefix := ansi.Cut(styled, 0, dispFrom)
		suffix := ansi.Cut(styled, dispTo, styledWidth)
		seg := messages.SliceColumns(plain, from, to)
		lines[absLine] = prefix + selStyle.Render(seg) + suffix
	}
	return strings.Join(lines, "\n")
}

// View renders the thread panel content without a border.
// The parent App is responsible for adding the border.
func (m *Model) View(height, width int) string {
	if m.IsEmpty() {
		return lipgloss.NewStyle().
			Width(width).
			Height(height).
			Background(styles.Background).
			Foreground(styles.TextMuted).
			Render("No thread selected")
	}

	// Header
	replyLabel := "replies"
	if len(m.replies) == 1 {
		replyLabel = "reply"
	}
	header := lipgloss.NewStyle().
		Width(width).
		Background(styles.Background).
		Foreground(styles.TextPrimary).
		Bold(true).
		Render(fmt.Sprintf("Thread  %d %s", len(m.replies), replyLabel))

	separator := lipgloss.NewStyle().
		Width(width).
		Background(styles.Background).
		Foreground(styles.Border).
		Render(strings.Repeat("-", width))

	// Parent message
	parentContent := m.renderThreadMessage(m.parent, width, m.userNames, false)

	chrome := header + "\n" + separator + "\n" + parentContent + "\n" + separator
	chromeHeight := lipgloss.Height(chrome)
	m.chromeHeight = chromeHeight

	// chromeHeight already counts every visual row of `chrome`; joining with
	// a single "\n" between chrome and the viewport produces exactly
	// chromeHeight+vp.Height() lines total. No extra row is consumed.
	replyAreaHeight := height - chromeHeight
	if replyAreaHeight < 1 {
		replyAreaHeight = 1
	}
	m.lastViewHeight = replyAreaHeight

	if len(m.replies) == 0 {
		empty := lipgloss.NewStyle().
			Width(width).
			Height(replyAreaHeight).
			Background(styles.Background).
			Foreground(styles.TextMuted).
			Render("No replies yet")
		result := chrome + "\n" + empty
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Background(styles.Background).Render(result)
	}

	// Rebuild render cache if replies or width changed
	if m.cache == nil || m.cacheWidth != width || m.cacheReplyLen != len(m.replies) {
		m.cache = make([]viewEntry, 0, len(m.replies))
		if m.replyIDToIdx == nil {
			m.replyIDToIdx = make(map[string]int, len(m.replies))
		} else {
			for k := range m.replyIDToIdx {
				delete(m.replyIDToIdx, k)
			}
		}
		for i, reply := range m.replies {
			rendered := m.renderThreadMessage(reply, width, m.userNames, i == m.selected)
			m.cache = append(m.cache, viewEntry{
				linesNormal:      strings.Split(rendered, "\n"),
				linesPlain:       messages.PlainLines(rendered),
				height:           lipgloss.Height(rendered),
				replyIdx:         i,
				contentColOffset: 1, // border applied during viewContent build
			})
			m.replyIDToIdx[reply.TS] = i
		}
		m.cacheWidth = width
		m.cacheReplyLen = len(m.replies)
		m.viewCacheValid = false
	}

	// Check if view-level cache (bordered content) can be reused
	if !m.viewCacheValid || m.viewSelected != m.selected || m.viewWidth != width || m.viewHeight != replyAreaHeight {
		// Pre-compute border styles for this frame (avoids NewStyle per reply)
		borderFill := lipgloss.NewStyle().Background(styles.Background)
		borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Background).BorderBackground(styles.Background)
		borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Accent).BorderBackground(styles.Background)
		// Visible separator drawn between replies. Uses the panel border color
		// over the themed background so it reads as a divider but doesn't
		// fight with the panel's outer border. Falls through full content
		// width.
		separatorStyle := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.Border)
		replySeparator := separatorStyle.Render(strings.Repeat("─", width))

		// "── new ──" landmark inserted just before the first reply with
		// TS > unreadBoundaryTS. Mirrors the channel pane's new-message
		// line (messages/model.go:642-664). The landmark line is NOT
		// inside any cache entry, matching the inter-reply separator
		// convention so selection overlay / extraction skip it naturally.
		landmarkStyle := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.Error).
			Bold(true).
			Align(lipgloss.Center)
		newLandmark := landmarkStyle.Render("── new ──")
		landmarkInserted := false

		var allRows []string
		startLine := 0
		endLine := 0
		currentLine := 0

		// entryOffsets / totalLines mirror the BORDERED viewContent. Each
		// reply takes lipgloss.Height(borderedReply) lines (== e.height,
		// since the thick left border is purely horizontal padding), plus
		// 1 line per inter-reply separator.
		m.entryOffsets = m.entryOffsets[:0]

		for i, e := range m.cache {
			// Insert the new-reply landmark before the first reply whose
			// TS exceeds the unread boundary. We check this BEFORE
			// recording the entry offset so the landmark sits above
			// reply i.
			if !landmarkInserted && m.unreadBoundaryTS != "" && i < len(m.replies) && m.replies[i].TS > m.unreadBoundaryTS {
				allRows = append(allRows, newLandmark)
				currentLine++
				landmarkInserted = true
			}

			content := strings.Join(e.linesNormal, "\n")
			if i == m.selected {
				startLine = currentLine
				filled := borderFill.Width(width - 1).Render(content)
				content = borderSelect.Render(filled)
			} else {
				filled := borderFill.Width(width - 1).Render(content)
				content = borderInvis.Render(filled)
			}
			h := lipgloss.Height(content)
			m.entryOffsets = append(m.entryOffsets, currentLine)
			if i == m.selected {
				endLine = currentLine + h
			}
			allRows = append(allRows, content)
			currentLine += h
			// Separator between replies (not after the last). Separator
			// lines are NOT inside any cache entry — selection overlay /
			// extraction skip them naturally because no entry covers
			// them.
			if i < len(m.cache)-1 {
				allRows = append(allRows, replySeparator)
				currentLine++
			}
		}

		m.viewContent = strings.Join(allRows, "\n")
		m.viewSelected = m.selected
		m.viewWidth = width
		m.viewHeight = replyAreaHeight
		m.selectedStartLine = startLine
		m.selectedEndLine = endLine
		m.totalLines = currentLine
		m.viewCacheValid = true
	}

	// Configure viewport
	m.vp.SetWidth(width)
	m.vp.SetHeight(replyAreaHeight)
	m.vp.KeyMap = viewport.KeyMap{}
	m.vp.SetContent(m.viewContent)

	// Scroll to keep selected item visible
	if m.selectedEndLine > m.vp.YOffset()+m.vp.Height() {
		m.vp.SetYOffset(m.selectedEndLine - m.vp.Height())
	}
	if m.selectedStartLine < m.vp.YOffset() {
		m.vp.SetYOffset(m.selectedStartLine)
	}

	// Overlay the active selection on top of viewContent. Done after
	// scroll-snapping so YOffset is settled, then re-apply the overlayed
	// content to the viewport for the final View() render.
	if m.hasSelection {
		m.vp.SetContent(m.applySelectionOverlay(m.viewContent))
	}

	result := chrome + "\n" + m.vp.View()
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Background(styles.Background).Render(result)
}

// renderThreadMessage renders a single message for the thread panel.
func (m *Model) renderThreadMessage(msg messages.MessageItem, width int, userNames map[string]string, isSelected bool) string {
	line := styles.Username.Render(msg.UserName) + lipgloss.NewStyle().Background(styles.Background).Render("  ") + styles.Timestamp.Render(msg.Timestamp)

	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	text := styles.MessageText.Render(messages.WordWrap(messages.RenderSlackMarkdown(msg.Text, userNames), contentWidth))

	var reactionLine string
	if len(msg.Reactions) > 0 {
		var pills []string
		for i, r := range msg.Reactions {
			// Drop any skin-tone modifier suffix so the pill renders the
			// base emoji at a well-known width. Skin-toned glyphs render
			// inconsistently across terminals and tend to break border
			// alignment regardless of how we measure them.
			emojiStr := emoji.Sprint(":" + emojiutil.StripSkinTone(r.Emoji) + ":")
			pillText := fmt.Sprintf("%s%d", emojiStr, r.Count)
			var style lipgloss.Style
			if isSelected && m.reactionNavActive && i == m.reactionNavIndex {
				style = styles.ReactionPillSelected
			} else if r.HasReacted {
				style = styles.ReactionPillOwn
			} else {
				style = styles.ReactionPillOther
			}
			pills = append(pills, style.Render(pillText))
		}
		if isSelected && m.reactionNavActive {
			plusStyle := styles.ReactionPillPlus
			if m.reactionNavIndex >= len(msg.Reactions) {
				plusStyle = styles.ReactionPillSelected
			}
			pills = append(pills, plusStyle.Render("+"))
		}
		bgSpace := lipgloss.NewStyle().Background(styles.Background).Render(" ")
		var reactionLines []string
		currentLine := ""
		for i, pill := range pills {
			candidate := currentLine
			if i > 0 {
				candidate += bgSpace
			}
			candidate += pill
			if emojiutil.Width(candidate) > contentWidth && currentLine != "" {
				reactionLines = append(reactionLines, currentLine)
				currentLine = pill
			} else {
				currentLine = candidate
			}
		}
		if currentLine != "" {
			reactionLines = append(reactionLines, currentLine)
		}
		reactionLine = "\n" + strings.Join(reactionLines, "\n")
	}

	var attachmentLines string
	if rendered := messages.RenderAttachments(msg.Attachments); rendered != "" {
		attachmentLines = "\n" + messages.WordWrap(rendered, contentWidth)
	}

	return line + "\n" + text + attachmentLines + reactionLine
}
