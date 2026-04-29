// internal/ui/statusbar/model.go
package statusbar

import (
	"fmt"
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
	toast       string    // "" == no toast; otherwise rendered verbatim in the right slot
	presence    string    // "active", "away", or "" (unknown — segment hidden)
	dndEnabled  bool
	dndEndTS    time.Time // zero if not in DND
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
func (m Model) channelGlyph() string {
	switch m.channelType {
	case "private":
		return "\u25c6"
	case "dm", "group_dm":
		return "\u25cf"
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

	// Workspace
	wsInfo := styles.StatusBar.Render(fmt.Sprintf(" %s ", m.workspace))

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

	left := lipgloss.JoinHorizontal(lipgloss.Center, modeLabel, channelInfo, wsInfo)

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

	// Fill the bar to full width
	gap := width - lipgloss.Width(left) - lipgloss.Width(rightContent)
	if gap < 0 {
		gap = 0
	}
	filler := styles.StatusBar.Render(fmt.Sprintf("%*s", gap, ""))

	return lipgloss.JoinHorizontal(lipgloss.Center, left, filler, rightContent)
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

// DeleteFailedMsg is delivered when chat.delete fails.
type DeleteFailedMsg struct{ Reason string }

// EditNotOwnMsg is delivered when E was pressed on a non-owned message.
type EditNotOwnMsg struct{}

// DeleteNotOwnMsg is delivered when D was pressed on a non-owned message.
type DeleteNotOwnMsg struct{}
