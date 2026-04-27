package messages

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
	emoji "github.com/kyokomi/emoji/v2"
	"github.com/muesli/reflow/wordwrap"
)

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
	Emoji      string // emoji name without colons, e.g. "thumbsup"
	Count      int
	HasReacted bool // whether the current user has reacted with this emoji
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
	channelName  string
	channelTopic string
	loading      bool
	avatarFn     AvatarFunc        // optional: returns half-block avatar for a userID
	userNames    map[string]string // user ID -> display name for mention resolution

	// Render cache -- invalidated when messages or width change
	cache       []viewEntry
	cacheWidth  int
	cacheMsgLen int

	// Viewport for scrolling
	vp viewport.Model

	reactionNavActive bool
	reactionNavIndex  int

	lastReadTS string
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

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes or style updates.
func (m *Model) InvalidateCache() {
	m.cache = nil
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
		return
	}
	// Start at the bottom -- newest messages visible
	m.selected = len(msgs) - 1
}

func (m *Model) AppendMessage(msg MessageItem) {
	wasAtBottom := m.selected >= len(m.messages)-1
	m.messages = append(m.messages, msg)
	m.cache = nil // invalidate cache
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
	}
}

func (m *Model) MoveDown() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected < len(m.messages)-1 {
		m.selected++
	}
}

func (m *Model) IsAtBottom() bool {
	return m.selected >= len(m.messages)-1
}

func (m *Model) GoToTop() {
	m.selected = 0
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

func (m *Model) EnterReactionNav() {
	if msg, ok := m.SelectedMessage(); ok && len(msg.Reactions) > 0 {
		m.reactionNavActive = true
		m.reactionNavIndex = 0
		m.cache = nil
	}
}

func (m *Model) ExitReactionNav() {
	m.reactionNavActive = false
	m.reactionNavIndex = 0
	m.cache = nil
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
}

func (m *Model) ReactionNavRight() {
	msg, ok := m.SelectedMessage()
	if !ok {
		return
	}
	total := len(msg.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex + 1) % total
	m.cache = nil
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
}

// IncrementReplyCount finds a message by TS and increments its ReplyCount.
func (m *Model) IncrementReplyCount(parentTS string) {
	for i, msg := range m.messages {
		if msg.TS == parentTS {
			m.messages[i].ReplyCount++
			m.cache = nil
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
			if m.reactionNavActive {
				m.ClampReactionNav()
			}
			return
		}
	}
}

func (m *Model) SetLoading(loading bool) {
	m.loading = loading
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
}

// SetLastReadTS sets the timestamp of the last read message.
// Messages with TS > lastReadTS are considered unread.
func (m *Model) SetLastReadTS(ts string) {
	m.lastReadTS = ts
	m.cache = nil // invalidate render cache
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
	newMsgLandmarkInserted := false
	for i, msg := range m.messages {
		msgDate := dateFromTS(msg.TS)
		if msgDate != "" && msgDate != lastDate {
			label := formatDateSeparator(msgDate)
			sepStr := "── " + label + " ──"
			sep := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted).Bold(true).
				Width(width).Align(lipgloss.Center).
				Render(sepStr)
			m.cache = append(m.cache, viewEntry{
				content: sep,
				height:  lipgloss.Height(sep),
				msgIdx:  -1,
			})
			lastDate = msgDate
		}

		// New message landmark: insert before the first unread message
		if m.lastReadTS != "" && !newMsgLandmarkInserted && msg.TS > m.lastReadTS {
			newStr := "── new ──"
			label := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.Error).Bold(true).
				Width(width).Align(lipgloss.Center).
				Render(newStr)
			m.cache = append(m.cache, viewEntry{
				content: label,
				height:  lipgloss.Height(label),
				msgIdx:  -1,
			})
			newMsgLandmarkInserted = true
		}

		avatarStr := ""
		if m.avatarFn != nil {
			avatarStr = m.avatarFn(msg.UserID)
		}
		rendered := m.renderMessagePlain(msg, width, avatarStr, m.userNames, i == m.selected)
		m.cache = append(m.cache, viewEntry{
			content: rendered,
			height:  lipgloss.Height(rendered),
			msgIdx:  i,
		})
	}
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

	text := styles.MessageText.Render(wordwrap.String(RenderSlackMarkdown(msg.Text, userNames), contentWidth))

	var threadLine string
	if msg.ReplyCount > 0 {
		threadLine = "\n" + styles.ThreadIndicator.Render(
			fmt.Sprintf("[%d replies ->]", msg.ReplyCount))
	}

	var reactionLine string
	if len(msg.Reactions) > 0 {
		var pills []string
		for i, r := range msg.Reactions {
			emojiStr := emoji.Sprint(":" + r.Emoji + ":")
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
		reactionLine = "\n" + strings.Join(pills, lipgloss.NewStyle().Background(styles.Background).Render(" "))
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

var thickLeftBorder = lipgloss.Border{Left: "▌"}

func (m *Model) View(height, width int) string {
	// Header
	header := styles.ChannelUnread.
		Width(width).
		Render(fmt.Sprintf("# %s", m.channelName))

	if m.channelTopic != "" {
		header += "\n" + styles.Timestamp.Render(wordwrap.String(m.channelTopic, width))
	}

	separator := lipgloss.NewStyle().Width(width).Foreground(styles.Border).Background(styles.Background).Render(strings.Repeat("-", width))

	chrome := header + "\n" + separator
	chromeHeight := lipgloss.Height(chrome)

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
		return header + "\n" + separator + "\n" + empty
	}

	// Rebuild cache if messages or width changed
	if m.cache == nil || m.cacheWidth != width || m.cacheMsgLen != len(m.messages) {
		m.buildCache(width)
	}

	entries := m.cache

	// Pre-compute border styles for this frame (avoids NewStyle per message)
	borderFill := lipgloss.NewStyle().Background(styles.Background)
	borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Background)
	borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Accent)
	spacerBg := lipgloss.NewStyle().Background(styles.Background)

	// Build the full content string, tracking line offsets per entry
	var allRows []string
	selectedStartLine := 0
	selectedEndLine := 0
	currentLine := 0

	for i, e := range entries {
		content := e.content
		if e.msgIdx == m.selected {
			selectedStartLine = currentLine
			filled := borderFill.Width(width - 1).Render(content)
			content = borderSelect.Render(filled)
		} else if e.msgIdx >= 0 {
			// Apply border to messages only, not day separators
			filled := borderFill.Width(width - 1).Render(content)
			content = borderInvis.Render(filled)
		}
		h := lipgloss.Height(content)
		if e.msgIdx == m.selected {
			selectedEndLine = currentLine + h
		}
		// Add a spacer after messages (not after last entry or separators)
		if e.msgIdx >= 0 && i < len(entries)-1 {
			content += "\n" + spacerBg.Width(width).Render("")
			h++
		}
		allRows = append(allRows, content)
		currentLine += h
	}

	fullContent := strings.Join(allRows, "\n")

	// Configure viewport
	m.vp.SetWidth(width)
	m.vp.SetHeight(msgAreaHeight)
	m.vp.KeyMap = viewport.KeyMap{}
	m.vp.SetContent(fullContent)

	// Scroll to keep selected item visible
	if selectedEndLine > m.vp.YOffset()+m.vp.Height() {
		m.vp.SetYOffset(selectedEndLine - m.vp.Height())
	}
	if selectedStartLine < m.vp.YOffset() {
		m.vp.SetYOffset(selectedStartLine)
	}

	// Scroll indicators
	var scrollUp, scrollDown string
	if m.loading {
		scrollUp = lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted).Render("  Loading older messages...")
	}

	if m.vp.YOffset()+m.vp.Height() < m.vp.TotalLineCount() {
		scrollDown = lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted).Render("  -- more below --")
	}

	vpView := m.vp.View()
	if scrollUp != "" {
		vpView = scrollUp + "\n" + vpView
	}
	if scrollDown != "" {
		vpView = vpView + "\n" + scrollDown
	}

	return header + "\n" + separator + "\n" + lipgloss.NewStyle().
		Width(width).
		Height(msgAreaHeight).
		MaxHeight(msgAreaHeight).
		Background(styles.Background).
		Render(vpView)
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
