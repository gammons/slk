package messages

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

type MessageItem struct {
	TS         string
	UserName   string
	Text       string
	Timestamp  string
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

// renderMessage renders a single message entry and returns its string representation.
func renderMessage(msg MessageItem, width int, isSelected bool) string {
	// Username + timestamp
	userStyle := styles.Username
	if isSelected {
		userStyle = userStyle.Underline(true)
	}
	line := userStyle.Render(msg.UserName) + "  " + styles.Timestamp.Render(msg.Timestamp)

	// Message text
	text := styles.MessageText.Width(width - 4).Render(msg.Text)

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

	// Pre-render all messages to know their heights
	type renderedMsg struct {
		content string
		height  int
	}
	rendered := make([]renderedMsg, len(m.messages))
	for i, msg := range m.messages {
		content := renderMessage(msg, width, i == m.selected)
		rendered[i] = renderedMsg{
			content: content,
			height:  lipgloss.Height(content),
		}
	}

	// Adjust offset to keep the selected message visible.
	// We build the viewport from offset downward, fitting as many messages as possible.
	// If selected is before offset, move offset up.
	// If selected would be below the viewport, move offset down.

	// First, ensure offset is not past selected
	if m.selected < m.offset {
		m.offset = m.selected
	}

	// Then, check if selected is visible from offset.
	// Walk from offset, accumulating height, to see if selected fits.
	for {
		usedHeight := 0
		selectedVisible := false
		for i := m.offset; i < len(rendered); i++ {
			entryHeight := rendered[i].height
			if i > m.offset {
				entryHeight += 1 // +1 for blank line between messages
			}
			if usedHeight+entryHeight > msgAreaHeight && i > m.offset {
				break
			}
			usedHeight += entryHeight
			if i == m.selected {
				selectedVisible = true
				break
			}
		}
		if selectedVisible {
			break
		}
		// Selected is not visible -- move offset down
		m.offset++
		if m.offset > m.selected {
			m.offset = m.selected
			break
		}
	}

	// Now render the visible window starting from offset.
	// Keep adding messages until we've filled or exceeded the area.
	// MaxHeight on the container will clip any overflow, so a partially
	// visible message at the bottom is fine (better than empty space).
	var visibleRows []string
	usedHeight := 0
	for i := m.offset; i < len(rendered); i++ {
		entryHeight := rendered[i].height
		gap := 0
		if len(visibleRows) > 0 {
			gap = 1 // blank line between messages
		}
		if gap > 0 {
			usedHeight += gap
		}
		usedHeight += entryHeight
		visibleRows = append(visibleRows, rendered[i].content)
		// Stop once we've filled past the viewport
		if usedHeight >= msgAreaHeight {
			break
		}
	}

	// Show scroll indicators
	var scrollUp, scrollDown string
	if m.offset > 0 {
		scrollUp = lipgloss.NewStyle().Foreground(styles.TextMuted).Render(
			fmt.Sprintf("  -- %d more above --", m.offset))
		visibleRows = append([]string{scrollUp}, visibleRows...)
	}
	lastVisible := m.offset + len(visibleRows)
	if scrollUp != "" {
		lastVisible-- // don't count the scroll indicator
	}
	if lastVisible < len(m.messages) {
		remaining := len(m.messages) - lastVisible
		scrollDown = lipgloss.NewStyle().Foreground(styles.TextMuted).Render(
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
