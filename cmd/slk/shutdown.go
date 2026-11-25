package main

import (
	"context"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slackhttp"
)

// pendingSaver is the part of *ui.App that shutdown needs.
type pendingSaver interface {
	// SavePendingTheme saves a theme a cycle applied but, waiting for
	// the pause after the last press, has not saved yet.
	SavePendingTheme()
	// UnsyncedDrafts reports drafts typed since the last sync, saved
	// before the connections close.
	UnsyncedDrafts() []core.DraftChange
}

// shutdown runs the steps that follow the Bubble Tea program's exit,
// whichever way it exits (the quit prompt, ctrl+c, SIGTERM).
func shutdown(app pendingSaver, router *workspaceRouter) {
	// A theme cycle saves only after a pause, so save a theme still
	// pending: quitting right after the last alt+y would lose it.
	app.SavePendingTheme()

	// Draft sync saves while typing, but up to two seconds behind. This
	// deadline is independent of draftTimeout (drafts.go): the engine's
	// per-conversation lock is ctx-aware, so a slower save already in
	// flight for the same conversation (bounded by draftTimeout) cannot
	// make this flush outlive its own 2s budget.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	flushDrafts(ctx, router, app.UnsyncedDrafts())
	cancel()

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
