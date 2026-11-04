// internal/ui/statusbar/model.go
package statusbar

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/muesli/reflow/truncate"
)

// ConnectionState represents the WebSocket connection status.
type ConnectionState int

const (
	StateConnecting ConnectionState = iota
	StateConnected
	StateDisconnected
)

type Model struct {
	mode        string
	channel     string
	channelType string // "channel" | "private" | "dm" | "group_dm"; drives glyph
	workspace   string
	unreadCount int
	connState   ConnectionState
	inThread    bool
	toast       string // "" == no toast; otherwise rendered verbatim in the right slot
	toastSeq    uint64 // bumped by every SetToast; see ToastSeq
	presence    string // "active", "away", or "" (unknown — segment hidden)
	dndEnabled  bool
	dndEndTS    time.Time // zero if not in DND
	statusText  string    // profile status preset text (e.g. "Working from home"); "" hides the segment
	statusEmoji string    // display glyph for statusText
	syncing     bool      // true while a background cache-verify fetch is in flight
	helpHint    string    // muted text rendered in the gap area; empty disables
	commandLine string    // "" == inactive; otherwise the :prompt shown in place of channel/workspace
	// commandCandidates is the :prompt's Tab-completion list (vim's
	// wildmenu), never non-empty while commandLine is ""; the name at
	// commandCandidateIdx is the one the prompt currently holds.
	commandCandidates   []string
	commandCandidateIdx int
	search              string // search segment ("/query  3/17" indicator / live prompt); "" hides
	version             int64
}

// Version returns a counter that increments any time the View() output could
// change.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

func New() Model {
	return Model{
		mode:      "NORMAL",
		connState: StateConnecting,
	}
}

// SetMode accepts a fmt.Stringer (such as ui.Mode) to avoid circular imports.
func (m *Model) SetMode(mode fmt.Stringer) {
	s := mode.String()
	if m.mode != s {
		m.mode = s
		m.dirty()
	}
}

func (m *Model) SetChannel(name string) {
	if m.channel != name {
		m.channel = name
		m.dirty()
	}
}

// SetChannelType updates the channel type used to pick a glyph in the
// status bar (# for public, \u25c6 for private, \u25cf for dm/group_dm).
func (m *Model) SetChannelType(chType string) {
	if m.channelType != chType {
		m.channelType = chType
		m.dirty()
	}
}

// channelGlyph returns the prefix glyph for the active channel type.
// Non-# glyphs carry a trailing space; "#" sits flush against the name.
func (m Model) channelGlyph() string {
	switch m.channelType {
	case "private":
		return "\u25c6 "
	case "dm", "group_dm":
		return "\u25cf "
	default:
		return "#"
	}
}

func (m *Model) SetWorkspace(name string) {
	if m.workspace != name {
		m.workspace = name
		m.dirty()
	}
}

func (m *Model) SetUnreadCount(count int) {
	if m.unreadCount != count {
		m.unreadCount = count
		m.dirty()
	}
}

func (m *Model) SetConnectionState(state ConnectionState) {
	if m.connState != state {
		m.connState = state
		m.dirty()
	}
}

// SetStatus updates the self-presence and DND segment. presence is one of
// "active", "away", or "" (segment hidden). dndEnabled with a zero or
// future dndEndTS toggles the DND glyph and countdown.
func (m *Model) SetStatus(presence string, dndEnabled bool, dndEndTS time.Time) {
	if m.presence == presence && m.dndEnabled == dndEnabled && m.dndEndTS.Equal(dndEndTS) {
		return
	}
	m.presence = presence
	m.dndEnabled = dndEnabled
	m.dndEndTS = dndEndTS
	m.dirty()
}

// SetCustomStatus updates the profile status preset segment (e.g.
// "Working from home"). Empty text hides the segment.
func (m *Model) SetCustomStatus(text, emoji string) {
	if m.statusText == text && m.statusEmoji == emoji {
		return
	}
	m.statusText = text
	m.statusEmoji = emoji
	m.dirty()
}

func (m *Model) SetInThread(inThread bool) {
	if m.inThread != inThread {
		m.inThread = inThread
		m.dirty()
	}
}

// SetSyncing toggles a small "verifying" indicator (a single ○ glyph)
// next to the channel name. Used by App's three-tier ChannelSelectedMsg
// dispatch to signal that the displayed cache is being verified
// against the network in the background.
func (m *Model) SetSyncing(syncing bool) {
	if m.syncing == syncing {
		return
	}
	m.syncing = syncing
	m.dirty()
}

// Search returns the current search segment text ("" when hidden).
func (m *Model) Search() string { return m.search }

// maxSearchWidth caps the search segment so a long query can't push
// the bar's left half past the available width and wrap the line.
const maxSearchWidth = 40

// SetSearch sets the search segment rendered after the workspace name
// (the `/query  3/17` indicator and live `/` prompt). "" hides it.
// Text longer than maxSearchWidth runes is elided with a trailing "…"
// (the segment is plain unstyled text, so a rune slice is safe).
func (m *Model) SetSearch(s string) {
	if r := []rune(s); len(r) > maxSearchWidth {
		s = string(r[:maxSearchWidth-1]) + "…"
	}
	if m.search != s {
		m.search = s
		m.dirty()
	}
}

// SetHelpHint sets a muted hint string rendered in the unused space between
// the left segments and the right pills. Pass "" to clear. The hint is
// dropped silently when the bar lacks room for it plus 4 columns of padding.
func (m *Model) SetHelpHint(s string) {
	if m.helpHint != s {
		m.helpHint = s
		m.dirty()
	}
}

// SetCommandLine shows a vi-style command prompt (e.g. ":vsp") in the
// left segment of the bar, replacing the channel/workspace segments
// while non-empty. Pass "" to restore the normal segments. The caller
// owns the ':' prefix; View appends a block-cursor glyph.
func (m *Model) SetCommandLine(s string) {
	changed := m.commandLine != s
	// The candidate list belongs to the prompt that produced it, so
	// clearing the prompt drops it in the same step; it can never outlive
	// the line it was matched against.
	if s == "" && (m.commandCandidates != nil || m.commandCandidateIdx != 0) {
		m.commandCandidates = nil
		m.commandCandidateIdx = 0
		changed = true
	}
	if changed {
		m.commandLine = s
		m.dirty()
	}
}

// SetCommandCandidates sets the completion list shown next to the active
// :prompt (vim's wildmenu), with the name the prompt currently holds at
// idx. Pass nil to hide it. The slice is copied, so the caller keeps
// ownership of its own; the version bumps only when the names or idx
// change, so setting the same list and cursor again does not re-render the
// bar.
func (m *Model) SetCommandCandidates(names []string, idx int) {
	if slices.Equal(m.commandCandidates, names) && m.commandCandidateIdx == idx {
		return
	}
	m.commandCandidates = slices.Clone(names)
	m.commandCandidateIdx = idx
	m.dirty()
}

// SetToast displays an arbitrary string in the right-side toast slot. Pass ""
// to clear. In internal/ui, do not call it with text directly: use
// toastWithClear, which also schedules the toast's clear, or
// toastUntilReplaced (the toast rule in AGENTS.md).
func (m *Model) SetToast(s string) {
	m.toastSeq++
	if m.toast != s {
		m.toast = s
		m.dirty()
	}
}

// ToastSeq identifies the toast now in the slot. Every SetToast call
// bumps it, also one that repeats the text or clears the slot, so a
// clear scheduled for one toast can tell that another replaced it.
func (m Model) ToastSeq() uint64 { return m.toastSeq }

// ClearCopied removes any toast.
func (m *Model) ClearCopied() {
	m.SetToast("")
}

func (m Model) View(width int) string {
	// Mode indicator
	var modeStyle lipgloss.Style
	switch m.mode {
	case "INSERT":
		modeStyle = styles.StatusModeInsert
	case "COMMAND":
		modeStyle = styles.StatusModeCommand
	default:
		modeStyle = styles.StatusMode
	}
	modeLabel := modeStyle.Render(fmt.Sprintf(" %s ", m.mode))

	// Channel info
	glyph := m.channelGlyph()
	channelLabel := fmt.Sprintf(" %s%s ", glyph, m.channel)
	if m.inThread {
		channelLabel = fmt.Sprintf(" %s%s > Thread ", glyph, m.channel)
	}
	channelInfo := styles.StatusBar.Render(channelLabel)
	if m.syncing {
		// Background-matched syncing glyph sits flush against the channel
		// segment so the bar reads as a single continuous strip.
		channelInfo += styles.StatusbarSyncing.Render("○ ")
	}

	// Workspace
	wsInfo := styles.StatusBar.Render(fmt.Sprintf(" %s ", m.workspace))

	// Search prompt / match indicator
	searchInfo := ""
	if m.search != "" {
		searchInfo = styles.StatusBar.Render(" " + m.search + " ")
	}

	// Right side: unread + connection
	var rightParts []string

	if m.unreadCount > 0 {
		rightParts = append(rightParts,
			styles.UnreadBadge.Render(fmt.Sprintf(" %d unread ", m.unreadCount)))
	}

	if m.toast != "" {
		rightParts = append(rightParts,
			lipgloss.NewStyle().
				Foreground(styles.Accent).
				Background(styles.SurfaceDark).
				Bold(true).
				Render(m.toast))
	}

	// The custom-status segment is sized to fit below, once the width
	// left over by every other segment is known; statusInsertIdx marks
	// where it belongs in the visual order (between toast and
	// presence/DND) without needing those segments built around it.
	statusInsertIdx := len(rightParts)

	// Presence + DND segment (hidden when presence is "" and not in DND)
	if m.dndEnabled {
		rightParts = append(rightParts,
			lipgloss.NewStyle().
				Foreground(styles.Warning).
				Background(styles.SurfaceDark).
				Render(formatDND(m.dndEndTS)))
	} else if m.presence == "active" {
		rightParts = append(rightParts,
			lipgloss.NewStyle().
				Foreground(styles.Accent).
				Background(styles.SurfaceDark).
				Render("● Active"))
	} else if m.presence == "away" {
		rightParts = append(rightParts,
			lipgloss.NewStyle().
				Foreground(styles.TextMuted).
				Background(styles.SurfaceDark).
				Render("○ Away"))
	}

	switch m.connState {
	case StateConnected:
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(styles.Accent).Background(styles.SurfaceDark).Render("● Connected"))
	case StateConnecting:
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(styles.Warning).Background(styles.SurfaceDark).Render("● Connecting"))
	case StateDisconnected:
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(styles.Error).Background(styles.SurfaceDark).Render("● Disconnected"))
	}

	// Render separators and trailing padding with the SurfaceDark background so
	// the right-side pills read as one continuous bar even when the terminal's
	// default background differs from the theme's surface_dark color (e.g. a
	// dark terminal running a light slk theme, which previously left a stray
	// dark cell between segments).
	sep := lipgloss.NewStyle().Background(styles.SurfaceDark).Render(" ")
	trailing := lipgloss.NewStyle().Background(styles.SurfaceDark).Render("  ")

	// Right-align so the rightmost segment ends at column (width-1),
	// matching the inner right edge of the message-pane / thread-pane
	// compose box (which ends 1 column before the screen edge -- the
	// rightmost panel's right border occupies that last column on
	// content rows above). Without this gap, the connection indicator
	// overhangs visually past the compose box.
	const rightGutter = 1

	// The :command prompt owns the left side while active (the
	// channel/workspace/search segments return when it clears). Its
	// Tab-completion list follows the prompt -- vim's wildmenu -- and is
	// budgeted the room the prompt leaves before the right-side pills, so
	// it can never push the bar past width. The custom-status segment
	// isn't counted: it is sized below from whatever the list leaves, so
	// a long status can't hide the list the user is cycling through.
	var left string
	// leftGap is the column the status segment's budget keeps free after
	// the completion list (pillGap); 0 outside the prompt.
	leftGap := 0
	if m.commandLine != "" {
		cmdInfo := styles.StatusBar.Render(fmt.Sprintf(" %s▌ ", m.commandLine))
		// pillGap keeps one column between the list and the right-side
		// pills, so a list that fills its room doesn't run into them.
		const pillGap = 1
		room := width - rightGutter - pillGap - lipgloss.Width(modeLabel) -
			lipgloss.Width(cmdInfo) - lipgloss.Width(joinRightParts(rightParts, sep)+trailing)
		left = lipgloss.JoinHorizontal(lipgloss.Center, modeLabel, cmdInfo,
			m.commandCandidatesView(room))
		leftGap = pillGap
	} else {
		left = lipgloss.JoinHorizontal(lipgloss.Center, modeLabel, channelInfo, wsInfo, searchInfo)
	}

	// Size the custom-status segment to whatever room is left after
	// every other segment. Slack allows up to 100 characters of status
	// text — unlike this feature's own two short presets, a status set
	// from the real Slack client has no length slk controls, so it
	// must degrade (truncate, then emoji-only, then dropped) rather
	// than push the bar over width.
	if m.statusText != "" || m.statusEmoji != "" {
		// otherWidth already counts len(rightParts)-1 separators (the
		// ones between the existing parts). Inserting one more part —
		// anywhere among them, not just at an end — always turns that
		// into len(rightParts) separators total: exactly one more than
		// otherWidth already has, never two, regardless of where it
		// lands.
		otherWidth := lipgloss.Width(joinRightParts(rightParts, sep))
		extraSep := 0
		if len(rightParts) > 0 {
			extraSep = 1
		}
		budget := width - rightGutter - leftGap - lipgloss.Width(left) - otherWidth - lipgloss.Width(trailing) - extraSep*lipgloss.Width(sep)
		if seg := fitStatusSegment(m.statusEmoji, m.statusText, budget); seg != "" {
			rightParts = append(rightParts[:statusInsertIdx:statusInsertIdx],
				append([]string{seg}, rightParts[statusInsertIdx:]...)...)
		}
	}

	rightContent := joinRightParts(rightParts, sep) + trailing // trailing padding (extra space for unicode width variance)

	gap := width - rightGutter - lipgloss.Width(left) - lipgloss.Width(rightContent)
	if gap < 0 {
		gap = 0
	}

	// Spacers must render exactly the width they're asked for, so they
	// can't go through styles.StatusBar -- it carries Padding(0, 1) and
	// would add 2 columns to each one.
	spacer := lipgloss.NewStyle().Background(styles.SurfaceDark)

	// Render the help hint into the gap when there's room for it plus a
	// small breathing margin. Drops silently when narrow.
	var filler string
	if m.helpHint != "" {
		hint := styles.StatusBar.
			Italic(true).
			Foreground(styles.TextMuted).
			Render(m.helpHint)
		hintW := lipgloss.Width(hint)
		const hintPadding = 4 // 2 cols of breathing room on each side
		if gap >= hintW+hintPadding {
			leftPad := (gap - hintW) / 2
			rightPad := gap - hintW - leftPad
			filler = spacer.Render(strings.Repeat(" ", leftPad)) +
				hint +
				spacer.Render(strings.Repeat(" ", rightPad))
		}
	}
	if filler == "" {
		filler = spacer.Render(fmt.Sprintf("%*s", gap, ""))
	}
	rightPad := spacer.Render(strings.Repeat(" ", rightGutter))

	return lipgloss.JoinHorizontal(lipgloss.Center, left, filler, rightContent, rightPad)
}

// commandCandidatesView renders the :prompt's Tab-completion list (vim's
// wildmenu) in room columns, or "" when there is no list to show. The
// current name carries the selection highlight; the rest keep the bar's
// colours, so the list reads as part of the prompt rather than a new
// surface. Names are separated by two spaces.
//
// room is what the mode label, the prompt and the right-side pills left
// over, so a long list can never widen the bar past the terminal: the
// list shows a window of names around the current one, a "< " prefix
// marks names dropped on the left and a " >" suffix names dropped on the
// right, as vim's wildmenu does. The current name is the anchor -- the
// user is cycling to it -- so it is never dropped; when even it has no
// room, nothing is drawn at all.
func (m Model) commandCandidatesView(room int) string {
	n := len(m.commandCandidates)
	if n == 0 || room <= 0 {
		return ""
	}
	idx := m.commandCandidateIdx
	if idx < 0 || idx >= n {
		idx = 0
	}
	// The bar's own style carries Padding(0, 1); the list spaces its names
	// itself, so only its colours are wanted here.
	plain := styles.StatusBar.UnsetPadding()
	rendered := make([]string, n)
	widths := make([]int, n)
	for i, name := range m.commandCandidates {
		if i == idx {
			rendered[i] = styles.SelectionStyle().Render(name)
		} else {
			rendered[i] = plain.Render(name)
		}
		widths[i] = lipgloss.Width(rendered[i])
	}

	// Two spaces separate the entries; when names to the left of the window
	// were dropped, a "< " prefix (its own trailing space included) marks
	// them, and a " >" suffix marks names dropped on the right.
	const marker, moreMarker = "< ", " >"
	markerWidth, moreWidth := lipgloss.Width(marker), lipgloss.Width(moreMarker)

	// windowWidth is the rendered width of names[start:end]: the names, two
	// spaces between each pair, the "< " marker when names[0] is hidden and
	// the " >" marker when the last name is.
	windowWidth := func(start, end int) int {
		w := 0
		for i := start; i < end; i++ {
			w += widths[i]
		}
		if end > start {
			w += 2 * (end - start - 1)
		}
		if start > 0 {
			w += markerWidth
		}
		if end < n {
			w += moreWidth
		}
		return w
	}

	// The current name is the anchor — the user is cycling to it — so it is
	// never dropped. If even it (with the markers for the names hidden on
	// either side) has no room, the list is not drawn at all: a truncated command
	// name would name nothing the user could type.
	if windowWidth(idx, idx+1) > room {
		return ""
	}

	start, end := idx, idx+1
	// Grow forward first: the names just past the current one are the ones
	// Tab reaches next.
	for end < n && windowWidth(start, end+1) <= room {
		end++
	}
	// Then take whatever room is left on the left, the current name still
	// anchored.
	for start > 0 && windowWidth(start-1, end) <= room {
		start--
	}

	// The gap between entries carries the bar's background explicitly, the
	// way the right-side separators do: a bare "  " would leave the
	// terminal's default colour showing through on a themed bar.
	gap := lipgloss.NewStyle().Background(styles.SurfaceDark).Render("  ")
	joined := strings.Join(rendered[start:end], gap)
	if start > 0 {
		joined = plain.Render(marker) + joined
	}
	if end < n {
		joined += plain.Render(moreMarker)
	}
	return joined
}

// joinRightParts joins the right-side segments with sep between each,
// matching how they're visually separated in the rendered bar.
func joinRightParts(parts []string, sep string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += sep
		}
		s += p
	}
	return s
}

// fitStatusSegment returns the styled custom-status segment sized to
// fit within maxWidth columns, degrading in steps: the full "emoji
// text", then emoji plus as much text as fits with a trailing
// ellipsis, then emoji alone, then "" if nothing fits. Needed because
// Slack allows up to 100 characters of status text — far longer than
// this feature's own two short presets — so a status set from the
// real Slack client must not be allowed to push the status bar over
// its exact-width budget.
func fitStatusSegment(emoji, text string, maxWidth int) string {
	style := lipgloss.NewStyle().Foreground(styles.Accent).Background(styles.SurfaceDark)
	full := strings.TrimSpace(emoji + " " + text)
	if full == "" || maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(full) <= maxWidth {
		return style.Render(full)
	}
	emojiAlone := func() string {
		if emoji != "" && lipgloss.Width(emoji) <= maxWidth {
			return style.Render(emoji)
		}
		return ""
	}
	if text == "" {
		return emojiAlone()
	}
	prefix := emoji
	if prefix != "" {
		prefix += " "
	}
	prefixWidth := lipgloss.Width(prefix)
	if prefixWidth >= maxWidth {
		return emojiAlone()
	}
	// truncate.StringWithTail is the same width-aware truncator already
	// used elsewhere in this codebase (internal/ui/presencemenu,
	// internal/ui/thread/breadcrumb.go, ...) — reused here rather than
	// hand-rolling rune-by-rune width summation, which can mismeasure
	// combined grapheme clusters (variation selectors, ZWJ sequences)
	// in the status text and let a segment exceed maxWidth.
	truncated := truncate.StringWithTail(text, uint(maxWidth-prefixWidth), "…")
	if truncated == "" || truncated == "…" {
		// Not even one character of text fit: an ellipsis with nothing
		// preceding it reads as broken, so fall back to emoji alone.
		return emojiAlone()
	}
	return style.Render(prefix + truncated)
}

// formatDND renders the DND segment with an optional countdown to the
// snooze end. Zero endTS or past endTS produces a bare "🌙 DND".
func formatDND(endTS time.Time) string {
	if endTS.IsZero() {
		return "🌙 DND"
	}
	d := time.Until(endTS)
	if d <= 0 {
		return "🌙 DND"
	}
	if d < time.Minute {
		return "🌙 DND <1m"
	}
	hours := int(d / time.Hour)
	minutes := int(d % time.Hour / time.Minute)
	if hours > 0 {
		return fmt.Sprintf("🌙 DND %dh %dm", hours, minutes)
	}
	return fmt.Sprintf("🌙 DND %dm", minutes)
}

// CopiedMsg is delivered when the messages or thread pane copies a
// selection to the clipboard. App shows "Copied N chars" through
// toastWithClear, which clears it after a short delay; N <= 0 shows
// nothing.
type CopiedMsg struct {
	N int
}

// CopiedClearMsg is the follow-up tick that clears the toast. Seq is the
// ToastSeq of the toast it clears; when another toast replaced that one,
// the tick leaves the slot alone.
type CopiedClearMsg struct{ Seq uint64 }

// PermalinkCopiedMsg is delivered when a message permalink has been copied to
// the clipboard. App handles it by setting the toast to "Copied permalink"
// and scheduling a CopiedClearMsg.
type PermalinkCopiedMsg struct{}

// PermalinkCopyFailedMsg is delivered when fetching the permalink fails.
// App handles it by setting the toast to "Failed to copy link" and
// scheduling a CopiedClearMsg.
type PermalinkCopyFailedMsg struct{}

// DNDTickMsg is delivered once a minute while DND is active so the
// status bar can refresh its countdown segment. The App schedules the
// tick on each StatusChangeMsg and reschedules from the tick handler
// while DND remains active.
type DNDTickMsg struct{}

// EditFailedMsg is delivered when chat.update fails. App handles by
// showing the toast and scheduling a CopiedClearMsg.
type EditFailedMsg struct{ Reason string }

// SendFailedMsg is delivered when chat.postMessage fails (either for a
// top-level send or a thread reply). App handles by showing the toast
// and scheduling a CopiedClearMsg.
type SendFailedMsg struct{ Reason string }

// DeleteFailedMsg is delivered when chat.delete fails.
type DeleteFailedMsg struct{ Reason string }

// EditNotOwnMsg is delivered when E was pressed on a non-owned message.
type EditNotOwnMsg struct{}

// DeleteNotOwnMsg is delivered when D was pressed on a non-owned message.
type DeleteNotOwnMsg struct{}

// MarkedUnreadMsg is delivered when the user successfully marks a
// message (or thread reply) as unread. App handles by setting the toast
// to "Marked unread" and scheduling a CopiedClearMsg.
type MarkedUnreadMsg struct{}

// MarkUnreadFailedMsg is delivered when conversations.mark or
// subscriptions.thread.mark fail during a mark-unread. App handles by
// setting the toast to "Mark unread failed" and scheduling a
// CopiedClearMsg.
type MarkUnreadFailedMsg struct{ Reason string }

// ThreadSavedMsg is delivered when a thread has been saved to a markdown file.
type ThreadSavedMsg struct{ Path string }

// ThreadSaveFailedMsg is delivered when saving a thread to file fails.
type ThreadSaveFailedMsg struct{ Reason string }
