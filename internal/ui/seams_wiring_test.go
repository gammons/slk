package ui

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/export"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/themeswitcher"
)

// How seams_test.go installs each collaborator on the App. The scenarios
// there must not change when the wiring does; this file is the only one
// that should.

func wireStatusSetter(a *App, fn func(action presencemenu.Action, mins int)) {
	a.SetPresenceService(core.NewPresenceService(fn, nil))
}

func wireTypingSender(a *App, fn func(channelID string)) {
	a.SetPresenceService(core.NewPresenceService(nil, fn))
}

func wireThemeSaver(a *App, fn func(name string, scope themeswitcher.ThemeScope)) {
	a.SetSettingsService(core.NewSettingsService(fn, nil))
}

func wireWidthSaver(a *App, fn func(width int)) {
	a.SetSettingsService(core.NewSettingsService(nil, fn))
}

func wireUploader(a *App, fn func(channelID, threadTS, caption string, atts []compose.PendingAttachment) tea.Cmd) {
	a.SetFileService(core.NewFileService(func(channelID, threadTS, caption string, atts []core.PendingAttachment) core.Cmd {
		return coreCmd(fn(channelID, threadTS, caption, atts))
	}, nil))
}

func wireWorkspaceSwitcher(a *App, fn func(teamID string) tea.Msg) {
	a.SetWorkspaceService(core.NewWorkspaceService(func(teamID string) core.Msg { return fn(teamID) }))
}

func wireUnreadReaders(a *App, channels func() map[string]cache.ReadState, workspaces func() []string) {
	a.SetUnreadService(core.NewUnreadService(channels, workspaces))
}

func wireStatusReporter(a *App, fn func(unread, otherUnread int, workspace, title string)) {
	a.setDesktopForTest(func(d *core.DesktopServiceFuncs) { d.ReportStatus = fn })
}

func wireAvatars(a *App, fn func(userID string) string) {
	a.SetAvatarService(core.NewAvatarService(fn))
}

// wireClipboard makes the clipboard hold text and/or PNG bytes.
func wireClipboard(a *App, text string, png []byte) {
	a.SetClipboardAvailable(true)
	a.setDesktopForTest(func(d *core.DesktopServiceFuncs) {
		d.ReadClipboard = func(f core.ClipboardFormat) []byte {
			if f == core.ClipboardImage {
				return png
			}
			return []byte(text)
		}
	})
}

// wireFilesystem gives the App the real filesystem for paste-a-path.
func wireFilesystem(a *App) {
	a.setDesktopForTest(func(d *core.DesktopServiceFuncs) { d.Stat = os.Stat })
}

// wireThreadExport gives the App the real thread exporter.
func wireThreadExport(a *App) {
	a.setDesktopForTest(func(d *core.DesktopServiceFuncs) { d.SaveThread = export.SaveThread })
}

// wireEditor gives the App the real external-editor plumbing, launching
// argv. nil leaves the editor unconfigured.
func wireEditor(a *App, argv []string) {
	a.SetComposeEditor(argv)
	a.setEditorForTest()
}

func wireChannelFetch(a *App, fn func(channelID ids.ChannelID, channelName string) tea.Msg) {
	a.SetChannelService(core.NewChannelService(core.ChannelServiceFuncs{
		Fetch: func(ch ids.ChannelID, name string) core.Msg { return fn(ch, name) },
	}))
}

func wireOpenConversation(a *App, fn func(userIDs []string, requestID uint64) tea.Cmd) {
	a.SetChannelService(core.NewChannelService(core.ChannelServiceFuncs{
		OpenConversation: func(userIDs []string, requestID uint64) core.Cmd { return coreCmd(fn(userIDs, requestID)) },
	}))
}

func wireMessageSend(a *App, fn func(channelID ids.ChannelID, text string) tea.Msg) {
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
		Send: func(ch ids.ChannelID, text string) core.Msg { return fn(ch, text) },
	}))
}

func wireThreadMark(a *App, fn func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd) {
	a.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
		Mark: func(ch ids.ChannelID, thread ids.ThreadTS, ts ids.MessageTS) core.Cmd {
			return coreCmd(fn(ch, thread, ts))
		},
	}))
}
