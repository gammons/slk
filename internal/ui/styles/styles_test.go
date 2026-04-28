package styles

import (
	"image/color"
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
