// internal/ui/reducer_files.go
//
// File-download routing: DownloadFileMsg (dispatched by the `d`
// keybinding, picker modal, or file-line click) starts an async
// download + OS open via App.downloadFileCmd.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

var reduceFiles reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(DownloadFileMsg)
	if !ok {
		return nil, false
	}
	toastUntilReplaced(a, "Downloading "+m.Attachment.Name+"…")
	return a.downloadFileCmd(m.Attachment), true
}
