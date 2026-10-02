package main

import (
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slackhttp"
)

// pendingSaver is the part of *ui.App that shutdown needs.
type pendingSaver interface {
	// SavePendingTheme saves a theme a cycle applied but, waiting for
	// the pause after the last press, has not saved yet.
	SavePendingTheme()
}

// shutdown runs the steps that follow the Bubble Tea program's exit,
// whichever way it exits (the quit prompt, ctrl+c, SIGTERM).
func shutdown(app pendingSaver, router *workspaceRouter) {
	// A theme cycle saves only after a pause, so save a theme still
	// pending: quitting right after the last alt+y would lose it.
	app.SavePendingTheme()

	// Dump the API request tally before the connections close.
	//
	// Phase 2b's success criteria are call counts -- "a boot issues
	// <= 10 API calls, with zero users.list and zero per-channel
	// conversations.history fan-out" -- and nothing in slk could
	// report them. Reconstructing the numbers from a debug log only
	// worked at all because triggerBackfill happens to log per
	// channel; there was no way to see users.list or a total.
	//
	// Nobody is testing slk against a real Enterprise Grid account
	// until the whole grid-parity series lands, so this is the only
	// feedback loop the work has.
	if debuglog.Enabled() {
		debuglog.General("shutdown API request tally:\n%s", slackhttp.DefaultCounter.Report())
	}

	// Clean up connection managers
	for _, wctx := range router.All() {
		if wctx.ConnMgr != nil {
			wctx.ConnMgr.Stop()
		}
	}
}
