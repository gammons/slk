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
// Fetch + MarkRead together), the helpers use a read-modify-write
// pattern against the App's current adapter so each helper call
// preserves previously-set funcs.
//
// File name ends in _test.go so these are invisible outside the test
// binary.
package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
)

func (a *App) setThreadFetcherForTest(fn ThreadFetchFunc) {
	a.SetThreadService(NewThreadService(ThreadServiceFuncs{Fetch: fn}))
}

func (a *App) setThreadsListFetcherForTest(fn ThreadsListFetchFunc) {
	a.SetThreadService(NewThreadService(ThreadServiceFuncs{ListFetch: fn}))
}

func (a *App) setPermalinkFetcherForTest(fn PermalinkFetchFunc) {
	a.SetMessageService(NewMessageService(MessageServiceFuncs{Permalink: fn}))
}

// channelFuncsForTest returns a copy of the current ChannelService's
// closures so per-method test helpers can read-modify-write without
// overwriting previously-set funcs. Tests that chain
// setChannelXxxForTest calls rely on this to compose multi-closure
// service wiring incrementally.
func channelFuncsForTest(a *App) ChannelServiceFuncs {
	if adapter, ok := a.channels.(channelAdapter); ok {
		return adapter.fns
	}
	return ChannelServiceFuncs{}
}

func (a *App) setChannelFetcherForTest(fn ChannelFetchFunc) {
	fns := channelFuncsForTest(a)
	fns.Fetch = fn
	a.SetChannelService(NewChannelService(fns))
}

func (a *App) setChannelReadMarkerForTest(fn func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg) {
	fns := channelFuncsForTest(a)
	fns.MarkRead = fn
	a.SetChannelService(NewChannelService(fns))
}

func (a *App) setChannelCacheReaderForTest(fn ChannelCacheReadFunc) {
	fns := channelFuncsForTest(a)
	fns.ReadCache = fn
	a.SetChannelService(NewChannelService(fns))
}

func (a *App) setChannelSyncedAtReaderForTest(fn func(channelID ids.ChannelID) int64) {
	fns := channelFuncsForTest(a)
	fns.SyncedAt = fn
	a.SetChannelService(NewChannelService(fns))
}

func (a *App) setOlderMessagesFetcherForTest(fn OlderMessagesFetchFunc) {
	fns := channelFuncsForTest(a)
	fns.FetchOlder = fn
	a.SetChannelService(NewChannelService(fns))
}

func setChannelFetchAroundForTest(a *App, fn func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg) {
	fns := channelFuncsForTest(a)
	fns.FetchAround = fn
	a.SetChannelService(NewChannelService(fns))
}

func (a *App) setChannelLookupFuncForTest(fn ChannelLookupFunc) {
	fns := channelFuncsForTest(a)
	fns.Lookup = fn
	a.SetChannelService(NewChannelService(fns))
}

func (a *App) setChannelVisitRecorderForTest(fn ChannelVisitRecorder) {
	fns := channelFuncsForTest(a)
	fns.RecordVisit = fn
	a.SetChannelService(NewChannelService(fns))
}

func (a *App) setChannelMembershipFetcherForTest(fn func(channelID ids.ChannelID)) {
	fns := channelFuncsForTest(a)
	fns.MembershipFetch = fn
	a.SetChannelService(NewChannelService(fns))
}
