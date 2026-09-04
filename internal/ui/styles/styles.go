// internal/ui/styles/styles.go
package styles

import (
	"hash/fnv"
	"image/color"
	"strings"

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

	// Selection highlight. Apply() either copies the theme's values or
	// derives a sensible default (Primary as bg, Background as fg) so
	// SelectionStyle() always produces visible highlight on any theme.
	SelectionBackground color.Color
	SelectionForeground color.Color

	// Search-match highlight. Apply() either copies the theme's values
	// or derives a default (Warning as bg, Background as fg) so
	// SearchHighlightStyle() is always visible on any theme.
	SearchHighlightBg color.Color
	SearchHighlightFg color.Color

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

	// ChannelNormal is the style for read channels in the sidebar. We use
	// the muted sidebar text color so that unread channels (which use the
	// full SidebarText) visibly pop against the read ones — bold alone is
	// not enough contrast on many terminals.
	ChannelNormal = lipgloss.NewStyle().
			Background(Background).
			Foreground(TextMuted).
			Padding(0, 1)

	ChannelUnread = lipgloss.NewStyle().
			Background(Background).
			Foreground(TextPrimary).
			Bold(true).
			Padding(0, 1)

	// ChannelMuted is the style for muted channels in the sidebar.
	// Always uses the dim sidebar foreground (no bold) regardless of
	// unread state — Slack treats muted channels as background noise,
	// so they should look quieter than even read-but-unmuted rows.
	// Pairs with the unread-dot suppression in buildCache: muted
	// channels never get the blue "•" indicator, even when their
	// UnreadCount is non-zero.
	ChannelMuted = lipgloss.NewStyle().
			Background(Background).
			Foreground(TextMuted).
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

	// UserColorPalette is the fixed set of colors used to deterministically
	// color usernames by hashing user IDs, mirroring how Slack clients assign
	// stable per-user colors. Mid-brightness colors are chosen for adequate
	// contrast on both dark and light theme backgrounds.
	UserColorPalette = []color.Color{
		lipgloss.Color("#E01E5A"),
		lipgloss.Color("#3B82F6"),
		lipgloss.Color("#2EB67D"),
		lipgloss.Color("#8B5CF6"),
		lipgloss.Color("#F43F5E"),
		lipgloss.Color("#06B6D4"),
		lipgloss.Color("#F97316"),
		lipgloss.Color("#10B981"),
		lipgloss.Color("#6366F1"),
		lipgloss.Color("#C026D3"),
		lipgloss.Color("#0EA5E9"),
		lipgloss.Color("#DB2777"),
	}

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

	// StatusbarSyncing styles the small "verifying" glyph that appears
	// adjacent to the channel name while a background cache-verify fetch
	// is in flight. Muted so it is ignorable but legible against the
	// status bar's surface_dark background.
	StatusbarSyncing = lipgloss.NewStyle().
				Background(SurfaceDark).
				Foreground(TextMuted)

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
			BorderForeground(Accent).
			BorderBackground(ComposeInsertBG).
			Background(ComposeInsertBG).
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

// version is bumped on every Apply() call. UI consumers use it to invalidate
// any caches that depend on theme colors / styles.
var version int64

// Version returns the current theme version, incremented on every Apply call.
func Version() int64 { return version }

// parseColor converts a color string to a lipgloss color. It recognizes
// "transparent", "none", and "default" as nil so terminal emulators with
// background opacity (e.g. Ghostty) can show the underlying wallpaper
// through slk's UI. A nil color makes lipgloss emit no color escape at
// all, unlike lipgloss.NoColor{}, whose RGBA() reports opaque black and
// gets painted as literal black by code paths that convert colors by
// hand (bgANSIFor, mixColors, blockkit colorString).
func parseColor(s string) color.Color {
	switch strings.ToLower(s) {
	case "transparent", "none", "default":
		return nil
	}
	return lipgloss.Color(s)
}

// Apply sets the color palette from a named theme with optional overrides,
// then rebuilds all composed styles.
func Apply(themeName string, overrides config.Theme) {
	version++
	colors := lookupTheme(themeName)

	Primary = parseColor(colors.Primary)
	Secondary = lipgloss.Color("#666666")
	Accent = parseColor(colors.Accent)
	Warning = parseColor(colors.Warning)
	Error = parseColor(colors.Error)
	Background = parseColor(colors.Background)
	Surface = parseColor(colors.Surface)
	SurfaceDark = parseColor(colors.SurfaceDark)
	TextPrimary = parseColor(colors.Text)
	TextMuted = parseColor(colors.TextMuted)
	Border = parseColor(colors.Border)

	if overrides.Primary != "" {
		Primary = parseColor(overrides.Primary)
	}
	if overrides.Accent != "" {
		Accent = parseColor(overrides.Accent)
	}
	if overrides.Warning != "" {
		Warning = parseColor(overrides.Warning)
	}
	if overrides.Error != "" {
		Error = parseColor(overrides.Error)
	}
	if overrides.Background != "" {
		Background = parseColor(overrides.Background)
	}
	if overrides.Surface != "" {
		Surface = parseColor(overrides.Surface)
	}
	if overrides.SurfaceDark != "" {
		SurfaceDark = parseColor(overrides.SurfaceDark)
	}
	if overrides.Text != "" {
		TextPrimary = parseColor(overrides.Text)
	}
	if overrides.TextMuted != "" {
		TextMuted = parseColor(overrides.TextMuted)
	}
	if overrides.Border != "" {
		Border = parseColor(overrides.Border)
	}

	// Sidebar/rail colors fall back to their message-pane equivalents when
	// unset on the theme, so existing themes render exactly as before. We
	// compute these AFTER overrides so a user override of Background also
	// updates SidebarBackground (when not explicitly set on the theme).
	if colors.SidebarBackground != "" {
		SidebarBackground = parseColor(colors.SidebarBackground)
	} else {
		SidebarBackground = Background
	}
	if colors.SidebarText != "" {
		SidebarText = parseColor(colors.SidebarText)
	} else {
		SidebarText = TextPrimary
	}
	if colors.SidebarTextMuted != "" {
		SidebarTextMuted = parseColor(colors.SidebarTextMuted)
	} else {
		SidebarTextMuted = TextMuted
	}
	if colors.RailBackground != "" {
		RailBackground = parseColor(colors.RailBackground)
	} else {
		RailBackground = SurfaceDark
	}

	if colors.SelectionBackground != "" {
		SelectionBackground = parseColor(colors.SelectionBackground)
	} else {
		// Default: Primary as the highlight bg gives readable contrast on
		// every built-in theme.
		SelectionBackground = Primary
	}
	if colors.SelectionForeground != "" {
		SelectionForeground = parseColor(colors.SelectionForeground)
	} else {
		// Default: theme background — paired with Primary bg this produces
		// the inverse of the theme's normal text rendering.
		SelectionForeground = Background
	}

	if colors.SearchHighlightBg != "" {
		SearchHighlightBg = parseColor(colors.SearchHighlightBg)
	} else {
		// Default: Warning is visible against message text in all
		// built-in themes.
		SearchHighlightBg = Warning
	}
	if colors.SearchHighlightFg != "" {
		SearchHighlightFg = parseColor(colors.SearchHighlightFg)
	} else {
		SearchHighlightFg = Background
	}

	// Compose-insert background: explicit theme override wins, otherwise
	// derive from Accent + Background at defaultTintAlpha.
	if colors.ComposeInsertBG != "" {
		ComposeInsertBG = parseColor(colors.ComposeInsertBG)
	} else {
		ComposeInsertBG = mixColors(Accent, Background, defaultTintAlpha)
	}

	// Pre-resolve selection tints (theme overrides take precedence). Caching
	// avoids recomputing on every render; resetDerivedTints clears them so
	// the next SelectionTintColor() call repopulates from the new theme.
	resetDerivedTints()
	if colors.SelectionBgFocused != "" {
		selectionBgFocused = parseColor(colors.SelectionBgFocused)
	}
	if colors.SelectionBgUnfocused != "" {
		selectionBgUnfocused = parseColor(colors.SelectionBgUnfocused)
	}

	buildStyles()
}

// SelectionBorderColor returns the foreground color used for the cursor /
// selected-row marker (the thick left "▌") in the sidebar, messages pane,
// and thread panel. When the panel has focus we use the bright Accent
// color; when it doesn't we dim to TextMuted so the cursor is still visible
// (so the user can see where they were when they switch back) but no longer
// competes with the focused panel for attention.
func SelectionBorderColor(focused bool) color.Color {
	if focused {
		return Accent
	}
	return TextMuted
}

// UserColor returns a deterministic color for the given user ID by hashing
// it into UserColorPalette via FNV-1a. Returns Primary for empty IDs (e.g.
// some bot messages) so they fall back to the theme default.
func UserColor(userID string) color.Color {
	if userID == "" {
		return Primary
	}
	h := fnv.New32a()
	h.Write([]byte(userID))
	return UserColorPalette[h.Sum32()%uint32(len(UserColorPalette))]
}

// Username returns a lipgloss style for rendering a username. When colored
// is true and userID is non-empty, the foreground color is deterministically
// derived from userID via UserColor. Otherwise the theme default (Primary)
// is used.
func Username(userID string, colored bool) lipgloss.Style {
	fg := Primary
	if colored && userID != "" {
		fg = UserColor(userID)
	}
	return lipgloss.NewStyle().
		Background(Background).
		Foreground(fg).
		Bold(true)
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
		Background(SidebarBackground).Foreground(SidebarTextMuted).Padding(0, 1)
	ChannelUnread = lipgloss.NewStyle().
		Background(SidebarBackground).Foreground(SidebarText).Bold(true).Padding(0, 1)
	ChannelMuted = lipgloss.NewStyle().
		Background(SidebarBackground).Foreground(SidebarTextMuted).Padding(0, 1)
	UnreadBadge = lipgloss.NewStyle().
		Background(Error).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1)
	SectionHeader = lipgloss.NewStyle().
		Background(SidebarBackground).Foreground(SidebarTextMuted).Bold(true).Padding(0, 1)
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
	StatusbarSyncing = lipgloss.NewStyle().
		Background(SurfaceDark).Foreground(TextMuted)
	ComposeBox = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Border).BorderBackground(SurfaceDark).
		Background(SurfaceDark).Foreground(TextPrimary).Padding(1, 1, 1, 1)
	ComposeFocused = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Primary).BorderBackground(SurfaceDark).
		Background(SurfaceDark).Foreground(TextPrimary).Padding(1, 1, 1, 1)
	ComposeInsert = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Accent).BorderBackground(ComposeInsertBG).
		Background(ComposeInsertBG).Foreground(TextPrimary).Padding(1, 1, 1, 1)
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

// SelectionStyle returns the lipgloss style used to highlight selected
// message text. Apply() always populates SelectionBackground and
// SelectionForeground (deriving sensible defaults when a theme omits
// them), so callers can use this without further nil-checking.
func SelectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(SelectionBackground).
		Foreground(SelectionForeground)
}

// SearchHighlightStyle returns the style used to mark in-channel
// search matches inside message text.
func SearchHighlightStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(SearchHighlightBg).
		Foreground(SearchHighlightFg)
}
