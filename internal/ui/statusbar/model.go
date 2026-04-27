// internal/ui/statusbar/model.go
package statusbar

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
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
	workspace   string
	unreadCount int
	connState   ConnectionState
	inThread    bool
}

func New() Model {
	return Model{
		mode:      "NORMAL",
		connState: StateConnecting,
	}
}

// SetMode accepts a fmt.Stringer (such as ui.Mode) to avoid circular imports.
func (m *Model) SetMode(mode fmt.Stringer) {
	m.mode = mode.String()
}

func (m *Model) SetChannel(name string) {
	m.channel = name
}

func (m *Model) SetWorkspace(name string) {
	m.workspace = name
}

func (m *Model) SetUnreadCount(count int) {
	m.unreadCount = count
}

func (m *Model) SetConnectionState(state ConnectionState) {
	m.connState = state
}

func (m *Model) SetInThread(inThread bool) {
	m.inThread = inThread
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
	channelLabel := fmt.Sprintf(" #%s ", m.channel)
	if m.inThread {
		channelLabel = fmt.Sprintf(" #%s > Thread ", m.channel)
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
