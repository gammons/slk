package main

import (
	"time"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/styles"
	versionpkg "github.com/gammons/slk/internal/version"
)

// uiSettings is the UI configuration (display and composer behaviour)
// run() and runDemo() both hand the App. It carries values rather than
// the config file so that demo mode can build one without loading any
// config.
type uiSettings struct {
	TimestampFormat  string
	TypingIndicators bool
	StaleAfter       time.Duration
	RightAlignBadges bool
	ChannelIndent    int
	SidebarStyle     ui.SidebarStyle
	MouseWheelLines  int
	ColoredUsernames bool
	ThemeOverrides   core.Theme
	// StayInInsertAfterSend is [compose] stay_in_insert_after_send
	// (default off).
	StayInInsertAfterSend bool
}

// sidebarStyleFromConfig is the [sidebar] look and behaviour switches.
func sidebarStyleFromConfig(s config.Sidebar) ui.SidebarStyle {
	return ui.SidebarStyle{
		Slack:        s.SlackStyle,
		PrivateGlyph: s.PrivateGlyph,
		ActiveOnly:   s.ActiveOnly,
		MouseResize:  s.MouseResize,
	}
}

// uiSettingsFromConfig is run()'s uiSettings, read from the loaded config.
func uiSettingsFromConfig(cfg config.Config) uiSettings {
	return uiSettings{
		TimestampFormat:       cfg.Appearance.TimestampFormat,
		TypingIndicators:      cfg.Animations.TypingIndicators,
		StaleAfter:            time.Duration(cfg.Sidebar.HideInactiveAfterDays) * 24 * time.Hour,
		RightAlignBadges:      cfg.Sidebar.RightAlignBadges,
		ChannelIndent:         cfg.Sidebar.ChannelIndent,
		SidebarStyle:          sidebarStyleFromConfig(cfg.Sidebar),
		MouseWheelLines:       cfg.Appearance.MouseWheelLines,
		ColoredUsernames:      cfg.Appearance.ColoredUsernames,
		ThemeOverrides:        cfg.Theme,
		StayInInsertAfterSend: cfg.Compose.StayInInsertAfterSend,
	}
}

// applyUISettings installs s on app. The timestamp formatter feeds the
// optimistic instant-display path on send: the App mints a placeholder
// before the chat.postMessage round-trip and needs a Timestamp string
// that renders identically to messages arriving through the load path.
func applyUISettings(app *ui.App, s uiSettings) {
	app.SetHelpFooter(versionpkg.ModalFooter(version))
	app.SetBuildInfo(versionpkg.Short(version, commit))
	app.SetNowTimestampFormatter(func() string {
		return time.Now().Format(s.TimestampFormat)
	})
	app.SetTypingEnabled(s.TypingIndicators)
	app.SetRightAlignBadges(s.RightAlignBadges)
	app.SetChannelIndent(s.ChannelIndent)
	app.SetSidebarStyle(s.SidebarStyle)
	app.SetSidebarStaleThreshold(s.StaleAfter)
	app.SetMouseWheelLines(s.MouseWheelLines)
	app.SetColoredUsernames(s.ColoredUsernames)
	app.SetThemeItems(styles.ThemeNames())
	app.SetThemeOverrides(s.ThemeOverrides)
	app.SetStayInInsertAfterSend(s.StayInInsertAfterSend)
}
