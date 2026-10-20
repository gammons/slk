// Package ansi holds the ANSI escape helpers shared by the components in
// internal/bubbles and by internal/ui. It is not a parser: for that, use
// github.com/charmbracelet/x/ansi.
package ansi

import (
	"image/color"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

// ReapplyAfterResets re-emits style after every SGR reset in text, so inline
// styled spans don't leak the terminal's defaults through.
//
// style is one or more escape sequences, commonly a background colour or a
// background+foreground pair. Pass only the background to restore it; pass the
// pair as well when plain text following a styled span (e.g. the body after a
// <@user> mention) must keep the theme's text colour. An empty style returns
// text unchanged.
func ReapplyAfterResets(text, style string) string {
	if style == "" {
		return text
	}
	// lipgloss v2 emits the short form, but image renderers and other
	// ANSI producers may emit the explicit zero form.
	text = strings.ReplaceAll(text, "\x1b[0m", "\x1b[0m"+style)
	return strings.ReplaceAll(text, "\x1b[m", "\x1b[m"+style)
}

// ForegroundSGR is the SGR that sets the foreground to c. Put it right
// after a kitty image placement when text in the same style follows:
// a placement carries its image ID in the foreground and ends by
// resetting it ("\x1b[39m"), which ReapplyAfterResets does not catch,
// so the text would otherwise draw in the terminal's default colour.
// Pass the style's foreground, e.g. style.GetForeground().
func ForegroundSGR(c color.Color) string {
	return xansi.Style{}.ForegroundColor(c).String()
}
