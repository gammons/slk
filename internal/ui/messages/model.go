package messages

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

var dateSeparatorStyle = lipgloss.NewStyle().
	Foreground(styles.TextMuted).
	Bold(true).
	Align(lipgloss.Center)

var selectedBg = lipgloss.NewStyle().
	Background(lipgloss.Color("#222233"))

type MessageItem struct {
	TS         string
	UserName   string
	UserID     string
	Text       string
	Timestamp  string // formatted display time (e.g. "3:04 PM")
	DateStr    string // date string for grouping (e.g. "2026-04-23")
	ThreadTS   string
	ReplyCount int
	Reactions  []ReactionItem
	IsEdited   bool
}

// AvatarFunc returns the rendered half-block avatar for a user ID, or empty string.
type AvatarFunc func(userID string) string

type ReactionItem struct {
	Emoji string
	Count int
}

// viewEntry is a pre-rendered row in the message list (message or date separator).
type viewEntry struct {
	content string // rendered content (without selection highlight)
	height  int    // number of terminal lines
	msgIdx  int    // index into messages, or -1 for separator
}

type Model struct {
	messages     []MessageItem
	selected     int
	offset       int // index into entries[] of first visible entry
	channelName  string
	channelTopic string
	loading      bool
	avatarFn     AvatarFunc        // optional: returns half-block avatar for a userID
	userNames    map[string]string // user ID -> display name for mention resolution

	// Render cache -- invalidated when messages or width change
	cache       []viewEntry
	cacheWidth  int
	cacheMsgLen int
}

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

func (m *Model) SetChannel(name, topic string) {
	m.channelName = name
	m.channelTopic = topic
}

func (m *Model) SetMessages(msgs []MessageItem) {
	m.messages = msgs
	m.cache = nil // invalidate cache
	if len(msgs) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	// Start at the bottom -- newest messages visible
	m.selected = len(msgs) - 1
	// Set offset high so View() will anchor from the bottom.
	// Use MaxInt to guarantee it's beyond any cache entry index
	// (cache includes date separators so it's larger than len(msgs)).
	m.offset = math.MaxInt32
}

func (m *Model) AppendMessage(msg MessageItem) {
	wasAtBottom := m.selected >= len(m.messages)-1
	m.messages = append(m.messages, msg)
	m.cache = nil // invalidate cache
	if wasAtBottom || len(m.messages) == 1 {
		// Auto-scroll to the new message
		m.selected = len(m.messages) - 1
		m.offset = math.MaxInt32
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
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) MoveDown() {
	if m.selected < len(m.messages)-1 {
		m.selected++
	}
}

func (m *Model) GoToTop() {
	m.selected = 0
	m.offset = 0
}

func (m *Model) GoToBottom() {
	if len(m.messages) > 0 {
		m.selected = len(m.messages) - 1
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
}

func (m *Model) SetLoading(loading bool) {
	m.loading = loading
}

func (m *Model) SetAvatarFunc(fn AvatarFunc) {
	m.avatarFn = fn
}

// SetUserNames sets the user ID -> display name map used to resolve @mentions.
func (m *Model) SetUserNames(names map[string]string) {
	m.userNames = names
	m.cache = nil // invalidate cache so mentions re-render
}

func (m *Model) OldestTS() string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[0].TS
}

// buildCache pre-renders all messages and day separators.
// Messages are rendered WITHOUT selection highlight (that's applied in View).
func (m *Model) buildCache(width int) {
	m.cache = nil
	m.cacheWidth = width
	m.cacheMsgLen = len(m.messages)

	var lastDate string
	for i, msg := range m.messages {
		msgDate := dateFromTS(msg.TS)
		if msgDate != "" && msgDate != lastDate {
			label := formatDateSeparator(msgDate)
			sep := dateSeparatorStyle.Width(width).Render("── " + label + " ──")
			m.cache = append(m.cache, viewEntry{
				content: sep,
				height:  lipgloss.Height(sep),
				msgIdx:  -1,
			})
			lastDate = msgDate
		}

		avatarStr := ""
		if m.avatarFn != nil {
			avatarStr = m.avatarFn(msg.UserID)
		}
		rendered := renderMessagePlain(msg, width, avatarStr, m.userNames)
		m.cache = append(m.cache, viewEntry{
			content: rendered,
			height:  lipgloss.Height(rendered),
			msgIdx:  i,
		})
	}
}

// renderMessagePlain renders a message without selection highlight.
func renderMessagePlain(msg MessageItem, width int, avatarStr string, userNames map[string]string) string {
	line := styles.Username.Render(msg.UserName) + "  " + styles.Timestamp.Render(msg.Timestamp)

	// If we have an avatar, reserve space on the left for it
	contentWidth := width - 4
	if avatarStr != "" {
		contentWidth = width - 7 // 4 cols avatar + 1 space + 2 padding
	}
	if contentWidth < 20 {
		contentWidth = 20
	}

	text := styles.MessageText.Width(contentWidth).Render(RenderSlackMarkdown(msg.Text, userNames))

	var threadLine string
	if msg.ReplyCount > 0 {
		threadLine = "\n" + styles.ThreadIndicator.Render(
			fmt.Sprintf("[%d replies ->]", msg.ReplyCount))
	}

	var reactionLine string
	if len(msg.Reactions) > 0 {
		var parts []string
		for _, r := range msg.Reactions {
			parts = append(parts, fmt.Sprintf("%s %d", r.Emoji, r.Count))
		}
		reactionLine = "\n" + lipgloss.NewStyle().Foreground(styles.TextMuted).Render(
			strings.Join(parts, "  "))
	}

	var editedMark string
	if msg.IsEdited {
		editedMark = " " + styles.Timestamp.Render("(edited)")
	}

	msgContent := line + editedMark + "\n" + text + threadLine + reactionLine

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
			left = avatarLines[i] + " "
		} else {
			// Empty space where avatar was (maintain alignment)
			left = strings.Repeat(" ", avatarWidth)
		}

		if i < len(contentLines) {
			right = contentLines[i]
		}

		result = append(result, left+right)
	}

	return strings.Join(result, "\n")
}

// applySelection wraps a rendered message with selection highlight.
func applySelection(content string, width int) string {
	// Re-render the username line with underline
	return selectedBg.Width(width-2).Padding(0, 1).Render(content)
}

func (m *Model) View(height, width int) string {
	// Header
	header := styles.ChannelUnread.
		Width(width).
		Render(fmt.Sprintf("# %s", m.channelName))

	if m.channelTopic != "" {
		header += "\n" + styles.Timestamp.Width(width).Render(m.channelTopic)
	}

	separator := lipgloss.NewStyle().Width(width).Foreground(styles.Border).Render(strings.Repeat("-", width))

	chrome := header + "\n" + separator
	chromeHeight := lipgloss.Height(chrome)

	msgAreaHeight := height - chromeHeight
	if msgAreaHeight < 1 {
		msgAreaHeight = 1
	}

	if len(m.messages) == 0 {
		empty := lipgloss.NewStyle().
			Width(width).
			Height(msgAreaHeight).
			Foreground(styles.TextMuted).
			Render("No messages yet")
		return header + "\n" + separator + "\n" + empty
	}

	// Rebuild cache if messages or width changed
	if m.cache == nil || m.cacheWidth != width || m.cacheMsgLen != len(m.messages) {
		m.buildCache(width)
	}

	entries := m.cache

	// Find the entry index for the selected message
	selectedEntry := 0
	for i, e := range entries {
		if e.msgIdx == m.selected {
			selectedEntry = i
			break
		}
	}

	// Build viewport anchored from the selected entry.
	// Start with the selected entry, then add entries above until the viewport is full.
	// This guarantees the selected (newest) message is always visible.

	// Reserve 1 row for "more above" indicator if there are entries before selectedEntry
	scrollIndicatorHeight := 0
	if selectedEntry > 0 {
		scrollIndicatorHeight = 1
	}
	availableHeight := msgAreaHeight - scrollIndicatorHeight

	// First, build the list of entries to show, bottom-up from selectedEntry
	var bottomUpEntries []int
	bottomUpEntries = append(bottomUpEntries, selectedEntry)

	// Measure height starting from selected entry
	selectedContent := entries[selectedEntry].content
	if entries[selectedEntry].msgIdx == m.selected {
		selectedContent = applySelection(selectedContent, width)
	}
	usedHeight := lipgloss.Height(selectedContent)

	// Add entries above until we fill the viewport
	for i := selectedEntry - 1; i >= 0; i-- {
		entryContent := entries[i].content
		if entries[i].msgIdx == m.selected {
			entryContent = applySelection(entryContent, width)
		}

		testRows := []string{entryContent}
		for _, idx := range bottomUpEntries {
			c := entries[idx].content
			if entries[idx].msgIdx == m.selected {
				c = applySelection(c, width)
			}
			testRows = append(testRows, c)
		}
		testHeight := lipgloss.Height(strings.Join(testRows, "\n"))

		if testHeight > availableHeight {
			break
		}
		bottomUpEntries = append([]int{i}, bottomUpEntries...)
		usedHeight = testHeight
	}

	m.offset = bottomUpEntries[0]

	// Build visible rows in order
	var visibleRows []string
	visibleCount := 0
	for _, idx := range bottomUpEntries {
		entryContent := entries[idx].content
		if entries[idx].msgIdx == m.selected {
			entryContent = applySelection(entryContent, width)
		}
		visibleRows = append(visibleRows, entryContent)
		visibleCount++
	}

	// If there's space below selectedEntry, add entries after it too
	for i := selectedEntry + 1; i < len(entries); i++ {
		entryContent := entries[i].content
		if entries[i].msgIdx == m.selected {
			entryContent = applySelection(entryContent, width)
		}
		candidate := append(append([]string{}, visibleRows...), entryContent)
		testHeight := lipgloss.Height(strings.Join(candidate, "\n"))
		if testHeight > availableHeight {
			break
		}
		visibleRows = append(visibleRows, entryContent)
		visibleCount++
	}
	_ = usedHeight

	// Scroll/loading indicators
	if m.offset > 0 {
		indicator := fmt.Sprintf("  -- %d more above --", m.offset)
		if m.loading {
			indicator = "  Loading older messages..."
		}
		scrollUp := lipgloss.NewStyle().Foreground(styles.TextMuted).Render(indicator)
		visibleRows = append([]string{scrollUp}, visibleRows...)
	} else if m.loading {
		loadingRow := lipgloss.NewStyle().Foreground(styles.TextMuted).Render("  Loading older messages...")
		visibleRows = append([]string{loadingRow}, visibleRows...)
	}

	lastVisibleEntry := m.offset + visibleCount
	remaining := 0
	for i := lastVisibleEntry; i < len(entries); i++ {
		if entries[i].msgIdx >= 0 {
			remaining++
		}
	}
	if remaining > 0 {
		scrollDown := lipgloss.NewStyle().Foreground(styles.TextMuted).Render(
			fmt.Sprintf("  -- %d more below --", remaining))
		visibleRows = append(visibleRows, scrollDown)
	}

	msgContent := strings.Join(visibleRows, "\n")

	return header + "\n" + separator + "\n" + lipgloss.NewStyle().
		Width(width).
		Height(msgAreaHeight).
		MaxHeight(msgAreaHeight).
		Render(msgContent)
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
