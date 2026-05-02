// internal/ui/styles/tint.go
package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// defaultTintAlpha is the share of the foreground color in mixColors when
// deriving compose-insert and selection backgrounds. 0.15 keeps the tint
// readable on every built-in theme without overpowering message text.
const defaultTintAlpha = 0.15

// ComposeInsertBG is the background color used by the compose box when
// focused (insert mode). Apply() populates this either from a theme's
// explicit colors.compose_insert_bg, or by mixing Accent into Background
// at defaultTintAlpha.
var ComposeInsertBG color.Color

// selectionBgFocused / selectionBgUnfocused hold the resolved tint colors
// for the selected-message row. They are populated by Apply() and read
// via SelectionTintColor. Held as package-private vars so callers always
// go through the function (which tolerates Apply() never having run yet
// during init-order edge cases).
var (
	selectionBgFocused   color.Color
	selectionBgUnfocused color.Color
)

// SelectionTintColor returns the background color used to fill the
// selected message row. When the panel has focus we tint with Accent;
// when it doesn't we tint with TextMuted (so the row is still visible
// but no longer competes with the focused panel).
func SelectionTintColor(focused bool) color.Color {
	if focused {
		if selectionBgFocused == nil {
			selectionBgFocused = mixColors(Accent, Background, defaultTintAlpha)
		}
		return selectionBgFocused
	}
	if selectionBgUnfocused == nil {
		selectionBgUnfocused = mixColors(TextMuted, Background, defaultTintAlpha)
	}
	return selectionBgUnfocused
}

// mixColors returns a straight-line RGB interpolation between fg and bg.
// alpha is the share of fg: 0.0 returns bg, 1.0 returns fg.
func mixColors(fg, bg color.Color, alpha float64) color.Color {
	if alpha <= 0 {
		return bg
	}
	if alpha >= 1 {
		return fg
	}
	fr, fg2, fb, _ := fg.RGBA()
	br, bg2, bb, _ := bg.RGBA()
	// RGBA returns 16-bit channels; collapse to 8-bit before mixing.
	fr8, fg8, fb8 := float64(fr>>8), float64(fg2>>8), float64(fb>>8)
	br8, bg8, bb8 := float64(br>>8), float64(bg2>>8), float64(bb>>8)
	r := uint8(fr8*alpha + br8*(1-alpha))
	g := uint8(fg8*alpha + bg8*(1-alpha))
	b := uint8(fb8*alpha + bb8*(1-alpha))
	return lipgloss.Color(rgbHex(r, g, b))
}

func rgbHex(r, g, b uint8) string {
	const hex = "0123456789ABCDEF"
	out := []byte("#000000")
	out[1] = hex[r>>4]
	out[2] = hex[r&0x0F]
	out[3] = hex[g>>4]
	out[4] = hex[g&0x0F]
	out[5] = hex[b>>4]
	out[6] = hex[b&0x0F]
	return string(out)
}

// resetDerivedTints invalidates the cached SelectionTintColor values so
// the next call recomputes from the current Accent/TextMuted/Background.
// Called by Apply() after the palette is rebuilt.
func resetDerivedTints() {
	selectionBgFocused = nil
	selectionBgUnfocused = nil
}
