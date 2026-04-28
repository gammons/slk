package messages

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/styles"
	emoji "github.com/kyokomi/emoji/v2"
)

type MessageItem struct {
	TS          string
	UserName    string
	UserID      string
	Text        string
	Timestamp   string // formatted display time (e.g. "3:04 PM")
	DateStr     string // date string for grouping (e.g. "2026-04-23")
	ThreadTS    string
	ReplyCount  int
	Reactions   []ReactionItem
	Attachments []Attachment
	IsEdited    bool
}

// Attachment represents a file or image attached to a message.
// Kind is "image" for image/* mimetypes, "file" otherwise.
// URL is the user-facing permalink (preferred) or fallback to url_private.
type Attachment struct {
	Kind string // "image" or "file"
	Name string // display filename / title
	URL  string // permalink (preferred) or url_private
}

// AvatarFunc returns the rendered half-block avatar for a user ID, or empty string.
type AvatarFunc func(userID string) string

type ReactionItem struct {
	Emoji      string // emoji name without colons, e.g. "thumbsup"
	Count      int
	HasReacted bool // whether the current user has reacted with this emoji
}

// viewEntry is a pre-rendered row in the message list (message or date separator).
//
// For messages we pre-render BOTH the selected and unselected bordered variants
// during buildCache so that selection movement (j/k) is a near-O(1) operation
// in View(): no lipgloss calls per keypress, just string concatenation.
//
// For separators (msgIdx == -1) only `content` is populated.
type viewEntry struct {
	// linesNormal / linesSelected hold the entry's rendered lines pre-split on
	// "\n" so View() can append them directly into the visible window without
	// any string scanning, splitting, or width measurement at render time.
	// For separator entries (msgIdx == -1) the two slices are identical.
	linesNormal   []string
	linesSelected []string
	height        int // == len(linesNormal); cached for scroll math
	msgIdx        int // index into messages, or -1 for separator
}

type Model struct {
	messages     []MessageItem
	selected     int
	channelName  string
	channelTopic string
	loading      bool
	avatarFn     AvatarFunc        // optional: returns half-block avatar for a userID
	userNames    map[string]string // user ID -> display name for mention resolution

	// Render cache -- invalidated when messages or width change.
	// Each entry holds pre-bordered variants so selection movement does not
	// re-invoke lipgloss per keypress.
	cache       []viewEntry
	cacheWidth  int
	cacheMsgLen int
	cacheSpacer       string // pre-rendered blank spacer line (1 row, full width, themed background)
	cacheLoadingHint  string // pre-rendered "Loading older messages..." line
	cacheMoreBelow    string // pre-rendered "-- more below --" line

	// Chrome cache: header line + separator. Depends on width, channelName, and
	// channelTopic only -- never on selection or scroll position.
	chromeCache       string
	chromeHeight      int
	chromeWidth       int
	chromeChannel     string
	chromeTopic       string
	chromeCacheValid  bool

	// Cumulative line offsets, computed in buildCache (only when content
	// changes). entryOffsets[i] is the line index where entry i starts in the
	// flattened content; totalLines is the total line count.
	entryOffsets []int
	totalLines   int

	// Custom scroll state -- replaces bubbles/viewport for the scrolling case
	// where we already know our content's line count and width. The bubbles
	// viewport calls ansi.StringWidth on every line of content per SetContent
	// (~55% of CPU on j/k); we skip that entirely.
	yOffset int

	// snappedSelection tracks the last selection index that View() snapped
	// yOffset to. While snappedSelection == selected, View() leaves yOffset
	// alone -- this allows the mouse wheel (or programmatic ScrollUp/Down)
	// to scroll freely without the next render yanking the viewport back to
	// the selected message.
	snappedSelection int
	hasSnapped       bool

	// Tracks the start / end line of the currently-selected entry so View()
	// can adjust yOffset to keep it on screen.
	selectedStartLine int
	selectedEndLine   int

	reactionNavActive bool
	reactionNavIndex  int

	lastReadTS string

	// version increments on every state change that could alter rendered
	// View() output. The App layer caches the WRAPPED panel output (border +
	// exactSize + ReapplyBgAfterResets) keyed on this counter, so on compose
	// keystrokes (where version is unchanged) we reuse the previous wrap.
	version int64
}

// Version returns a counter that increments every time the View() output
// could change.
func (m *Model) Version() int64 { return m.version }

// dirty bumps the render-version counter.
func (m *Model) dirty() { m.version++ }

func New(msgs []MessageItem, channelName string) Model {
	selected := 0
	if len(msgs) > 0 {
		selected = len(msgs) - 1
	}
	return Model{
		messages:    msgs,
		selected:    selected,
		channelName: channelName,
	}
}

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes or style updates.
func (m *Model) InvalidateCache() {
	m.cache = nil
	m.chromeCacheValid = false
	m.dirty()
}

func (m *Model) SetChannel(name, topic string) {
	if m.channelName != name || m.channelTopic != topic {
		m.chromeCacheValid = false
		m.dirty()
	}
	m.channelName = name
	m.channelTopic = topic
}

func (m *Model) SetMessages(msgs []MessageItem) {
	m.messages = msgs
	m.cache = nil // invalidate cache
	// Force the next View() to re-snap yOffset to the new selection -- without
	// this, switching to a channel that happens to have the same selected
	// index as the previous channel would leave yOffset at its old value.
	m.hasSnapped = false
	m.dirty()

	if len(msgs) == 0 {
		m.selected = 0
		return
	}
	// Start at the bottom -- newest messages visible
	m.selected = len(msgs) - 1
}

func (m *Model) AppendMessage(msg MessageItem) {
	wasAtBottom := m.selected >= len(m.messages)-1
	m.messages = append(m.messages, msg)
	m.cache = nil // invalidate cache
	m.dirty()

	if wasAtBottom || len(m.messages) == 1 {
		// Auto-scroll to the new message
		m.selected = len(m.messages) - 1
	}
}

func (m *Model) Messages() []MessageItem {
	return m.messages
}

func (m *Model) SelectedIndex() int {
	return m.selected
}

func (m *Model) SelectedMessage() (MessageItem, bool) {
	if len(m.messages) == 0 {
		return MessageItem{}, false
	}
	return m.messages[m.selected], true
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

// ScrollUp moves the viewport up by n lines without changing the selected
// message. The selection may scroll off-screen; pressing j/k will snap the
// viewport back to keep the (new) selection visible.
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.yOffset -= n
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	// Mark the current selection as already snapped so View() leaves yOffset
	// alone on the next render.
	m.snappedSelection = m.selected
	m.hasSnapped = true
	m.dirty()
}

// ScrollDown moves the viewport down by n lines without changing the selected
// message. View() clamps yOffset to the maximum allowed for the current
// content height.
func (m *Model) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	m.yOffset += n
	m.snappedSelection = m.selected
	m.hasSnapped = true
	m.dirty()
}

func (m *Model) MoveDown() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected < len(m.messages)-1 {
		m.selected++
		m.dirty()
	}
}

func (m *Model) IsAtBottom() bool {
	return m.selected >= len(m.messages)-1
}

func (m *Model) GoToTop() {
	if m.selected != 0 {
		m.selected = 0
		m.dirty()
	}
}

func (m *Model) GoToBottom() {
	if len(m.messages) > 0 && m.selected != len(m.messages)-1 {
		m.selected = len(m.messages) - 1
		m.dirty()
	}
}

func (m *Model) AtTop() bool {
	return m.selected == 0 && len(m.messages) > 0
}

func (m *Model) PrependMessages(msgs []MessageItem) {
	if len(msgs) == 0 {
		return
	}
	count := len(msgs)
	m.messages = append(msgs, m.messages...)
	m.selected += count
	m.cache = nil // invalidate cache
	m.dirty()
}

func (m *Model) EnterReactionNav() {
	if msg, ok := m.SelectedMessage(); ok && len(msg.Reactions) > 0 {
		m.reactionNavActive = true
		m.reactionNavIndex = 0
		m.cache = nil
		m.dirty()
	}
}

func (m *Model) ExitReactionNav() {
	if !m.reactionNavActive && m.reactionNavIndex == 0 {
		return
	}
	m.reactionNavActive = false
	m.reactionNavIndex = 0
	m.cache = nil
	m.dirty()
}

func (m *Model) ReactionNavActive() bool {
	return m.reactionNavActive
}

func (m *Model) ReactionNavLeft() {
	msg, ok := m.SelectedMessage()
	if !ok {
		return
	}
	total := len(msg.Reactions) + 1 // +1 for [+] pill
	m.reactionNavIndex = (m.reactionNavIndex - 1 + total) % total
	m.cache = nil
	m.dirty()
}

func (m *Model) ReactionNavRight() {
	msg, ok := m.SelectedMessage()
	if !ok {
		return
	}
	total := len(msg.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex + 1) % total
	m.cache = nil
	m.dirty()
}

func (m *Model) SelectedReaction() (emoji string, isPlus bool) {
	msg, ok := m.SelectedMessage()
	if !ok {
		return "", false
	}
	if m.reactionNavIndex >= len(msg.Reactions) {
		return "", true
	}
	return msg.Reactions[m.reactionNavIndex].Emoji, false
}

func (m *Model) ClampReactionNav() {
	msg, ok := m.SelectedMessage()
	if !ok || len(msg.Reactions) == 0 {
		m.ExitReactionNav()
		return
	}
	total := len(msg.Reactions) + 1
	if m.reactionNavIndex >= total {
		m.reactionNavIndex = total - 1
	}
	m.cache = nil
	m.dirty()
}

// IncrementReplyCount finds a message by TS and increments its ReplyCount.
func (m *Model) IncrementReplyCount(parentTS string) {
	for i, msg := range m.messages {
		if msg.TS == parentTS {
			m.messages[i].ReplyCount++
			m.cache = nil
			m.dirty()
			return
		}
	}
}

func (m *Model) UpdateReaction(messageTS, emojiName, userID string, remove bool) {
	for i, msg := range m.messages {
		if msg.TS == messageTS {
			if remove {
				for j, r := range msg.Reactions {
					if r.Emoji == emojiName {
						r.Count--
						if r.Count <= 0 {
							m.messages[i].Reactions = append(msg.Reactions[:j], msg.Reactions[j+1:]...)
						} else {
							r.HasReacted = false
							m.messages[i].Reactions[j] = r
						}
						break
					}
				}
			} else {
				found := false
				for j, r := range msg.Reactions {
					if r.Emoji == emojiName {
						r.Count++
						r.HasReacted = true
						m.messages[i].Reactions[j] = r
						found = true
						break
					}
				}
				if !found {
					m.messages[i].Reactions = append(m.messages[i].Reactions, ReactionItem{
						Emoji:      emojiName,
						Count:      1,
						HasReacted: true,
					})
				}
			}
			m.cache = nil
			m.dirty()
			if m.reactionNavActive {
				m.ClampReactionNav()
			}
			return
		}
	}
}

func (m *Model) SetLoading(loading bool) {
	if m.loading != loading {
		m.loading = loading
		m.dirty()
	}
}

func (m *Model) SetAvatarFunc(fn AvatarFunc) {
	m.avatarFn = fn
}

// ResolveUserName returns the display name for a user ID, or empty string if unknown.
func (m *Model) ResolveUserName(userID string) string {
	if m.userNames == nil {
		return ""
	}
	return m.userNames[userID]
}

// SetUserNames sets the user ID -> display name map used to resolve @mentions.
func (m *Model) SetUserNames(names map[string]string) {
	m.userNames = names
	m.cache = nil // invalidate cache so mentions re-render
	m.dirty()
}

// SetLastReadTS sets the timestamp of the last read message.
// Messages with TS > lastReadTS are considered unread.
func (m *Model) SetLastReadTS(ts string) {
	if m.lastReadTS == ts {
		return
	}
	m.lastReadTS = ts
	m.cache = nil // invalidate render cache
	m.dirty()
}

func (m *Model) OldestTS() string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[0].TS
}

// buildCache pre-renders all messages and day separators, splitting each
// rendered string on "\n" so View() can flatten everything into the visible
// window with zero string-scanning per frame. Runs only on width / message-set
// / theme / reaction changes -- never on simple j/k navigation.
func (m *Model) buildCache(width int) {
	m.cache = m.cache[:0]
	m.cacheWidth = width
	m.cacheMsgLen = len(m.messages)

	// Pre-build the border styles once for the whole cache build.
	borderFill := lipgloss.NewStyle().Background(styles.Background)
	borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Background).BorderBackground(styles.Background)
	borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Accent).BorderBackground(styles.Background)
	spacerBg := lipgloss.NewStyle().Background(styles.Background)
	m.cacheSpacer = spacerBg.Width(width).Render("")
	hintStyle := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted)
	m.cacheLoadingHint = hintStyle.Render("  Loading older messages...")
	m.cacheMoreBelow = hintStyle.Render("  -- more below --")
	spacerLines := []string{m.cacheSpacer}

	if cap(m.cache) < len(m.messages)+8 {
		m.cache = make([]viewEntry, 0, len(m.messages)+8)
	}

	appendSeparator := func(rendered string) {
		lines := strings.Split(rendered, "\n")
		m.cache = append(m.cache, viewEntry{
			linesNormal:   lines,
			linesSelected: lines,
			height:        len(lines),
			msgIdx:        -1,
		})
	}

	var lastDate string
	newMsgLandmarkInserted := false
	for i, msg := range m.messages {
		msgDate := dateFromTS(msg.TS)
		if msgDate != "" && msgDate != lastDate {
			label := formatDateSeparator(msgDate)
			sepStr := "── " + label + " ──"
			sep := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted).Bold(true).
				Width(width).Align(lipgloss.Center).
				Render(sepStr)
			appendSeparator(sep)
			lastDate = msgDate
		}

		// New message landmark: insert before the first unread message
		if m.lastReadTS != "" && !newMsgLandmarkInserted && msg.TS > m.lastReadTS {
			newStr := "── new ──"
			label := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.Error).Bold(true).
				Width(width).Align(lipgloss.Center).
				Render(newStr)
			appendSeparator(label)
			newMsgLandmarkInserted = true
		}

		avatarStr := ""
		if m.avatarFn != nil {
			avatarStr = m.avatarFn(msg.UserID)
		}
		rendered := m.renderMessagePlain(msg, width, avatarStr, m.userNames, i == m.selected)
		filled := borderFill.Width(width - 1).Render(rendered)
		normal := borderInvis.Render(filled)
		selected := borderSelect.Render(filled)

		linesN := strings.Split(normal, "\n")
		linesS := strings.Split(selected, "\n")
		// Append a trailing spacer line after every message except the last.
		// Both variants share the same spacer (it has no border styling).
		if i < len(m.messages)-1 {
			linesN = append(linesN, spacerLines...)
			linesS = append(linesS, spacerLines...)
		}
		m.cache = append(m.cache, viewEntry{
			linesNormal:   linesN,
			linesSelected: linesS,
			height:        len(linesN),
			msgIdx:        i,
		})
	}

	// Compute cumulative line offsets for fast yOffset math in View().
	if cap(m.entryOffsets) < len(m.cache) {
		m.entryOffsets = make([]int, len(m.cache))
	} else {
		m.entryOffsets = m.entryOffsets[:len(m.cache)]
	}
	off := 0
	for i, e := range m.cache {
		m.entryOffsets[i] = off
		off += e.height
	}
	m.totalLines = off
}

// renderMessagePlain renders a message without selection highlight.
func (m *Model) renderMessagePlain(msg MessageItem, width int, avatarStr string, userNames map[string]string, isSelected bool) string {
	line := styles.Username.Render(msg.UserName) + lipgloss.NewStyle().Background(styles.Background).Render("  ") + styles.Timestamp.Render(msg.Timestamp)

	// If we have an avatar, reserve space on the left for it
	contentWidth := width - 4
	if avatarStr != "" {
		contentWidth = width - 7 // 4 cols avatar + 1 space + 2 padding
	}
	if contentWidth < 20 {
		contentWidth = 20
	}

	text := styles.MessageText.Render(WordWrap(RenderSlackMarkdown(msg.Text, userNames), contentWidth))

	var threadLine string
	if msg.ReplyCount > 0 {
		threadLine = "\n" + styles.ThreadIndicator.Render(
			fmt.Sprintf("[%d replies ->]", msg.ReplyCount))
	}

	var reactionLine string
	if len(msg.Reactions) > 0 {
		var pills []string
		for i, r := range msg.Reactions {
			emojiStr := emojiutil.FuseModifierSequences(emoji.Sprint(":" + r.Emoji + ":"))
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
		// Join pills with wrapping. emojiutil.Width() consults the
		// terminal-probed width cache so wrapping decisions match what
		// the user's terminal will actually render.
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

	var editedMark string
	if msg.IsEdited {
		editedMark = " " + styles.Timestamp.Render("(edited)")
	}

	var attachmentLines string
	if rendered := RenderAttachments(msg.Attachments); rendered != "" {
		attachmentLines = "\n" + WordWrap(rendered, contentWidth)
	}

	msgContent := line + editedMark + "\n" + text + attachmentLines + threadLine + reactionLine

	// Place avatar next to message content
	if avatarStr != "" {
		return placeAvatarBeside(avatarStr, msgContent)
	}

	return msgContent
}

// placeAvatarBeside renders the avatar to the left of the message content.
// The avatar is 4 cols wide, 2 rows tall. Message content flows to the right.
func placeAvatarBeside(avatar, content string) string {
	avatarLines := strings.Split(avatar, "\n")
	contentLines := strings.Split(content, "\n")

	// Pad avatar to consistent width (4 visible chars + reset codes)
	avatarWidth := 5 // 4 chars + 1 space gap

	var result []string
	maxLines := len(contentLines)
	if len(avatarLines) > maxLines {
		maxLines = len(avatarLines)
	}

	for i := 0; i < maxLines; i++ {
		var left, right string

		if i < len(avatarLines) {
			left = avatarLines[i] + lipgloss.NewStyle().Background(styles.Background).Render(" ")
		} else {
			// Empty space where avatar was (maintain alignment)
			left = lipgloss.NewStyle().Background(styles.Background).Width(avatarWidth).Render("")
		}

		if i < len(contentLines) {
			right = contentLines[i]
		}

		result = append(result, left+right)
	}

	return strings.Join(result, "\n")
}

// ClickAt handles a mouse click at the given y-coordinate (relative to message pane content top).
// Selects the message at that position.
func (m *Model) ClickAt(y int) {
	absoluteY := y + m.yOffset

	// Walk through cached view entries to find which message is at this line
	currentLine := 0
	for _, entry := range m.cache {
		if entry.msgIdx < 0 {
			// Date separator or "new messages" line — skip
			currentLine += entry.height
			continue
		}
		if absoluteY >= currentLine && absoluteY < currentLine+entry.height {
			if m.selected != entry.msgIdx {
				m.selected = entry.msgIdx
				m.dirty()
			}
			return
		}
		currentLine += entry.height
	}
}

var thickLeftBorder = lipgloss.Border{Left: "▌"}



func (m *Model) View(height, width int) string {
	// Chrome (header + separator) is cached; only rebuilt on width / channel
	// name / topic change. This avoids per-keypress strings.Repeat + lipgloss
	// renders that don't depend on the selection.
	if !m.chromeCacheValid || m.chromeWidth != width || m.chromeChannel != m.channelName || m.chromeTopic != m.channelTopic {
		// Channel title sits in the message pane, so it uses the message-pane
		// background (not the sidebar's). Bold + TextPrimary on Background
		// matches the surrounding messages, and the separator below acts as
		// a bottom border between the title and the message list.
		headerStyle := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.TextPrimary).
			Bold(true).
			Padding(0, 1)
		header := headerStyle.Render(fmt.Sprintf("# %s", m.channelName))
		if m.channelTopic != "" {
			header += "\n" + styles.Timestamp.Render(WordWrap(m.channelTopic, width))
		}
		separator := lipgloss.NewStyle().Width(width).Foreground(styles.Border).Background(styles.Background).Render(strings.Repeat("─", width))
		m.chromeCache = header + "\n" + separator
		m.chromeHeight = lipgloss.Height(m.chromeCache)
		m.chromeWidth = width
		m.chromeChannel = m.channelName
		m.chromeTopic = m.channelTopic
		m.chromeCacheValid = true
	}
	chrome := m.chromeCache
	chromeHeight := m.chromeHeight

	msgAreaHeight := height - chromeHeight
	if msgAreaHeight < 1 {
		msgAreaHeight = 1
	}

	if len(m.messages) == 0 {
		text := "No messages yet"
		if m.loading {
			text = "Loading messages..."
		}
		empty := lipgloss.NewStyle().
			Width(width).
			Height(msgAreaHeight).
			Foreground(styles.TextMuted).
			Background(styles.Background).
			Render(text)
		return chrome + "\n" + empty
	}

	// Rebuild cache if messages or width changed
	if m.cache == nil || m.cacheWidth != width || m.cacheMsgLen != len(m.messages) {
		m.buildCache(width)
	}

	entries := m.cache

	// Locate selected entry's line range. O(N) scan over entryOffsets; cheap.
	m.selectedStartLine = 0
	m.selectedEndLine = 0
	for i, e := range entries {
		if e.msgIdx == m.selected {
			m.selectedStartLine = m.entryOffsets[i]
			m.selectedEndLine = m.selectedStartLine + e.height
			// The trailing spacer is part of e.height; subtract it from the
			// scroll-to-keep-visible target so we don't push the spacer into
			// view above the selection.
			if i < len(entries)-1 && e.msgIdx >= 0 {
				m.selectedEndLine--
			}
			break
		}
	}

	// Adjust yOffset to keep selection visible -- but only when the selection
	// has actually changed since the last snap. This lets the mouse wheel
	// (or programmatic ScrollUp/Down) move the viewport away from the
	// selected message without the next render yanking it back.
	if !m.hasSnapped || m.snappedSelection != m.selected {
		if m.selectedEndLine > m.yOffset+msgAreaHeight {
			m.yOffset = m.selectedEndLine - msgAreaHeight
		}
		if m.selectedStartLine < m.yOffset {
			m.yOffset = m.selectedStartLine
		}
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	maxOffset := m.totalLines - msgAreaHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.yOffset > maxOffset {
		m.yOffset = maxOffset
	}

	// Build the visible window directly from per-entry pre-split line slices.
	// No lipgloss, no uniseg, no width measurement.
	visible := make([]string, 0, msgAreaHeight)
	want := msgAreaHeight
	for i, e := range entries {
		if want == 0 {
			break
		}
		entryStart := m.entryOffsets[i]
		entryEnd := entryStart + e.height
		if entryEnd <= m.yOffset {
			continue
		}
		if entryStart >= m.yOffset+msgAreaHeight {
			break
		}
		var lines []string
		if e.msgIdx == m.selected {
			lines = e.linesSelected
		} else {
			lines = e.linesNormal
		}
		// Slice the portion of this entry that falls within [yOffset, yOffset+height).
		from := 0
		if entryStart < m.yOffset {
			from = m.yOffset - entryStart
		}
		to := len(lines)
		if entryEnd > m.yOffset+msgAreaHeight {
			to = len(lines) - (entryEnd - (m.yOffset + msgAreaHeight))
		}
		visible = append(visible, lines[from:to]...)
		want = msgAreaHeight - len(visible)
	}

	// Pad vertically with the themed spacer if content is shorter than the pane.
	for len(visible) < msgAreaHeight {
		visible = append(visible, m.cacheSpacer)
	}

	// Scroll indicators replace the first / last line when applicable.
	if m.loading && len(visible) > 0 {
		visible[0] = m.cacheLoadingHint
	}
	if m.yOffset+msgAreaHeight < m.totalLines && len(visible) > 0 {
		visible[len(visible)-1] = m.cacheMoreBelow
	}

	return chrome + "\n" + strings.Join(visible, "\n")
}

func dateFromTS(ts string) string {
	parts := strings.SplitN(ts, ".", 2)
	if len(parts) == 0 {
		return ""
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(sec, 0).Format("2006-01-02")
}

func formatDateSeparator(dateStr string) string {
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	diff := today.Sub(d).Hours() / 24

	switch {
	case diff < 1:
		return "Today"
	case diff < 2:
		return "Yesterday"
	case diff < 7:
		return d.Format("Monday")
	default:
		return d.Format("Monday, January 2, 2006")
	}
}
