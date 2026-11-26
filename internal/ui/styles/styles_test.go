package styles

import (
	"fmt"
	"image/color"
	"maps"
	"slices"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/core"
)

// colorEqual compares two color.Color values.
func colorEqual(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

func TestApplyDarkDefaults(t *testing.T) {
	Apply("dark", core.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#4A9EFF")) {
		t.Errorf("expected dark primary #4A9EFF")
	}
	if !colorEqual(Background, lipgloss.Color("#1A1A2E")) {
		t.Errorf("expected dark background #1A1A2E")
	}
}

func TestApplyLightDefaults(t *testing.T) {
	Apply("light", core.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#0366D6")) {
		t.Errorf("expected light primary #0366D6")
	}
	if !colorEqual(Background, lipgloss.Color("#FFFFFF")) {
		t.Errorf("expected light background #FFFFFF")
	}
	Apply("dark", core.Theme{})
}

func TestApplyDracula(t *testing.T) {
	Apply("dracula", core.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#BD93F9")) {
		t.Errorf("expected dracula primary #BD93F9")
	}
	Apply("dark", core.Theme{})
}

func TestApplyOverrides(t *testing.T) {
	Apply("dark", core.Theme{Primary: "#FF0000"})
	if !colorEqual(Primary, lipgloss.Color("#FF0000")) {
		t.Errorf("expected overridden primary #FF0000")
	}
	if !colorEqual(Accent, lipgloss.Color("#50C878")) {
		t.Errorf("expected dark accent #50C878")
	}
	Apply("dark", core.Theme{})
}

func TestApplyUnknownPresetFallsToDark(t *testing.T) {
	Apply("nonexistent", core.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#4A9EFF")) {
		t.Errorf("expected dark fallback primary #4A9EFF")
	}
}

func TestApplyCaseInsensitive(t *testing.T) {
	Apply("Dracula", core.Theme{})
	if !colorEqual(Primary, lipgloss.Color("#BD93F9")) {
		t.Errorf("expected dracula primary #BD93F9")
	}
	Apply("dark", core.Theme{})
}

func TestCurrentThemeReportsDisplayName(t *testing.T) {
	t.Cleanup(func() { Apply("dark", core.Theme{}) })
	for _, tc := range []struct{ applied, want string }{
		{"dracula", "Dracula"},
		{"ANSI Dark", "ANSI Dark"},
		{"nonexistent", "Dark"},
	} {
		Apply(tc.applied, core.Theme{})
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
		Apply("dark", core.Theme{})
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
		Apply(name, core.Theme{})
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
	Apply("dracula", core.Theme{})
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
	Apply("seltest", core.Theme{})
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
	Apply("seltest2", core.Theme{})
	// Now apply a theme that does NOT specify selection colors.
	Apply("dracula", core.Theme{})
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
	Apply("dark", core.Theme{})
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
	Apply("dark", core.Theme{})
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

func TestApply_UsernameAndSidebarSelectedFromTheme(t *testing.T) {
	RegisterCustomTheme("slacklike", ThemeColors{
		Primary: "#1264A3", Accent: "#007A5A", Warning: "#ECB22E",
		Error: "#C01343", Background: "#FFFFFF", Surface: "#F8F8F8",
		SurfaceDark: "#F2F2F2", Text: "#1D1C1D", TextMuted: "#616061",
		Border: "#DDDDDD", SidebarText: "#1D1C1D",
		Username: "#1D1C1D", SidebarSelectedBg: "#333133",
	})
	Apply("slacklike", core.Theme{})
	if !colorEqual(Username("U123", false).GetForeground(), lipgloss.Color("#1D1C1D")) {
		t.Fatal("username role not applied")
	}
	if !colorEqual(SidebarSelectedBg, lipgloss.Color("#333133")) {
		t.Fatal("sidebar_selected_bg not applied")
	}
	if !colorEqual(SidebarSelectedFg, SidebarText) {
		t.Fatal("sidebar_selected_fg must default to SidebarText")
	}

	if bg := MentionBadgeStyle().GetBackground(); !colorEqual(bg, SelectionBackground) {
		t.Fatal("badge must keep the selection colors when mention_badge_bg is unset")
	}

	Apply("dark", core.Theme{})
	if SidebarSelectedBg != nil || !colorEqual(UsernameColor, Primary) {
		t.Fatal("username / sidebar_selected_bg leaked into a theme without them")
	}
}

func TestMentionBadgeStyle_ThemeRole(t *testing.T) {
	RegisterCustomTheme("badgetest", ThemeColors{
		Primary: "#1264A3", Accent: "#007A5A", Warning: "#ECB22E",
		Error: "#C01343", Background: "#FFFFFF", Surface: "#F8F8F8",
		SurfaceDark: "#F2F2F2", Text: "#1D1C1D", TextMuted: "#616061",
		Border: "#DDDDDD", MentionBadgeBg: "#C01343", MentionBadgeFg: "#FFFFFF",
	})
	defer Apply("dark", core.Theme{})
	Apply("badgetest", core.Theme{})
	s := MentionBadgeStyle()
	if !colorEqual(s.GetBackground(), lipgloss.Color("#C01343")) || !colorEqual(s.GetForeground(), lipgloss.Color("#FFFFFF")) {
		t.Fatalf("badge = %v on %v, want #FFFFFF on #C01343", s.GetForeground(), s.GetBackground())
	}
	Apply("dark", core.Theme{})
	if MentionBadgeBg != nil || !colorEqual(MentionBadgeStyle().GetBackground(), SelectionBackground) {
		t.Fatal("mention_badge_bg leaked into a theme without it")
	}
}

// The mention badge uses the theme's highlight pair rather than a
// hardcoded color, so it stays legible on every theme. Apply() derives
// Primary-on-Background when a theme omits the pair, so the style is
// never blank.
func TestMentionBadgeStyle_UsesThemeSelectionColors(t *testing.T) {
	Apply("dark", core.Theme{})
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
	Apply("dark", core.Theme{})
	darkBg := MentionBadgeStyle().GetBackground()

	Apply("light", core.Theme{})
	lightBg := MentionBadgeStyle().GetBackground()

	if colorEqual(darkBg, lightBg) {
		t.Errorf("badge background did not change between dark and light themes (both %v)", darkBg)
	}

	// Restore the default so later tests in this package see a known theme.
	Apply("dark", core.Theme{})
}

// An explicit theme override for the selection pair must reach the badge.
func TestMentionBadgeStyle_HonorsSelectionOverride(t *testing.T) {
	Apply("dark", core.Theme{})
	defer Apply("dark", core.Theme{})

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

// Mention colors default to Warning (Slack's gold) and follow the theme
// when it sets them; switching back drops the theme's values.
func TestMentionColors_DefaultAndThemeRole(t *testing.T) {
	defer Apply("dark", core.Theme{})
	Apply("dark", core.Theme{})
	bg, fg := SelfMentionColors()
	if !colorEqual(bg, Warning) || !colorEqual(fg, ThemeBackground()) {
		t.Errorf("self mention = %v on %v, want Background on Warning", fg, bg)
	}
	if !colorEqual(MentionHighlightBar(), Warning) {
		t.Errorf("bar = %v, want Warning", MentionHighlightBar())
	}
	if want := mixColors(Warning, ThemeBackground(), defaultTintAlpha); !colorEqual(MentionHighlightBg(), want) {
		t.Errorf("highlight bg = %v, want %v", MentionHighlightBg(), want)
	}

	RegisterCustomTheme("mentiontest", ThemeColors{
		Primary: "#1264A3", Accent: "#007A5A", Warning: "#ECB22E",
		Error: "#C01343", Background: "#FFFFFF", Surface: "#F8F8F8",
		SurfaceDark: "#F2F2F2", Text: "#1D1C1D", TextMuted: "#616061", Border: "#DDDDDD",
		SelfMentionBg: "#111111", SelfMentionFg: "#222222",
		MentionHighlightBg: "#333333", MentionHighlightBar: "#444444",
	})
	Apply("mentiontest", core.Theme{})
	bg, fg = SelfMentionColors()
	for _, c := range []struct {
		name      string
		got, want color.Color
	}{
		{"self_mention_bg", bg, lipgloss.Color("#111111")},
		{"self_mention_fg", fg, lipgloss.Color("#222222")},
		{"mention_highlight_bg", MentionHighlightBg(), lipgloss.Color("#333333")},
		{"mention_highlight_bar", MentionHighlightBar(), lipgloss.Color("#444444")},
	} {
		if !colorEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	Apply("dark", core.Theme{})
	if !colorEqual(MentionHighlightBar(), Warning) {
		t.Error("mention_highlight_bar leaked into a theme without it")
	}
}
