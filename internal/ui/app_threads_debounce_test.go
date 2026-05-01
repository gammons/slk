package ui

import (
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
)

// newTestAppWithThreadsView is the threads-view analogue of
// newTestAppWithMessages (app_selection_test.go:13). Wires summaries into an
// App, activates ViewThreads, and returns the App for the caller to drive
// openSelectedThreadCmd / Update directly.
func newTestAppWithThreadsView(t *testing.T, summaries []cache.ThreadSummary) *App {
	t.Helper()
	a := NewApp()
	a.width = 120
	a.height = 30
	a.threadsView.SetSummaries(summaries)
	a.view = ViewThreads
	_ = a.View() // populate layout offsets and caches
	return a
}

// TestThreadsViewDebouncesNetworkFetchOnRapidJK guards Task 3:
// holding j down across multiple summaries must collapse to exactly one
// network fetch (against the row the user finally lands on), not one per row.
func TestThreadsViewDebouncesNetworkFetchOnRapidJK(t *testing.T) {
	var fetchCount int32
	var fetchedTS []string
	a := newTestAppWithThreadsView(t, []cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0", ParentText: "p1", ChannelName: "g1"},
		{ChannelID: "C2", ThreadTS: "2.0", ParentText: "p2", ChannelName: "g2"},
		{ChannelID: "C3", ThreadTS: "3.0", ParentText: "p3", ChannelName: "g3"},
	})
	a.threadFetcher = func(channelID, threadTS string) tea.Msg {
		atomic.AddInt32(&fetchCount, 1)
		fetchedTS = append(fetchedTS, threadTS)
		return ThreadRepliesLoadedMsg{ThreadTS: threadTS}
	}

	// Simulate held-j: three openSelectedThreadCmd(true) invocations
	// interleaved with MoveDown. Each returned Cmd is a tea.Tick scheduled
	// at openThreadDebounceDelay; we don't actually wait for the timer —
	// we synthesize the threadFetchDebounceMsg the tick would emit and feed
	// it into Update. Ticks 1 and 2 carry stale generations and must be
	// dropped; tick 3 must produce the (single) fetcher Cmd.
	var emitted []threadFetchDebounceMsg
	for i := 0; i < 3; i++ {
		cmd := a.openSelectedThreadCmd(true)
		if cmd != nil {
			// We can't directly observe the tick's payload (it's behind
			// tea.Tick), so we synthesize one matching the App's current
			// (lastOpened*, pendingThreadFetchGen) tuple — the exact
			// payload openSelectedThreadCmd would have scheduled.
			emitted = append(emitted, threadFetchDebounceMsg{
				channelID: a.lastOpenedChannelID,
				threadTS:  a.lastOpenedThreadTS,
				gen:       a.pendingThreadFetchGen,
			})
		}
		a.threadsView.MoveDown()
	}
	if len(emitted) < 2 {
		t.Fatalf("expected 3 debounce ticks scheduled, got %d", len(emitted))
	}
	// Replay the first two ticks (stale generations) and confirm Update
	// drops them.
	for i := 0; i < len(emitted)-1; i++ {
		stale := emitted[i]
		_, cmd := a.Update(stale)
		if cmd != nil {
			t.Errorf("stale debounce tick %d should be dropped; got cmd %v", i, cmd)
		}
	}
	// Replay the last (current) tick and run its returned fetcher Cmd.
	last := emitted[len(emitted)-1]
	_, cmd := a.Update(last)
	if cmd == nil {
		t.Fatalf("latest debounce tick should return the fetcher Cmd")
	}
	for _, m := range drainBatch(cmd) {
		_, _ = a.Update(m)
	}
	if got := atomic.LoadInt32(&fetchCount); got != 1 {
		t.Errorf("expected exactly 1 network fetch after rapid j/k, got %d (TS=%v)", got, fetchedTS)
	}
}

// TestOpenSelectedThread_NonDebouncedPathFiresImmediately guards Task 3's
// Option B behavior: only j/k key handlers debounce; other call sites
// (activation, list reload, G jump) must return the fetcher Cmd directly so
// thread content lands without artificial delay.
func TestOpenSelectedThread_NonDebouncedPathFiresImmediately(t *testing.T) {
	var fetched int32
	a := newTestAppWithThreadsView(t, []cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "1.0", ParentText: "p1", ChannelName: "g1"},
	})
	a.threadFetcher = func(channelID, threadTS string) tea.Msg {
		atomic.AddInt32(&fetched, 1)
		return ThreadRepliesLoadedMsg{ThreadTS: threadTS}
	}
	cmd := a.openSelectedThreadCmd(false)
	if cmd == nil {
		t.Fatalf("non-debounced openSelectedThreadCmd should return a Cmd, got nil")
	}
	for _, m := range drainBatch(cmd) {
		_, _ = a.Update(m)
	}
	if got := atomic.LoadInt32(&fetched); got != 1 {
		t.Errorf("non-debounced path should fire 1 fetch immediately; got %d", got)
	}
}
