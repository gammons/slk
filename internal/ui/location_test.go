// internal/ui/location_test.go
//
// Tests for the shared Location type and the currentLocation helper
// that snapshots the user's position from the app's state.
package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestCurrentLocation_ChannelTimeline(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000001.000000", Text: "a"},
		{TS: "1700000002.000000", Text: "b"},
	})
	app.messagepane.SelectByIndex(1)

	loc, ok := app.currentLocation()
	if !ok {
		t.Fatal("expected a location with a message selected")
	}
	if loc.TeamID != ids.TeamID("T1") || loc.ChannelID != ids.ChannelID("C1") {
		t.Errorf("location workspace/channel = %+v", loc)
	}
	if loc.MessageTS != ids.MessageTS("1700000002.000000") {
		t.Errorf("MessageTS = %q, want 1700000002.000000", loc.MessageTS)
	}
	if loc.ThreadTS != "" {
		t.Errorf("ThreadTS = %q, want empty for a channel message", loc.ThreadTS)
	}
}

func TestCurrentLocation_ThreadReply(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.focusedPanel = PanelThread
	app.threadPanel.SetThread(
		messages.MessageItem{TS: "1700000100.000000", ThreadTS: "1700000100.000000", Text: "parent"},
		[]messages.MessageItem{
			{TS: "1700000101.000000", ThreadTS: "1700000100.000000", Text: "reply 1"},
			{TS: "1700000102.000000", ThreadTS: "1700000100.000000", Text: "reply 2"},
		},
		"C1", "1700000100.000000")
	app.threadPanel.SelectByIndex(0)

	loc, ok := app.currentLocation()
	if !ok {
		t.Fatal("expected a location with a reply selected")
	}
	if loc.TeamID != ids.TeamID("T1") || loc.ChannelID != ids.ChannelID("C1") {
		t.Errorf("location workspace/channel = %+v", loc)
	}
	if loc.MessageTS != ids.MessageTS("1700000101.000000") {
		t.Errorf("MessageTS = %q, want the selected reply", loc.MessageTS)
	}
	if loc.ThreadTS != ids.ThreadTS("1700000100.000000") {
		t.Errorf("ThreadTS = %q, want the thread parent", loc.ThreadTS)
	}
}

func TestCurrentLocation_ThreadParentSelected(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.focusedPanel = PanelThread
	app.threadPanel.SetThread(
		messages.MessageItem{TS: "1700000100.000000", ThreadTS: "1700000100.000000", Text: "parent"},
		[]messages.MessageItem{{TS: "1700000101.000000", ThreadTS: "1700000100.000000", Text: "reply"}},
		"C1", "1700000100.000000")
	app.threadPanel.MoveUp()

	loc, ok := app.currentLocation()
	if !ok {
		t.Fatal("expected a location with the parent selected")
	}
	if loc.MessageTS != ids.MessageTS("1700000100.000000") || loc.ThreadTS != ids.ThreadTS("1700000100.000000") {
		t.Errorf("location = %+v, want parent ts as both message and thread", loc)
	}
}

func TestCurrentLocation_NoSelection(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.focusedPanel = PanelMessages

	if _, ok := app.currentLocation(); ok {
		t.Fatal("expected ok=false with no selected message")
	}
}

func TestCurrentLocation_EmptyThread(t *testing.T) {
	app := NewApp()
	app.activeTeamID = "T1"
	app.activeChannelID = "C1"
	app.focusedPanel = PanelThread

	if _, ok := app.currentLocation(); ok {
		t.Fatal("expected ok=false with no open thread")
	}
}
