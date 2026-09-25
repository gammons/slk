// internal/ui/statusbar/model.go
package statusbar

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
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
	presence    string // "active", "away", or "" (unknown — segment hidden)
	dndEnabled  bool
	dndEndTS    time.Time // zero if not in DND
	syncing     bool      // true while a background cache-verify fetch is in flight
	helpHint    string    // muted text rendered in the gap area; empty disables
	commandLine string    // "" == inactive; otherwise the :prompt shown in place of channel/workspace
	search      string    // search segment ("/query  3/17" indicator / live prompt); "" hides
	version     int64
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
	if m.commandLine != s {
		m.commandLine = s
		m.dirty()
	}
}

// SetToast displays an arbitrary string in the right-side toast slot. Pass ""
// to clear. Callers are responsible for clearing the toast (typically via a
// tea.Tick that delivers CopiedClearMsg).
func (m *Model) SetToast(s string) {
	if m.toast != s {
		m.toast = s
		m.dirty()
	}
}

// ShowCopied is a backwards-compatible shim that sets the toast to
// "Copied N chars". Pass 0 for a no-op.
func (m *Model) ShowCopied(n int) {
	if n <= 0 {
		return
	}
	m.SetToast(fmt.Sprintf("Copied %d chars", n))
}

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

	// The :command prompt owns the left side while active (the
	// channel/workspace/search segments return when it clears).
	var left string
	if m.commandLine != "" {
		cmdInfo := styles.StatusBar.Render(fmt.Sprintf(" %s▌ ", m.commandLine))
		left = lipgloss.JoinHorizontal(lipgloss.Center, modeLabel, cmdInfo)
	} else {
		left = lipgloss.JoinHorizontal(lipgloss.Center, modeLabel, channelInfo, wsInfo, searchInfo)
	}

	// Render separators and trailing padding with the SurfaceDark background so
	// the right-side pills read as one continuous bar even when the terminal's
	// default background differs from the theme's surface_dark color (e.g. a
	// dark terminal running a light slk theme, which previously left a stray
	// dark cell between segments).
	sep := lipgloss.NewStyle().Background(styles.SurfaceDark).Render(" ")
	trailing := lipgloss.NewStyle().Background(styles.SurfaceDark).Render("  ")

	rightContent := ""
	for i, p := range rightParts {
		if i > 0 {
			rightContent += sep
		}
		rightContent += p
	}
	rightContent += trailing // trailing padding (extra space for unicode width variance)

	// Right-align so the rightmost segment ends at column (width-1),
	// matching the inner right edge of the message-pane / thread-pane
	// compose box (which ends 1 column before the screen edge -- the
	// rightmost panel's right border occupies that last column on
	// content rows above). Without this gap, the connection indicator
	// overhangs visually past the compose box.
	const rightGutter = 1
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
// selection to the clipboard. App handles it by calling ShowCopied and
// scheduling a ClearCopied after a short delay.
type CopiedMsg struct {
	N int
}

// CopiedClearMsg is the follow-up tick that clears the toast.
type CopiedClearMsg struct{}

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
