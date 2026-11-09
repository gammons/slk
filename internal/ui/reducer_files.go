// internal/ui/reducer_files.go
//
// File-download routing: DownloadFileMsg (dispatched by the `d`
// keybinding, picker modal, or file-line click) starts an async
// download + OS open via App.downloadFileCmd.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

var reduceFiles reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case DownloadFileMsg:
		a.pendingDownloads = append(a.pendingDownloads, m.Attachment.Name)
		toastUntilReplaced(a, downloadProgressText(a))
		return a.downloadFileCmd(m.Attachment), true

	case downloadFileResultMsg:
		removePendingDownload(a, m.Name)
		if len(a.pendingDownloads) > 0 {
			// Keep the toast to active progress; combining filenames and results
			// can overflow the status row.
			toastUntilReplaced(a, downloadProgressText(a))
			return nil, true
		}
		return toastWithClear(a, m.Text, 3*time.Second), true

	default:
		return nil, false
	}
}

func downloadProgressText(a *App) string {
	return "Downloading " + a.pendingDownloads[len(a.pendingDownloads)-1] + "…"
}

func removePendingDownload(a *App, name string) {
	for i, pending := range a.pendingDownloads {
		if pending == name {
			a.pendingDownloads = append(a.pendingDownloads[:i], a.pendingDownloads[i+1:]...)
			return
		}
	}
}
