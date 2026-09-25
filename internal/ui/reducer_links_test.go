package ui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
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
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
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
	app.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) core.Msg {
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

func TestOpenLink_ThreadPermalink_SelectsExactTarget(t *testing.T) {
	const parentTS = "1779284700.000100"
	const targetTS = "1779284733.270139"
	parent := messages.MessageItem{TS: parentTS, Text: "parent"}
	target := messages.MessageItem{TS: targetTS, Text: "older reply"}
	newer := messages.MessageItem{TS: "1779284734.000000", Text: "newer reply"}
	for _, tc := range []struct {
		name, permalinkTS, wantTS string
		cached                    []messages.MessageItem
	}{
		{"older reply", "1779284733270139", targetTS, nil},
		{"cache contains target", "1779284733270139", targetTS, []messages.MessageItem{parent, target, newer}},
		{"cache misses target", "1779284733270139", targetTS, []messages.MessageItem{parent, newer}},
		{"parent", "1779284700000100", parentTS, []messages.MessageItem{parent, target, newer}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := linkTestApp(t)
			app.activeChannelID = "C054JFCBN69"
			app.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
				CacheRead: func(ids.ChannelID, ids.ThreadTS) []messages.MessageItem { return tc.cached },
				Fetch: func(ids.ChannelID, ids.ThreadTS) core.Msg {
					return ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: []messages.MessageItem{target, newer}}
				},
			}))
			_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p" + tc.permalinkTS + "?thread_ts=" + parentTS})
			loads := drainCmd(cmd)
			for i, m := range loads {
				app.Update(m)
				if i == len(loads)-1 || tc.name == "cache contains target" || tc.name == "parent" {
					if sel := app.threadPanel.SelectedReply(); sel == nil || sel.TS != tc.wantTS {
						t.Fatalf("load %d selected %+v, want %s", i, sel, tc.wantTS)
					}
				}
				if i < len(loads)-1 && app.pendingLinkNav == nil {
					t.Fatal("cached load cleared target before authoritative reload")
				}
			}
			if app.pendingLinkNav != nil {
				t.Fatal("authoritative load did not clear pending navigation")
			}
			// A subsequent ordinary refresh must not retain permalink targeting.
			app.Update(ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: []messages.MessageItem{target, newer}})
			if sel := app.threadPanel.SelectedReply(); sel == nil || sel.TS != newer.TS {
				t.Fatalf("ordinary reload selected %+v, want newest", sel)
			}
		})
	}
}

func TestOpenLink_ThreadPermalink_RejectsStaleLoads(t *testing.T) {
	const parentTS = "1779284700.000100"
	target := messages.MessageItem{TS: "1779284733.270139", Text: "target"}
	newer := messages.MessageItem{TS: "1779284734.000000", Text: "keep"}
	for _, scenario := range []string{"other thread", "other channel", "other team", "closed", "new permalink"} {
		t.Run(scenario, func(t *testing.T) {
			app, _ := linkTestApp(t)
			app.activeChannelID = "C054JFCBN69"
			app.setThreadFetcherForTest(func(ids.ChannelID, ids.ThreadTS) core.Msg {
				return ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: []messages.MessageItem{target, newer}}
			})
			_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=" + parentTS})
			loads := drainCmd(cmd)
			channelID, threadTS := app.activeChannelID, parentTS
			switch scenario {
			case "other thread":
				threadTS = "1779284600.000100"
			case "other channel":
				channelID = "COTHER"
				app.activeChannelID = channelID
			case "other team":
				app.activeTeamID = "T2"
			case "closed":
				app.CloseThread()
			case "new permalink":
				app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284734000000?thread_ts=" + parentTS})
			}
			app.threadPanel.SetThread(messages.MessageItem{TS: threadTS}, []messages.MessageItem{newer}, channelID, threadTS)
			pending := app.pendingLinkNav
			for _, m := range loads {
				app.Update(m)
			}
			if got := app.threadPanel.Replies(); len(got) != 1 || got[0].TS != newer.TS {
				t.Fatalf("stale load replaced replies: %+v", got)
			}
			if scenario == "new permalink" {
				if app.pendingLinkNav != pending {
					t.Fatal("stale load cleared newer navigation")
				}
			} else if app.pendingLinkNav != nil {
				t.Fatal("stale navigation not cleared")
			}
		})
	}
}

func TestOpenLink_ThreadPermalink_LoadOrderingAndCompletion(t *testing.T) {
	const parentTS = "1779284700.000100"
	target := messages.MessageItem{TS: "1779284733.270139", Text: "target"}
	newer := messages.MessageItem{TS: "1779284734.000000", Text: "newer"}
	for _, scenario := range []string{"fetch before cache", "nil fetch", "failed fetch", "failure before cache", "missing target", "empty thread"} {
		t.Run(scenario, func(t *testing.T) {
			app, _ := linkTestApp(t)
			app.activeChannelID = "C054JFCBN69"
			app.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
				CacheRead: func(ids.ChannelID, ids.ThreadTS) []messages.MessageItem {
					return []messages.MessageItem{{TS: parentTS, Text: "parent"}, target, newer}
				},
				Fetch: func(ids.ChannelID, ids.ThreadTS) core.Msg {
					switch scenario {
					case "nil fetch":
						return nil
					case "failed fetch", "failure before cache":
						return ThreadRepliesLoadedMsg{ThreadTS: parentTS}
					case "missing target":
						return ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: []messages.MessageItem{newer}}
					case "empty thread":
						return ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: []messages.MessageItem{}}
					}
					return ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: []messages.MessageItem{target}}
				},
			}))
			_, cmd := app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=" + parentTS})
			loads := drainCmd(cmd)
			if len(loads) != 2 {
				t.Fatalf("got %d loads, want cache + fetch", len(loads))
			}
			if cmd := app.completePendingLinkNav(app.activeChannelID, true); cmd != nil {
				t.Fatal("channel reload reopened the pending thread")
			}
			if scenario == "fetch before cache" || scenario == "failure before cache" {
				loads[0], loads[1] = loads[1], loads[0]
			}
			for _, m := range loads {
				app.Update(m)
			}
			wantTS := target.TS
			if scenario == "missing target" {
				wantTS = newer.TS
			} else if scenario == "empty thread" {
				wantTS = parentTS
			}
			if sel := app.threadPanel.SelectedReply(); sel == nil || sel.TS != wantTS {
				t.Fatalf("selected %+v, want %s", sel, wantTS)
			}
			if scenario == "fetch before cache" && len(app.threadPanel.Replies()) != 1 {
				t.Fatal("late cache overwrote authoritative replies")
			}
			if app.pendingLinkNav != nil {
				t.Fatal("completed load left pending target armed")
			}
		})
	}
}

func TestOpenLink_ThreadPermalink_DoesNotTargetOrdinaryLoads(t *testing.T) {
	app, _ := linkTestApp(t)
	app.activeChannelID = "C054JFCBN69"
	const parentTS = "1779284700.000100"
	replies := []messages.MessageItem{{TS: "1779284733.270139"}, {TS: "1779284734.000000"}}
	app.setThreadFetcherForTest(func(ids.ChannelID, ids.ThreadTS) core.Msg {
		return ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: replies}
	})
	app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=" + parentTS})
	// An untagged refresh during the navigation cannot apply its target.
	app.Update(ThreadRepliesLoadedMsg{ThreadTS: parentTS, Replies: replies})
	if sel := app.threadPanel.SelectedReply(); sel == nil || sel.TS != replies[1].TS {
		t.Fatalf("ordinary refresh selected %+v, want newest", sel)
	}
	app.pendingLinkNav = nil
	cmd := app.openThreadPanel(messages.MessageItem{TS: parentTS}, app.activeChannelID, parentTS)
	for _, m := range drainCmd(cmd) {
		app.Update(m)
	}
	if sel := app.threadPanel.SelectedReply(); sel == nil || sel.TS != replies[1].TS {
		t.Fatalf("ordinary thread open selected %+v, want newest", sel)
	}
}

func TestOpenLink_ActiveChannel_RevealsSource(t *testing.T) {
	for _, fromThreads := range []bool{false, true} {
		app, _ := linkTestApp(t)
		app.activeChannelID = "C054JFCBN69"
		app.focusedPanel = PanelThread
		app.threadVisible = true
		if fromThreads {
			app.view = ViewThreads
			app.sidebar.SetThreadsActive(true)
		}
		app.messagepane.SetMessages([]messages.MessageItem{{TS: "1779284733.270139", Text: "source"}})
		app.Update(OpenLinkMsg{URL: "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"})
		if app.view != ViewChannels || app.focusedPanel != PanelMessages {
			t.Fatalf("fromThreads=%v: view=%v focus=%v", fromThreads, app.view, app.focusedPanel)
		}
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
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
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
	setChannelFetchAroundForTest(app, func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
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
