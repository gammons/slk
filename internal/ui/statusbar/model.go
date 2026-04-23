// internal/ui/statusbar/model.go
package statusbar

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slack-tui/internal/ui"
	"github.com/gammons/slack-tui/internal/ui/styles"
)

type Model struct {
	mode        ui.Mode
	channel     string
	workspace   string
	unreadCount int
	connected   bool
}

func New() Model {
	return Model{
		mode:      ui.ModeNormal,
		connected: true,
	}
}

func (m *Model) SetMode(mode ui.Mode) {
	m.mode = mode
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

func (m *Model) SetConnected(connected bool) {
	m.connected = connected
}

func (m Model) View(width int) string {
	// Mode indicator
	var modeStyle lipgloss.Style
	switch m.mode {
	case ui.ModeInsert:
		modeStyle = styles.StatusModeInsert
	case ui.ModeCommand:
		modeStyle = styles.StatusModeCommand
	default:
		modeStyle = styles.StatusMode
	}
	modeLabel := modeStyle.Render(fmt.Sprintf(" %s ", m.mode.String()))

	// Channel info
	channelInfo := styles.StatusBar.Render(fmt.Sprintf(" #%s ", m.channel))

	// Workspace
	wsInfo := styles.StatusBar.Render(fmt.Sprintf(" %s ", m.workspace))

	// Right side: unread + connection
	var rightParts []string

	if m.unreadCount > 0 {
		rightParts = append(rightParts,
			styles.UnreadBadge.Render(fmt.Sprintf(" %d unread ", m.unreadCount)))
	}

	if m.connected {
		rightParts = append(rightParts, styles.PresenceOnline.Render("*"))
	} else {
		disconnectedStyle := lipgloss.NewStyle().Foreground(styles.Error)
		rightParts = append(rightParts, disconnectedStyle.Render("DISCONNECTED"))
	}

	left := lipgloss.JoinHorizontal(lipgloss.Center, modeLabel, channelInfo, wsInfo)

	rightContent := ""
	for _, p := range rightParts {
		rightContent += p + " "
	}

	// Fill the bar to full width
	gap := width - lipgloss.Width(left) - lipgloss.Width(rightContent)
	if gap < 0 {
		gap = 0
	}
	filler := styles.StatusBar.Render(fmt.Sprintf("%*s", gap, ""))

	return lipgloss.JoinHorizontal(lipgloss.Center, left, filler, rightContent)
}
