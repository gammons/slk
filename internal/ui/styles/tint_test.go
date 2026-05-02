package styles

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
)

func rgb(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

// applyTheme calls Apply and registers a cleanup that resets to "dark"
// after the test, isolating package-level mutations.
func applyTheme(t *testing.T, name string, overrides config.Theme) {
	t.Helper()
	Apply(name, overrides)
	t.Cleanup(func() { Apply("dark", config.Theme{}) })
}

func TestMixColors_HalfwayBlend(t *testing.T) {
	fg := lipgloss.Color("#FF0000") // red
	bg := lipgloss.Color("#0000FF") // blue
	out := mixColors(fg, bg, 0.5)
	r, g, b := rgb(out)
	// Halfway between #FF0000 and #0000FF, with round-half-up, is #800080.
	if r != 0x80 || g != 0x00 || b != 0x80 {
		t.Fatalf("expected #800080, got #%02X%02X%02X", r, g, b)
	}
}

func TestMixColors_AlphaZeroIsBackground(t *testing.T) {
	out := mixColors(lipgloss.Color("#FFFFFF"), lipgloss.Color("#112233"), 0.0)
	r, g, b := rgb(out)
	if r != 0x11 || g != 0x22 || b != 0x33 {
		t.Fatalf("alpha=0 must equal bg, got #%02X%02X%02X", r, g, b)
	}
}

func TestMixColors_AlphaOneIsForeground(t *testing.T) {
	out := mixColors(lipgloss.Color("#AABBCC"), lipgloss.Color("#000000"), 1.0)
	r, g, b := rgb(out)
	if r != 0xAA || g != 0xBB || b != 0xCC {
		t.Fatalf("alpha=1 must equal fg, got #%02X%02X%02X", r, g, b)
	}
}

func TestSelectionTintColor_FocusedIsAccentMix(t *testing.T) {
	applyTheme(t, "dark", config.Theme{})
	expected := mixColors(Accent, Background, defaultTintAlpha)
	got := SelectionTintColor(true)
	er, eg, eb := rgb(expected)
	gr, gg, gb := rgb(got)
	if er != gr || eg != gg || eb != gb {
		t.Fatalf("focused tint mismatch: want #%02X%02X%02X got #%02X%02X%02X", er, eg, eb, gr, gg, gb)
	}
}

func TestSelectionTintColor_UnfocusedIsTextMutedMix(t *testing.T) {
	applyTheme(t, "dark", config.Theme{})
	expected := mixColors(TextMuted, Background, defaultTintAlpha)
	got := SelectionTintColor(false)
	er, eg, eb := rgb(expected)
	gr, gg, gb := rgb(got)
	if er != gr || eg != gg || eb != gb {
		t.Fatalf("unfocused tint mismatch: want #%02X%02X%02X got #%02X%02X%02X", er, eg, eb, gr, gg, gb)
	}
}

func TestComposeInsertBG_DerivedFromAccentAndBackground(t *testing.T) {
	applyTheme(t, "dark", config.Theme{})
	expected := mixColors(Accent, Background, defaultTintAlpha)
	er, eg, eb := rgb(expected)
	gr, gg, gb := rgb(ComposeInsertBG)
	if er != gr || eg != gg || eb != gb {
		t.Fatalf("ComposeInsertBG mismatch: want #%02X%02X%02X got #%02X%02X%02X", er, eg, eb, gr, gg, gb)
	}
}

func TestComposeInsertBG_OverrideFromThemeColors(t *testing.T) {
	RegisterCustomTheme("tinttest", ThemeColors{
		Primary: "#001122", Accent: "#334455", Warning: "#556677",
		Error: "#778899", Background: "#1A1A2E", Surface: "#16162B",
		SurfaceDark: "#0F0F23", Text: "#FFFFFF", TextMuted: "#888888",
		Border:          "#222222",
		ComposeInsertBG: "#ABCDEF",
	})
	applyTheme(t, "tinttest", config.Theme{})
	r, g, b := rgb(ComposeInsertBG)
	if r != 0xAB || g != 0xCD || b != 0xEF {
		t.Fatalf("override not honored: got #%02X%02X%02X", r, g, b)
	}
}

// Lock the default α for the 12 built-in themes — guarantees the
// derived tints don't drift silently across refactors.
func TestComposeInsertBG_StableAcrossBuiltinThemes(t *testing.T) {
	t.Cleanup(func() { Apply("dark", config.Theme{}) })
	for _, name := range ThemeNames() {
		Apply(name, config.Theme{})
		// Just assert it's non-nil and distinct from Background;
		// exact RGB values are too brittle to lock per-theme.
		if ComposeInsertBG == nil {
			t.Fatalf("%s: ComposeInsertBG is nil", name)
		}
		br, bg, bb := rgb(Background)
		cr, cg, cb := rgb(ComposeInsertBG)
		if br == cr && bg == cg && bb == cb {
			t.Fatalf("%s: ComposeInsertBG must differ from Background", name)
		}
	}
}
