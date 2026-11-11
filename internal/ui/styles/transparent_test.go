package styles

import (
	"image/color"
	"maps"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
)

// withTransparent turns the transparent background on for one test and
// puts the dark theme back, opaque, afterwards.
func withTransparent(t *testing.T) {
	t.Helper()
	SetTransparentBackground(true)
	t.Cleanup(func() {
		SetTransparentBackground(false)
		Apply("dark", config.Theme{})
	})
}

func isNoColor(c color.Color) bool {
	_, ok := c.(lipgloss.NoColor)
	return ok
}

// derived is every colour Apply or a lazy helper works out from the base
// colours. None of them may change with the transparent background:
// they are maths on the real theme colours, not fills.
func derived() map[string]color.Color {
	return map[string]color.Color{
		"SelectionTintColor(true)":  SelectionTintColor(true),
		"SelectionTintColor(false)": SelectionTintColor(false),
		"ComposeInsertBG":           ComposeInsertBG,
		"SelectionForeground":       SelectionForeground,
		"SearchHighlightFg":         SearchHighlightFg,
		"MutedMentionBadge bg":      MutedMentionBadgeStyle().GetBackground(),
		"ThemeBackground":           ThemeBackground(),
		"ThemeSidebarBackground":    ThemeSidebarBackground(),
	}
}

func TestTransparentBackground_DerivedColoursMatchOpaque(t *testing.T) {
	for _, theme := range []string{"dark", "light", "slack default", "nord"} {
		t.Run(theme, func(t *testing.T) {
			t.Cleanup(func() {
				SetTransparentBackground(false)
				Apply("dark", config.Theme{})
			})
			SetTransparentBackground(false)
			Apply(theme, config.Theme{})
			want := derived()
			SetTransparentBackground(true)
			Apply(theme, config.Theme{})
			got := derived()
			for k, w := range want {
				if !colorEq(got[k], w) {
					t.Errorf("%s = %v with the transparent background, want %v as when opaque", k, got[k], w)
				}
			}
		})
	}
}

func colorEq(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestTransparentBackground_PaintColoursAreNoColor(t *testing.T) {
	withTransparent(t)
	Apply("dark", config.Theme{})
	for name, c := range map[string]color.Color{
		"Background":          Background,
		"SidebarBackground":   SidebarBackground,
		"RailBackground":      RailBackground,
		"StatusBarBackground": StatusBarBackground,
		"SplashBackground":    SplashBackground,
	} {
		if !isNoColor(c) {
			t.Errorf("%s = %v, want lipgloss.NoColor{}", name, c)
		}
	}
	if !isNoColor(StatusBar.GetBackground()) || !isNoColor(MessageText.GetBackground()) {
		t.Error("buildStyles did not pick up the paint colours")
	}
	// Surfaces that stay solid keep their colour.
	for name, c := range map[string]color.Color{
		"SurfaceDark":     SurfaceDark,
		"Surface":         Surface,
		"ComposeBox bg":   ComposeBox.GetBackground(),
		"ThemeBackground": ThemeBackground(),
	} {
		if c == nil || isNoColor(c) {
			t.Errorf("%s = %v, want a solid colour", name, c)
		}
	}
}

func TestTransparentBackground_OffKeepsThemeColours(t *testing.T) {
	SetTransparentBackground(false)
	Apply("dark", config.Theme{})
	if !colorEq(StatusBarBackground, SurfaceDark) {
		t.Errorf("StatusBarBackground = %v, want SurfaceDark %v", StatusBarBackground, SurfaceDark)
	}
	if !colorEq(SplashBackground, SurfaceDark) {
		t.Errorf("SplashBackground = %v, want SurfaceDark %v", SplashBackground, SurfaceDark)
	}
	if ThemeBackground() != Background || ThemeSidebarBackground() != SidebarBackground {
		t.Error("Theme*() differ from the paint colours with the option off")
	}
	// A test that assigns the paint colour directly still sees it
	// through ThemeBackground.
	saved := Background
	t.Cleanup(func() { Background = saved })
	Background = lipgloss.Color("#123456")
	if ThemeBackground() != Background {
		t.Error("ThemeBackground() does not follow a direct assignment with the option off")
	}
}

func TestTransparentBackground_SurvivesThemeSwitchAndCustomTheme(t *testing.T) {
	withTransparent(t)
	saved := maps.Clone(customThemes)
	t.Cleanup(func() { customThemes = saved })

	Apply("dark", config.Theme{})
	Apply("dracula", config.Theme{})
	if !isNoColor(Background) {
		t.Fatal("a theme switch dropped the transparent background")
	}
	if !colorEq(ThemeBackground(), lipgloss.Color("#282A36")) {
		t.Errorf("ThemeBackground() = %v, want dracula's #282A36", ThemeBackground())
	}

	RegisterCustomTheme("Transparent Custom", ThemeColors{
		Primary: "#4A9EFF", Accent: "#50C878", Warning: "#E0A030", Error: "#E04040",
		Background: "#101820", Surface: "#202830", SurfaceDark: "#080C10",
		Text: "#E0E0E0", TextMuted: "#888888", Border: "#333333",
	})
	Apply("Transparent Custom", config.Theme{})
	if !isNoColor(Background) || !isNoColor(StatusBarBackground) {
		t.Fatal("a custom theme dropped the transparent background")
	}
	if !colorEq(ThemeBackground(), lipgloss.Color("#101820")) {
		t.Errorf("ThemeBackground() = %v, want the custom #101820", ThemeBackground())
	}
}

func TestInvisibleLeftBorder(t *testing.T) {
	SetTransparentBackground(false)
	Apply("dark", config.Theme{})
	if got := InvisibleLeftBorder().Left; got != "▌" {
		t.Errorf("opaque border = %q, want ▌", got)
	}
	withTransparent(t)
	if got := InvisibleLeftBorder().Left; got != "▌" {
		t.Errorf("border before Apply = %q, want ▌: the switch takes effect on Apply", got)
	}
	Apply("dark", config.Theme{})
	if got := InvisibleLeftBorder().Left; got != " " {
		t.Errorf("transparent border = %q, want a space", got)
	}
}
