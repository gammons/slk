package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// makeBenchApp builds a populated App approximating a real user session: a
// sidebar with 100 channels, a message pane with 200 messages, a workspace
// rail with 3 workspaces, in INSERT mode with focus on the messages panel
// (the typical typing-in-compose state).
func makeBenchApp() *App {
	a := NewApp()
	_, _ = a.Update(tea.WindowSizeMsg{Width: 200, Height: 50})

	channels := make([]sidebar.ChannelItem, 100)
	for i := range channels {
		channels[i] = sidebar.ChannelItem{
			ID:          fmt.Sprintf("C%d", i),
			Name:        fmt.Sprintf("channel-%d", i),
			Type:        "channel",
			UnreadCount: i % 5,
		}
	}
	a.sidebar.SetItems(channels)

	msgs := make([]messages.MessageItem, 200)
	for i := range msgs {
		msgs[i] = messages.MessageItem{
			TS:        fmt.Sprintf("%d.0", 1700000000+i),
			UserName:  "alice",
			UserID:    "U1",
			Text:      "Hello world this is a moderately long message with **bold** and _italic_ formatting.",
			Timestamp: "10:30 AM",
		}
	}
	a.messagepane.SetMessages(msgs)
	a.activeChannelID = "C1"
	a.activeTeamID = "T1"

	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	_, _ = a.compose.Focus(), a.compose.Focus() // ensure focused

	// Prime caches with one full render.
	_ = a.View()
	return a
}

// BenchmarkAppViewCompose measures the cost of one full top-level App.View()
// call after the user types a single character into the compose box. This
// is the steady-state user-typing cost: keystroke -> Update -> View.
func BenchmarkAppViewCompose(b *testing.B) {
	a := makeBenchApp()

	keyMsg := tea.KeyPressMsg{Code: 'a', Text: "a"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = a.Update(keyMsg)
		_ = a.View()
	}
}

// BenchmarkAppViewIdle measures the cost of one App.View() call when nothing
// at all changed since the last render. With ideal caching this should be
// O(1) -- if it isn't, we're doing avoidable work.
func BenchmarkAppViewIdle(b *testing.B) {
	a := makeBenchApp()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.View()
	}
}

// TestComposeKeystrokeKeepsSidePanelsCached verifies that typing a single
// character into the compose box does NOT bump the version counters of the
// sidebar / messages / workspace rail panels. Without this guarantee, the
// App-level panel cache would miss on every keystroke and rebuild work that
// the user can't see changing.
func TestComposeKeystrokeKeepsSidePanelsCached(t *testing.T) {
	a := makeBenchApp()
	_ = a.View() // prime caches

	sbBefore := a.sidebar.Version()
	msgBefore := a.messagepane.Version()
	railBefore := a.workspaceRail.Version()
	statusBefore := a.statusbar.Version()
	threadBefore := a.threadPanel.Version()

	keyMsg := tea.KeyPressMsg{Code: 'a', Text: "a"}
	_, _ = a.Update(keyMsg)
	_ = a.View()

	if v := a.sidebar.Version(); v != sbBefore {
		t.Errorf("sidebar.Version bumped on compose keystroke: before=%d after=%d", sbBefore, v)
	}
	if v := a.messagepane.Version(); v != msgBefore {
		t.Errorf("messagepane.Version bumped on compose keystroke: before=%d after=%d", msgBefore, v)
	}
	if v := a.workspaceRail.Version(); v != railBefore {
		t.Errorf("workspaceRail.Version bumped on compose keystroke: before=%d after=%d", railBefore, v)
	}
	if v := a.statusbar.Version(); v != statusBefore {
		t.Errorf("statusbar.Version bumped on compose keystroke: before=%d after=%d", statusBefore, v)
	}
	if v := a.threadPanel.Version(); v != threadBefore {
		t.Errorf("threadPanel.Version bumped on compose keystroke: before=%d after=%d", threadBefore, v)
	}

	// And compose IS expected to bump.
	if a.compose.Version() == 0 {
		t.Error("compose.Version did not bump on keystroke")
	}
}
