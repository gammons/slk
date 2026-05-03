// internal/ui/app_olderfetch_spinner_test.go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui/messages"
)

// Regression: when the user scrolls to the top of an already-loaded
// channel and triggers an older-messages backfill, the loading-spinner
// glyph must keep animating. Previously, the call sites flipped
// messagepane.SetLoading(true) without dispatching a SpinnerTickMsg.
// If a.loading was already false (workspace fully loaded), no
// self-perpetuating tick was alive, so the glyph froze on its last
// frame for the entire fetch.
func TestHandleUp_BackfillEmitsSpinnerTick(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.view = ViewChannels
	app.loading = false
	app.fetchingOlder = false

	// Two messages with selection at index 0 -> AtTop() == true.
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "first"},
		{TS: "2.0", UserName: "bob", Text: "second"},
	})

	called := false
	app.SetOlderMessagesFetcher(func(channelID, oldestTS string) tea.Msg {
		called = true
		return nil
	})

	cmd := app.handleUp()
	if cmd == nil {
		t.Fatal("expected non-nil cmd from handleUp at top of channel")
	}

	got := drainBatch(cmd)
	var sawSpinner bool
	for _, m := range got {
		if _, ok := m.(SpinnerTickMsg); ok {
			sawSpinner = true
		}
	}
	if !sawSpinner {
		t.Fatalf("expected SpinnerTickMsg in dispatched cmds, got %#v", got)
	}
	if !called {
		t.Fatal("expected fetcher cmd to also have been dispatched")
	}
	if !app.fetchingOlder {
		t.Fatal("expected fetchingOlder=true after dispatch")
	}
}
