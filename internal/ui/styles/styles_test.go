package styles

import (
	"fmt"
	"image/color"
	"maps"
	"slices"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
)

// colorEqual compares two color.Color values.
func colorEqual(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

func TestApplyDarkDefaults(t *testing.T) {
	Apply("dark", config.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#4A9EFF")) {
		t.Errorf("expected dark primary #4A9EFF")
	}
	if !colorEqual(Background, lipgloss.Color("#1A1A2E")) {
		t.Errorf("expected dark background #1A1A2E")
	}
}

func TestApplyLightDefaults(t *testing.T) {
	Apply("light", config.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#0366D6")) {
		t.Errorf("expected light primary #0366D6")
	}
	if !colorEqual(Background, lipgloss.Color("#FFFFFF")) {
		t.Errorf("expected light background #FFFFFF")
	}
	Apply("dark", config.Theme{})
}

func TestApplyDracula(t *testing.T) {
	Apply("dracula", config.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#BD93F9")) {
		t.Errorf("expected dracula primary #BD93F9")
	}
	Apply("dark", config.Theme{})
}

func TestApplyOverrides(t *testing.T) {
	Apply("dark", config.Theme{Primary: "#FF0000"})
	if !colorEqual(Primary, lipgloss.Color("#FF0000")) {
		t.Errorf("expected overridden primary #FF0000")
	}
	if !colorEqual(Accent, lipgloss.Color("#50C878")) {
		t.Errorf("expected dark accent #50C878")
	}
	Apply("dark", config.Theme{})
}

func TestApplyUnknownPresetFallsToDark(t *testing.T) {
	Apply("nonexistent", config.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#4A9EFF")) {
		t.Errorf("expected dark fallback primary #4A9EFF")
	}
}

func TestApplyCaseInsensitive(t *testing.T) {
	Apply("Dracula", config.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#BD93F9")) {
		t.Errorf("expected dracula primary #BD93F9")
	}
	Apply("dark", config.Theme{})
}

func TestCurrentThemeReportsDisplayName(t *testing.T) {
	t.Cleanup(func() { Apply("dark", config.Theme{}) })
	for _, tc := range []struct{ applied, want string }{
		{"dracula", "Dracula"},
		{"ANSI Dark", "ANSI Dark"},
		{"nonexistent", "Dark"},
	} {
		Apply(tc.applied, config.Theme{})
		if got := CurrentTheme(); got != tc.want {
			t.Errorf("after Apply(%q): CurrentTheme() = %q, want %q", tc.applied, got, tc.want)
		}
	}
}

// TestCurrentThemeRoundTripsEveryListedTheme pins the property theme
// cycling relies on: every name ThemeNames lists applies as itself, so
// CurrentTheme reports the listed name and the cycle finds its place in
// the list again. It includes custom themes, one of them overriding a
// built-in under a different case, which ThemeNames then lists as the
// custom spelling.
func TestCurrentThemeRoundTripsEveryListedTheme(t *testing.T) {
	saved := maps.Clone(customThemes)
	t.Cleanup(func() {
		customThemes = saved
		Apply("dark", config.Theme{})
	})
	colors := ThemeColors{Primary: "#AABBCC", Background: "#000000", Text: "#FFFFFF"}
	RegisterCustomTheme("My Cycle Theme", colors)
	RegisterCustomTheme("dracula", colors)

	names := ThemeNames()
	for _, want := range []string{"My Cycle Theme", "dracula"} {
		if !slices.Contains(names, want) {
			t.Fatalf("ThemeNames() does not list the custom theme %q", want)
		}
	}
	if slices.Contains(names, "Dracula") {
		t.Error("ThemeNames() lists the built-in Dracula next to the custom theme that overrides it")
	}
	for _, name := range names {
		Apply(name, config.Theme{})
		if got := CurrentTheme(); got != name {
			t.Errorf("after Apply(%q): CurrentTheme() = %q, want the listed name", name, got)
		}
	}
}

func TestThemeNames(t *testing.T) {
	names := ThemeNames()
	if len(names) < 12 {
		t.Errorf("expected at least 12 built-in themes, got %d", len(names))
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, expected := range []string{"Dark", "Light", "Dracula", "Nord"} {
		if !found[expected] {
			t.Errorf("expected theme %q in list", expected)
		}
	}
}

func TestApply_DefaultsSelectionColors(t *testing.T) {
	Apply("dracula", config.Theme{})
	if SelectionBackground == nil {
		t.Fatal("SelectionBackground must be non-nil after Apply (use sensible default when theme omits it)")
	}
	if SelectionForeground == nil {
		t.Fatal("SelectionForeground must be non-nil after Apply (use sensible default when theme omits it)")
	}
	rendered := SelectionStyle().Render("hello")
	if rendered == "" {
		t.Fatal("SelectionStyle().Render returned empty string")
	}
}

func TestApply_CustomSelectionFromTheme(t *testing.T) {
	RegisterCustomTheme("seltest", ThemeColors{
		Primary: "#000000", Accent: "#000000", Warning: "#000000",
		Error: "#000000", Background: "#000000", Surface: "#000000",
		SurfaceDark: "#000000", Text: "#FFFFFF", TextMuted: "#888888",
		Border:              "#222222",
		SelectionBackground: "#FF00FF",
		SelectionForeground: "#00FF00",
	})
	Apply("seltest", config.Theme{})
	r, g, b, _ := SelectionBackground.RGBA()
	if r>>8 != 0xFF || g>>8 != 0x00 || b>>8 != 0xFF {
		t.Fatalf("custom SelectionBackground not applied: got %02x%02x%02x", r>>8, g>>8, b>>8)
	}
	r, g, b, _ = SelectionForeground.RGBA()
	if r>>8 != 0x00 || g>>8 != 0xFF || b>>8 != 0x00 {
		t.Fatalf("custom SelectionForeground not applied: got %02x%02x%02x", r>>8, g>>8, b>>8)
	}
}

func TestApply_ResetsSelectionColorsBetweenThemes(t *testing.T) {
	// First apply seltest (registered above or re-register here for isolation).
	RegisterCustomTheme("seltest2", ThemeColors{
		Primary: "#111111", Accent: "#222222", Warning: "#333333",
		Error: "#444444", Background: "#555555", Surface: "#666666",
		SurfaceDark: "#777777", Text: "#888888", TextMuted: "#999999",
		Border:              "#AAAAAA",
		SelectionBackground: "#ABCDEF",
		SelectionForeground: "#FEDCBA",
	})
	Apply("seltest2", config.Theme{})
	// Now apply a theme that does NOT specify selection colors.
	Apply("dracula", config.Theme{})
	// SelectionBackground should not still be #ABCDEF.
	r, g, b, _ := SelectionBackground.RGBA()
	if r>>8 == 0xAB && g>>8 == 0xCD && b>>8 == 0xEF {
		t.Fatal("SelectionBackground leaked from previous theme; must reset to default when new theme omits it")
	}
}

func TestUserColorDeterministic(t *testing.T) {
	c1 := UserColor("U12345")
	c2 := UserColor("U12345")
	if !colorEqual(c1, c2) {
		t.Fatal("same userID must return same color")
	}
}

func TestUserColorEmptyFallback(t *testing.T) {
	Apply("dark", config.Theme{})
	if !colorEqual(UserColor(""), Primary) {
		t.Fatal("empty userID must fall back to Primary")
	}
}

func TestUserColorPaletteSpread(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range []string{"U1", "U2", "U3", "U4", "U5", "U6", "U7", "U8", "U9", "U10", "U11", "U12"} {
		r, g, b, _ := UserColor(id).RGBA()
		key := fmt.Sprintf("%02x%02x%02x", r>>8, g>>8, b>>8)
		seen[key] = true
	}
	if len(seen) < 8 {
		t.Fatalf("poor distribution: only %d distinct colors for 12 users", len(seen))
	}
}

func TestUsernameColoredToggle(t *testing.T) {
	Apply("dark", config.Theme{})
	// colored=false → Primary foreground
	s := Username("U123", false)
	if !colorEqual(s.GetForeground(), Primary) {
		t.Fatal("Username with colored=false must use Primary")
	}
	// colored=true → UserColor foreground
	s = Username("U123", true)
	if !colorEqual(s.GetForeground(), UserColor("U123")) {
		t.Fatal("Username with colored=true must use UserColor")
	}
	// colored=true + empty userID → Primary fallback
	s = Username("", true)
	if !colorEqual(s.GetForeground(), Primary) {
		t.Fatal("Username with empty userID must fall back to Primary")
	}
}

// The mention badge uses the theme's highlight pair rather than a
// hardcoded color, so it stays legible on every theme. Apply() derives
// Primary-on-Background when a theme omits the pair, so the style is
// never blank.
func TestMentionBadgeStyle_UsesThemeSelectionColors(t *testing.T) {
	Apply("dark", config.Theme{})
	s := MentionBadgeStyle()
	if !colorEqual(s.GetBackground(), SelectionBackground) {
		t.Errorf("background = %v, want SelectionBackground %v", s.GetBackground(), SelectionBackground)
	}
	if !colorEqual(s.GetForeground(), SelectionForeground) {
		t.Errorf("foreground = %v, want SelectionForeground %v", s.GetForeground(), SelectionForeground)
	}
}

// Switching themes must change the badge. A package-level var composed at
// init time would not, which is why this is a function.
func TestMentionBadgeStyle_TracksThemeChange(t *testing.T) {
	Apply("dark", config.Theme{})
	darkBg := MentionBadgeStyle().GetBackground()

	Apply("light", config.Theme{})
	lightBg := MentionBadgeStyle().GetBackground()

	if colorEqual(darkBg, lightBg) {
		t.Errorf("badge background did not change between dark and light themes (both %v)", darkBg)
	}

	// Restore the default so later tests in this package see a known theme.
	Apply("dark", config.Theme{})
}

// An explicit theme override for the selection pair must reach the badge.
func TestMentionBadgeStyle_HonorsSelectionOverride(t *testing.T) {
	Apply("dark", config.Theme{})
	defer Apply("dark", config.Theme{})

	SelectionBackground = lipgloss.Color("#123456")
	SelectionForeground = lipgloss.Color("#ABCDEF")
	s := MentionBadgeStyle()
	if !colorEqual(s.GetBackground(), lipgloss.Color("#123456")) {
		t.Errorf("background = %v, want #123456", s.GetBackground())
	}
	if !colorEqual(s.GetForeground(), lipgloss.Color("#ABCDEF")) {
		t.Errorf("foreground = %v, want #ABCDEF", s.GetForeground())
	}
}
