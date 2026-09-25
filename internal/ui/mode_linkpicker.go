// internal/ui/mode_linkpicker.go
//
// Key handler for ModeLinkPicker: the chooser modal opened by the `o`
// keybinding (multiple links) or the `d` keybinding (multiple file
// attachments). Enter dispatches OpenLinkMsg or DownloadFileMsg
// depending on the kind recorded when the picker was opened; esc/q
// closes.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func handleLinkPickerMode(a *App, msg tea.KeyMsg) tea.Cmd {
	item, chosen := a.linkPicker.HandleKey(msg.String())
	if chosen {
		a.SetMode(ModeNormal)
		if a.pickerKind == "files" {
			files := a.pickerFiles
			a.pickerFiles = nil
			a.pickerKind = ""
			if item.Index < 0 || item.Index >= len(files) {
				return nil
			}
			att := files[item.Index]
			return func() tea.Msg { return DownloadFileMsg{Attachment: att} }
		}
		url := item.URL
		return func() tea.Msg { return OpenLinkMsg{URL: url} }
	}
	if !a.linkPicker.IsVisible() {
		// esc/q closed the picker.
		a.SetMode(ModeNormal)
		a.pickerFiles = nil
		a.pickerKind = ""
	}
	return nil
}
