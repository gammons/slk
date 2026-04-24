package messages

import (
	"fmt"
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

type MessageItem struct {
	TS         string
	UserName   string
	Text       string
	Timestamp  string // formatted display time (e.g. "3:04 PM")
	DateStr    string // date string for grouping (e.g. "2026-04-23")
	ThreadTS   string
	ReplyCount int
	Reactions  []ReactionItem
	IsEdited   bool
}

type ReactionItem struct {
	Emoji string
	Count int
}

type Model struct {
	messages     []MessageItem
	selected     int
	offset       int // index of the first message visible in the viewport
	channelName  string
	channelTopic string
	loading      bool // true while fetching older messages
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
	if len(msgs) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	// Start at the bottom (newest messages)
	m.selected = len(msgs) - 1
	m.offset = 0 // will be adjusted in View()
}

func (m *Model) AppendMessage(msg MessageItem) {
	m.messages = append(m.messages, msg)
	// Auto-scroll to bottom if we were at the bottom
	if m.selected == len(m.messages)-2 || len(m.messages) == 1 {
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

// AtTop returns true if the selected message is the first one.
func (m *Model) AtTop() bool {
	return m.selected == 0 && len(m.messages) > 0
}

// PrependMessages adds older messages to the beginning of the list.
// Adjusts selected index and offset to maintain the user's current position.
func (m *Model) PrependMessages(msgs []MessageItem) {
	if len(msgs) == 0 {
		return
	}
	count := len(msgs)
	m.messages = append(msgs, m.messages...)
	m.selected += count
	m.offset += count
}

func (m *Model) SetLoading(loading bool) {
	m.loading = loading
}

// OldestTS returns the timestamp of the oldest message, or empty string if none.
func (m *Model) OldestTS() string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[0].TS
}

// renderMessage renders a single message entry and returns its string representation.
func renderMessage(msg MessageItem, width int, isSelected bool) string {
	// Username + timestamp
	userStyle := styles.Username
	if isSelected {
		userStyle = userStyle.Underline(true)
	}
	line := userStyle.Render(msg.UserName) + "  " + styles.Timestamp.Render(msg.Timestamp)

	// Message text with Slack markdown + emoji rendering
	text := styles.MessageText.Width(width - 4).Render(RenderSlackMarkdown(msg.Text))

	// Thread indicator
	var threadLine string
	if msg.ReplyCount > 0 {
		threadLine = "\n" + styles.ThreadIndicator.Render(
			fmt.Sprintf("[%d replies ->]", msg.ReplyCount))
	}

	// Reactions
	var reactionLine string
	if len(msg.Reactions) > 0 {
		var parts []string
		for _, r := range msg.Reactions {
			parts = append(parts, fmt.Sprintf("%s %d", r.Emoji, r.Count))
		}
		reactionLine = "\n" + lipgloss.NewStyle().Foreground(styles.TextMuted).Render(
			strings.Join(parts, "  "))
	}

	// Edited indicator
	var editedMark string
	if msg.IsEdited {
		editedMark = " " + styles.Timestamp.Render("(edited)")
	}

	entry := line + editedMark + "\n" + text + threadLine + reactionLine

	if isSelected {
		entry = lipgloss.NewStyle().
			Background(lipgloss.Color("#222233")).
			Width(width-2).
			Padding(0, 1).
			Render(entry)
	}

	return entry
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

	// Measure the chrome: header + "\n" + separator + "\n"
	chrome := header + "\n" + separator + "\n"
	chromeHeight := lipgloss.Height(chrome)

	// Messages area gets the remaining height
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

	// Pre-render all messages, inserting day separators between different dates.
	// Each entry in `rendered` maps to a message index (or -1 for separators).
	type renderEntry struct {
		content string
		msgIdx  int // -1 for date separators
	}
	var entries []renderEntry
	var lastDate string
	for i, msg := range m.messages {
		msgDate := dateFromTS(msg.TS)
		if msgDate != "" && msgDate != lastDate {
			label := formatDateSeparator(msgDate)
			sep := dateSeparatorStyle.Width(width).Render(
				"── " + label + " ──")
			entries = append(entries, renderEntry{content: sep, msgIdx: -1})
			lastDate = msgDate
		}
		entries = append(entries, renderEntry{
			content: renderMessage(msg, width, i == m.selected),
			msgIdx:  i,
		})
	}

	// Map selected message index to entries index
	selectedEntry := 0
	for i, e := range entries {
		if e.msgIdx == m.selected {
			selectedEntry = i
			break
		}
	}

	// Ensure offset refers to entries (not messages). We need a separate offset for entries.
	// Clamp offset
	if m.offset < 0 {
		m.offset = 0
	}
	if m.offset > selectedEntry {
		m.offset = selectedEntry
	}

	// Check if selected is visible from current offset by measuring actual content
	for {
		if m.offset > selectedEntry {
			m.offset = selectedEntry
			break
		}
		var testRows []string
		for i := m.offset; i <= selectedEntry && i < len(entries); i++ {
			testRows = append(testRows, entries[i].content)
		}
		testContent := strings.Join(testRows, "\n")
		if lipgloss.Height(testContent) <= msgAreaHeight {
			break
		}
		m.offset++
	}

	// Render from offset, adding entries until we fill or exceed the area.
	var visibleRows []string
	for i := m.offset; i < len(entries); i++ {
		candidate := append(visibleRows, entries[i].content)
		candidateHeight := lipgloss.Height(strings.Join(candidate, "\n"))
		if candidateHeight > msgAreaHeight && len(visibleRows) > 0 {
			visibleRows = append(visibleRows, entries[i].content)
			break
		}
		visibleRows = candidate
		if candidateHeight >= msgAreaHeight {
			break
		}
	}

	// Show scroll/loading indicators
	visibleCount := len(visibleRows)
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
	// Count remaining messages (not separators) below the viewport
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

// dateFromTS extracts a "2006-01-02" date string from a Slack timestamp like "1700000001.000000".
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

// formatDateSeparator turns "2006-01-02" into a human-readable label like
// "Today", "Yesterday", or "Monday, January 2, 2006".
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
