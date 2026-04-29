// internal/ui/compose/model.go
package compose

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/emojipicker"
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

	// Emoji picker state. emojiStartCol is the byte offset of the FIRST
	// CHARACTER AFTER the trigger ':' within input.Value() (mirrors
	// mentionStartCol semantics; the trigger ':' itself sits at
	// emojiStartCol-1).
	emojiPicker   emojipicker.Model
	emojiActive   bool
	emojiStartCol int

	// placeholderOverride, when non-empty, replaces the default
	// "Message #channel..." placeholder. Used by edit mode to display
	// "Editing message — Enter to save, Esc to cancel".
	placeholderOverride string

	// version increments on every Update / state mutation. Used by App's
	// panel-cache layer so the wrapped compose panel only re-renders when
	// the compose has actually changed.
	version int64
}

// Version returns the render version. Increments on Update and any state
// mutation that alters View() output.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

// defaultPlaceholder returns the default channel-aware placeholder text.
func (m *Model) defaultPlaceholder() string {
	return "Message #" + m.channelName + "... (i to insert)"
}

// effectivePlaceholder returns the override if set, else the default.
func (m *Model) effectivePlaceholder() string {
	if m.placeholderOverride != "" {
		return m.placeholderOverride
	}
	return m.defaultPlaceholder()
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
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark).Foreground(styles.TextPrimary)
	s := ta.Styles()
	s.Focused.Base = bg
	s.Focused.Text = bg
	s.Focused.CursorLine = bg
	s.Focused.EndOfBuffer = bg
	s.Focused.Prompt = bg
	s.Blurred.Base = bg
	s.Blurred.Text = bg
	s.Blurred.CursorLine = bg
	s.Blurred.EndOfBuffer = bg
	s.Blurred.Prompt = bg
	s.Focused.Placeholder = bg.Foreground(styles.TextMuted)
	s.Blurred.Placeholder = bg.Foreground(styles.TextMuted)
	ta.SetStyles(s)

	return Model{
		input:       ta,
		channelName: channelName,
	}
}

// RefreshStyles re-applies textarea styles from current theme colors.
// Call after theme changes.
func (m *Model) RefreshStyles() {
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark).Foreground(styles.TextPrimary)
	s := m.input.Styles()
	s.Focused.Base = bg
	s.Focused.Text = bg
	s.Focused.CursorLine = bg
	s.Focused.EndOfBuffer = bg
	s.Focused.Prompt = bg
	s.Blurred.Base = bg
	s.Blurred.Text = bg
	s.Blurred.CursorLine = bg
	s.Blurred.EndOfBuffer = bg
	s.Blurred.Prompt = bg
	s.Focused.Placeholder = bg.Foreground(styles.TextMuted)
	s.Blurred.Placeholder = bg.Foreground(styles.TextMuted)
	m.input.SetStyles(s)
}

func (m *Model) SetChannel(name string) {
	if m.channelName != name {
		m.channelName = name
		if m.placeholderOverride == "" {
			m.input.Placeholder = m.defaultPlaceholder()
		}
		m.dirty()
	}
}

// SetPlaceholderOverride sets a custom placeholder string. Pass "" to
// clear the override and restore the default channel-aware placeholder.
//
// The override persists across Blur, SetChannel, and Reset, and is
// hidden while the textarea is focused. Callers entering an "edit
// mode" should set the override on entry and clear it on exit.
func (m *Model) SetPlaceholderOverride(text string) {
	m.placeholderOverride = text
	m.input.Placeholder = m.effectivePlaceholder()
	m.dirty()
}

func (m *Model) Focus() tea.Cmd {
	m.input.Placeholder = "" // hide placeholder when focused
	m.dirty()
	return m.input.Focus()
}

func (m *Model) Blur() {
	m.input.Placeholder = m.effectivePlaceholder()
	m.input.Blur()
	m.dirty()
}

func (m *Model) Value() string {
	return m.input.Value()
}

func (m *Model) SetValue(s string) {
	m.input.SetValue(s)
	m.dirty()
}

func (m *Model) Reset() {
	m.input.Reset()
	m.input.SetHeight(1)
	m.mentionActive = false
	m.mentionPicker.Close()
	m.emojiActive = false
	m.emojiPicker.Close()
	m.dirty()
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
		lineWidth := lipgloss.Width(line)
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
		m.dirty()
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)

	// If emoji picker is active, intercept keys (takes precedence over mention).
	if m.emojiActive && isKey {
		m2, cmd := m.handleEmojiKey(keyMsg)
		m2.dirty()
		return m2, cmd
	}
	// If mention picker is active, intercept keys.
	if m.mentionActive && isKey {
		m2, cmd := m.handleMentionKey(keyMsg)
		m2.dirty()
		return m2, cmd
	}

	// Normal textarea update
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Check if @ was just typed at a word boundary
	if isKey && keyMsg.Key().Text == "@" {
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

	// Emoji trigger: ':' at word boundary, plus 2 query chars before the
	// popup opens. We re-check on every keystroke (cheap) so the popup
	// appears the moment the threshold is hit.
	m.maybeOpenEmojiPicker()

	m.autoGrow()
	// Conservative: bump version on every Update. The textarea's internal
	// state (cursor blink, content) almost always changes per call, so a
	// per-call bump is correct and cheaper than introspecting.
	m.dirty()
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
	k := msg.Key()
	switch {
	case k.Code == tea.KeyUp || (k.Code == 'p' && k.Mod == tea.ModCtrl):
		m.mentionPicker.MoveUp()
		return m, nil

	case k.Code == tea.KeyDown || (k.Code == 'n' && k.Mod == tea.ModCtrl):
		m.mentionPicker.MoveDown()
		return m, nil

	case k.Code == tea.KeyEnter || k.Code == tea.KeyTab:
		result := m.mentionPicker.Select()
		if result != nil {
			m.insertMention(result)
		}
		m.mentionActive = false
		m.mentionPicker.Close()
		return m, nil

	case k.Code == tea.KeyEscape:
		m.mentionActive = false
		m.mentionPicker.Close()
		return m, nil

	case k.Code == tea.KeyBackspace:
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

	case len(k.Text) > 0:
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

// SetEmojiEntries provides the searchable emoji list (built-ins + workspace
// customs). Safe to call any time, including while the picker is visible.
func (m *Model) SetEmojiEntries(entries []emoji.EmojiEntry) {
	m.emojiPicker.SetEntries(entries)
	m.dirty()
}

// IsEmojiActive returns whether the emoji picker is currently showing.
func (m Model) IsEmojiActive() bool { return m.emojiActive }

// CloseEmoji dismisses the emoji picker without selecting.
func (m *Model) CloseEmoji() {
	m.emojiActive = false
	m.emojiPicker.Close()
	m.dirty()
}

// EmojiPickerView returns the rendered emoji picker dropdown, or "" if not active.
func (m Model) EmojiPickerView(width int) string {
	if !m.emojiActive {
		return ""
	}
	return m.emojiPicker.View(width)
}

// emojiQueryChar reports whether r is a valid character inside an emoji
// shortcode query (the run of chars after ':' the user is currently typing).
// Mirrors the character set kyokomi recognizes in shortcodes.
func emojiQueryChar(r byte) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_' || r == '+' || r == '-':
		return true
	}
	return false
}

// maybeOpenEmojiPicker scans backward from the cursor to find an emoji
// trigger of the form `:xy` (at start-of-line or after whitespace, with
// at least 2 valid query characters and no closing ':' yet). Opens the
// picker if the threshold is met; updates the query if already open.
func (m *Model) maybeOpenEmojiPicker() {
	val := m.input.Value()
	pos := m.cursorPosition()
	if pos > len(val) {
		pos = len(val)
	}

	// Walk backward from the cursor over query chars to find the trigger ':'.
	i := pos
	for i > 0 && emojiQueryChar(val[i-1]) {
		i--
	}
	// Now val[i:pos] is the candidate query. We need val[i-1] == ':' and
	// either i-1 == 0 or val[i-2] is whitespace.
	if i == 0 || val[i-1] != ':' {
		// No trigger; if we had one open, close it (cursor moved off).
		if m.emojiActive {
			m.emojiActive = false
			m.emojiPicker.Close()
		}
		return
	}
	if i-1 != 0 {
		prev := val[i-2]
		if prev != ' ' && prev != '\t' && prev != '\n' {
			if m.emojiActive {
				m.emojiActive = false
				m.emojiPicker.Close()
			}
			return
		}
	}
	query := val[i:pos]
	if len(query) < 2 {
		// Below threshold; close if open.
		if m.emojiActive {
			m.emojiActive = false
			m.emojiPicker.Close()
		}
		return
	}

	if !m.emojiActive {
		m.emojiActive = true
		m.emojiStartCol = i // first char AFTER the trigger ':'
		m.emojiPicker.Open(query)
	} else {
		m.emojiPicker.SetQuery(query)
	}
}

// handleEmojiKey processes key events when the emoji picker is active.
// Mirrors handleMentionKey.
func (m Model) handleEmojiKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.Key()
	switch {
	case k.Code == tea.KeyUp || (k.Code == 'p' && k.Mod == tea.ModCtrl):
		m.emojiPicker.MoveUp()
		return m, nil

	case k.Code == tea.KeyDown || (k.Code == 'n' && k.Mod == tea.ModCtrl):
		m.emojiPicker.MoveDown()
		return m, nil

	case k.Code == tea.KeyEnter || k.Code == tea.KeyTab:
		if entry, ok := m.emojiPicker.SelectedEntry(); ok {
			m.insertEmoji(entry.Name)
		}
		m.emojiActive = false
		m.emojiPicker.Close()
		m.autoGrow()
		return m, nil

	case k.Code == tea.KeyEscape:
		m.emojiActive = false
		m.emojiPicker.Close()
		return m, nil

	case k.Code == tea.KeyBackspace:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.maybeOpenEmojiPicker()
		m.autoGrow()
		return m, cmd

	case len(k.Text) > 0:
		// If the user types a non-query char (space, ':', punctuation), let
		// the textarea record it, then close the picker.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		// A single rune in k.Text — check the first byte for ASCII set.
		ch := k.Text[0]
		if !emojiQueryChar(ch) {
			m.emojiActive = false
			m.emojiPicker.Close()
		} else {
			m.maybeOpenEmojiPicker()
		}
		m.autoGrow()
		return m, cmd

	default:
		m.emojiActive = false
		m.emojiPicker.Close()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.autoGrow()
		return m, cmd
	}
}

// insertEmoji replaces the in-progress :query (the bytes from the trigger
// ':' through the cursor) with `:name: ` (note the trailing space, so the
// user can continue typing without manually inserting one).
func (m *Model) insertEmoji(name string) {
	val := m.input.Value()
	pos := m.cursorPosition()
	colonPos := m.emojiStartCol - 1 // byte offset of the trigger ':'
	if colonPos < 0 {
		colonPos = 0
	}
	if pos > len(val) {
		pos = len(val)
	}
	before := val[:colonPos]
	after := ""
	if pos < len(val) {
		after = val[pos:]
	}
	newText := before + ":" + name + ": " + after
	m.input.SetValue(newText)
}

func (m Model) View(width int, focused bool) string {
	// ComposeBox has BorderLeft(1) + Padding(1,1,1,1) = 3 chars overhead.
	// lipgloss Width includes padding but excludes border.
	// Total rendered = Width + border = (width-1) + 1 = width.
	innerWidth := width - 3 // content area: width - border(1) - padding(2)

	var style = styles.ComposeBox.Width(width - 1)
	if focused {
		style = styles.ComposeInsert.Width(width - 1)
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
		Foreground(styles.TextPrimary).
		Width(innerWidth).
		Render(m.input.View())
	return style.Render(content)
}
