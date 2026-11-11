package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Transparent background ([appearance] transparent_background).
//
// Terminals only draw a cell see-through (window opacity, blur) when it
// has the default background. Any explicit colour is solid. So with the
// option on, Apply sets the colours the base surfaces are filled with
// (Background, SidebarBackground, RailBackground, StatusBarBackground,
// SplashBackground) to lipgloss.NoColor{}, which lipgloss renders as no
// background at all.
//
// Those variables are paint colours: fill with them. Anything that uses a
// base colour as a colour (a mix, a tint, a foreground, a dialog that must
// stay solid) reads ThemeBackground() / ThemeSidebarBackground() instead.
// With the option off both give the same colour, so nothing changes.

var (
	// transparent is what SetTransparentBackground asked for; applied is
	// what the last Apply did. Everything reads applied, so the paint
	// colours, the accessors and the border glyph always agree.
	transparent bool
	applied     bool

	// The theme's real base colours, kept by Apply while the paint
	// colours are NoColor.
	themeBackground        color.Color
	themeSidebarBackground color.Color
)

// SetTransparentBackground turns the transparent background on or off.
// It takes effect on the next Apply, and every later Apply (a theme
// switch, a custom theme) keeps it.
func SetTransparentBackground(on bool) { transparent = on }

// TransparentBackground reports whether the last Apply made the base
// surfaces transparent.
func TransparentBackground() bool { return applied }

// ThemeBackground is the theme's message-pane background colour, also
// when the transparent background paints it as NoColor. Use it for maths
// and for surfaces that stay solid, such as dialogs.
func ThemeBackground() color.Color {
	if applied {
		return themeBackground
	}
	return Background
}

// ThemeSidebarBackground is ThemeBackground for the sidebar.
func ThemeSidebarBackground() color.Color {
	if applied {
		return themeSidebarBackground
	}
	return SidebarBackground
}

// InvisibleLeftBorder is the left border of an unselected message row: it
// keeps the column the selected row's "▌" marker takes. It is drawn with
// the background colour as foreground, which only hides the glyph while
// the background is a real colour, so with the transparent background
// on it is a space instead.
func InvisibleLeftBorder() lipgloss.Border {
	if applied {
		return lipgloss.Border{Left: " "}
	}
	return lipgloss.Border{Left: "▌"}
}

// applyTransparency runs at the end of Apply, after all the maths on the
// real colours. It keeps the real colours and, with the option on, sets
// the paint colours to NoColor.
func applyTransparency() {
	themeBackground = Background
	themeSidebarBackground = SidebarBackground
	StatusBarBackground = SurfaceDark
	SplashBackground = SurfaceDark
	applied = transparent
	if !transparent {
		return
	}
	Background = lipgloss.NoColor{}
	SidebarBackground = lipgloss.NoColor{}
	RailBackground = lipgloss.NoColor{}
	StatusBarBackground = lipgloss.NoColor{}
	SplashBackground = lipgloss.NoColor{}
}
