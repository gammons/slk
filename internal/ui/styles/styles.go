// internal/ui/styles/styles.go
package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
)

var (
	// Colors
	Primary     color.Color = lipgloss.Color("#4A9EFF")
	Secondary   color.Color = lipgloss.Color("#666666")
	Accent      color.Color = lipgloss.Color("#50C878")
	Warning     color.Color = lipgloss.Color("#E0A030")
	Error       color.Color = lipgloss.Color("#E04040")
	Background  color.Color = lipgloss.Color("#1A1A2E")
	Surface     color.Color = lipgloss.Color("#16162B")
	SurfaceDark color.Color = lipgloss.Color("#0F0F23")
	TextPrimary color.Color = lipgloss.Color("#E0E0E0")
	TextMuted   color.Color = lipgloss.Color("#888888")
	Border      color.Color = lipgloss.Color("#333333")

	// Sidebar/rail colors (default to Background/Text/TextMuted/SurfaceDark
	// for backwards compatibility with themes that don't set them).
	SidebarBackground color.Color = lipgloss.Color("#1A1A2E")
	SidebarText       color.Color = lipgloss.Color("#E0E0E0")
	SidebarTextMuted  color.Color = lipgloss.Color("#888888")
	RailBackground    color.Color = lipgloss.Color("#0F0F23")

	// Panel styles
	FocusedBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(Primary).
			BorderBackground(Background).
			Background(Background)

	UnfocusedBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Border).
			BorderBackground(Background).
			Background(Background)

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
			Background(Background).
			Foreground(TextPrimary).
			Bold(true).
			Padding(0, 1)

	ChannelNormal = lipgloss.NewStyle().
			Background(Background).
			Foreground(TextPrimary).
			Padding(0, 1)

	ChannelUnread = lipgloss.NewStyle().
			Background(Background).
			Foreground(TextPrimary).
			Bold(true).
			Padding(0, 1)

	UnreadBadge = lipgloss.NewStyle().
			Background(Error).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)

	SectionHeader = lipgloss.NewStyle().
			Background(Background).
			Foreground(TextMuted).
			Bold(true).
			Padding(0, 1)

	// Messages
	Username = lipgloss.NewStyle().
			Background(Background).
			Foreground(Primary).
			Bold(true)

	Timestamp = lipgloss.NewStyle().
			Background(Background).
			Foreground(TextMuted).
			Italic(true)

	MessageText = lipgloss.NewStyle().
			Background(Background).
			Foreground(TextPrimary)

	ThreadIndicator = lipgloss.NewStyle().
			Background(Background).
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
			BorderBackground(SurfaceDark).
			Background(SurfaceDark).
			Foreground(TextPrimary).
			Padding(1, 1, 1, 1)

	ComposeFocused = lipgloss.NewStyle().
			BorderStyle(thickLeftBorder).
			BorderLeft(true).
			BorderForeground(Primary).
			BorderBackground(SurfaceDark).
			Background(SurfaceDark).
			Foreground(TextPrimary).
			Padding(1, 1, 1, 1)

	ComposeInsert = lipgloss.NewStyle().
			BorderStyle(thickLeftBorder).
			BorderLeft(true).
			BorderForeground(Primary).
			BorderBackground(SurfaceDark).
			Background(SurfaceDark).
			Foreground(TextPrimary).
			Padding(1, 1, 1, 1)

	// Presence indicators. Foreground only — the background is inherited
	// from the surrounding context (sidebar row, workspace tile, etc.) so
	// the same style works on both the sidebar (SidebarBackground) and the
	// workspace rail (RailBackground) without painting a contrasting square
	// around the dot when those colors differ from the message-pane bg.
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
			Background(Background).
			Foreground(TextMuted).
			Bold(true).
			Align(lipgloss.Center)

	// New message landmark
	NewMessageSeparator = lipgloss.NewStyle().
				Background(Background).
				Foreground(Error).
				Bold(true).
				Align(lipgloss.Center)

	// Typing indicator
	TypingIndicator = lipgloss.NewStyle().
			Background(Background).
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

	// Sidebar/rail colors fall back to their message-pane equivalents when
	// unset on the theme, so existing themes render exactly as before. We
	// compute these AFTER overrides so a user override of Background also
	// updates SidebarBackground (when not explicitly set on the theme).
	if colors.SidebarBackground != "" {
		SidebarBackground = lipgloss.Color(colors.SidebarBackground)
	} else {
		SidebarBackground = Background
	}
	if colors.SidebarText != "" {
		SidebarText = lipgloss.Color(colors.SidebarText)
	} else {
		SidebarText = TextPrimary
	}
	if colors.SidebarTextMuted != "" {
		SidebarTextMuted = lipgloss.Color(colors.SidebarTextMuted)
	} else {
		SidebarTextMuted = TextMuted
	}
	if colors.RailBackground != "" {
		RailBackground = lipgloss.Color(colors.RailBackground)
	} else {
		RailBackground = SurfaceDark
	}

	buildStyles()
}

func buildStyles() {
	FocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).BorderForeground(Primary).BorderBackground(Background).Background(Background)
	UnfocusedBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).BorderForeground(Border).BorderBackground(Background).Background(Background)
	WorkspaceActive = lipgloss.NewStyle().
		Background(Primary).Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).Padding(0, 1).Align(lipgloss.Center)
	WorkspaceInactive = lipgloss.NewStyle().
		Background(RailBackground).Foreground(SidebarText).
		Padding(0, 1).Align(lipgloss.Center)
	ChannelSelected = lipgloss.NewStyle().
		Background(SidebarBackground).Foreground(SidebarText).Bold(true).Padding(0, 1)
	ChannelNormal = lipgloss.NewStyle().
		Background(SidebarBackground).Foreground(SidebarText).Padding(0, 1)
	ChannelUnread = lipgloss.NewStyle().
		Background(SidebarBackground).Foreground(SidebarText).Bold(true).Padding(0, 1)
	UnreadBadge = lipgloss.NewStyle().
		Background(Error).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1)
	SectionHeader = lipgloss.NewStyle().
		Background(SidebarBackground).Foreground(SidebarTextMuted).Bold(true).Padding(0, 1)
	Username = lipgloss.NewStyle().
		Background(Background).Foreground(Primary).Bold(true)
	Timestamp = lipgloss.NewStyle().
		Background(Background).Foreground(TextMuted).Italic(true)
	MessageText = lipgloss.NewStyle().
		Background(Background).Foreground(TextPrimary)
	ThreadIndicator = lipgloss.NewStyle().
		Background(Background).Foreground(Primary).Italic(true)
	StatusBar = lipgloss.NewStyle().
		Background(SurfaceDark).Foreground(TextPrimary).Padding(0, 1)
	StatusMode = lipgloss.NewStyle().
		Background(Primary).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	StatusModeInsert = lipgloss.NewStyle().
		Background(Accent).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	StatusModeCommand = lipgloss.NewStyle().
		Background(Warning).Foreground(lipgloss.Color("#000000")).Bold(true).Padding(0, 1)
	ComposeBox = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Border).BorderBackground(SurfaceDark).
		Background(SurfaceDark).Foreground(TextPrimary).Padding(1, 1, 1, 1)
	ComposeFocused = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Primary).BorderBackground(SurfaceDark).
		Background(SurfaceDark).Foreground(TextPrimary).Padding(1, 1, 1, 1)
	ComposeInsert = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Primary).BorderBackground(SurfaceDark).
		Background(SurfaceDark).Foreground(TextPrimary).Padding(1, 1, 1, 1)
	PresenceOnline = lipgloss.NewStyle().Foreground(Accent)
	PresenceAway = lipgloss.NewStyle().Foreground(TextMuted)
	ReactionPillOwn = lipgloss.NewStyle().
		Background(Surface).Foreground(Accent).Padding(0, 1)
	ReactionPillOther = lipgloss.NewStyle().
		Background(Surface).Foreground(TextMuted).Padding(0, 1)
	ReactionPillSelected = lipgloss.NewStyle().
		Background(Surface).Foreground(Primary).Padding(0, 1)
	ReactionPillPlus = lipgloss.NewStyle().
		Background(Surface).Foreground(Primary).Padding(0, 1)
	DateSeparator = lipgloss.NewStyle().
		Background(Background).Foreground(TextMuted).Bold(true).Align(lipgloss.Center)
	NewMessageSeparator = lipgloss.NewStyle().
		Background(Background).Foreground(Error).Bold(true).Align(lipgloss.Center)
	TypingIndicator = lipgloss.NewStyle().
		Background(Background).Foreground(TextMuted).Italic(true).PaddingLeft(2)
}
