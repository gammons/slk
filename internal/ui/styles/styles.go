// internal/ui/styles/styles.go
package styles

import "github.com/charmbracelet/lipgloss"

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
				Background(lipgloss.Color("#444444")).
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
