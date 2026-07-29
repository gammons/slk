// Package activityview is the UI model for the "Activity" panel: a vertical
// list of items the user was notified about — @mentions, thread replies,
// reactions to their messages, and DMs — sourced from slack.ActivityItem.
//
// It mirrors internal/ui/threadsview so the App layer can wire it
// symmetrically: callers push a fresh page via SetItems and read
// SelectedItem to drive panel switching when the user activates a row.
//
// Each item renders as a two-line card: line 1 is author + activity context
// ("Mention/Thread/Reacted in #ch", or "DM") + relative time; line 2 is the
// hydrated message-body preview (empty until the messages.list hydration
// lands — see SetBodies).
package activityview

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	slack "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackfmt"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// cardStride is the number of flat-list lines a single activity row occupies.
// Rows are two-line cards (line 1: author + context + time; line 2: the
// hydrated message-body preview), with no inter-card separator. Kept as a
// named constant so ClickAt / snapToSelected read the same way as
// threadsview's cardStride-based math.
const cardStride = 2

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

	// bodies holds hydrated message text/author for each item ref,
	// keyed by slack.ActivityMsgKey(channelID, ts). Filled by SetBodies
	// after the messages.list second fetch; a row renders ref-only until
	// its body arrives.
	bodies map[string]slack.ActivityMessage

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
		bodies:       map[string]slack.ActivityMessage{},
	}
}

// SetBodies installs hydrated message bodies (keyed by
// slack.ActivityMsgKey) fetched via messages.list, and forces a re-render.
func (m *Model) SetBodies(bodies map[string]slack.ActivityMessage) {
	if bodies == nil {
		bodies = map[string]slack.ActivityMessage{}
	}
	m.bodies = bodies
	m.dirty()
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
// negative rowY. Cards are two-line (cardStride == 2) with no separators;
// a click on either visual line of a card maps to the same item via
// absLine / cardStride.
func (m *Model) ClickAt(rowY int) bool {
	if rowY < 0 {
		return false
	}
	absLine := m.yOffset + rowY
	idx := absLine / cardStride
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

// snapToSelected adjusts yOffset so the selected two-line card (cardStride
// lines) is fully inside the viewport.
func (m *Model) snapToSelected(height, totalLines int) {
	start := m.selected * cardStride
	end := start + cardStride

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

// renderRows builds the full (un-windowed) line list, flattening each
// item's two-line card into the flat list (cardStride lines per item) so
// the windowing / snap / click math stays stride-based.
func (m *Model) renderRows(width int) []string {
	lines := make([]string, 0, len(m.items)*cardStride)
	for i, it := range m.items {
		l1, l2 := m.renderCard(it, width, i == m.selected)
		lines = append(lines, l1, l2)
	}
	return lines
}

// blankLine returns an exactly `width`-column-wide empty line.
func blankLine(width int) string {
	return lipgloss.NewStyle().Width(width).Render("")
}

// renderCard returns the two lines for one activity item:
//
//	<glyph> <author>  <Verb> in #<channel>              <relTime> [●]
//	  <body preview>            (reactions prefix the :emoji:)
//
// Line 1 is author-first with a natural-language context ("Mention in
// #x" / "Thread in #x" / "Reacted in #x" / "DM"), mirroring the desktop
// Activity feed; line 2 is the hydrated message body (empty until the
// messages.list fetch lands). Selection is a green left border (▌) on
// both lines; unread items get a trailing dot on line 1.
func (m *Model) renderCard(it slack.ActivityItem, width int, selected bool) (string, string) {
	contentWidth := width - 1
	if contentWidth < 1 {
		contentWidth = 1
	}

	body := m.bodies[slack.ActivityMsgKey(it.ChannelID, it.TS)]

	// Author = the actor (reactor for reactions, message author
	// otherwise), falling back to the hydrated message's user when the
	// ref carried no author (thread_v2 / dm).
	authorID := it.AuthorID
	if authorID == "" {
		authorID = body.UserID
	}
	author := m.resolveUser(authorID)

	left := activityGlyph(it.Type) + " "
	if author != "" {
		left += authorStyle().Render(author)
	}
	if ctx := m.contextLabel(it); ctx != "" {
		if author != "" {
			left += "  "
		}
		left += ctx
	}
	right := ""
	if rel := formatRelTime(it.FeedTS); rel != "" {
		right = mutedStyle().Render(rel)
	}
	if it.IsUnread {
		if right != "" {
			right += " "
		}
		right += unreadDotStyle().Render("●")
	}
	line1 := layoutLine(left, right, contentWidth)

	// Collapse the body to a single line: a card is exactly cardStride
	// lines, so any newline in the message would desync the flat-list
	// windowing/click math. strings.Fields folds newlines/tabs/runs of
	// spaces into single spaces.
	preview := strings.Join(strings.Fields(slackfmt.StripMarkup(body.Text, m.userNames)), " ")
	if detail := activityDetail(it); detail != "" {
		if preview != "" {
			preview = detail + "  " + preview
		} else {
			preview = detail
		}
	}
	line2 := "  " + mutedStyle().Render(preview)

	return m.wrapLine(line1, contentWidth, selected), m.wrapLine(line2, contentWidth, selected)
}

// contextLabel renders the natural-language activity context for line 1:
// "Mention in #ch" / "Thread in #ch" / "Reacted in #ch" / "DM" (no channel
// for DMs). Empty for unknown types.
func (m *Model) contextLabel(it slack.ActivityItem) string {
	verb := contextVerb(it.Type)
	if verb == "" {
		return ""
	}
	if it.Type == "dm" || it.Type == "bot_dm_bundle" {
		return mutedStyle().Render(verb)
	}
	ch := m.resolveChannel(it.ChannelID)
	if ch == "" {
		return mutedStyle().Render(verb)
	}
	return mutedStyle().Render(verb+" in ") + channelNameStyle().Render("#"+ch)
}

// contextVerb maps an item type to its Activity-feed verb.
func contextVerb(itemType string) string {
	switch itemType {
	case "at_user", "at_user_group", "at_channel", "at_everyone",
		"keyword", "list_user_mentioned", "unjoined_channel_mention", "channel":
		return "Mention"
	case "thread_v2":
		return "Thread"
	case "message_reaction":
		return "Reacted"
	case "dm", "bot_dm_bundle":
		return "DM"
	default:
		return ""
	}
}

// authorStyle renders the actor name in bold so it anchors the card.
func authorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}

// layoutLine places `right` flush against the right edge of a width-column
// line with `left` on the left, padding between. When there's no room for
// both, the right meta is dropped and the left is clipped. Widths are
// measured with lipgloss so ANSI styling doesn't skew the math.
func layoutLine(left, right string, width int) string {
	if right == "" {
		return clipToWidth(left, width)
	}
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	if lw+1+rw > width {
		return clipToWidth(left, width)
	}
	return left + strings.Repeat(" ", width-lw-rw) + right
}

// wrapLine clips a rendered line to contentWidth and applies the selection
// border + background fill (shared by both card lines so selection spans
// the whole card).
func (m *Model) wrapLine(line string, contentWidth int, selected bool) string {
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

// activityDetail returns a short detail (currently the reaction emoji
// ":name:") that renderCard prefixes to the body preview for
// message_reaction items. Pure so it can be unit-tested.
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
