package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// findMessagepaneReactionHit drives a render and walks the messages-
// pane content area looking for any coordinate where
// messagepane.HitTestReaction returns ok. Returns (X, Y) in app-level
// coordinates suitable for tea.MouseClickMsg, plus the emoji that was
// hit. Fails the test when nothing is found within the search window
// — the rendered geometry must include at least one pill.
func findMessagepaneReactionHit(t *testing.T, a *App) (x, y int, emoji string) {
	t.Helper()
	chrome := a.messagepane.ChromeHeight()
	// Pane-local x is bounded by the messages pane width; subtract the
	// 1-col left border on each side. Pane-local y for reaction rows is
	// "small" since pills sit on a line below the body text — scanning
	// up to height-1 covers any reasonable layout.
	maxPaneY := a.height - 2 // minus top border and status bar
	maxPaneX := a.layout.msgEnd - a.layout.sidebarEnd - 2
	for paneY := chrome; paneY < maxPaneY; paneY++ {
		contentY := paneY - chrome
		for paneX := 0; paneX < maxPaneX; paneX++ {
			if _, e, ok := a.messagepane.HitTestReaction(contentY, paneX); ok {
				return a.layout.sidebarEnd + 1 + paneX, paneY + 1, e
			}
		}
	}
	t.Fatal("no reaction hit found in messages pane after render; layout assumption broken")
	return 0, 0, ""
}

// TestApp_ClickOnReactionPillTogglesReaction asserts that clicking a
// reaction pill in the messages pane invokes the registered add
// callback (when the user has not already reacted) with the correct
// (channelID, ts, emoji) triple. The click must NOT begin a
// drag-to-copy selection — pill click takes precedence.
func TestApp_ClickOnReactionPillAddsReaction(t *testing.T) {
	a := NewApp()
	a.width = 120
	a.height = 30
	a.activeChannelID = "C-react"
	a.currentUserID = "U-self"
	a.messagepane.SetMessages([]messages.MessageItem{
		{
			TS:        "1700000001.000000",
			UserID:    "U1",
			UserName:  "alice",
			Text:      "hello",
			Timestamp: "10:30 AM",
			Reactions: []messages.ReactionItem{
				{Emoji: "thumbsup", Count: 1, HasReacted: false},
			},
		},
	})
	// Drive a render so lastReactionHits is populated.
	_ = a.View()

	type call struct {
		channelID string
		ts        string
		emoji     string
	}
	var added []call
	var removed []call
	a.SetReactionService(core.NewReactionService(
		func(channelID ids.ChannelID, ts ids.MessageTS, emoji string) error {
			added = append(added, call{string(channelID), string(ts), emoji})
			return nil
		},
		func(channelID ids.ChannelID, ts ids.MessageTS, emoji string) error {
			removed = append(removed, call{string(channelID), string(ts), emoji})
			return nil
		},
		nil, nil, // no frecent in this test
	))

	x, y, emoji := findMessagepaneReactionHit(t, a)
	if emoji != "thumbsup" {
		t.Fatalf("expected to find :thumbsup: pill, got %q", emoji)
	}
	_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})

	if cmd == nil {
		t.Fatal("expected a tea.Cmd returned for the reaction click; got nil")
	}
	// Drain the command so the registered add fn fires.
	_ = drainBatch(cmd)

	if len(added) != 1 {
		t.Fatalf("expected exactly 1 add call, got %d (removed=%d)", len(added), len(removed))
	}
	got := added[0]
	if got.channelID != "C-react" {
		t.Errorf("add channelID got %q want %q", got.channelID, "C-react")
	}
	if got.ts != "1700000001.000000" {
		t.Errorf("add ts got %q want %q", got.ts, "1700000001.000000")
	}
	if got.emoji != "thumbsup" {
		t.Errorf("add emoji got %q want %q", got.emoji, "thumbsup")
	}
	if len(removed) != 0 {
		t.Errorf("expected no remove calls, got %d", len(removed))
	}

	// The click must NOT have started a drag selection on the
	// messages pane (pill click takes precedence over the
	// click-to-select-message path).
	if a.messagepane.HasSelection() {
		t.Error("pill click must not begin a drag selection")
	}
}

// TestApp_ClickOnAlreadyReactedPillRemovesReaction asserts that
// clicking a pill the current user has already reacted with invokes
// the remove callback rather than the add callback.
func TestApp_ClickOnAlreadyReactedPillRemovesReaction(t *testing.T) {
	a := NewApp()
	a.width = 120
	a.height = 30
	a.activeChannelID = "C-react"
	a.currentUserID = "U-self"
	a.messagepane.SetMessages([]messages.MessageItem{
		{
			TS:        "1700000002.000000",
			UserID:    "U1",
			UserName:  "alice",
			Text:      "hi",
			Timestamp: "10:30 AM",
			Reactions: []messages.ReactionItem{
				{Emoji: "tada", Count: 1, HasReacted: true},
			},
		},
	})
	_ = a.View()

	var addCount, removeCount int
	var lastRemoveEmoji string
	a.SetReactionService(core.NewReactionService(
		func(channelID ids.ChannelID, ts ids.MessageTS, emoji string) error {
			addCount++
			return nil
		},
		func(channelID ids.ChannelID, ts ids.MessageTS, emoji string) error {
			removeCount++
			lastRemoveEmoji = emoji
			return nil
		},
		nil, nil, // no frecent in this test
	))

	x, y, emoji := findMessagepaneReactionHit(t, a)
	if emoji != "tada" {
		t.Fatalf("expected :tada: pill, got %q", emoji)
	}
	_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected a tea.Cmd; got nil")
	}
	_ = drainBatch(cmd)

	if addCount != 0 {
		t.Errorf("expected 0 add calls (pill is already reacted), got %d", addCount)
	}
	if removeCount != 1 {
		t.Fatalf("expected 1 remove call, got %d", removeCount)
	}
	if lastRemoveEmoji != "tada" {
		t.Errorf("remove emoji got %q want %q", lastRemoveEmoji, "tada")
	}
}
