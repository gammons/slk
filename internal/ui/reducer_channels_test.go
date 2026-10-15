package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// Anchor validation: an in-flight FetchOlder keyed to the OLD buffer's
// OldestTS must be dropped when it lands after FetchAround replaced the
// buffer (otherwise the prepend splices an unrelated older block onto
// the new window, producing an out-of-order/duplicated buffer).
func TestOlderMessagesLoaded_StaleAnchorDropped(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	// Seed the original buffer; a FetchOlder is in flight keyed to its
	// oldest ts.
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000010.000000", Text: "old-buffer oldest"},
		{TS: "1700000011.000000", Text: "old-buffer newer"},
	})
	app.fetchingOlder["C1"] = true
	app.messagepane.SetLoading(true)

	// Jump-to-message replaces the buffer mid-flight.
	app.Update(MessagesAroundLoadedMsg{
		ChannelID: "C1",
		TargetTS:  "1700000050.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000049.000000", Text: "window a"},
			{TS: "1700000050.000000", Text: "window b"},
		},
	})

	// The stale FetchOlder result lands, anchored to the OLD oldest.
	app.Update(OlderMessagesLoadedMsg{
		ChannelID: "C1",
		AnchorTS:  "1700000010.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000001.000000", Text: "stale older"},
		},
	})

	if got := app.messagepane.OldestTS(); got != "1700000049.000000" {
		t.Errorf("stale-anchor prepend was applied: OldestTS = %q, want %q",
			got, "1700000049.000000")
	}
	if app.fetchingOlder["C1"] {
		t.Error("fetchingOlder not reset after stale-anchor drop")
	}
	if app.messagepane.IsLoading() {
		t.Error("messagepane loading not cleared after stale-anchor drop")
	}
}

// Matching anchor: the normal backfill path must still prepend.
func TestOlderMessagesLoaded_MatchingAnchorPrepends(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000010.000000", Text: "oldest"},
	})
	app.fetchingOlder["C1"] = true

	app.Update(OlderMessagesLoadedMsg{
		ChannelID: "C1",
		AnchorTS:  "1700000010.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000001.000000", Text: "older"},
		},
	})

	if got := app.messagepane.OldestTS(); got != "1700000001.000000" {
		t.Errorf("prepend not applied: OldestTS = %q, want %q",
			got, "1700000001.000000")
	}
	if app.fetchingOlder["C1"] {
		t.Error("fetchingOlder not reset after successful prepend")
	}
}

// Stale-channel drop must still reset fetchingOlder, otherwise
// scroll-backfill is permanently disabled after navigating away
// mid-fetch.
func TestOlderMessagesLoaded_StaleChannelResetsFetchingOlder(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C2"
	app.fetchingOlder["C1"] = true

	app.Update(OlderMessagesLoadedMsg{
		ChannelID: "C1",
		AnchorTS:  "1700000010.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000001.000000", Text: "for old channel"},
		},
	})

	if app.fetchingOlder["C1"] {
		t.Error("fetchingOlder not reset on stale-channel drop")
	}
}

// SelectByTS miss: when the fetched window doesn't contain TargetTS,
// surface a toast instead of silently leaving the selection wherever
// it was.
func TestMessagesAroundLoaded_TargetMissingToasts(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	_, cmd := app.Update(MessagesAroundLoadedMsg{
		ChannelID: "C1",
		TargetTS:  "1700000099.000000", // not in Messages
		Messages: []messages.MessageItem{
			{TS: "1700000001.000000", Text: "a"},
			{TS: "1700000002.000000", Text: "b"},
		},
	})
	found := false
	for _, m := range drainCmd(cmd) {
		if _, ok := m.(ToastMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("expected ToastMsg when TargetTS is missing from the loaded window")
	}
}

// Selecting an app/bot DM fires a MessagingCapability probe.
func TestChannelSelected_ProbesMessagingCapabilityForAppDMs(t *testing.T) {
	app := NewApp()
	var probed ids.ChannelID
	setChannelFuncsForTest(app, core.ChannelServiceFuncs{
		MessagingCapability: func(channelID ids.ChannelID) core.Cmd {
			probed = channelID
			return nil
		},
	})

	app.Update(ChannelSelectedMsg{ID: "D1", Name: "some-bot", Type: "app"})

	if probed != "D1" {
		t.Errorf("MessagingCapability probed channel = %q, want %q", probed, "D1")
	}
}

func TestChannelSelected_DoesNotProbeNonAppChannels(t *testing.T) {
	app := NewApp()
	var probed []ids.ChannelID
	setChannelFuncsForTest(app, core.ChannelServiceFuncs{
		MessagingCapability: func(channelID ids.ChannelID) core.Cmd {
			probed = append(probed, channelID)
			return nil
		},
	})

	for _, typ := range []string{"group_dm", "private", "channel"} {
		app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: typ})
	}
	if len(probed) != 0 {
		t.Errorf("MessagingCapability probed a non-app channel type: %v", probed)
	}
}

// A stale composeDisabled must not leak into the next channel.
func TestChannelSelected_ResetsComposeDisabled(t *testing.T) {
	app := NewApp()
	app.composeDisabled = true

	app.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})

	if app.composeDisabled {
		t.Error("composeDisabled not reset to false on channel switch")
	}
}

func TestChannelMessagingCapability_DisablesComposeForActiveChannel(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "D1"

	app.Update(ChannelMessagingCapabilityMsg{ChannelID: "D1", CanSend: false})

	if !app.composeDisabled {
		t.Error("composeDisabled not set for a CanSend:false result on the active channel")
	}
}

// The probe lands after the user already entered insert mode: the
// composer must be left, and Enter must not send.
func TestChannelMessagingCapability_LandsInInsertMode(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "D1"
	app.SetMode(ModeInsert)
	app.focusedPanel = PanelMessages
	app.compose.Focus()

	app.Update(ChannelMessagingCapabilityMsg{ChannelID: "D1", CanSend: false})

	if app.mode != ModeNormal {
		t.Fatalf("mode = %v, want ModeNormal after an unsendable probe lands in insert mode", app.mode)
	}

	// Backstop: even forced back into insert mode, Enter must not send.
	app.SetMode(ModeInsert)
	app.compose.SetValue("hello")
	_, cmd := app.Update(keyCode(tea.KeyEnter))
	for _, m := range drainCmd(cmd) {
		if _, ok := m.(SendMessageMsg); ok {
			t.Fatal("Enter sent a message to an app DM that cannot receive messages")
		}
	}
}

func TestChannelMessagingCapability_IgnoresStaleChannel(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "D2"

	app.Update(ChannelMessagingCapabilityMsg{ChannelID: "D1", CanSend: false})

	if app.composeDisabled {
		t.Error("composeDisabled set from a result for a channel that is no longer active")
	}
}

func TestChannelMessagingCapability_CanSendTrueLeavesComposeEnabled(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "D1"
	app.composeDisabled = true // disabled by an earlier probe

	app.Update(ChannelMessagingCapabilityMsg{ChannelID: "D1", CanSend: true})

	if app.composeDisabled {
		t.Error("composeDisabled left true after a CanSend:true result for the active channel")
	}
}
