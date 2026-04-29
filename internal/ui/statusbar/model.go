// internal/ui/statusbar/model.go
package statusbar

import (
	"fmt"

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

	rightContent := ""
	for i, p := range rightParts {
		if i > 0 {
			rightContent += " "
		}
		rightContent += p
	}
	rightContent += "  " // trailing padding (extra space for unicode width variance)

	// Fill the bar to full width
	gap := width - lipgloss.Width(left) - lipgloss.Width(rightContent)
	if gap < 0 {
		gap = 0
	}
	filler := styles.StatusBar.Render(fmt.Sprintf("%*s", gap, ""))

	return lipgloss.JoinHorizontal(lipgloss.Center, left, filler, rightContent)
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

// EditFailedMsg is delivered when chat.update fails. App handles by
// showing the toast and scheduling a CopiedClearMsg.
type EditFailedMsg struct{ Reason string }

// DeleteFailedMsg is delivered when chat.delete fails.
type DeleteFailedMsg struct{ Reason string }

// EditNotOwnMsg is delivered when E was pressed on a non-owned message.
type EditNotOwnMsg struct{}

// DeleteNotOwnMsg is delivered when D was pressed on a non-owned message.
type DeleteNotOwnMsg struct{}
