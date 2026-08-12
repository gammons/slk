package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// Pressing `a` from a channel dispatches ChannelSelectedMsg for the next
// unread channel, carrying its ID/Name/Type (Type drives the message-pane
// glyph), and moves the sidebar cursor onto it.
func TestJumpToUnread_EmitsChannelSelected(t *testing.T) {
	a := NewApp()
	a.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "one", Type: "channel", Section: "Eng"},
		{ID: "C2", Name: "two", Type: "dm", Section: "Eng"},
	})
	a.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C2": {HasUnread: true}}
	})
	a.activeChannelID = "C1"

	cmd := a.handleNormalMode(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("`a` should emit a command when an unread channel exists")
	}
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	if sel.ID != "C2" || sel.Name != "two" || sel.Type != "dm" {
		t.Fatalf("wrong target: %+v", sel)
	}
	if it, ok := a.sidebar.SelectedItem(); !ok || it.ID != "C2" {
		t.Fatalf("sidebar cursor should be on C2, got %+v ok=%v", it, ok)
	}
}

// `A` walks backward.
func TestJumpToPrevUnread_EmitsChannelSelected(t *testing.T) {
	a := NewApp()
	a.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "one", Type: "channel", Section: "Eng"},
		{ID: "C2", Name: "two", Type: "channel", Section: "Eng"},
		{ID: "C3", Name: "three", Type: "channel", Section: "Eng"},
	})
	a.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true}, "C3": {HasUnread: true}}
	})
	a.activeChannelID = "C3"

	cmd := a.handleNormalMode(tea.KeyPressMsg{Code: 'A', Text: "A"})
	if cmd == nil {
		t.Fatal("`A` should emit a command")
	}
	sel, ok := cmd().(ChannelSelectedMsg)
	if !ok || sel.ID != "C1" {
		t.Fatalf("prev unread from C3 want C1, got %+v ok=%v", sel, ok)
	}
}

// With nothing else unread, `a` shows a status toast and dispatches no
// ChannelSelectedMsg.
func TestJumpToUnread_NoUnread_Toasts(t *testing.T) {
	a := NewApp()
	a.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "one", Type: "channel", Section: "Eng"},
	})
	a.SetReadStateReader(func() map[string]cache.ReadState { return map[string]cache.ReadState{} })
	a.activeChannelID = "C1"

	// jumpToUnread returns the toast's clear-tick cmd on the empty path
	// (not a ChannelSelectedMsg producer); don't invoke it -- it's a 2s
	// timer. Asserting the toast text is enough, since the select and
	// toast paths are mutually exclusive (early return on !ok).
	a.handleNormalMode(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !strings.Contains(a.statusbar.View(80), "No other unread channels") {
		t.Fatalf("expected 'No other unread channels' toast; got %q", a.statusbar.View(80))
	}
	// The sidebar cursor must not have moved onto a phantom target.
	if it, ok := a.sidebar.SelectedItem(); ok && it.ID != "C1" {
		t.Fatalf("sidebar selection should be unchanged, got %+v", it)
	}
}
