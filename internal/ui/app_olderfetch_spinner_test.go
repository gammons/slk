// internal/ui/app_olderfetch_spinner_test.go
package ui

import (
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
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
	app.bootstrap.loading = false
	delete(app.fetchingOlder, "C1")

	// Two messages with selection at index 0 -> AtTop() == true.
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "first"},
		{TS: "2.0", UserName: "bob", Text: "second"},
	})

	called := false
	app.setOlderMessagesFetcherForTest(func(channelID ids.ChannelID, oldestTS ids.MessageTS) core.Msg {
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
	if !app.fetchingOlder["C1"] {
		t.Fatal("expected fetchingOlder[C1]=true after dispatch")
	}
}

// PageUp / wheel-up at the top of the messages pane (viewport scrolled all
// the way up, regardless of where selection sits) must also kick off an
// older-history backfill -- otherwise users can never load history without
// also moving selection. Defends the issue-#23 "scroll past long messages"
// fix path through scrollFocusedPanel.
func TestScrollFocusedPanel_BackfillAtViewportTop(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.view = ViewChannels
	app.bootstrap.loading = false
	delete(app.fetchingOlder, "C1")
	app.layout.msgHeight = 20

	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "first"},
		{TS: "2.0", UserName: "bob", Text: "second"},
	})

	called := false
	app.setOlderMessagesFetcherForTest(func(channelID ids.ChannelID, oldestTS ids.MessageTS) core.Msg {
		called = true
		return nil
	})

	// PageUp -> scrollFocusedPanel(-pageSize). With only 2 messages the
	// viewport is already at the top, so this should immediately trigger
	// the backfill path via ViewportAtTop().
	cmd := app.scrollFocusedPanel(-app.pageSize())
	if cmd == nil {
		t.Fatal("expected non-nil cmd from PageUp at top of viewport")
	}
	got := drainBatch(cmd)
	var sawSpinner bool
	for _, m := range got {
		if _, ok := m.(SpinnerTickMsg); ok {
			sawSpinner = true
		}
	}
	if !sawSpinner {
		t.Fatalf("expected SpinnerTickMsg from PageUp backfill, got %#v", got)
	}
	if !called {
		t.Fatal("expected fetcher cmd to have been dispatched from PageUp")
	}
	if !app.fetchingOlder["C1"] {
		t.Fatal("expected fetchingOlder[C1]=true after PageUp dispatch")
	}
}
