// internal/ui/services_helpers_test.go
//
// Test-only helper methods on App that wire single service-method
// closures. The production surface takes a full XxxServiceFuncs
// bundle; most tests only need one closure, and these helpers
// preserve the original per-method SetXxx call style without
// polluting the production API.
//
// For services where tests routinely chain multiple SetXxx calls
// (notably ChannelService — many tests wire ReadCache + SyncedAt +
// Fetch + MarkRead together), the helpers remember the closures each
// App was last wired with, so each helper call preserves previously-set
// funcs.
//
// File name ends in _test.go so these are invisible outside the test
// binary.
package ui

import (
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"golang.design/x/clipboard"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/editor"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/themeswitcher"
)

func (a *App) setThreadFetcherForTest(fn core.ThreadFetchFunc) {
	a.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{Fetch: fn}))
}

func (a *App) setThreadsListFetcherForTest(fn core.ThreadsListFetchFunc) {
	a.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{ListFetch: fn}))
}

func (a *App) setPermalinkFetcherForTest(fn core.PermalinkFetchFunc) {
	a.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{Permalink: fn}))
}

var (
	channelFuncsMu sync.Mutex
	channelFuncs   = map[*App]core.ChannelServiceFuncs{}
)

// setChannelFuncsForTest installs a ChannelService built from fns and
// remembers fns, so the per-method helpers below can add one closure at
// a time without dropping the others.
func setChannelFuncsForTest(a *App, fns core.ChannelServiceFuncs) {
	channelFuncsMu.Lock()
	channelFuncs[a] = fns
	channelFuncsMu.Unlock()
	a.SetChannelService(core.NewChannelService(fns))
}

// channelFuncsForTest returns the closures a's ChannelService was last
// built from via setChannelFuncsForTest.
func channelFuncsForTest(a *App) core.ChannelServiceFuncs {
	channelFuncsMu.Lock()
	defer channelFuncsMu.Unlock()
	return channelFuncs[a]
}

func (a *App) setChannelFetcherForTest(fn core.ChannelFetchFunc) {
	fns := channelFuncsForTest(a)
	fns.Fetch = fn
	setChannelFuncsForTest(a, fns)
}

func (a *App) setChannelReadMarkerForTest(fn func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg) {
	fns := channelFuncsForTest(a)
	fns.MarkRead = fn
	setChannelFuncsForTest(a, fns)
}

func (a *App) setChannelCacheReaderForTest(fn core.ChannelCacheReadFunc) {
	fns := channelFuncsForTest(a)
	fns.ReadCache = fn
	setChannelFuncsForTest(a, fns)
}

func (a *App) setChannelSyncedAtReaderForTest(fn func(channelID ids.ChannelID) int64) {
	fns := channelFuncsForTest(a)
	fns.SyncedAt = fn
	setChannelFuncsForTest(a, fns)
}

func (a *App) setOlderMessagesFetcherForTest(fn core.OlderMessagesFetchFunc) {
	fns := channelFuncsForTest(a)
	fns.FetchOlder = fn
	setChannelFuncsForTest(a, fns)
}

func setChannelFetchAroundForTest(a *App, fn func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg) {
	fns := channelFuncsForTest(a)
	fns.FetchAround = fn
	setChannelFuncsForTest(a, fns)
}

func (a *App) setChannelLookupFuncForTest(fn core.ChannelLookupFunc) {
	fns := channelFuncsForTest(a)
	fns.Lookup = fn
	setChannelFuncsForTest(a, fns)
}

func (a *App) setChannelVisitRecorderForTest(fn core.ChannelVisitRecorder) {
	fns := channelFuncsForTest(a)
	fns.RecordVisit = fn
	setChannelFuncsForTest(a, fns)
}

func (a *App) setChannelMembershipFetcherForTest(fn func(channelID ids.ChannelID)) {
	fns := channelFuncsForTest(a)
	fns.MembershipFetch = fn
	setChannelFuncsForTest(a, fns)
}

// wiring holds the closures a test App's single-closure-built services
// were given, so helpers for sibling methods don't drop each other.
type wiring struct {
	upload           func(channelID, threadTS, caption string, attachments []core.PendingAttachment) core.Cmd
	desktop          core.DesktopServiceFuncs
	saveTheme        func(name string, scope core.ThemeScope)
	saveSidebarWidth func(width int)
	readStates       func() map[string]core.ReadState
	unreadWorkspaces func() []string
}

var (
	wiringsMu sync.Mutex
	wirings   = map[*App]*wiring{}
)

// rewire applies change to a's recorded closures and returns a copy.
func rewire(a *App, change func(w *wiring)) wiring {
	wiringsMu.Lock()
	defer wiringsMu.Unlock()
	w := wirings[a]
	if w == nil {
		w = &wiring{}
		wirings[a] = w
	}
	change(w)
	return *w
}

// coreCmd is teaCmd in reverse, for tests written against tea.Cmd.
func coreCmd(c tea.Cmd) core.Cmd {
	if c == nil {
		return nil
	}
	return func() core.Msg { return c() }
}

func (a *App) setUploaderForTest(fn func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd) {
	w := rewire(a, func(w *wiring) {
		w.upload = func(channelID, threadTS, caption string, attachments []core.PendingAttachment) core.Cmd {
			return coreCmd(fn(channelID, threadTS, caption, attachments))
		}
	})
	a.SetFileService(core.NewFileService(w.upload, nil))
}

func (a *App) setDesktopForTest(change func(d *core.DesktopServiceFuncs)) {
	w := rewire(a, func(w *wiring) { change(&w.desktop) })
	a.SetDesktopService(core.NewDesktopService(w.desktop))
}

// setClipboardReaderForTest keeps the x/clipboard-shaped fakes tests
// were written with.
func (a *App) setClipboardReaderForTest(fn func(format clipboard.Format) []byte) {
	a.setDesktopForTest(func(d *core.DesktopServiceFuncs) {
		d.ReadClipboard = func(f core.ClipboardFormat) []byte {
			if f == core.ClipboardImage {
				return fn(clipboard.FmtImage)
			}
			return fn(clipboard.FmtText)
		}
	})
}

// setFilesystemForTest lets paste-a-path see real files.
func (a *App) setFilesystemForTest() {
	a.setDesktopForTest(func(d *core.DesktopServiceFuncs) { d.Stat = os.Stat })
}

// setEditorForTest gives Ctrl+E the real temp file and editor process.
func (a *App) setEditorForTest() {
	a.SetEditorService(core.NewEditorService(editor.WriteDraft, editor.Edit, editor.TakeDraft))
}

func (a *App) setStatusReporterForTest(fn func(unread, otherUnread int, workspace, title string)) {
	a.setDesktopForTest(func(d *core.DesktopServiceFuncs) { d.ReportStatus = fn })
}

func (a *App) setStatusSetterForTest(fn func(action presencemenu.Action, snoozeMinutes int)) {
	a.SetPresenceService(core.NewPresenceService(fn, nil))
}

func (a *App) setThemeSaverForTest(fn func(name string, scope themeswitcher.ThemeScope)) {
	w := rewire(a, func(w *wiring) { w.saveTheme = fn })
	a.SetSettingsService(core.NewSettingsService(w.saveTheme, w.saveSidebarWidth))
}

func (a *App) setWidthSaverForTest(fn func(width int)) {
	w := rewire(a, func(w *wiring) { w.saveSidebarWidth = fn })
	a.SetSettingsService(core.NewSettingsService(w.saveTheme, w.saveSidebarWidth))
}

func (a *App) setReadStateReaderForTest(fn func() map[string]core.ReadState) {
	w := rewire(a, func(w *wiring) { w.readStates = fn })
	a.SetUnreadService(core.NewUnreadService(w.readStates, w.unreadWorkspaces))
}

func (a *App) setWorkspaceUnreadReaderForTest(fn func() []string) {
	w := rewire(a, func(w *wiring) { w.unreadWorkspaces = fn })
	a.SetUnreadService(core.NewUnreadService(w.readStates, w.unreadWorkspaces))
}

func (a *App) setWorkspaceSwitcherForTest(fn func(teamID string) tea.Msg) {
	a.SetWorkspaceService(core.NewWorkspaceService(func(teamID string) core.Msg { return fn(teamID) }))
}
