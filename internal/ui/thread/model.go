package thread

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gammons/slack-tui/internal/ui/messages"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

var selectedBg = lipgloss.NewStyle().
	Background(lipgloss.Color("#222233"))

// Model represents the thread panel UI component.
// It displays a parent message and its replies with cursor navigation.
type Model struct {
	parent    messages.MessageItem
	replies   []messages.MessageItem
	channelID string
	threadTS  string
	selected  int
	focused   bool
	avatarFn  messages.AvatarFunc
	userNames map[string]string
}

// New creates an empty thread panel.
func New() *Model {
	return &Model{}
}

// SetThread populates the thread panel with a parent message and replies.
// The cursor starts at the bottom (newest reply).
func (m *Model) SetThread(parent messages.MessageItem, replies []messages.MessageItem, channelID, threadTS string) {
	m.parent = parent
	m.replies = replies
	m.channelID = channelID
	m.threadTS = threadTS
	if len(replies) > 0 {
		m.selected = len(replies) - 1
	} else {
		m.selected = 0
	}
}

// AddReply appends a reply to the thread. If the cursor was at the bottom,
// it auto-scrolls to the new reply.
func (m *Model) AddReply(msg messages.MessageItem) {
	wasAtBottom := len(m.replies) == 0 || m.selected >= len(m.replies)-1
	m.replies = append(m.replies, msg)
	if wasAtBottom {
		m.selected = len(m.replies) - 1
	}
}

// Clear resets all thread state.
func (m *Model) Clear() {
	m.parent = messages.MessageItem{}
	m.replies = nil
	m.channelID = ""
	m.threadTS = ""
	m.selected = 0
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

// SetFocused sets whether the thread panel has focus.
func (m *Model) SetFocused(focused bool) {
	m.focused = focused
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
}

// MoveUp moves the selection cursor up one reply.
func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

// MoveDown moves the selection cursor down one reply.
func (m *Model) MoveDown() {
	if m.selected < len(m.replies)-1 {
		m.selected++
	}
}

// GoToTop moves the selection to the first reply.
func (m *Model) GoToTop() {
	m.selected = 0
}

// GoToBottom moves the selection to the last reply.
func (m *Model) GoToBottom() {
	if len(m.replies) > 0 {
		m.selected = len(m.replies) - 1
	}
}

// View renders the thread panel content without a border.
// The parent App is responsible for adding the border.
func (m *Model) View(height, width int) string {
	if m.IsEmpty() {
		return lipgloss.NewStyle().
			Width(width).
			Height(height).
			Foreground(styles.TextMuted).
			Render("No thread selected")
	}

	// Header
	replyLabel := "replies"
	if len(m.replies) == 1 {
		replyLabel = "reply"
	}
	header := lipgloss.NewStyle().
		Foreground(styles.TextPrimary).
		Bold(true).
		Render(fmt.Sprintf("Thread  %d %s", len(m.replies), replyLabel))

	separator := lipgloss.NewStyle().
		Width(width).
		Foreground(styles.Border).
		Render(strings.Repeat("-", width))

	// Parent message
	parentContent := renderThreadMessage(m.parent, width, m.userNames)

	chrome := header + "\n" + separator + "\n" + parentContent + "\n" + separator
	chromeHeight := lipgloss.Height(chrome)

	replyAreaHeight := height - chromeHeight - 1 // -1 for the newline joining
	if replyAreaHeight < 1 {
		replyAreaHeight = 1
	}

	if len(m.replies) == 0 {
		empty := lipgloss.NewStyle().
			Width(width).
			Height(replyAreaHeight).
			Foreground(styles.TextMuted).
			Render("No replies yet")
		result := chrome + "\n" + empty
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(result)
	}

	// Render replies with viewport scrolling
	var visibleRows []string
	usedHeight := 0

	// Build visible replies anchored from selected, going upward
	type renderedEntry struct {
		content string
		height  int
	}

	var entries []renderedEntry
	for i, reply := range m.replies {
		content := renderThreadMessage(reply, width, m.userNames)
		if i == m.selected {
			content = applySelection(content, width)
		}
		entries = append(entries, renderedEntry{
			content: content,
			height:  lipgloss.Height(content),
		})
	}

	// Start from selected and go upward to fill viewport
	var showIndices []int
	showIndices = append(showIndices, m.selected)
	usedHeight = entries[m.selected].height

	for i := m.selected - 1; i >= 0; i-- {
		if usedHeight+entries[i].height > replyAreaHeight {
			break
		}
		showIndices = append([]int{i}, showIndices...)
		usedHeight += entries[i].height
	}

	// Fill remaining space below selected
	for i := m.selected + 1; i < len(entries); i++ {
		if usedHeight+entries[i].height > replyAreaHeight {
			break
		}
		showIndices = append(showIndices, i)
		usedHeight += entries[i].height
	}

	for _, idx := range showIndices {
		visibleRows = append(visibleRows, entries[idx].content)
	}

	replyContent := strings.Join(visibleRows, "\n")

	result := chrome + "\n" + lipgloss.NewStyle().
		Width(width).
		Height(replyAreaHeight).
		MaxHeight(replyAreaHeight).
		Render(replyContent)

	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Render(result)
}

// applySelection highlights a message by applying the selection background
// to each line individually, padding each to the full width with spaces
// so the background extends edge-to-edge.
func applySelection(content string, width int) string {
	lines := strings.Split(content, "\n")
	bg := selectedBg
	for i, line := range lines {
		// lipgloss.Width measures visible width, ignoring ANSI escape codes
		visWidth := lipgloss.Width(line)
		pad := width - visWidth
		if pad < 0 {
			pad = 0
		}
		lines[i] = bg.Render(line + strings.Repeat(" ", pad))
	}
	return strings.Join(lines, "\n")
}

// renderThreadMessage renders a single message for the thread panel.
func renderThreadMessage(msg messages.MessageItem, width int, userNames map[string]string) string {
	line := styles.Username.Render(msg.UserName) + "  " + styles.Timestamp.Render(msg.Timestamp)

	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	text := styles.MessageText.Width(contentWidth).Render(messages.RenderSlackMarkdown(msg.Text, userNames))

	return line + "\n" + text
}
