// internal/ui/compose/model.go
package compose

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/channelpicker"
	"github.com/gammons/slk/internal/ui/emojipicker"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/styles"
)

// PendingAttachment is a file (or in-memory image) waiting to be
// uploaded with the next send. Bytes and Path are mutually exclusive:
// Bytes is set for clipboard-pasted images; Path is set for
// file-path-pasted files (read at upload time, not at attach time).
type PendingAttachment struct {
	Filename string
	Bytes    []byte // non-nil for clipboard images
	Path     string // non-empty for file-path attachments
	Mime     string
	Size     int64
}

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

	// Channel picker state. Mirrors the mention picker exactly:
	// channelStartCol is the byte offset of the FIRST CHARACTER AFTER
	// the trigger '#' within input.Value() (the '#' itself sits at
	// channelStartCol-1). channels is the available channel set;
	// reverseChannels maps channel-name -> channel-ID for outbound
	// translation in TranslateMentionsForSend.
	channelPicker   channelpicker.Model
	channelActive   bool
	channelStartCol int
	channels        []channelpicker.Channel
	reverseChannels map[string]string // channel name -> channel ID

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

	// pending lists attachments queued for the next send. Cleared on
	// successful submit; preserved on failure for retry.
	pending []PendingAttachment

	// uploading is true while attachments are mid-upload. Causes the
	// chip row to render in muted style and the Update() to refuse
	// Esc / Backspace-clear.
	uploading bool

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
	// MaxHeight is the textarea's logical-line cap, which also gates
	// InsertNewline via atContentLimit(). We want users to be able to
	// compose arbitrarily long multi-line drafts (Slack itself imposes
	// no line cap below the 40k char limit), so we set this high. The
	// *visible* row cap is enforced separately in autoGrow() via
	// composeMaxVisibleRows so the box still scrolls internally instead
	// of pushing the rest of the UI off-screen.
	ta.MaxHeight = 1000
	// DynamicHeight delegates height-tracking to the textarea itself,
	// which calls recalculateHeight() after every input mutation
	// (insert / delete / paste / SetValue). Without it, the textarea
	// keeps its initial height of 1 forever and a soft-wrapped long
	// line is invisibly scrolled inside a single-row viewport. Our
	// autoGrow() also adjusts height, but only fires from compose's
	// Update path; SetValue (used by emoji/mention insertion) bypassed
	// it and could leave the textarea visually stuck at 1 row.
	ta.DynamicHeight = true
	ta.MinHeight = 1
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

// AddAttachment appends a pending attachment. Newest is last.
func (m *Model) AddAttachment(a PendingAttachment) {
	m.pending = append(m.pending, a)
	m.dirty()
}

// RemoveLastAttachment removes the most-recently-added pending
// attachment and returns it. Returns ok=false if pending is empty.
func (m *Model) RemoveLastAttachment() (PendingAttachment, bool) {
	if len(m.pending) == 0 {
		return PendingAttachment{}, false
	}
	last := m.pending[len(m.pending)-1]
	m.pending = m.pending[:len(m.pending)-1]
	m.dirty()
	return last, true
}

// Attachments returns a copy of the current pending attachments.
func (m *Model) Attachments() []PendingAttachment {
	if len(m.pending) == 0 {
		return nil
	}
	out := make([]PendingAttachment, len(m.pending))
	copy(out, m.pending)
	return out
}

// ClearAttachments removes all pending attachments.
func (m *Model) ClearAttachments() {
	if len(m.pending) == 0 {
		return
	}
	m.pending = nil
	m.dirty()
}

// SetUploading sets the uploading flag, which causes the chip row to
// render in muted style and certain inputs to be ignored.
func (m *Model) SetUploading(on bool) {
	if m.uploading == on {
		return
	}
	m.uploading = on
	m.dirty()
}

// Uploading reports whether an upload is currently in flight.
func (m *Model) Uploading() bool { return m.uploading }

// CursorAtFirstLine reports whether the textarea cursor is on the
// first (topmost) line.
func (m *Model) CursorAtFirstLine() bool {
	return m.input.Line() == 0
}

// CursorAtLastLine reports whether the textarea cursor is on the
// last (bottom-most) line.
func (m *Model) CursorAtLastLine() bool {
	return m.input.Line() >= m.input.LineCount()-1
}

// MoveCursorToStart positions the cursor at column 0 of the first
// line.
func (m *Model) MoveCursorToStart() {
	for m.input.Line() > 0 {
		m.input.CursorUp()
	}
	m.input.CursorStart()
	m.dirty()
}

// MoveCursorToEnd positions the cursor at the end of the last line.
func (m *Model) MoveCursorToEnd() {
	for m.input.Line() < m.input.LineCount()-1 {
		m.input.CursorDown()
	}
	m.input.CursorEnd()
	m.dirty()
}

func (m *Model) Reset() {
	m.input.Reset()
	m.input.SetHeight(1)
	m.mentionActive = false
	m.mentionPicker.Close()
	m.channelActive = false
	m.channelPicker.Close()
	m.emojiActive = false
	m.emojiPicker.Close()
	m.pending = nil
	m.uploading = false
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

	// Backspace at column 0 of an empty textarea removes the last
	// pending attachment instead of forwarding to the textarea.
	// Skipped while uploading or while a picker is active (those
	// have their own Backspace semantics).
	if isKey && !m.uploading && !m.emojiActive && !m.mentionActive && !m.channelActive &&
		len(m.pending) > 0 &&
		keyMsg.Key().Code == tea.KeyBackspace &&
		m.input.Value() == "" &&
		m.input.Line() == 0 &&
		m.input.Column() == 0 {
		m.RemoveLastAttachment()
		m.dirty()
		return m, nil
	}

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
	// If channel picker is active, intercept keys. Mirrors the mention
	// picker exactly, including precedence: emoji > mention > channel.
	// In practice the three are mutually exclusive (each requires its
	// own trigger char and word boundary) but the precedence still
	// matters for the empty-trigger edge case.
	if m.channelActive && isKey {
		m2, cmd := m.handleChannelKey(keyMsg)
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

	// Channel trigger: '#' at a word boundary, mirroring the @ logic
	// above. Sharing identical word-boundary semantics keeps "after a
	// space, newline, or at start-of-text" as the only place a # opens
	// the picker -- so URLs, anchors mid-word ("issue#42"), and so on
	// don't accidentally fire the popup.
	if isKey && keyMsg.Key().Text == "#" {
		val := m.input.Value()
		cursorAbsPos := m.cursorPosition()
		hashPos := cursorAbsPos - 1
		if hashPos >= 0 && hashPos < len(val) && val[hashPos] == '#' {
			if hashPos == 0 || val[hashPos-1] == ' ' || val[hashPos-1] == '\n' {
				m.channelActive = true
				m.channelStartCol = cursorAbsPos // cursor is after the #
				m.channelPicker.Open()
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

// composeMaxVisibleRows caps the on-screen height of the compose box.
// Beyond this, the textarea scrolls internally rather than pushing the
// messages pane off-screen. Decoupled from textarea.MaxHeight (the
// logical-line content cap) so users can compose drafts longer than
// what fits on screen.
const composeMaxVisibleRows = 20

// autoGrow adjusts the textarea height to match the visual line count.
func (m *Model) autoGrow() {
	lines := m.visualLineCount()
	if lines < 1 {
		lines = 1
	}
	if lines > composeMaxVisibleRows {
		lines = composeMaxVisibleRows
	}
	if m.input.Height() != lines {
		// SetHeight alone is sufficient for the modern textarea
		// library to reflow its viewport. Earlier versions required
		// a follow-up SetValue to force a recalculation, but that
		// resets the cursor (a real bug for any keystroke that moves
		// the cursor while also triggering a height change — e.g.,
		// Up arrow on a multi-line draft).
		m.input.SetHeight(lines)
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

// handleChannelKey processes key events when the channel picker is active.
// Mirrors handleMentionKey one-to-one; kept as a separate method so the
// two pickers can diverge later (e.g. a different completion glyph)
// without entangling their state machines.
func (m Model) handleChannelKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.Key()
	switch {
	case k.Code == tea.KeyUp || (k.Code == 'p' && k.Mod == tea.ModCtrl):
		m.channelPicker.MoveUp()
		return m, nil

	case k.Code == tea.KeyDown || (k.Code == 'n' && k.Mod == tea.ModCtrl):
		m.channelPicker.MoveDown()
		return m, nil

	case k.Code == tea.KeyEnter || k.Code == tea.KeyTab:
		result := m.channelPicker.Select()
		if result != nil {
			m.insertChannel(result)
		}
		m.channelActive = false
		m.channelPicker.Close()
		return m, nil

	case k.Code == tea.KeyEscape:
		m.channelActive = false
		m.channelPicker.Close()
		return m, nil

	case k.Code == tea.KeyBackspace:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		pos := m.cursorPosition()
		if pos < m.channelStartCol {
			m.channelActive = false
			m.channelPicker.Close()
		} else {
			m.updateChannelQuery()
		}
		m.autoGrow()
		return m, cmd

	case len(k.Text) > 0:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.updateChannelQuery()
		m.autoGrow()
		return m, cmd

	default:
		m.channelActive = false
		m.channelPicker.Close()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.autoGrow()
		return m, cmd
	}
}

// updateChannelQuery extracts the text between the # trigger and the
// cursor and updates the channel picker's filter query.
func (m *Model) updateChannelQuery() {
	val := m.input.Value()
	pos := m.cursorPosition()
	if pos > len(val) {
		pos = len(val)
	}
	if m.channelStartCol > pos {
		m.channelActive = false
		m.channelPicker.Close()
		return
	}
	query := val[m.channelStartCol:pos]
	m.channelPicker.SetQuery(query)
}

// insertChannel replaces the #query text with the selected channel name
// (preserving the visible "#name" form). The actual <#CHANNELID> wire
// translation happens at send time in TranslateMentionsForSend so the
// user sees a readable draft.
func (m *Model) insertChannel(result *channelpicker.ChannelResult) {
	val := m.input.Value()
	pos := m.cursorPosition()
	hashPos := m.channelStartCol - 1
	if hashPos < 0 {
		hashPos = 0
	}
	before := val[:hashPos]
	after := ""
	if pos < len(val) {
		after = val[pos:]
	}
	newText := before + "#" + result.Name + " " + after
	m.input.SetValue(newText)
}

// SetChannels provides the list of workspace channels for # autocomplete.
// Mirrors SetUsers: stores the list, pushes it into the picker, and
// builds the reverse map used by TranslateMentionsForSend.
func (m *Model) SetChannels(channels []channelpicker.Channel) {
	m.channels = channels
	m.channelPicker.SetChannels(channels)
	m.reverseChannels = make(map[string]string)
	for _, c := range channels {
		if c.Name != "" {
			m.reverseChannels[c.Name] = c.ID
		}
	}
}

// IsChannelActive returns whether the channel picker is currently showing.
func (m Model) IsChannelActive() bool { return m.channelActive }

// CloseChannel dismisses the channel picker without selecting.
func (m *Model) CloseChannel() {
	m.channelActive = false
	m.channelPicker.Close()
}

// ChannelPickerView returns the rendered channel picker dropdown, or "" if not active.
func (m Model) ChannelPickerView(width int) string {
	if !m.channelActive {
		return ""
	}
	return m.channelPicker.View(width)
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

// TranslateMentionsForSend replaces @DisplayName with <@UserID> and
// #channel-name with <#CHANNELID> in the text. Both kinds of mention
// share one translation pass because they share the same word-boundary
// and longest-first sorting concerns -- e.g., a longer channel name
// must be tried before a shorter one with the same prefix, and a user
// named "here" (if such a thing existed) must not match inside @heretic.
func (m Model) TranslateMentionsForSend(text string) string {
	// trigger is the leading character ('@' for users/specials, '#' for
	// channels); name is what follows; replacement is the wire form.
	type entry struct {
		trigger     byte
		name        string
		replacement string
	}

	var entries []entry

	// Special @ mentions
	entries = append(entries,
		entry{'@', "here", "<!here>"},
		entry{'@', "channel", "<!channel>"},
		entry{'@', "everyone", "<!everyone>"},
	)

	// User mentions
	for name, userID := range m.reverseNames {
		entries = append(entries, entry{'@', name, "<@" + userID + ">"})
	}

	// Channel mentions. We emit <#CHANNELID|name> rather than the
	// shorter <#CHANNELID> because the messages-pane renderer uses
	// the regex `<#[A-Z0-9]+\|([^>]+)>` to recover the display name --
	// without the embedded name, the user's own optimistically-added
	// message would render as the raw "<#CID>" until the next reload.
	// Slack accepts both forms on the wire and normalizes echoes
	// to the |name form anyway.
	for name, channelID := range m.reverseChannels {
		entries = append(entries, entry{'#', name, "<#" + channelID + "|" + name + ">"})
	}

	// Sort by name length descending. This lives across both trigger
	// kinds because the substring-corruption concern is per-trigger:
	// the loop below builds the search needle as trigger+name, so a
	// longer @name still won't collide with a shorter #name even when
	// they share the same suffix.
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].name) > len(entries[j].name)
	})

	for _, e := range entries {
		mention := string(e.trigger) + e.name
		searchFrom := 0
		for {
			rel := strings.Index(text[searchFrom:], mention)
			if rel < 0 {
				break
			}
			idx := searchFrom + rel
			end := idx + len(mention)
			// Require a word boundary after the mention so #general
			// doesn't match inside #general-help, etc.
			if end < len(text) {
				next := text[end]
				if next != ' ' && next != '\n' && next != '\t' && next != ',' && next != '.' && next != '!' && next != '?' && next != ':' && next != ';' && next != ')' && next != '>' {
					searchFrom = end
					continue
				}
			}
			text = text[:idx] + e.replacement + text[end:]
			searchFrom = idx + len(e.replacement)
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

// formatChipSize formats a byte count as "12 KB", "3.4 MB", or "<1 KB".
func formatChipSize(size int64) string {
	const kb = 1024
	const mb = 1024 * kb
	switch {
	case size >= mb:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(mb))
	case size >= kb:
		return fmt.Sprintf("%d KB", size/kb)
	default:
		return "<1 KB"
	}
}

// renderChips returns the rendered chip row for current pending
// attachments, or "" if there are none. Width is the available
// horizontal space; chip-row content is constrained via MaxWidth so
// long chip rows wrap rather than extending past the compose box.
//
// While uploading, chips render in muted color so the user can see
// they're in flight.
func (m Model) renderChips(width int) string {
	if len(m.pending) == 0 {
		return ""
	}
	bg := styles.SurfaceDark
	fg := styles.TextPrimary
	if m.uploading {
		fg = styles.TextMuted
	}

	chipStyle := lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Padding(0, 1).
		MarginRight(1)

	const maxNameLen = 32
	var rendered []string
	for _, p := range m.pending {
		name := p.Filename
		runes := []rune(name)
		if len(runes) > maxNameLen {
			name = string(runes[:maxNameLen-1]) + "…"
		}
		label := fmt.Sprintf("📎 %s %s", name, formatChipSize(p.Size))
		rendered = append(rendered, chipStyle.Render(label))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	return lipgloss.NewStyle().MaxWidth(width).Render(row)
}

func (m Model) View(width int, focused bool) string {
	chips := m.renderChips(width)

	// ComposeBox has BorderLeft(1) + Padding(1,1,1,1) = 3 chars overhead.
	// lipgloss Width includes padding but excludes border.
	// Total rendered = Width + border = (width-1) + 1 = width.
	innerWidth := width - 3 // content area: width - border(1) - padding(2)

	var style = styles.ComposeBox.Width(width - 1)
	if focused {
		style = styles.ComposeInsert.Width(width - 1)
	}

	// Pick the inner background to match the outer style: ComposeInsertBG
	// when focused, SurfaceDark when not. Without this, the textarea's
	// internal Inline(true) styles only paint behind text and the row's
	// trailing whitespace shows the WRONG bg.
	innerBG := styles.SurfaceDark
	if focused {
		innerBG = styles.ComposeInsertBG
	}

	var box string
	// If empty and unfocused, render placeholder manually with correct background.
	// When focused, show an empty compose box with cursor (no placeholder).
	if m.input.Value() == "" && !focused {
		placeholder := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Background(innerBG).
			Width(innerWidth).
			Render(m.input.Placeholder)
		box = style.Render(placeholder)
	} else {
		// Wrap textarea output with full-width tinted background.
		// The textarea's internal styles use Inline(true) which only covers text,
		// not the full line width. This wrapper ensures consistent background.
		content := lipgloss.NewStyle().
			Background(innerBG).
			Foreground(styles.TextPrimary).
			Width(innerWidth).
			Render(m.input.View())
		box = style.Render(content)
	}

	if chips == "" {
		return box
	}
	return lipgloss.JoinVertical(lipgloss.Left, chips, box)
}
