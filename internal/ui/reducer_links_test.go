package ui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

func linkTestApp(t *testing.T) (*App, *string) {
	t.Helper()
	// withSize(0, 0) preserves NewApp's unsized state: this fixture never
	// renders, and the original builder set no dimensions.
	app := newTestApp(t, withSize(0, 0), withActiveTeam("T1"))
	app.workspaceDomains["T1"] = "myteam"
	var opened string
	app.browserOpener = func(url string) tea.Cmd {
		opened = url
		return nil
	}
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		if channelID == "C054JFCBN69" {
			return "general", "channel", true
		}
		return "", "", false
	})
	return app, &opened
}

func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	var out []tea.Msg
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			out = append(out, drainCmd(c)...)
		}
		return out
	}
	if msg != nil {
		out = append(out, msg)
	}
	return out
}

func TestOpenLink_NonSlackURL_OpensBrowser(t *testing.T) {
	app, opened := linkTestApp(t)
	_, cmd := app.Update(OpenLinkMsg{URL: "https://github.com/foo/bar"})
	drainCmd(cmd)
	if *opened != "https://github.com/foo/bar" {
		t.Errorf("browser opened %q", *opened)
	}
}

func TestOpenLink_ForeignWorkspace_OpensBrowser(t *testing.T) {
	app, opened := linkTestApp(t)
	url := "https://otherteam.slack.com/archives/C054JFCBN69/p1779284733270139"
	_, cmd := app.Update(OpenLinkMsg{URL: url})
	drainCmd(cmd)
	if *opened != url {
		t.Errorf("browser opened %q, want %q", *opened, url)
	}
}

func TestOpenLink_UnknownChannel_OpensBrowser(t *testing.T) {
	app, opened := linkTestApp(t)
	url := "https://myteam.slack.com/archives/CUNKNOWN1/p1779284733270139"
	_, cmd := app.Update(OpenLinkMsg{URL: url})
	drainCmd(cmd)
	if *opened != url {
		t.Errorf("browser opened %q, want %q", *opened, url)
	}
}

func TestOpenLink_OtherChannel_DispatchesChannelSelected(t *testing.T) {
	app, opened := linkTestApp(t)
	app.activeChannelID = "CELSEWHERE"
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
	msgs := drainCmd(cmd)
	var sel *ChannelSelectedMsg
	for _, m := range msgs {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			sel = &cs
		}
	}
	if sel == nil {
		t.Fatalf("no ChannelSelectedMsg in %#v", msgs)
	}
	if sel.ID != "C054JFCBN69" || sel.Name != "general" || sel.Type != "channel" {
		t.Errorf("ChannelSelectedMsg = %+v", sel)
	}
	if app.pendingLinkNav == nil || app.pendingLinkNav.messageTS != "1779284733.270139" {
		t.Errorf("pendingLinkNav = %+v", app.pendingLinkNav)
	}
	if *opened != "" {
		t.Errorf("browser should not open, got %q", *opened)
	}
}

func TestOpenLink_ActiveChannel_SelectsMessage(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1779284733.270139", Text: "target"},
		{TS: "1779284734.000000", Text: "newer"},
	})
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
	drainCmd(cmd)
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "1779284733.270139" {
		t.Errorf("selected = %+v ok=%v", sel, ok)
	}
	if app.pendingLinkNav != nil {
		t.Errorf("pendingLinkNav not cleared: %+v", app.pendingLinkNav)
	}
}

func TestOpenLink_ActiveChannel_TSNotLoaded_FetchesAround(t *testing.T) {
	app, _ := linkTestApp(t)
	var fetchedChannel, fetchedTS string
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
		fetchedChannel, fetchedTS = string(channelID), string(ts)
		return nil
	})
	app.activeChannelID = "C054JFCBN69"
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1779284734.000000", Text: "only newer"},
	})
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
	drainCmd(cmd)
	if fetchedChannel != "C054JFCBN69" || fetchedTS != "1779284733.270139" {
		t.Errorf("FetchAround not dispatched: ch=%q ts=%q", fetchedChannel, fetchedTS)
	}
	if app.pendingLinkNav != nil {
		t.Errorf("pendingLinkNav not cleared: %+v", app.pendingLinkNav)
	}
}

func TestOpenLink_ThreadPermalink_OpensThread(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	var fetchedChannel, fetchedThread string
	app.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
		fetchedChannel, fetchedThread = string(channelID), string(threadTS)
		return nil
	})
	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100"})
	drainCmd(cmd)
	if !app.threadVisible {
		t.Fatal("thread panel not visible")
	}
	if got := app.threadPanel.ThreadTS(); got != "1779284700.000100" {
		t.Errorf("ThreadTS = %q", got)
	}
	if fetchedChannel != "C054JFCBN69" || fetchedThread != "1779284700.000100" {
		t.Errorf("fetch = (%q, %q)", fetchedChannel, fetchedThread)
	}
}

func TestMessagesLoaded_CompletesPendingNav(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	app.pendingLinkNav = &pendingLinkNav{
		channelID: "C054JFCBN69",
		messageTS: "1779284733.270139",
	}
	_, cmd := app.Update(MessagesLoadedMsg{
		ChannelID: "C054JFCBN69",
		Messages: []messages.MessageItem{
			{TS: "1779284733.270139", Text: "target"},
			{TS: "1779284734.000000", Text: "newer"},
		},
	})
	drainCmd(cmd)
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "1779284733.270139" {
		t.Errorf("selected = %+v ok=%v", sel, ok)
	}
	if app.pendingLinkNav != nil {
		t.Errorf("pendingLinkNav not cleared: %+v", app.pendingLinkNav)
	}
}

func TestOpenLink_OtherChannel_FreshCacheMissingTS_FetchesAround(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "CELSEWHERE"
	// Wire C054JFCBN69 as a tier-1 "fresh" channel (synced just now, so
	// reduceChannelSelected renders cache and fires NO fetch) whose
	// cached buffer does NOT contain the permalink's target ts.
	app.setChannelCacheReaderForTest(func(channelID ids.ChannelID) []messages.MessageItem {
		if channelID == "C054JFCBN69" {
			return []messages.MessageItem{{TS: "1779284734.000000", Text: "newer only"}}
		}
		return nil
	})
	app.setChannelSyncedAtReaderForTest(func(channelID ids.ChannelID) int64 {
		return time.Now().Unix()
	})
	var fetchedChannel, fetchedTS string
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
		fetchedChannel, fetchedTS = string(channelID), string(ts)
		return nil
	})

	_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
	// routeLink dispatched a ChannelSelectedMsg; feed it back through Update
	// (as the real program loop would) so the tier-1 fresh-cache path
	// completes the pending nav authoritatively.
	for _, m := range drainCmd(cmd) {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			_, c2 := app.Update(cs)
			drainCmd(c2)
		}
	}
	if fetchedChannel != "C054JFCBN69" || fetchedTS != "1779284733.270139" {
		t.Errorf("FetchAround not dispatched on tier-1 fresh path: ch=%q ts=%q", fetchedChannel, fetchedTS)
	}
	if app.pendingLinkNav != nil {
		t.Errorf("pendingLinkNav leaked on tier-1 fresh path: %+v", app.pendingLinkNav)
	}
}

func TestChannelSelected_DifferentChannel_DropsPendingNav(t *testing.T) {
	app, _ := linkTestApp(t)
	app.pendingLinkNav = &pendingLinkNav{channelID: "C054JFCBN69", messageTS: "1.0"}
	_, cmd := app.Update(ChannelSelectedMsg{ID: "COTHER", Name: "other", Type: "channel"})
	drainCmd(cmd)
	if app.pendingLinkNav != nil {
		t.Errorf("pendingLinkNav should be dropped on unrelated navigation: %+v", app.pendingLinkNav)
	}
}

func TestMessagesAroundLoaded_ReplacesBufferAndSelects(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.Update(MessagesAroundLoadedMsg{
		ChannelID: "C1",
		TargetTS:  "1700000004.000000",
		Messages: []messages.MessageItem{
			{TS: "1700000003.000000", Text: "a"},
			{TS: "1700000004.000000", Text: "b"},
			{TS: "1700000005.000000", Text: "c"},
		},
	})
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.TS != "1700000004.000000" {
		t.Fatalf("selected %v ok=%v, want target ts", sel.TS, ok)
	}
}

// A failed jump must be non-destructive: if the fetched window doesn't
// contain the target, the current buffer (and position) stays intact —
// per the spec's error table — and the user just gets a toast.
func TestMessagesAroundLoaded_TargetMissingKeepsBuffer(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", Text: "keep"}})
	_, cmd := app.Update(MessagesAroundLoadedMsg{
		ChannelID: "C1",
		TargetTS:  "9.0",
		Messages:  []messages.MessageItem{{TS: "2.0", Text: "window"}},
	})
	sel, ok := app.messagepane.SelectedMessage()
	if !ok || sel.Text != "keep" {
		t.Fatalf("buffer replaced on failed jump: sel=%+v ok=%v", sel, ok)
	}
	var toast string
	for _, m := range drainCmd(cmd) {
		if tm, ok := m.(ToastMsg); ok {
			toast = tm.Text
		}
	}
	if toast != "Message not found in loaded history" {
		t.Fatalf("toast = %q", toast)
	}
}

func TestMessagesAroundLoaded_ErrorToasts(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	_, cmd := app.Update(MessagesAroundLoadedMsg{ChannelID: "C1", TargetTS: "1", Err: errors.New("boom")})
	msgs := drainCmd(cmd)
	found := false
	for _, m := range msgs {
		if _, ok := m.(ToastMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("expected ToastMsg on fetch failure")
	}
}

func TestMessagesAroundLoaded_StaleChannelDropped(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C2"
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", Text: "keep"}})
	app.Update(MessagesAroundLoadedMsg{
		ChannelID: "C1",
		TargetTS:  "2.0",
		Messages:  []messages.MessageItem{{TS: "2.0", Text: "stale"}},
	})
	sel, _ := app.messagepane.SelectedMessage()
	if sel.Text != "keep" {
		t.Fatal("stale MessagesAroundLoadedMsg replaced active channel buffer")
	}
}

// Permalink upgrade: target outside the buffer now triggers FetchAround
// instead of the "older than loaded history" toast.
func TestCompletePendingNav_OffBufferTriggersFetchAround(t *testing.T) {
	app, _ := linkTestApp(t)
	var fetchedChannel, fetchedTS string
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
		fetchedChannel, fetchedTS = string(channelID), string(ts)
		return nil
	})
	app.activeChannelID = "C054JFCBN69"
	app.pendingLinkNav = &pendingLinkNav{channelID: "C054JFCBN69", messageTS: "1700000001.000000"}

	_, cmd := app.Update(MessagesLoadedMsg{ChannelID: "C054JFCBN69", Messages: []messages.MessageItem{{TS: "1700000099.000000"}}})
	drainCmd(cmd)

	if fetchedChannel != "C054JFCBN69" || fetchedTS != "1700000001.000000" {
		t.Fatalf("FetchAround not dispatched: ch=%q ts=%q", fetchedChannel, fetchedTS)
	}
	if app.pendingLinkNav != nil {
		t.Fatal("pendingLinkNav not cleared")
	}
}
