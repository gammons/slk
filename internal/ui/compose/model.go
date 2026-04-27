// internal/ui/compose/model.go
package compose

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/styles"
)

type Model struct {
	input       textarea.Model
	channelName string
	width       int // display width, set by SetWidth

	// Mention picker state
	mentionPicker   mentionpicker.Model
	mentionActive   bool
	mentionStartCol int // cursor column where @ was typed
	users           []mentionpicker.User
	reverseNames    map[string]string // displayName -> userID
}

func New(channelName string) Model {
	ta := textarea.New()
	ta.Placeholder = "Message #" + channelName + "... (i to insert)"
	ta.CharLimit = 40000
	ta.MaxHeight = 5
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetWidth(40)

	// Override textarea styles to use our dark background consistently
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark)
	ta.FocusedStyle.Base = bg
	ta.FocusedStyle.Text = bg
	ta.FocusedStyle.CursorLine = bg
	ta.FocusedStyle.EndOfBuffer = bg
	ta.FocusedStyle.Prompt = bg
	ta.BlurredStyle.Base = bg
	ta.BlurredStyle.Text = bg
	ta.BlurredStyle.CursorLine = bg
	ta.BlurredStyle.EndOfBuffer = bg
	ta.BlurredStyle.Prompt = bg
	ta.FocusedStyle.Placeholder = bg.Foreground(styles.TextMuted)
	ta.BlurredStyle.Placeholder = bg.Foreground(styles.TextMuted)

	return Model{
		input:       ta,
		channelName: channelName,
	}
}

func (m *Model) SetChannel(name string) {
	m.channelName = name
	m.input.Placeholder = "Message #" + name + "... (i to insert)"
}

func (m *Model) Focus() tea.Cmd {
	m.input.Placeholder = "" // hide placeholder when focused
	return m.input.Focus()
}

func (m *Model) Blur() {
	m.input.Placeholder = "Message #" + m.channelName + "... (i to insert)"
	m.input.Blur()
}

func (m *Model) Value() string {
	return m.input.Value()
}

func (m *Model) SetValue(s string) {
	m.input.SetValue(s)
}

func (m *Model) Reset() {
	m.input.Reset()
	m.input.SetHeight(1)
}

// visualLineCount returns the number of visual lines the text occupies,
// accounting for soft wraps when a line exceeds the textarea width.
func (m Model) visualLineCount() int {
	val := m.input.Value()
	if val == "" {
		return 1
	}
	w := m.input.Width()
	if w <= 0 {
		return m.input.LineCount()
	}
	total := 0
	for _, line := range strings.Split(val, "\n") {
		lineWidth := runewidth.StringWidth(line)
		if lineWidth == 0 {
			total++
		} else {
			total += (lineWidth + w - 1) / w // ceiling division
		}
	}
	if total < 1 {
		total = 1
	}
	return total
}

// SetWidth updates the textarea's internal width so text wraps correctly.
func (m *Model) SetWidth(width int) {
	innerWidth := width - 5 // account for left border + left/right padding
	if innerWidth < 10 {
		innerWidth = 10
	}
	if m.width != width {
		m.width = width
		m.input.SetWidth(innerWidth)
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)

	// If mention picker is active, intercept keys
	if m.mentionActive && isKey {
		return m.handleMentionKey(keyMsg)
	}

	// Normal textarea update
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Check if @ was just typed at a word boundary
	if isKey && keyMsg.Type == tea.KeyRunes && len(keyMsg.Runes) == 1 && keyMsg.Runes[0] == '@' {
		val := m.input.Value()
		cursorAbsPos := m.cursorPosition()
		// The @ is at cursorAbsPos-1 (just typed)
		atPos := cursorAbsPos - 1
		if atPos >= 0 && atPos < len(val) && val[atPos] == '@' {
			if atPos == 0 || val[atPos-1] == ' ' || val[atPos-1] == '\n' {
				m.mentionActive = true
				m.mentionStartCol = cursorAbsPos // cursor is after the @
				m.mentionPicker.Open()
			}
		}
	}

	m.autoGrow()
	return m, cmd
}

// cursorPosition computes the absolute byte offset of the cursor within
// the textarea's Value() string, using Line() (logical line number) and
// LineInfo() (column offset within the logical line, in rune space).
func (m Model) cursorPosition() int {
	val := m.input.Value()
	lines := strings.Split(val, "\n")
	pos := 0
	curLine := m.input.Line()
	for i := 0; i < curLine && i < len(lines); i++ {
		pos += len(lines[i]) + 1 // +1 for \n
	}
	// Get the rune offset within the current line
	li := m.input.LineInfo()
	col := li.StartColumn + li.ColumnOffset
	// Convert rune offset to byte offset within this line
	if curLine < len(lines) {
		runes := []rune(lines[curLine])
		if col > len(runes) {
			col = len(runes)
		}
		pos += len(string(runes[:col]))
	}
	return pos
}

// autoGrow adjusts the textarea height to match the visual line count.
func (m *Model) autoGrow() {
	lines := m.visualLineCount()
	if lines < 1 {
		lines = 1
	}
	if lines > m.input.MaxHeight {
		lines = m.input.MaxHeight
	}
	if m.input.Height() != lines {
		m.input.SetHeight(lines)
		// Force viewport recalculation by re-setting the value.
		val := m.input.Value()
		m.input.SetValue(val)
	}
}

// handleMentionKey processes key events when the mention picker is active.
func (m Model) handleMentionKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyUp || msg.Type == tea.KeyCtrlP:
		m.mentionPicker.MoveUp()
		return m, nil

	case msg.Type == tea.KeyDown || msg.Type == tea.KeyCtrlN:
		m.mentionPicker.MoveDown()
		return m, nil

	case msg.Type == tea.KeyEnter || msg.Type == tea.KeyTab:
		result := m.mentionPicker.Select()
		if result != nil {
			m.insertMention(result)
		}
		m.mentionActive = false
		m.mentionPicker.Close()
		return m, nil

	case msg.Type == tea.KeyEscape:
		m.mentionActive = false
		m.mentionPicker.Close()
		return m, nil

	case msg.Type == tea.KeyBackspace:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		pos := m.cursorPosition()
		if pos < m.mentionStartCol {
			m.mentionActive = false
			m.mentionPicker.Close()
		} else {
			m.updateMentionQuery()
		}
		m.autoGrow()
		return m, cmd

	case msg.Type == tea.KeyRunes:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.updateMentionQuery()
		m.autoGrow()
		return m, cmd

	default:
		m.mentionActive = false
		m.mentionPicker.Close()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.autoGrow()
		return m, cmd
	}
}

// updateMentionQuery extracts the text between the @ trigger and the cursor
// and updates the mention picker's filter query.
func (m *Model) updateMentionQuery() {
	val := m.input.Value()
	pos := m.cursorPosition()
	if pos > len(val) {
		pos = len(val)
	}
	if m.mentionStartCol > pos {
		m.mentionActive = false
		m.mentionPicker.Close()
		return
	}
	query := val[m.mentionStartCol:pos]
	m.mentionPicker.SetQuery(query)
}

// insertMention replaces the @query text with the selected mention.
func (m *Model) insertMention(result *mentionpicker.MentionResult) {
	val := m.input.Value()
	pos := m.cursorPosition()
	atPos := m.mentionStartCol - 1
	if atPos < 0 {
		atPos = 0
	}
	before := val[:atPos]
	after := ""
	if pos < len(val) {
		after = val[pos:]
	}
	newText := before + "@" + result.DisplayName + " " + after
	m.input.SetValue(newText)
}

// SetUsers provides the list of workspace users for mention autocomplete.
func (m *Model) SetUsers(users []mentionpicker.User) {
	m.users = users
	m.mentionPicker.SetUsers(users)
	m.reverseNames = make(map[string]string)
	for _, u := range users {
		if u.DisplayName != "" {
			m.reverseNames[u.DisplayName] = u.ID
		}
	}
}

// IsMentionActive returns whether the mention picker is currently showing.
func (m Model) IsMentionActive() bool {
	return m.mentionActive
}

// CloseMention dismisses the mention picker without selecting.
func (m *Model) CloseMention() {
	m.mentionActive = false
	m.mentionPicker.Close()
}

// TranslateMentionsForSend replaces @DisplayName with <@UserID> in the text.
func (m Model) TranslateMentionsForSend(text string) string {
	// Collect all mention patterns: special mentions + user mentions
	// Use a single pass with longest-first matching to avoid partial corruption
	// (e.g., @here must not match inside @heretic)
	type mentionEntry struct {
		name        string // e.g., "here", "Alice"
		replacement string // e.g., "<!here>", "<@U1234>"
	}

	var entries []mentionEntry

	// Special mentions
	entries = append(entries,
		mentionEntry{"here", "<!here>"},
		mentionEntry{"channel", "<!channel>"},
		mentionEntry{"everyone", "<!everyone>"},
	)

	// User mentions
	for name, userID := range m.reverseNames {
		entries = append(entries, mentionEntry{name, "<@" + userID + ">"})
	}

	// Sort by name length descending to avoid partial matches
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].name) > len(entries[j].name)
	})

	for _, e := range entries {
		mention := "@" + e.name
		for {
			idx := strings.Index(text, mention)
			if idx < 0 {
				break
			}
			// Check that the character after the mention is a word boundary
			end := idx + len(mention)
			if end < len(text) {
				next := text[end]
				if next != ' ' && next != '\n' && next != '\t' && next != ',' && next != '.' && next != '!' && next != '?' && next != ':' && next != ';' && next != ')' && next != '>' {
					// Not a word boundary -- skip this occurrence
					// Move past it to avoid infinite loop
					text = text[:idx] + text[idx:end] + text[end:]
					continue
				}
			}
			text = text[:idx] + e.replacement + text[end:]
		}
	}

	return text
}

// MentionPickerView returns the rendered mention picker dropdown, or "" if not active.
func (m Model) MentionPickerView(width int) string {
	if !m.mentionActive {
		return ""
	}
	return m.mentionPicker.View(width)
}

func (m Model) View(width int, focused bool) string {
	innerWidth := width - 5 // account for left border + left/right padding

	var style = styles.ComposeBox.Width(width - 2)
	if focused {
		style = styles.ComposeInsert.Width(width - 2)
	}

	// If empty and unfocused, render placeholder manually with correct background.
	// When focused, show an empty compose box with cursor (no placeholder).
	if m.input.Value() == "" && !focused {
		placeholder := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Background(styles.SurfaceDark).
			Width(innerWidth).
			Render(m.input.Placeholder)
		return style.Render(placeholder)
	}

	// Wrap textarea output with full-width dark background.
	// The textarea's internal styles use Inline(true) which only covers text,
	// not the full line width. This wrapper ensures consistent background.
	content := lipgloss.NewStyle().
		Background(styles.SurfaceDark).
		Width(innerWidth).
		Render(m.input.View())
	return style.Render(content)
}
