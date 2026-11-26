package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
	slackclient "github.com/gammons/slk/internal/slack"
)

// recordingSaver counts SavePendingTheme calls and hands out drafts.
type recordingSaver struct {
	saves  int
	drafts []core.DraftChange
}

func (r *recordingSaver) SavePendingTheme()                  { r.saves++ }
func (r *recordingSaver) UnsyncedDrafts() []core.DraftChange { return r.drafts }

// TestShutdown_SavesPendingTheme pins the exit flush: a theme cycle
// saves only after a pause, so shutdown must save one still pending.
func TestShutdown_SavesPendingTheme(t *testing.T) {
	app := &recordingSaver{}
	shutdown(app, newWorkspaceRouter())
	if app.saves != 1 {
		t.Errorf("SavePendingTheme called %d times, want 1", app.saves)
	}
}

func TestFlushDrafts_SavesAndDiscardsThroughEachWorkspace(t *testing.T) {
	api := &fakeDraftAPI{}
	eng := newDraftEngine(api, "T1")
	_, _ = eng.Save(context.Background(), draftDest{channelID: "C2"}, "old", time.Time{}, nil)
	router := newWorkspaceRouter()
	router.Add(&WorkspaceContext{TeamID: "T1", Drafts: eng})
	flushDrafts(context.Background(), router, []core.DraftChange{
		{Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "typed", Edited: time.UnixMilli(7)},
		{Key: core.DraftKey{TeamID: "T1", ChannelID: "C2"}},
		{Key: core.DraftKey{TeamID: "T9", ChannelID: "C1"}, Text: "unknown workspace"},
	})
	want := "[create C2 old create C1 typed delete Dr1]"
	if got := fmt.Sprint(api.Calls()); got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
}

// The quit flush must not wait out a save already in flight for the same
// conversation (that save runs under its own, much longer draftTimeout):
// flushDrafts's own ctx deadline bounds it instead.
func TestFlushDrafts_BoundedByDeadlineWhenASaveIsInFlight(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	api := &fakeDraftAPI{}
	api.create = func(w slackclient.DraftWrite) (slackclient.Draft, error) {
		close(entered)
		<-release
		return attached("Dr1", w.ChannelID, w.ThreadTS, "1.0"), nil
	}
	eng := newDraftEngine(api, "T1")
	router := newWorkspaceRouter()
	router.Add(&WorkspaceContext{TeamID: "T1", Drafts: eng})

	done := make(chan struct{})
	go func() {
		_, _ = eng.Save(context.Background(), draftDest{channelID: "C1"}, "in flight", time.Time{}, nil)
		close(done)
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	flushDrafts(ctx, router, []core.DraftChange{
		{Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "typed at quit", Edited: time.UnixMilli(7)},
	})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("flushDrafts took %v, want bounded by its ctx deadline, not the in-flight save", elapsed)
	}

	close(release)
	<-done
}
