// internal/ui/compose/model.go
package compose

import (
	"fmt"
	"image/color"
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
	"github.com/gammons/slk/internal/usergroups"
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
	userGroups      map[string]string // usergroup ID -> handle for active workspace

	// activeChannelID is the channel currently shown in the messages
	// pane. Drives the InChannel computation when rebuilding the
	// derived mention-picker user list. Updated via SetActiveChannel.
	activeChannelID string
	// channelMembers maps channelID -> set of user IDs. We keep all
	// channels we've seen so a rapid A→B→A switch doesn't lose state,
	// but only the activeChannelID drives the derived picker list.
	channelMembers map[string]map[string]struct{}
	// channelMembersOK records whether membership data has been loaded
	// for a channel. Used to distinguish "no members" (empty set, after
	// load) from "not loaded yet" (default-everyone-in-channel state).
	channelMembersOK map[string]bool

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

	// broadcast is Slack's "Also send to #channel" toggle for thread
	// replies: when true, the next send posts the reply to the parent
	// channel feed as well (reply_broadcast=true). Only meaningful for
	// the thread compose; the channel compose never exposes the toggle.
	// Non-sticky: cleared on send / Reset() and when a different thread
	// opens, so a high-visibility broadcast never silently repeats.
	broadcast bool

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

// ToggleBroadcast flips the "also send to channel" flag and reports
// the new value. Bound to ctrl+o while composing a thread reply.
func (m *Model) ToggleBroadcast() bool {
	m.broadcast = !m.broadcast
	m.dirty()
	return m.broadcast
}

// SetBroadcast explicitly sets the "also send to channel" flag. The
// host clears it when a different thread opens so the toggle never
// silently carries over between threads.
func (m *Model) SetBroadcast(on bool) {
	if m.broadcast == on {
		return
	}
	m.broadcast = on
	m.dirty()
}

// Broadcast reports whether the next send will also post to the
// parent channel.
func (m Model) Broadcast() bool { return m.broadcast }

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
	m.broadcast = false
	m.dirty()
}

// visualLineCount returns the number of visual lines the text occupies,
// accounting for soft wraps when a line exceeds the textarea width.
//
// When a logical line's width is an EXACT multiple of the wrap width
// AND the cursor lands at the very end (the common case during
// typing), the textarea moves the cursor to the start of a new line
// — its height-tracking treats that overflow as occupying an extra
// visual row. We mirror that here so autoGrow expands the visible
// height in lockstep. Without this, typing the wrap-width-th character
// leaves the textarea at height 1 with the cursor on a logical line 2
// that's blank in the viewport — the user sees their content vanish
// for one keystroke until the next keystroke pushes the count past
// the boundary.
func (m Model) visualLineCount() int {
	val := m.input.Value()
	if val == "" {
		return 1
	}
	w := m.input.Width()
	if w <= 0 {
		return m.input.LineCount()
	}
	lines := strings.Split(val, "\n")
	total := 0
	for i, line := range lines {
		lineWidth := lipgloss.Width(line)
		if lineWidth == 0 {
			total++
			continue
		}
		wrapped := (lineWidth + w - 1) / w // ceiling division
		// On the LAST logical line, a content width that is an exact
		// non-zero multiple of w means the next typed character lands
		// at column 0 of an additional visual row. Pre-grow the
		// height by one so the textarea's height-1 viewport doesn't
		// scroll to follow the cursor off-screen.
		if i == len(lines)-1 && lineWidth%w == 0 {
			wrapped++
		}
		total += wrapped
	}
	if total < 1 {
		total = 1
	}
	return total
}

// SetWidth updates the textarea's internal width so text wraps correctly.
//
// We deliberately wrap at `width - 5` rather than the strict content
// area (`width - 3`: border 1 + padding 2). The textarea's own line
// style uses Inline(true), so its bg paint covers only the chars
// behind text — trailing whitespace inside the textarea has no bg.
// View() wraps the textarea output in a lipgloss content style with
// Width(width-3) and Background(innerBG); the extra 2 cols beyond the
// textarea's width let that wrapper paint the trailing edge with the
// compose box's background so the bg flows cleanly to the inner-right
// padding. Without the 2-col margin, the right edge of the textarea
// shows the outer panel's background through.
func (m *Model) SetWidth(width int) {
	innerWidth := width - 5 // textarea wrap, intentionally 2 cols narrower than the visible content area
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
				// Rebuild on open so usergroup entries reflect the
				// registry's current contents — the background
				// usergroups.list fetch may have landed after the
				// last SetUsers/SetActiveChannel rebuild.
				m.rebuildMentionUsers()
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
// The stored list is the canonical workspace roster; the derived
// []mentionpicker.User pushed to the embedded picker is recomputed by
// rebuildMentionUsers, which carries IsExternal forward and computes
// InChannel from the active channel's member set (if loaded).
func (m *Model) SetUsers(users []mentionpicker.User) {
	m.users = users
	m.reverseNames = make(map[string]string)
	for _, u := range users {
		if u.DisplayName != "" {
			m.reverseNames[u.DisplayName] = u.ID
		}
	}
	m.rebuildMentionUsers()
}

// SetUserGroups provides the active workspace's Slack usergroups for
// @handle autocomplete and send-time <!subteam^SID|@handle> translation.
func (m *Model) SetUserGroups(groups map[string]string) {
	m.userGroups = usergroups.Copy(groups)
	m.rebuildMentionUsers()
}

// SetActiveChannel tells the compose model which channel context to
// use when computing InChannel for the mention picker. Called from
// App on every ChannelSelectedMsg. Idempotent: a no-op if the channel
// hasn't actually changed (avoids needless picker rebuilds on the
// hot reselect path).
func (m *Model) SetActiveChannel(channelID string) {
	if m.activeChannelID == channelID {
		return
	}
	m.activeChannelID = channelID
	m.rebuildMentionUsers()
}

// ActiveChannel returns the channel ID currently active for
// mention-picker context.
func (m *Model) ActiveChannel() string { return m.activeChannelID }

// SetChannelMembership records the member set for a channel and, if
// that channel is active, rebuilds the picker's user list. Membership
// for non-active channels is still stored so a rapid A→B→A switch
// doesn't lose state; only the active channel drives the visible
// picker view.
func (m *Model) SetChannelMembership(channelID string, memberIDs []string) {
	set := make(map[string]struct{}, len(memberIDs))
	for _, id := range memberIDs {
		set[id] = struct{}{}
	}
	if m.channelMembers == nil {
		m.channelMembers = map[string]map[string]struct{}{}
	}
	if m.channelMembersOK == nil {
		m.channelMembersOK = map[string]bool{}
	}
	m.channelMembers[channelID] = set
	m.channelMembersOK[channelID] = true
	if channelID == m.activeChannelID {
		m.rebuildMentionUsers()
	}
}

// MentionUsers returns the most recent derived user list handed to
// the embedded picker. Intended for tests.
func (m *Model) MentionUsers() []mentionpicker.User {
	return m.mentionPicker.Users()
}

// rebuildMentionUsers recomputes the derived []mentionpicker.User
// for the active channel and pushes it to the embedded picker.
//
// Rules:
//   - If membership data for activeChannelID is not yet loaded, every
//     user defaults to InChannel=true (preserves today's behavior on
//     first switch; spec's "Loading state" section).
//   - If membership is loaded, InChannel = userID ∈ memberIDs.
//   - External users that are NOT in the active channel are omitted
//     entirely (spec's "External user visibility rule" — no
//     "(ext) (not in channel)" combination).
func (m *Model) rebuildMentionUsers() {
	members := m.channelMembers[m.activeChannelID]
	loaded := m.channelMembersOK[m.activeChannelID]

	derived := make([]mentionpicker.User, 0, len(m.users))
	for _, u := range m.users {
		inCh := true
		if loaded {
			_, inCh = members[u.ID]
		}
		if u.IsExternal && !inCh {
			continue
		}
		u.InChannel = inCh
		derived = append(derived, u)
	}
	// Usergroups (@backend-team etc.) come from the active workspace's
	// usergroup map rather than m.users: they aren't channel members,
	// and pinging one is channel-agnostic, so they bypass the membership
	// filter and always rank in the in-channel tier. The "usergroup:"
	// ID prefix keeps them out of the <@UID> translation path —
	// TranslateMentionsForSend maps their handle via m.userGroups
	// instead.
	for id, handle := range m.userGroups {
		derived = append(derived, mentionpicker.User{
			ID:          "usergroup:" + id,
			DisplayName: handle,
			Username:    handle,
			InChannel:   true,
		})
	}
	m.mentionPicker.SetUsers(derived)
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
	// rank breaks ties when two kinds of mention share a name (a user
	// and a usergroup can both be "@design") -- lower wins.
	type entry struct {
		trigger     byte
		name        string
		replacement string
		rank        int
	}
	const (
		rankSpecial = iota
		rankUser
		rankUserGroup
		rankChannel
	)

	var entries []entry

	// Special @ mentions
	entries = append(entries,
		entry{'@', "here", "<!here>", rankSpecial},
		entry{'@', "channel", "<!channel>", rankSpecial},
		entry{'@', "everyone", "<!everyone>", rankSpecial},
	)

	// User mentions
	for name, userID := range m.reverseNames {
		entries = append(entries, entry{'@', name, "<@" + userID + ">", rankUser})
	}

	// Usergroup mentions. Like channels below, we emit the labeled
	// wire form <!subteam^SID|@handle> so the optimistically-added
	// echo renders the handle even if the server echo arrives before
	// usergroups.list refreshes this workspace's map.
	for id, handle := range m.userGroups {
		entries = append(entries, entry{'@', handle, "<!subteam^" + id + "|@" + handle + ">", rankUserGroup})
	}

	// Channel mentions. We emit <#CHANNELID|name> rather than the
	// shorter <#CHANNELID> because the messages-pane renderer uses
	// the regex `<#[A-Z0-9]+\|([^>]+)>` to recover the display name --
	// without the embedded name, the user's own optimistically-added
	// message would render as the raw "<#CID>" until the next reload.
	// Slack accepts both forms on the wire and normalizes echoes
	// to the |name form anyway.
	for name, channelID := range m.reverseChannels {
		entries = append(entries, entry{'#', name, "<#" + channelID + "|" + name + ">", rankChannel})
	}

	// Sort by name length descending. This lives across both trigger
	// kinds because the substring-corruption concern is per-trigger:
	// the loop below builds the search needle as trigger+name, so a
	// longer @name still won't collide with a shorter #name even when
	// they share the same suffix.
	//
	// sort.Slice is unstable and the entries above come out of map
	// iteration, so equal-length names need an explicit tie-break or
	// the same text translates to different wire forms run to run.
	// Earlier entries win: the loop below rewrites every occurrence, so
	// by the time a later entry with the same name is tried, the text
	// no longer contains it. Precedence is rank (a name shared by a
	// person and a usergroup resolves to the person), then name, then
	// replacement to settle same-rank duplicates.
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if len(a.name) != len(b.name) {
			return len(a.name) > len(b.name)
		}
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.name != b.name {
			return a.name < b.name
		}
		return a.replacement < b.replacement
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

// SetEmojiContext forwards the emoji-image rendering context to the
// underlying autocomplete picker. Called from App at startup and
// whenever the customs map changes.
func (m *Model) SetEmojiContext(ctx emojipicker.EmojiContext) {
	m.emojiPicker.SetEmojiContext(ctx)
}

// SetEmojiCustoms forwards the workspace customs map to the underlying
// autocomplete picker so URL lookup at View() time can resolve custom
// emoji names. Called from App.SetCustomEmoji when CustomEmojisLoadedMsg
// arrives.
func (m *Model) SetEmojiCustoms(customs map[string]string) {
	m.emojiPicker.SetEmojiCustoms(customs)
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

// renderBroadcastHint returns the "also send to channel" indicator
// shown under the input while the broadcast toggle is on, or "" when
// it is off. innerBG must match the background used by the input box
// so the hint row reads as part of the compose area.
func (m Model) renderBroadcastHint(width int, innerBG color.Color) string {
	if !m.broadcast {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(styles.Accent).
		Background(innerBG).
		Width(width).
		Render(fmt.Sprintf("↪ also send to #%s  (ctrl+o to cancel)", m.channelName))
}

func (m Model) View(width int, focused bool) string {
	chips := m.renderChips(width)

	// ComposeBox has BorderLeft(1) + Padding(1,1,1,1) = 3 chars overhead.
	// lipgloss Width includes padding but excludes border.
	// Total rendered = Width + border = (width-1) + 1 = width.
	innerWidth := width - 3 // content area: width - border(1) - padding(2)
	if innerWidth < 1 {
		innerWidth = 1
	}

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

	var content string
	// If empty and unfocused, render placeholder manually with correct background.
	// When focused, show an empty compose box with cursor (no placeholder).
	if m.input.Value() == "" && !focused {
		content = lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Background(innerBG).
			Width(innerWidth).
			Render(m.input.Placeholder)
	} else {
		// Wrap textarea output with full-width tinted background.
		// The textarea's internal styles use Inline(true) which only covers text,
		// not the full line width. This wrapper ensures consistent background.
		content = lipgloss.NewStyle().
			Background(innerBG).
			Foreground(styles.TextPrimary).
			Width(innerWidth).
			Render(m.input.View())
	}

	hint := m.renderBroadcastHint(innerWidth, innerBG)
	if hint != "" {
		content = content + "\n" + hint
	}
	box := style.Render(content)

	if chips == "" {
		return box
	}
	return lipgloss.JoinVertical(lipgloss.Left, chips, box)
}
