// Package blockkit color.go: maps Slack attachment color tokens to
// terminal-renderable color strings.
package blockkit

import (
	"fmt"
	"image/color"
	"regexp"

	"github.com/gammons/slk/internal/ui/styles"
)

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ResolveAttachmentColor maps a Slack attachment-color value to a
// hex color string ("#rrggbb") suitable for use with lipgloss.Color.
// It accepts:
//   - "good"     → theme accent
//   - "warning"  → theme warning
//   - "danger"   → theme error
//   - "#RRGGBB"  → passthrough
//   - anything else (including "") → theme border (subdued)
//
// The output is always non-empty.
func ResolveAttachmentColor(c string) string {
	switch c {
	case "good":
		return colorString(styles.Accent)
	case "warning":
		return colorString(styles.Warning)
	case "danger":
		return colorString(styles.Error)
	}
	if hexColorRe.MatchString(c) {
		return c
	}
	return colorString(styles.Border)
}

// colorString converts a color.Color (the type used by the styles
// package's exported theme colors) into a "#rrggbb" hex string. We
// go through the color.Color RGBA() interface method because the
// concrete lipgloss.Color values land as image/color.RGBA, which
// has no String method and whose %v formatting ("{r g b a}") is not
// re-parseable by lipgloss.
func colorString(c color.Color) string {
	r, g, b, _ := c.RGBA()
	// RGBA returns 16-bit per channel; shift down to 8-bit.
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
