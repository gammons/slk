// internal/ui/styles/styles.go
package styles

import (
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
)

var (
	// Colors
	Primary     = lipgloss.Color("#4A9EFF")
	Secondary   = lipgloss.Color("#666666")
	Accent      = lipgloss.Color("#50C878")
	Warning     = lipgloss.Color("#E0A030")
	Error       = lipgloss.Color("#E04040")
	Background  = lipgloss.Color("#1A1A2E")
	Surface     = lipgloss.Color("#16162B")
	SurfaceDark = lipgloss.Color("#0F0F23")
	TextPrimary = lipgloss.Color("#E0E0E0")
	TextMuted   = lipgloss.Color("#888888")
	Border      = lipgloss.Color("#333333")

	// Panel styles
	FocusedBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(Primary)

	UnfocusedBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Border)

	// Workspace rail
	WorkspaceActive = lipgloss.NewStyle().
			Background(Primary).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1).
			Align(lipgloss.Center)

	WorkspaceInactive = lipgloss.NewStyle().
				Background(Surface).
				Foreground(TextPrimary).
				Padding(0, 1).
				Align(lipgloss.Center)

	// Channel sidebar
	ChannelSelected = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1)

	ChannelNormal = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Padding(0, 1)

	ChannelUnread = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1)

	UnreadBadge = lipgloss.NewStyle().
			Background(Error).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)

	SectionHeader = lipgloss.NewStyle().
			Foreground(TextMuted).
			Bold(true).
			Padding(0, 1)

	// Messages
	Username = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)

	Timestamp = lipgloss.NewStyle().
			Foreground(TextMuted).
			Italic(true)

	MessageText = lipgloss.NewStyle().
			Foreground(TextPrimary)

	ThreadIndicator = lipgloss.NewStyle().
			Foreground(Primary).
			Italic(true)

	// Status bar
	StatusBar = lipgloss.NewStyle().
			Background(SurfaceDark).
			Foreground(TextPrimary).
			Padding(0, 1)

	StatusMode = lipgloss.NewStyle().
			Background(Primary).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1)

	StatusModeInsert = lipgloss.NewStyle().
				Background(Accent).
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true).
				Padding(0, 1)

	StatusModeCommand = lipgloss.NewStyle().
				Background(Warning).
				Foreground(lipgloss.Color("#000000")).
				Bold(true).
				Padding(0, 1)

	// Compose box -- thick left border, like opencode's input style
	thickLeftBorder = lipgloss.Border{
		Left: "▌",
	}

	ComposeBox = lipgloss.NewStyle().
			BorderStyle(thickLeftBorder).
			BorderLeft(true).
			BorderForeground(Border).
			Background(SurfaceDark).
			MarginTop(1).
			Padding(1, 1, 1, 1)

	ComposeFocused = lipgloss.NewStyle().
			BorderStyle(thickLeftBorder).
			BorderLeft(true).
			BorderForeground(Primary).
			Background(SurfaceDark).
			MarginTop(1).
			Padding(1, 1, 1, 1)

	ComposeInsert = lipgloss.NewStyle().
			BorderStyle(thickLeftBorder).
			BorderLeft(true).
			BorderForeground(Primary).
			Background(SurfaceDark).
			MarginTop(1).
			Padding(1, 1, 1, 1)

	// Presence indicators
	PresenceOnline = lipgloss.NewStyle().Foreground(Accent)
	PresenceAway   = lipgloss.NewStyle().Foreground(TextMuted)

	// Reaction pill styles
	ReactionPillOwn = lipgloss.NewStyle().
			Background(lipgloss.Color("#1a2e1a")).
			Foreground(lipgloss.Color("#50C878")).
			Padding(0, 1)

	ReactionPillOther = lipgloss.NewStyle().
				Background(lipgloss.Color("#1a1a2e")).
				Foreground(lipgloss.Color("#888888")).
				Padding(0, 1)

	ReactionPillSelected = lipgloss.NewStyle().
				Background(lipgloss.Color("#252540")).
				Foreground(lipgloss.Color("#4A9EFF")).
				Padding(0, 1)

	ReactionPillPlus = lipgloss.NewStyle().
				Background(lipgloss.Color("#1a1a2e")).
				Foreground(lipgloss.Color("#4A9EFF")).
				Padding(0, 1)

	// Day separator
	DateSeparator = lipgloss.NewStyle().
			Foreground(TextMuted).
			Bold(true).
			Align(lipgloss.Center)

	// New message landmark
	NewMessageSeparator = lipgloss.NewStyle().
				Foreground(Error).
				Bold(true).
				Align(lipgloss.Center)

	// Typing indicator
	TypingIndicator = lipgloss.NewStyle().
			Foreground(TextMuted).
			Italic(true).
			PaddingLeft(2)
)

// Apply sets the color palette from a named theme with optional overrides,
// then rebuilds all composed styles.
func Apply(themeName string, overrides config.Theme) {
	colors := lookupTheme(themeName)

	Primary = lipgloss.Color(colors.Primary)
	Secondary = lipgloss.Color("#666666")
	Accent = lipgloss.Color(colors.Accent)
	Warning = lipgloss.Color(colors.Warning)
	Error = lipgloss.Color(colors.Error)
	Background = lipgloss.Color(colors.Background)
	Surface = lipgloss.Color(colors.Surface)
	SurfaceDark = lipgloss.Color(colors.SurfaceDark)
	TextPrimary = lipgloss.Color(colors.Text)
	TextMuted = lipgloss.Color(colors.TextMuted)
	Border = lipgloss.Color(colors.Border)

	if overrides.Primary != "" {
		Primary = lipgloss.Color(overrides.Primary)
	}
	if overrides.Accent != "" {
		Accent = lipgloss.Color(overrides.Accent)
	}
	if overrides.Warning != "" {
		Warning = lipgloss.Color(overrides.Warning)
	}
	if overrides.Error != "" {
		Error = lipgloss.Color(overrides.Error)
	}
	if overrides.Background != "" {
		Background = lipgloss.Color(overrides.Background)
	}
	if overrides.Surface != "" {
		Surface = lipgloss.Color(overrides.Surface)
	}
	if overrides.SurfaceDark != "" {
		SurfaceDark = lipgloss.Color(overrides.SurfaceDark)
	}
	if overrides.Text != "" {
		TextPrimary = lipgloss.Color(overrides.Text)
	}
	if overrides.TextMuted != "" {
		TextMuted = lipgloss.Color(overrides.TextMuted)
	}
	if overrides.Border != "" {
		Border = lipgloss.Color(overrides.Border)
	}

	buildStyles()
}

func buildStyles() {
	FocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).BorderForeground(Primary).
		Background(Background)
	UnfocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).BorderForeground(Border).
		Background(Background)
	WorkspaceActive = lipgloss.NewStyle().
		Background(Primary).Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).Padding(0, 1).Align(lipgloss.Center)
	WorkspaceInactive = lipgloss.NewStyle().
		Background(Surface).Foreground(TextPrimary).
		Padding(0, 1).Align(lipgloss.Center)
	ChannelSelected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1).
		Background(Background)
	ChannelNormal = lipgloss.NewStyle().
		Foreground(TextPrimary).Padding(0, 1).
		Background(Background)
	ChannelUnread = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1).
		Background(Background)
	UnreadBadge = lipgloss.NewStyle().
		Background(Error).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1)
	SectionHeader = lipgloss.NewStyle().
		Foreground(TextMuted).Bold(true).Padding(0, 1).
		Background(Background)
	Username = lipgloss.NewStyle().
		Foreground(Primary).Bold(true).Background(Background)
	Timestamp = lipgloss.NewStyle().
		Foreground(TextMuted).Italic(true).Background(Background)
	MessageText = lipgloss.NewStyle().
		Foreground(TextPrimary).Background(Background)
	ThreadIndicator = lipgloss.NewStyle().
		Foreground(Primary).Italic(true).Background(Background)
	StatusBar = lipgloss.NewStyle().
		Background(SurfaceDark).Foreground(TextPrimary).Padding(0, 1)
	StatusMode = lipgloss.NewStyle().
		Background(Primary).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	StatusModeInsert = lipgloss.NewStyle().
		Background(Accent).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	StatusModeCommand = lipgloss.NewStyle().
		Background(Warning).Foreground(lipgloss.Color("#000000")).Bold(true).Padding(0, 1)
	ComposeBox = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Border).
		Background(SurfaceDark).MarginTop(1).Padding(1, 1, 1, 1)
	ComposeFocused = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Primary).
		Background(SurfaceDark).MarginTop(1).Padding(1, 1, 1, 1)
	ComposeInsert = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Primary).
		Background(SurfaceDark).MarginTop(1).Padding(1, 1, 1, 1)
	PresenceOnline = lipgloss.NewStyle().Foreground(Accent).Background(Background)
	PresenceAway = lipgloss.NewStyle().Foreground(TextMuted).Background(Background)
	ReactionPillOwn = lipgloss.NewStyle().
		Background(Surface).Foreground(Accent).Padding(0, 1)
	ReactionPillOther = lipgloss.NewStyle().
		Background(Surface).Foreground(TextMuted).Padding(0, 1)
	ReactionPillSelected = lipgloss.NewStyle().
		Background(Surface).Foreground(Primary).Padding(0, 1)
	ReactionPillPlus = lipgloss.NewStyle().
		Background(Surface).Foreground(Primary).Padding(0, 1)
	DateSeparator = lipgloss.NewStyle().
		Foreground(TextMuted).Bold(true).Align(lipgloss.Center).Background(Background)
	NewMessageSeparator = lipgloss.NewStyle().
		Foreground(Error).Bold(true).Align(lipgloss.Center).Background(Background)
	TypingIndicator = lipgloss.NewStyle().
		Foreground(TextMuted).Italic(true).PaddingLeft(2).Background(Background)
}
