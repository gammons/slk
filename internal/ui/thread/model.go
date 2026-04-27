package thread

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	emojiutil "github.com/gammons/slk/internal/emoji"
	emoji "github.com/kyokomi/emoji/v2"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

var thickLeftBorder = lipgloss.Border{Left: "▌"}

// Model represents the thread panel UI component.
// It displays a parent message and its replies with cursor navigation.
type Model struct {
	parent            messages.MessageItem
	replies           []messages.MessageItem
	channelID         string
	threadTS          string
	selected          int
	focused           bool
	avatarFn          messages.AvatarFunc
	userNames         map[string]string
	vp                viewport.Model
	reactionNavActive bool
	reactionNavIndex  int

	// Render cache -- pre-rendered reply strings (without borders)
	cache         []string
	cacheWidth    int
	cacheReplyLen int

	// View-level cache -- bordered content ready for viewport
	viewContent       string
	viewSelected      int
	viewWidth         int
	viewHeight        int
	viewCacheValid    bool
	selectedStartLine int
	selectedEndLine   int
}

// New creates an empty thread panel.
func New() *Model {
	return &Model{}
}

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes or style updates.
func (m *Model) InvalidateCache() {
	m.cache = nil
	m.viewCacheValid = false
}

// SetThread populates the thread panel with a parent message and replies.
// The cursor starts at the bottom (newest reply).
func (m *Model) SetThread(parent messages.MessageItem, replies []messages.MessageItem, channelID, threadTS string) {
	m.parent = parent
	m.replies = replies
	m.channelID = channelID
	m.threadTS = threadTS
	m.selected = 0
	m.InvalidateCache()
}

// AddReply appends a reply to the thread. If the cursor was at the bottom,
// it auto-scrolls to the new reply.
func (m *Model) AddReply(msg messages.MessageItem) {
	wasAtBottom := len(m.replies) == 0 || m.selected >= len(m.replies)-1
	m.replies = append(m.replies, msg)
	m.InvalidateCache()
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
	m.InvalidateCache()
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
	m.InvalidateCache()
}

// SelectedReply returns the currently selected reply, or nil if none.
func (m *Model) SelectedReply() *messages.MessageItem {
	if m.selected < 0 || m.selected >= len(m.replies) {
		return nil
	}
	return &m.replies[m.selected]
}

// MoveUp moves the selection cursor up one reply.
func (m *Model) MoveUp() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected > 0 {
		m.selected--
	}
}

// MoveDown moves the selection cursor down one reply.
func (m *Model) MoveDown() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected < len(m.replies)-1 {
		m.selected++
	}
}

func (m *Model) IsAtBottom() bool {
	return m.selected >= len(m.replies)-1
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

// EnterReactionNav activates reaction navigation on the selected reply.
func (m *Model) EnterReactionNav() {
	if reply := m.SelectedReply(); reply != nil && len(reply.Reactions) > 0 {
		m.reactionNavActive = true
		m.reactionNavIndex = 0
		m.InvalidateCache()
	}
}

// ExitReactionNav deactivates reaction navigation.
func (m *Model) ExitReactionNav() {
	m.reactionNavActive = false
	m.reactionNavIndex = 0
	m.InvalidateCache()
}

// ReactionNavActive returns whether reaction navigation is active.
func (m *Model) ReactionNavActive() bool {
	return m.reactionNavActive
}

// ReactionNavLeft moves the reaction cursor left with wrapping.
func (m *Model) ReactionNavLeft() {
	reply := m.SelectedReply()
	if reply == nil {
		return
	}
	total := len(reply.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex - 1 + total) % total
	m.InvalidateCache()
}

// ReactionNavRight moves the reaction cursor right with wrapping.
func (m *Model) ReactionNavRight() {
	reply := m.SelectedReply()
	if reply == nil {
		return
	}
	total := len(reply.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex + 1) % total
	m.InvalidateCache()
}

// SelectedReaction returns the currently highlighted reaction emoji name,
// or isPlus=true if the "+" button is highlighted.
func (m *Model) SelectedReaction() (emojiName string, isPlus bool) {
	reply := m.SelectedReply()
	if reply == nil {
		return "", false
	}
	if m.reactionNavIndex >= len(reply.Reactions) {
		return "", true
	}
	return reply.Reactions[m.reactionNavIndex].Emoji, false
}

// ClampReactionNav ensures the reaction nav index is within bounds.
func (m *Model) ClampReactionNav() {
	reply := m.SelectedReply()
	if reply == nil || len(reply.Reactions) == 0 {
		m.ExitReactionNav()
		return
	}
	total := len(reply.Reactions) + 1
	if m.reactionNavIndex >= total {
		m.reactionNavIndex = total - 1
	}
	m.InvalidateCache()
}

// UpdateReaction updates the reaction state for a specific message in the thread.
func (m *Model) UpdateReaction(messageTS, emojiName, userID string, remove bool) {
	for i, reply := range m.replies {
		if reply.TS == messageTS {
			if remove {
				for j, r := range reply.Reactions {
					if r.Emoji == emojiName {
						r.Count--
						if r.Count <= 0 {
							m.replies[i].Reactions = append(reply.Reactions[:j], reply.Reactions[j+1:]...)
						} else {
							r.HasReacted = false
							m.replies[i].Reactions[j] = r
						}
						break
					}
				}
			} else {
				found := false
				for j, r := range reply.Reactions {
					if r.Emoji == emojiName {
						r.Count++
						r.HasReacted = true
						m.replies[i].Reactions[j] = r
						found = true
						break
					}
				}
				if !found {
					m.replies[i].Reactions = append(m.replies[i].Reactions, messages.ReactionItem{
						Emoji:      emojiName,
						Count:      1,
						HasReacted: true,
					})
				}
			}
			m.InvalidateCache()
			if m.reactionNavActive {
				m.ClampReactionNav()
			}
			return
		}
	}
}

// View renders the thread panel content without a border.
// The parent App is responsible for adding the border.
func (m *Model) View(height, width int) string {
	if m.IsEmpty() {
		return lipgloss.NewStyle().
			Width(width).
			Height(height).
			Background(styles.Background).
			Foreground(styles.TextMuted).
			Render("No thread selected")
	}

	// Header
	replyLabel := "replies"
	if len(m.replies) == 1 {
		replyLabel = "reply"
	}
	header := lipgloss.NewStyle().
		Width(width).
		Background(styles.Background).
		Foreground(styles.TextPrimary).
		Bold(true).
		Render(fmt.Sprintf("Thread  %d %s", len(m.replies), replyLabel))

	separator := lipgloss.NewStyle().
		Width(width).
		Background(styles.Background).
		Foreground(styles.Border).
		Render(strings.Repeat("-", width))

	// Parent message
	parentContent := m.renderThreadMessage(m.parent, width, m.userNames, false)

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
			Background(styles.Background).
			Foreground(styles.TextMuted).
			Render("No replies yet")
		result := chrome + "\n" + empty
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Background(styles.Background).Render(result)
	}

	// Rebuild render cache if replies or width changed
	if m.cache == nil || m.cacheWidth != width || m.cacheReplyLen != len(m.replies) {
		m.cache = make([]string, len(m.replies))
		for i, reply := range m.replies {
			m.cache[i] = m.renderThreadMessage(reply, width, m.userNames, i == m.selected)
		}
		m.cacheWidth = width
		m.cacheReplyLen = len(m.replies)
		m.viewCacheValid = false
	}

	// Check if view-level cache (bordered content) can be reused
	if !m.viewCacheValid || m.viewSelected != m.selected || m.viewWidth != width || m.viewHeight != replyAreaHeight {
		// Pre-compute border styles for this frame (avoids NewStyle per reply)
		borderFill := lipgloss.NewStyle().Background(styles.Background)
		borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Background).BorderBackground(styles.Background)
		borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Accent).BorderBackground(styles.Background)

		var allRows []string
		startLine := 0
		endLine := 0
		currentLine := 0

		for i, cached := range m.cache {
			content := cached
			if i == m.selected {
				startLine = currentLine
				filled := borderFill.Width(width - 1).Render(content)
				content = borderSelect.Render(filled)
			} else {
				filled := borderFill.Width(width - 1).Render(content)
				content = borderInvis.Render(filled)
			}
			h := lipgloss.Height(content)
			if i == m.selected {
				endLine = currentLine + h
			}
			allRows = append(allRows, content)
			currentLine += h
		}

		m.viewContent = strings.Join(allRows, "\n")
		m.viewSelected = m.selected
		m.viewWidth = width
		m.viewHeight = replyAreaHeight
		m.selectedStartLine = startLine
		m.selectedEndLine = endLine
		m.viewCacheValid = true
	}

	// Configure viewport
	m.vp.SetWidth(width)
	m.vp.SetHeight(replyAreaHeight)
	m.vp.KeyMap = viewport.KeyMap{}
	m.vp.SetContent(m.viewContent)

	// Scroll to keep selected item visible
	if m.selectedEndLine > m.vp.YOffset()+m.vp.Height() {
		m.vp.SetYOffset(m.selectedEndLine - m.vp.Height())
	}
	if m.selectedStartLine < m.vp.YOffset() {
		m.vp.SetYOffset(m.selectedStartLine)
	}

	result := chrome + "\n" + m.vp.View()
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Background(styles.Background).Render(result)
}

// renderThreadMessage renders a single message for the thread panel.
func (m *Model) renderThreadMessage(msg messages.MessageItem, width int, userNames map[string]string, isSelected bool) string {
	line := styles.Username.Render(msg.UserName) + lipgloss.NewStyle().Background(styles.Background).Render("  ") + styles.Timestamp.Render(msg.Timestamp)

	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	text := styles.MessageText.Render(messages.WordWrap(messages.RenderSlackMarkdown(msg.Text, userNames), contentWidth))

	var reactionLine string
	if len(msg.Reactions) > 0 {
		var pills []string
		for i, r := range msg.Reactions {
			emojiStr := emojiutil.NormalizeEmojiPresentation(emoji.Sprint(":" + r.Emoji + ":"))
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
		bgSpace := lipgloss.NewStyle().Background(styles.Background).Render(" ")
		var reactionLines []string
		currentLine := ""
		for i, pill := range pills {
			candidate := currentLine
			if i > 0 {
				candidate += bgSpace
			}
			candidate += pill
			if lipgloss.Width(candidate) > contentWidth && currentLine != "" {
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

	return line + "\n" + text + reactionLine
}
