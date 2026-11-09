// internal/ui/link_picker.go
//
// Wiring for the link/file picker: theme -> Styles and opening it. The
// compositing over the dimmed backdrop is in view_overlays.go.
package ui

import (
	"github.com/gammons/slk/internal/bubbles/linkpicker"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// linkPickerStyles maps the active theme onto the picker's styles. The
// picker keeps a snapshot, so it is pushed a fresh one on open and by
// applyTheme on every theme change.
func linkPickerStyles() linkpicker.Styles {
	cs := messages.ChromeStyles()
	return linkpicker.Styles{
		Chrome: cs,
		Badge:  cs.Row.Foreground(styles.Accent),
	}
}

// openLinkPicker pushes the current theme and width, then shows the picker
// over items.
func (a *App) openLinkPicker(title string, items []linkpicker.Item) {
	a.linkPicker.SetStyles(linkPickerStyles())
	a.linkPicker.SetWidth(a.width)
	a.linkPicker.Open(title, items)
}
