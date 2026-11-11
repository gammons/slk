package messages

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/ui/styles"
)

// ChromeStyles maps the current theme onto the dialog chrome
// (internal/bubbles/chrome). It is built on every call, so a dialog that
// calls it while drawing always has the current theme and applyTheme
// pushes nothing. Each style is exactly the one the dialogs built inline
// before Phase 4 (TestChromeStyles_MatchTheInlineStyles).
func ChromeStyles() chrome.Styles {
	bg := styles.ThemeBackground()
	on := func() lipgloss.Style { return lipgloss.NewStyle().Background(bg) }
	return chrome.Styles{
		Box: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.Primary).
			BorderBackground(bg).
			Background(bg).
			Padding(1, 1),
		Popup: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(styles.Primary).
			Background(styles.SurfaceDark),
		Title: on().Foreground(styles.Primary).Bold(true),
		Input: lipgloss.NewStyle().
			BorderStyle(lipgloss.Border{Left: "\u258c"}).
			BorderLeft(true).
			BorderForeground(styles.Primary).
			BorderBackground(bg).
			PaddingLeft(1).
			Background(bg).
			Foreground(styles.TextPrimary),
		Placeholder: on().Foreground(styles.TextMuted),
		Row:         on().Foreground(styles.TextPrimary),
		RowSelected: on().Foreground(styles.Primary).Bold(true),
		Marker:      on().Foreground(styles.Accent),
		PopupMarker: lipgloss.NewStyle().Foreground(styles.Accent),
		Empty:       on().Foreground(styles.TextMuted).Italic(true),
		Footer:      on().Foreground(styles.TextMuted),
		Thumb:       on().Foreground(styles.Primary),
		Track:       on().Foreground(styles.Border),
		BaseANSI:    SolidBgANSI() + FgANSI(),
		PopupANSI:   popupANSI(),
	}
}

// popupANSI keeps the composer popups solid with the transparent
// background on: the composer area re-asserts BgANSI after resets, and
// that is the default background then, so the popup re-asserts its own
// colour after it. Off, it's empty and the popups render as before.
func popupANSI() string {
	if !styles.TransparentBackground() {
		return ""
	}
	return bgANSIFor(styles.SurfaceDark)
}
