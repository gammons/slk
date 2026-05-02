// Package threadsview is the UI model for the "Threads" panel: a vertical
// list of threads the user is involved in, sourced from cache.ThreadSummary.
//
// The model is purely presentation: callers (typically the App layer) push
// new summaries via SetSummaries whenever the cache produces a fresh ranking,
// and read SelectedSummary / Selected to drive panel switching when the user
// activates a row.
package threadsview

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// cardStride is the number of lines a single rendered thread card occupies
// in the flat line list, including the trailing blank separator. The very
// last card has no trailing separator, so the last card occupies
// cardContentLines lines instead.
const (
	cardContentLines = 3 // header + preview + footer
	cardStride       = cardContentLines + 1
)

// Local styles (kept package-private so we don't pollute the shared styles
// package for one panel). Built from the shared color tokens so theme
// changes still propagate via styles.Apply().
func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.TextMuted)
}

func unreadDotStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
}

// channelNameStyle themes the channel name in a thread card so it remains
// readable on light themes (where the default foreground is dark on a dark
// Background panel). Uses the theme's primary/link color, mirroring the
// sidebar's "channel link" convention.
func channelNameStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
}

// thickLeftBorder mirrors the messages package convention: a 1-column-wide
// left border using "▌". Selected rows render with Accent (green) foreground;
// non-selected rows render with the panel background so the column is
// reserved (keeps content alignment uniform) but invisible.
var thickLeftBorder = lipgloss.Border{Left: "▌"}

func borderInvisStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.Background).
		BorderBackground(styles.Background)
}

// borderSelectStyle returns the bordered style for the selected card.
// focused drives both the border foreground (Accent vs TextMuted) and
// the row background (focused tint vs unfocused tint), matching the
// "you're acting on this" language used by the messages and thread
// panels.
func borderSelectStyle(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.SelectionBorderColor(focused)).
		BorderBackground(styles.SelectionTintColor(focused)).
		Background(styles.SelectionTintColor(focused))
}

// borderFillStyle returns the row-fill style for unselected cards
// (themed Background). Selected cards use a SelectionTintColor fill
// inside renderCard so the tint reaches the right edge.
func borderFillStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(styles.Background)
}

// Model holds the threads-list state.
type Model struct {
	summaries  []cache.ThreadSummary
	userNames    map[string]string
	channelNames map[string]string
	selfUserID string

	selected int
	yOffset  int
	focused  bool

	// snappedSelection lets View() avoid snapping yOffset back to the
	// selected row on every render. While snappedSelection == selected,
	// programmatic scrolls (ScrollUp/ScrollDown) are preserved. The flag
	// is reset by ScrollUp/ScrollDown so the next selection move re-snaps,
	// matching the sidebar's behavior (sidebar/model.go:471-483).
	snappedSelection int
	hasSnapped       bool

	version int64
}

// New creates an empty Model. userNames is the user-id -> display-name map
// used to resolve mention IDs in the parent-text preview and the author /
// last-reply-by fields. selfUserID is the current user's ID; rows whose
// author / replier is selfUserID render as "me".
func New(userNames map[string]string, selfUserID string) Model {
	if userNames == nil {
		userNames = map[string]string{}
	}
	return Model{
		userNames:  userNames,
		selfUserID: selfUserID,
	}
}

// Version returns a counter that increments any time View() output could
// change. App's panel-output cache uses this to reuse rendered frames.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

// SetUserNames replaces the user id -> display name map. No-op (no version
// bump) when the new map has the same length and the same key/value pairs as
// the current one — required so the App-level panel cache (app.go:4068-4093)
// can hit on idle re-renders.
func (m *Model) SetUserNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	if stringMapsEqual(m.userNames, names) {
		return
	}
	m.userNames = names
	m.dirty()
}

// stringMapsEqual reports whether two map[string]string have identical
// contents. Used by SetUserNames and SetChannelNames to short-circuit Set*
// calls with unchanged input so the App-level panel cache can hit on idle
// re-renders.
//
// Hand-rolled (rather than reflect.DeepEqual) because this runs on the render
// hot path — every threadsview render checks it. Please don't "simplify" this
// to reflect.DeepEqual without re-benchmarking.
func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || vb != va {
			return false
		}
	}
	return true
}

// SetChannelNames sets the channel ID -> name map used to resolve bare
// <#CHANNELID> mentions. No-op when the new map matches the current one.
func (m *Model) SetChannelNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	if stringMapsEqual(m.channelNames, names) {
		return
	}
	m.channelNames = names
	m.dirty()
}

// SetSelfUserID updates the current user's ID. Used to render "me" labels.
func (m *Model) SetSelfUserID(id string) {
	if m.selfUserID != id {
		m.selfUserID = id
		m.dirty()
	}
}

// SetFocused marks whether the panel currently has keyboard focus. Stored
// here so the App can query it; rendering does not currently use it.
func (m *Model) SetFocused(f bool) {
	if m.focused != f {
		m.focused = f
		m.dirty()
	}
}

// Focused reports whether the panel currently has keyboard focus.
func (m *Model) Focused() bool { return m.focused }

// SetSummaries replaces the list of thread summaries. If the previously-
// selected (channelID, threadTS) pair is still present in the new list, the
// selection follows it to its new position; otherwise the selection resets
// to the top.
func (m *Model) SetSummaries(s []cache.ThreadSummary) {
	prevCh, prevTS, hadSel := m.selectedKey()
	m.summaries = s

	newSel := 0
	if hadSel {
		for i, t := range s {
			if t.ChannelID == prevCh && t.ThreadTS == prevTS {
				newSel = i
				break
			}
		}
	}
	m.selected = newSel
	m.clampSelection()
	m.hasSnapped = false // force re-snap on next render
	m.dirty()
}

// Summaries returns the current list of thread summaries.
func (m *Model) Summaries() []cache.ThreadSummary { return m.summaries }

// SelectedIndex returns the selection cursor's position, or 0 when the list
// is empty.
func (m *Model) SelectedIndex() int { return m.selected }

// Selected returns the (channelID, threadTS) of the currently selected row,
// with ok=false if the list is empty.
func (m *Model) Selected() (channelID, threadTS string, ok bool) {
	s, ok := m.SelectedSummary()
	if !ok {
		return "", "", false
	}
	return s.ChannelID, s.ThreadTS, true
}

// SelectedSummary returns the currently selected ThreadSummary.
func (m *Model) SelectedSummary() (cache.ThreadSummary, bool) {
	if len(m.summaries) == 0 || m.selected < 0 || m.selected >= len(m.summaries) {
		return cache.ThreadSummary{}, false
	}
	return m.summaries[m.selected], true
}

// selectedKey returns the (channelID, threadTS) pair currently selected,
// with ok=false when the list is empty. Used by SetSummaries to re-anchor
// selection across re-rankings.
func (m *Model) selectedKey() (string, string, bool) {
	s, ok := m.SelectedSummary()
	if !ok {
		return "", "", false
	}
	return s.ChannelID, s.ThreadTS, true
}

// MoveDown advances the cursor by one row, clamping at the bottom.
func (m *Model) MoveDown() {
	if m.selected < len(m.summaries)-1 {
		m.selected++
		m.dirty()
	}
}

// MoveUp moves the cursor up by one row, clamping at zero.
func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
		m.dirty()
	}
}

// GoToTop jumps to the first row.
func (m *Model) GoToTop() {
	if m.selected != 0 {
		m.selected = 0
		m.dirty()
	}
}

// GoToBottom jumps to the last row.
func (m *Model) GoToBottom() {
	if n := len(m.summaries); n > 0 && m.selected != n-1 {
		m.selected = n - 1
		m.dirty()
	}
}

// ScrollUp moves the viewport up n lines without changing the selection.
// Resetting hasSnapped lets the next selection move re-snap to keep the
// selection visible (sidebar uses the same convention).
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.yOffset -= n
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	m.hasSnapped = false
	m.dirty()
}

// ScrollDown moves the viewport down n lines without changing the
// selection. View() clamps yOffset against the actual content height.
// Resetting hasSnapped lets the next selection move re-snap.
func (m *Model) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	m.yOffset += n
	m.hasSnapped = false
	m.dirty()
}

// MarkSelectedRead clears the local Unread flag on the currently selected
// summary, if any. Returns true when a flag was actually flipped (so callers
// can refresh dependent state, e.g. the sidebar's threads-row badge). This
// is a presentation-only update: it does not touch Slack server state and
// does not advance the parent channel's last_read_ts. The next refresh
// from cache.ListInvolvedThreads may re-set the flag if its heuristic
// (LastReplyTS > channel.last_read_ts) still considers the thread unread,
// but in practice the user opening the parent channel will eventually
// resolve that.
func (m *Model) MarkSelectedRead() bool {
	if m.selected < 0 || m.selected >= len(m.summaries) {
		return false
	}
	if !m.summaries[m.selected].Unread {
		return false
	}
	m.summaries[m.selected].Unread = false
	m.dirty()
	return true
}

// MarkByThreadTSRead clears the local Unread flag on the summary matching
// (channelID, threadTS), regardless of whether it is the currently selected
// row. Returns true when a flag was actually flipped, so callers can refresh
// dependent state (sidebar threads-row badge). Like MarkSelectedRead this is
// presentation-only and does not touch Slack server state.
func (m *Model) MarkByThreadTSRead(channelID, threadTS string) bool {
	if channelID == "" || threadTS == "" {
		return false
	}
	for i := range m.summaries {
		if m.summaries[i].ChannelID == channelID && m.summaries[i].ThreadTS == threadTS {
			if !m.summaries[i].Unread {
				return false
			}
			m.summaries[i].Unread = false
			m.dirty()
			return true
		}
	}
	return false
}

// MarkByThreadTSUnread sets the local Unread flag on the summary matching
// (channelID, threadTS) to true. Returns true when a flag was actually
// flipped (i.e., the row existed and was previously read). Like
// MarkByThreadTSRead this is presentation-only: it does not touch Slack
// server state. Used by the U-key mark-unread flow and by the inbound
// thread_marked WS handler.
//
// Note: the threads-view's underlying heuristic
// (LastReplyTS > channel.last_read_ts AND LastReplyBy != self) may
// re-clear this flag on the next refresh from cache.ListInvolvedThreads
// if the heuristic considers the thread read. This is the documented
// v1 limitation; a per-thread last_read_ts column is future work.
func (m *Model) MarkByThreadTSUnread(channelID, threadTS string) bool {
	if channelID == "" || threadTS == "" {
		return false
	}
	for i := range m.summaries {
		if m.summaries[i].ChannelID == channelID && m.summaries[i].ThreadTS == threadTS {
			if m.summaries[i].Unread {
				return false
			}
			m.summaries[i].Unread = true
			m.dirty()
			return true
		}
	}
	return false
}

// UnreadCount returns the number of summaries currently flagged as unread.
func (m *Model) UnreadCount() int {
	n := 0
	for _, s := range m.summaries {
		if s.Unread {
			n++
		}
	}
	return n
}

func (m *Model) clampSelection() {
	if m.selected < 0 {
		m.selected = 0
	}
	if n := len(m.summaries); n == 0 {
		m.selected = 0
	} else if m.selected >= n {
		m.selected = n - 1
	}
}

// View renders the threads list to a string of `height` lines, each
// `width` columns wide. Argument order matches sidebar.View and
// thread.View (height first).
func (m *Model) View(height, width int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	if len(m.summaries) == 0 {
		empty := mutedStyle().Render("no threads")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, empty)
	}

	// Build the full content as a flat slice of lines.
	lines := m.renderRows(width)

	// Snap-to-selected: when the selection moves, adjust yOffset so the
	// selected card is fully visible. Skipped when the selection hasn't
	// changed since the last snap so manual scrolls (ScrollUp/Down) are
	// preserved across renders.
	if !m.hasSnapped || m.snappedSelection != m.selected {
		m.snapToSelected(height, len(lines))
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}

	// Clamp yOffset against actual content height.
	maxOffset := len(lines) - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.yOffset > maxOffset {
		m.yOffset = maxOffset
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}

	end := m.yOffset + height
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[m.yOffset:end]

	// Pad to `height` so the panel always fills its allotted vertical
	// space. Filler is width-padded so column count stays uniform.
	if pad := height - len(visible); pad > 0 {
		filler := blankLine(width)
		out := make([]string, 0, height)
		out = append(out, visible...)
		for i := 0; i < pad; i++ {
			out = append(out, filler)
		}
		visible = out
	}
	return strings.Join(visible, "\n")
}

// snapToSelected adjusts yOffset so the entire selected card (3 content
// lines) is inside the viewport. The selected card occupies absolute lines
// [start, start+cardContentLines) inside the flat line list. If the card
// is taller than `height`, prefer pinning its top edge to the viewport top.
func (m *Model) snapToSelected(height, totalLines int) {
	start := m.selected * cardStride
	end := start + cardContentLines

	if end > m.yOffset+height {
		m.yOffset = end - height
	}
	if start < m.yOffset {
		m.yOffset = start
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	maxOffset := totalLines - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.yOffset > maxOffset {
		m.yOffset = maxOffset
	}
}

// renderRows builds the full (un-windowed) line list for the current
// summaries, with one width-padded blank separator between cards. All
// emitted lines are exactly `width` columns wide.
func (m *Model) renderRows(width int) []string {
	separator := blankLine(width)
	var lines []string
	for i, s := range m.summaries {
		if i > 0 {
			lines = append(lines, separator)
		}
		lines = append(lines, m.renderCard(s, width, i == m.selected)...)
	}
	return lines
}

// blankLine returns an exactly `width`-column-wide empty line, used both
// for inter-card separators and bottom padding when the content is shorter
// than the viewport.
func blankLine(width int) string {
	return lipgloss.NewStyle().Width(width).Render("")
}

// renderCard returns the 3 lines of a single thread row: header, preview,
// footer. Selection is indicated by a green left border (▌) — the same
// mechanism used for messages and thread replies. Non-selected rows
// reserve the same 1-column gutter with a background-colored (invisible)
// border so column alignment is uniform.
func (m *Model) renderCard(s cache.ThreadSummary, width int, selected bool) []string {
	// The left border occupies 1 column; content fills the remainder.
	contentWidth := width - 1
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Header: <glyph><channel> · <author> · <relTime>  [• if unread]
	glyph := channelGlyph(s.ChannelType)
	author := m.resolveUser(s.ParentUserID)
	relTime := formatRelTime(s.ParentTS)
	header := glyph + channelNameStyle().Render(s.ChannelName) + "  " + mutedStyle().Render("·") + "  " + author + "  " + mutedStyle().Render("· "+relTime)
	if s.Unread {
		header += "  " + unreadDotStyle().Render("●")
	}
	header = clipToWidth(header, contentWidth)

	// Preview: "  > <parent text>"; falls back to "(parent not loaded)"
	// when both ParentText and ParentUserID are empty (cache hasn't seen
	// the parent yet).
	var previewBody string
	if s.ParentText == "" && s.ParentUserID == "" {
		previewBody = mutedStyle().Render("(parent not loaded)")
	} else {
		preview := messages.RenderSlackMarkdown(s.ParentText, m.userNames, m.channelNames)
		preview = strings.ReplaceAll(preview, "\n", " ")
		previewMax := contentWidth - 4
		if previewMax < 0 {
			previewMax = 0
		}
		previewBody = truncate.StringWithTail(preview, uint(previewMax), "…")
	}
	previewLine := clipToWidth("  > "+previewBody, contentWidth)

	// Footer: "  N replies · last by <user> <relTime>".
	replyWord := "replies"
	if s.ReplyCount == 1 {
		replyWord = "reply"
	}
	lastBy := m.resolveUser(s.LastReplyBy)
	footerText := "  " + strconv.Itoa(s.ReplyCount) + " " + replyWord + " · last by " + lastBy + " " + formatRelTime(s.LastReplyTS)
	footerText = clipToWidth(footerText, contentWidth)

	// Pick border + fill (themed Background for unselected; tinted
	// SelectionTintColor for selected so trailing whitespace carries
	// the tint to the right edge — same per-variant fill pattern used
	// in internal/ui/messages/model.go).
	borderStyle := borderInvisStyle()
	fill := borderFillStyle().Width(contentWidth)
	if selected {
		borderStyle = borderSelectStyle(m.focused)
		fill = lipgloss.NewStyle().
			Background(styles.SelectionTintColor(m.focused)).
			Width(contentWidth)
	}

	headerOut := borderStyle.Render(fill.Render(header))
	previewOut := borderStyle.Render(fill.Render(previewLine))
	footerOut := borderStyle.Render(fill.Foreground(styles.TextMuted).Render(footerText))

	return []string{headerOut, previewOut, footerOut}
}

// channelGlyph returns the leading glyph for a channel row, matching the
// sidebar's conventions: "#" for public channels, "◆ " for private channels,
// "● " for DMs and group DMs.
func channelGlyph(channelType string) string {
	switch channelType {
	case "private":
		return lipgloss.NewStyle().Foreground(styles.Warning).Render("◆ ")
	case "dm", "group_dm":
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("● ")
	default:
		return "# "
	}
}

// resolveUser maps a Slack user ID to a display label: "me" for the current
// user, the cached display name when known, and the raw ID otherwise.
func (m *Model) resolveUser(uid string) string {
	if uid == "" {
		return ""
	}
	if uid == m.selfUserID {
		return "me"
	}
	if name, ok := m.userNames[uid]; ok && name != "" {
		return name
	}
	return uid
}

// formatRelTime parses a Slack-style "1700000000.000000" timestamp and
// returns a coarse "Nm ago" / "Nh ago" / "Nd ago" string. Empty / unparseable
// inputs return "".
func formatRelTime(ts string) string {
	if ts == "" {
		return ""
	}
	secStr := ts
	if dot := strings.IndexByte(ts, '.'); dot >= 0 {
		secStr = ts[:dot]
	}
	sec, err := strconv.ParseInt(secStr, 10, 64)
	if err != nil {
		return ""
	}
	d := time.Since(time.Unix(sec, 0))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h ago"
	default:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d ago"
	}
}

// clipToWidth truncates an already-styled string to at most `width` display
// columns (using a trailing ellipsis), but does NOT pad short strings.
// Padding is left to the caller's width-bounded style.Render so background
// fills correctly. lipgloss.Width measures display columns even with
// embedded ANSI escapes.
func clipToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(width), "…")
}
