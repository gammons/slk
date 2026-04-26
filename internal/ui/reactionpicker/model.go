package reactionpicker

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	emoji "github.com/kyokomi/emoji/v2"

	"github.com/gammons/slack-tui/internal/ui/styles"
)

// EmojiEntry represents an emoji with its name and Unicode character.
type EmojiEntry struct {
	Name    string // e.g. "thumbsup"
	Unicode string // e.g. "\U0001f44d"
}

// ReactionResult is returned when the user selects an emoji.
type ReactionResult struct {
	Emoji  string // emoji name without colons
	Remove bool   // true if toggling off an existing reaction
}

// Model is the reaction picker overlay.
type Model struct {
	allEmoji          []EmojiEntry
	frecent           []EmojiEntry
	filtered          []EmojiEntry
	query             string
	selected          int
	visible           bool
	messageTS         string
	channelID         string
	existingReactions []string
}

// New creates a new reaction picker with the full emoji list.
func New() *Model {
	m := &Model{}
	m.buildEmojiList()
	return m
}

func (m *Model) buildEmojiList() {
	codeMap := emoji.CodeMap()
	seen := make(map[string]bool)
	m.allEmoji = make([]EmojiEntry, 0, len(codeMap))

	for code, unicode := range codeMap {
		name := strings.Trim(code, ":")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		m.allEmoji = append(m.allEmoji, EmojiEntry{Name: name, Unicode: unicode})
	}

	sort.Slice(m.allEmoji, func(i, j int) bool {
		return m.allEmoji[i].Name < m.allEmoji[j].Name
	})
}

// Open shows the picker for a specific message.
func (m *Model) Open(channelID, messageTS string, existingReactions []string) {
	m.channelID = channelID
	m.messageTS = messageTS
	m.existingReactions = existingReactions
	m.query = ""
	m.selected = 0
	m.filtered = nil
	m.visible = true
}

// Close hides the picker and resets state.
func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

// IsVisible returns whether the picker is showing.
func (m *Model) IsVisible() bool {
	return m.visible
}

// SetFrecentEmoji sets the frequently/recently used emoji list.
func (m *Model) SetFrecentEmoji(entries []EmojiEntry) {
	m.frecent = entries
}

// ChannelID returns the target channel.
func (m *Model) ChannelID() string {
	return m.channelID
}

// MessageTS returns the target message timestamp.
func (m *Model) MessageTS() string {
	return m.messageTS
}

// displayedList returns the list currently shown (frecent or filtered).
func (m *Model) displayedList() []EmojiEntry {
	if m.query == "" {
		return m.frecent
	}
	return m.filtered
}

func (m *Model) filter() {
	if m.query == "" {
		m.filtered = nil
		m.selected = 0
		return
	}

	q := strings.ToLower(m.query)
	m.filtered = m.filtered[:0]

	var substringMatches []EmojiEntry
	for _, e := range m.allEmoji {
		if strings.HasPrefix(e.Name, q) {
			m.filtered = append(m.filtered, e)
		} else if strings.Contains(e.Name, q) {
			substringMatches = append(substringMatches, e)
		}
		if len(m.filtered)+len(substringMatches) >= 50 {
			break
		}
	}
	m.filtered = append(m.filtered, substringMatches...)
	m.selected = 0
}

func (m *Model) isExistingReaction(emojiName string) bool {
	for _, r := range m.existingReactions {
		if r == emojiName {
			return true
		}
	}
	return false
}

// HandleKey processes a key event and returns a result if an emoji was selected.
func (m *Model) HandleKey(keyStr string) *ReactionResult {
	switch keyStr {
	case "esc", "escape":
		m.Close()
		return nil

	case "enter":
		list := m.displayedList()
		if len(list) == 0 || m.selected >= len(list) {
			return nil
		}
		selected := list[m.selected]
		return &ReactionResult{
			Emoji:  selected.Name,
			Remove: m.isExistingReaction(selected.Name),
		}

	case "up":
		if m.selected > 0 {
			m.selected--
		}
		return nil

	case "down":
		list := m.displayedList()
		if m.selected < len(list)-1 {
			m.selected++
		}
		return nil

	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.filter()
		}
		return nil

	default:
		if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 {
			m.query += keyStr
			m.filter()
		}
		return nil
	}
}

// View renders the picker box content.
func (m *Model) View(termWidth int) string {
	boxWidth := termWidth * 30 / 100
	if boxWidth < 35 {
		boxWidth = 35
	}
	if boxWidth > 50 {
		boxWidth = 50
	}
	innerWidth := boxWidth - 4

	var b strings.Builder

	title := lipgloss.NewStyle().
		Foreground(styles.Primary).
		Bold(true).
		Render("Add Reaction")
	b.WriteString(title)
	b.WriteString("\n")

	cursor := "|"
	queryDisplay := m.query + cursor
	inputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(styles.Primary).
		PaddingLeft(1)
	b.WriteString(inputStyle.Render(queryDisplay))
	b.WriteString("\n")

	sep := lipgloss.NewStyle().
		Foreground(styles.Border).
		Render(strings.Repeat("\u2500", innerWidth))
	b.WriteString(sep)
	b.WriteString("\n")

	list := m.displayedList()
	maxVisible := 10
	if len(list) < maxVisible {
		maxVisible = len(list)
	}

	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(list) {
		end = len(list)
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}

	if len(list) == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Render("No matching emoji")
		b.WriteString(noResults)
	}

	for i := start; i < end; i++ {
		entry := list[i]
		prefix := "  "
		if i == m.selected {
			prefix = lipgloss.NewStyle().
				Foreground(styles.Accent).
				Render("\u258c ")
		}

		emojiDisplay := entry.Unicode + " " + entry.Name

		suffix := ""
		if m.isExistingReaction(entry.Name) {
			suffix = lipgloss.NewStyle().
				Foreground(styles.Accent).
				Render(" \u2713")
		}

		line := prefix + emojiDisplay + suffix
		b.WriteString(line)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// ViewOverlay composites the picker on top of the background.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	boxWidth := termWidth * 30 / 100
	if boxWidth < 35 {
		boxWidth = 35
	}
	if boxWidth > 50 {
		boxWidth = 50
	}

	content := m.View(termWidth)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 1).
		Width(boxWidth).
		Render(content)

	return lipgloss.Place(
		termWidth, termHeight,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0F0F1A")),
	)
}
