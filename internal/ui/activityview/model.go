// Package activityview is the UI model for the "Activity" panel: a vertical
// list of items the user was notified about — @mentions, thread replies,
// reactions to their messages, and DMs — sourced from slack.ActivityItem.
//
// It mirrors internal/ui/threadsview so the App layer can wire it
// symmetrically: callers push a fresh page via SetItems and read
// SelectedItem to drive panel switching when the user activates a row.
//
// Unlike threadsview each item renders on a single line (type/channel/actor/
// time only); the actual message body preview is phase 2 and deliberately
// absent here.
package activityview

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	slack "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// rowStride is the number of flat-list lines a single activity row occupies.
// Rows are single-line with no inter-row separator, so this is 1. Kept as a
// named constant so ClickAt / snapToSelected read the same way as
// threadsview's cardStride-based math.
const rowStride = 1

// Local styles, kept package-private and built from the shared color tokens
// so theme changes propagate via styles.Apply().
func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.TextMuted)
}

func unreadDotStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
}

// channelNameStyle themes the channel name so it stays readable on light
// themes, mirroring the sidebar's "channel link" convention.
func channelNameStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(styles.Primary).Bold(true)
}

// thickLeftBorder mirrors the messages/threadsview convention: a 1-column
// left border using "▌" that marks the selected row.
var thickLeftBorder = lipgloss.Border{Left: "▌"}

func borderInvisStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.Background).
		BorderBackground(styles.Background)
}

func borderSelectStyle(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.SelectionBorderColor(focused)).
		BorderBackground(styles.SelectionTintColor(focused)).
		Background(styles.SelectionTintColor(focused))
}

func borderFillStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(styles.Background)
}

// Model holds the activity-list state.
type Model struct {
	items        []slack.ActivityItem
	userNames    map[string]string
	channelNames map[string]string
	selfUserID   string

	// unreadOnly tracks the read/unread filter flag. The view only tracks
	// it and shows an indicator; the App re-fetches with unread_only=true
	// when it flips (the server does the filtering).
	unreadOnly bool

	selected int
	yOffset  int
	focused  bool

	// snappedSelection lets View() avoid snapping yOffset back to the
	// selected row on every render (see threadsview for the rationale).
	snappedSelection int
	hasSnapped       bool

	version int64
}

// New creates an empty Model. userNames resolves actor IDs to display names;
// selfUserID renders the current user as "me".
func New(userNames map[string]string, selfUserID string) Model {
	if userNames == nil {
		userNames = map[string]string{}
	}
	return Model{
		userNames:    userNames,
		selfUserID:   selfUserID,
		channelNames: map[string]string{},
	}
}

// Version returns a counter that increments any time View() output could
// change. App's panel-output cache uses this to reuse rendered frames.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

// SetUserNames replaces the user id -> display name map. No-op (no version
// bump) when the new map is content-equal to the current one so the
// App-level panel cache can hit on idle re-renders.
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

// SetChannelNames replaces the channel id -> name map. No-op when
// content-equal to the current one.
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

// stringMapsEqual reports whether two map[string]string have identical
// contents. Hand-rolled (rather than reflect.DeepEqual) because it runs on
// the render hot path.
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

// SetSelfUserID updates the current user's ID. Used to render "me" labels.
func (m *Model) SetSelfUserID(id string) {
	if m.selfUserID != id {
		m.selfUserID = id
		m.dirty()
	}
}

// SetFocused marks whether the panel currently has keyboard focus.
func (m *Model) SetFocused(f bool) {
	if m.focused != f {
		m.focused = f
		m.dirty()
	}
}

// Focused reports whether the panel currently has keyboard focus.
func (m *Model) Focused() bool { return m.focused }

// UnreadOnly reports whether the unread-only filter is currently on.
func (m *Model) UnreadOnly() bool { return m.unreadOnly }

// ToggleUnreadOnly flips the unread-only filter flag and returns its new
// value. The App observes the returned value and re-fetches the feed with
// unread_only set accordingly; the view merely tracks the flag.
func (m *Model) ToggleUnreadOnly() bool {
	m.unreadOnly = !m.unreadOnly
	m.dirty()
	return m.unreadOnly
}

// SetItems replaces the list of activity items. If the previously-selected
// item (by Key) is still present, the selection follows it to its new
// position; otherwise the selection resets to the top.
func (m *Model) SetItems(items []slack.ActivityItem) {
	prevKey, hadSel := m.selectedKey()
	m.items = items

	newSel := 0
	if hadSel {
		for i, it := range items {
			if it.Key == prevKey {
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

// Items returns the current list of activity items.
func (m *Model) Items() []slack.ActivityItem { return m.items }

// SelectedIndex returns the selection cursor's position, or 0 when empty.
func (m *Model) SelectedIndex() int { return m.selected }

// SelectedItem returns the currently selected ActivityItem (ChannelID / TS /
// ThreadTS carry enough to open the target), with ok=false when the list is
// empty.
func (m *Model) SelectedItem() (slack.ActivityItem, bool) {
	if len(m.items) == 0 || m.selected < 0 || m.selected >= len(m.items) {
		return slack.ActivityItem{}, false
	}
	return m.items[m.selected], true
}

// selectedKey returns the Key of the currently selected item, with ok=false
// when empty. Used by SetItems to re-anchor selection across refreshes.
func (m *Model) selectedKey() (string, bool) {
	it, ok := m.SelectedItem()
	if !ok {
		return "", false
	}
	return it.Key, true
}

// MoveDown advances the cursor by one row, clamping at the bottom.
func (m *Model) MoveDown() {
	if m.selected < len(m.items)-1 {
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
	if n := len(m.items); n > 0 && m.selected != n-1 {
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
	m.snappedSelection = m.selected
	m.hasSnapped = true
	m.dirty()
}

// ScrollDown moves the viewport down n lines without changing the selection.
// View() clamps yOffset against the actual content height.
func (m *Model) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	m.yOffset += n
	m.snappedSelection = m.selected
	m.hasSnapped = true
	m.dirty()
}

// ViewportAtTop reports whether the viewport is scrolled to the very top.
func (m *Model) ViewportAtTop() bool {
	return m.yOffset == 0 && len(m.items) > 0
}

// ClickAt selects the activity row whose visual row contains rowY (the
// panel-local Y inside the bordered messages-pane content area). Returns
// true when a row was selected and the caller should follow up with the
// open command; false for the blank-fill region past the last row and for
// negative rowY. Rows are single-line (rowStride == 1) with no separators.
func (m *Model) ClickAt(rowY int) bool {
	if rowY < 0 {
		return false
	}
	absLine := m.yOffset + rowY
	idx := absLine / rowStride
	if idx < 0 || idx >= len(m.items) {
		return false
	}
	if m.selected != idx {
		m.selected = idx
		m.dirty()
	}
	return true
}

// UnreadCount returns the number of items currently flagged unread.
func (m *Model) UnreadCount() int {
	n := 0
	for _, it := range m.items {
		if it.IsUnread {
			n++
		}
	}
	return n
}

func (m *Model) clampSelection() {
	if m.selected < 0 {
		m.selected = 0
	}
	if n := len(m.items); n == 0 {
		m.selected = 0
	} else if m.selected >= n {
		m.selected = n - 1
	}
}

// View renders the activity list to `height` lines, each `width` columns
// wide. Argument order matches sidebar.View / thread.View (height first).
func (m *Model) View(height, width int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	if len(m.items) == 0 {
		msg := "no activity"
		if m.unreadOnly {
			msg = "no unread activity"
		}
		empty := mutedStyle().Render(msg)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, empty)
	}

	lines := m.renderRows(width)
	if !m.hasSnapped || m.snappedSelection != m.selected {
		m.snapToSelected(height, len(lines))
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}
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

// snapToSelected adjusts yOffset so the selected single-line row is inside
// the viewport.
func (m *Model) snapToSelected(height, totalLines int) {
	start := m.selected * rowStride
	end := start + rowStride

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

// renderRows builds the full (un-windowed) line list, one line per item.
func (m *Model) renderRows(width int) []string {
	lines := make([]string, 0, len(m.items))
	for i, it := range m.items {
		lines = append(lines, m.renderRow(it, width, i == m.selected))
	}
	return lines
}

// blankLine returns an exactly `width`-column-wide empty line.
func blankLine(width int) string {
	return lipgloss.NewStyle().Width(width).Render("")
}

// renderRow returns the single line for one activity item:
//
//	<glyph> <channel>  ·  <actor><detail>  · <relTime>  [●]
//
// Selection is a green left border (▌); unread items render bold with a
// trailing unread dot. Non-selected rows reserve the same gutter with a
// background-colored (invisible) border for uniform alignment.
func (m *Model) renderRow(it slack.ActivityItem, width int, selected bool) string {
	contentWidth := width - 1
	if contentWidth < 1 {
		contentWidth = 1
	}

	glyph := activityGlyph(it.Type)
	channel := m.resolveChannel(it.ChannelID)
	actor := m.resolveUser(it.AuthorID)

	line := glyph + " " + channelNameStyle().Render(channel)
	if actor != "" {
		line += "  " + mutedStyle().Render("·") + "  " + actor
	}
	if detail := activityDetail(it); detail != "" {
		line += " " + mutedStyle().Render(detail)
	}
	if rel := formatRelTime(it.FeedTS); rel != "" {
		line += "  " + mutedStyle().Render("· "+rel)
	}
	if it.IsUnread {
		line += "  " + unreadDotStyle().Render("●")
	}
	line = clipToWidth(line, contentWidth)

	borderStyle := borderInvisStyle()
	fill := borderFillStyle().Width(contentWidth)
	if selected {
		borderStyle = borderSelectStyle(m.focused)
		fill = lipgloss.NewStyle().
			Background(styles.SelectionTintColor(m.focused)).
			Width(contentWidth)
	}
	return borderStyle.Render(fill.Render(line))
}

// activityGlyph returns the leading glyph for a row keyed on the item type:
// "@" for mentions, a thread flag for thread replies, a face for reactions,
// and an envelope for DMs. Pure (no styling) so it can be unit-tested.
func activityGlyph(itemType string) string {
	switch itemType {
	case "at_user", "at_user_group", "at_channel", "at_everyone",
		"keyword", "list_user_mentioned", "unjoined_channel_mention", "channel":
		return "@"
	case "thread_v2":
		return "⚑"
	case "message_reaction":
		return "☺"
	case "dm", "bot_dm_bundle":
		return "✉"
	default:
		return "•"
	}
}

// activityDetail returns a short trailing detail for the row, currently only
// the reaction emoji name (":name:") for message_reaction items. Pure so it
// can be unit-tested.
func activityDetail(it slack.ActivityItem) string {
	if it.Type == "message_reaction" && it.Reaction != "" {
		return ":" + it.Reaction + ":"
	}
	return ""
}

// resolveChannel maps a channel ID to its cached name (falling back to the
// raw ID when uncached — acceptable for MVP; a mention can reference a
// channel slk hasn't cached).
func (m *Model) resolveChannel(id string) string {
	if id == "" {
		return ""
	}
	if name, ok := m.channelNames[id]; ok && name != "" {
		return name
	}
	return id
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
// returns a coarse "Nm ago" / "Nh ago" / "Nd ago" string. Empty /
// unparseable inputs return "".
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
// columns (trailing ellipsis) without padding short strings.
func clipToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(width), "…")
}
