package reactionpicker

import (
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	emoji "github.com/kyokomi/emoji/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
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
	return m.renderBox(termWidth)
}

// ViewOverlay composites the picker on top of the background.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}

	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}

	result := overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
	// Clamp to exactly termHeight lines to prevent terminal scrolling.
	// Emoji with unpredictable terminal widths can cause lipgloss to wrap
	// lines, producing output taller than expected.
	lines := strings.Split(result, "\n")
	if len(lines) > termHeight {
		lines = lines[:termHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}

	overlayWidth := termWidth * 30 / 100
	if overlayWidth < 35 {
		overlayWidth = 35
	}
	if overlayWidth > 50 {
		overlayWidth = 50
	}
	innerWidth := overlayWidth - 4 // border + padding

	// Title
	title := lipgloss.NewStyle().
		Foreground(styles.Primary).
		Bold(true).
		Render("Add Reaction")

	// Query input with blue left border
	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Foreground(styles.TextMuted).Render("Type to filter...")
		inputText = "█ " + placeholder
	} else {
		inputText = m.query + "█"
	}
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "▌"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		PaddingLeft(1).
		Foreground(styles.TextPrimary).
		Render(inputText)

	// Results (max 10)
	list := m.displayedList()
	maxVisible := 10
	if maxVisible > len(list) {
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

	var resultRows []string
	for i := start; i < end; i++ {
		entry := list[i]
		// Display Unicode emoji only for single-codepoint characters.
		// Multi-codepoint emoji (skin tones, ZWJ sequences, flags) have
		// terminal widths that disagree with go-runewidth, breaking borders.
		// runewidth.StringWidth() is unreliable for these — it returns 2
		// for skin tone variants that render as 4 cells. Rune count is
		// the reliable signal: 1 rune = predictable width, 2+ = problematic.
		var line string
		if len([]rune(entry.Unicode)) == 1 {
			line = entry.Unicode + " " + entry.Name
		} else {
			line = ":" + entry.Name + ":"
		}

		if m.isExistingReaction(entry.Name) {
			line += " ✓"
		}

		if lipgloss.Width(line) > innerWidth-1 {
			line = truncate.StringWithTail(line, uint(innerWidth-1), "…")
		}

		if i == m.selected {
			indicator := lipgloss.NewStyle().Foreground(styles.Accent).Render("▌")
			row := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true).
				Width(innerWidth - 1).
				MaxWidth(innerWidth - 1).
				Render(line)
			resultRows = append(resultRows, indicator+row)
		} else {
			row := lipgloss.NewStyle().
				Foreground(styles.TextPrimary).
				Width(innerWidth - 1).
				MaxWidth(innerWidth - 1).
				Render(line)
			resultRows = append(resultRows, " "+row)
		}
	}

	if len(list) == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching emoji")
		resultRows = append(resultRows, noResults)
	}

	// Compose content
	content := title + "\n" + input + "\n\n" + strings.Join(resultRows, "\n")

	// Wrap in bordered box
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)
}
