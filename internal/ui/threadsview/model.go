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

// Local styles (kept package-private so we don't pollute the shared styles
// package for one panel). Built from the shared color tokens so theme
// changes still propagate via styles.Apply().
func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.TextMuted)
}

func unreadDotStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.Error).Bold(true)
}

// Model holds the threads-list state.
type Model struct {
	summaries  []cache.ThreadSummary
	userNames  map[string]string
	selfUserID string

	selected int
	yOffset  int
	focused  bool

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

// SetUserNames replaces the user id -> display name map.
func (m *Model) SetUserNames(names map[string]string) {
	if names == nil {
		names = map[string]string{}
	}
	m.userNames = names
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
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.yOffset -= n
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	m.dirty()
}

// ScrollDown moves the viewport down n lines without changing the
// selection. View() clamps yOffset against the actual content height.
func (m *Model) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	m.yOffset += n
	m.dirty()
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
// `width` columns wide.
func (m *Model) View(width, height int) string {
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

	// Build the full content as a flat slice of lines, then window into it.
	lines := m.renderRows(width)

	// Clamp yOffset.
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

	// Pad to `height` so the panel always fills its allotted vertical space.
	if pad := height - len(visible); pad > 0 {
		filler := lipgloss.NewStyle().Width(width).Render("")
		out := make([]string, 0, height)
		out = append(out, visible...)
		for i := 0; i < pad; i++ {
			out = append(out, filler)
		}
		visible = out
	}
	return strings.Join(visible, "\n")
}

// renderRows builds the full (un-windowed) line list for the current
// summaries, with one blank separator between cards.
func (m *Model) renderRows(width int) []string {
	var lines []string
	for i, s := range m.summaries {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.renderCard(s, width, i == m.selected)...)
	}
	return lines
}

// renderCard returns the 3 lines of a single thread row: header, preview,
// footer. The selected row uses styles.ChannelSelected (bold); non-selected
// rows use styles.MessageText.
func (m *Model) renderCard(s cache.ThreadSummary, width int, selected bool) []string {
	rowStyle := styles.MessageText
	if selected {
		rowStyle = styles.ChannelSelected
	}

	// Header: <glyph><channel> · <author> · <relTime>  [• if unread]
	glyph := channelGlyph(s.ChannelType)
	author := m.resolveUser(s.ParentUserID)
	relTime := formatRelTime(s.ParentTS)
	header := glyph + s.ChannelName + "  " + mutedStyle().Render("·") + "  " + author + "  " + mutedStyle().Render("· "+relTime)
	if s.Unread {
		header += "  " + unreadDotStyle().Render("•")
	}
	header = truncateLine(header, width)

	// Preview: "  > <parent text>"; falls back to "(parent not loaded)" when
	// we have no parent message at all in the cache yet.
	var previewBody string
	if s.ParentText == "" && s.ParentUserID == "" {
		previewBody = mutedStyle().Render("(parent not loaded)")
	} else {
		preview := messages.RenderSlackMarkdown(s.ParentText, m.userNames)
		preview = strings.ReplaceAll(preview, "\n", " ")
		maxWidth := width - 4
		if maxWidth < 0 {
			maxWidth = 0
		}
		previewBody = truncate.StringWithTail(preview, uint(maxWidth), "…")
	}
	previewLine := "  > " + previewBody
	previewLine = truncateLine(previewLine, width)

	// Footer: "  N replies · last by <user> <relTime>" (muted).
	replyWord := "replies"
	if s.ReplyCount == 1 {
		replyWord = "reply"
	}
	lastBy := m.resolveUser(s.LastReplyBy)
	footerText := "  " + strconv.Itoa(s.ReplyCount) + " " + replyWord + " · last by " + lastBy + " " + formatRelTime(s.LastReplyTS)
	footer := mutedStyle().Render(truncateLine(footerText, width))

	return []string{
		rowStyle.Render(header),
		rowStyle.Render(previewLine),
		footer,
	}
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

// truncateLine clips an already-rendered string to `width` display columns
// using a trailing ellipsis. lipgloss.Width measures display columns
// correctly even with embedded ANSI escapes from styled segments.
func truncateLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(width), "…")
}
