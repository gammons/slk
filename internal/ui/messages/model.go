// internal/ui/messages/model.go
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
	if m.selected >= len(msgs) {
		m.selected = len(msgs) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
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
}

func (m *Model) GoToBottom() {
	if len(m.messages) > 0 {
		m.selected = len(m.messages) - 1
	}
}

func (m Model) View(height, width int) string {
	// Header
	header := styles.ChannelUnread.
		Width(width).
		Render(fmt.Sprintf("# %s", m.channelName))

	if m.channelTopic != "" {
		header += "\n" + styles.Timestamp.Width(width).Render(m.channelTopic)
	}

	headerHeight := lipgloss.Height(header) + 1 // +1 for separator
	separator := lipgloss.NewStyle().Width(width).Foreground(styles.Border).Render(strings.Repeat("-", width))

	// Messages area
	msgAreaHeight := height - headerHeight - 1
	if msgAreaHeight < 1 {
		msgAreaHeight = 1
	}

	var msgRows []string
	for i, msg := range m.messages {
		isSelected := i == m.selected

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

		msgRows = append(msgRows, entry)
	}

	msgContent := strings.Join(msgRows, "\n\n")

	return header + "\n" + separator + "\n" + lipgloss.NewStyle().
		Width(width).
		Height(msgAreaHeight).
		Render(msgContent)
}
