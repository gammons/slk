package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/channelpicker"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
)

func TestChannelMention_ConversationOpenedRefreshesRenderedContent(t *testing.T) {
	a := newTestApp(t, withSize(160, 40), withActiveTeam("T1"))
	a.SetChannels([]sidebar.ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "Alice", Text: "See <#C9>", Timestamp: "1:00 PM"}
	reply := messages.MessageItem{TS: "2.0", UserID: "U1", UserName: "Alice", Text: "Reply in <#C9>", Timestamp: "1:01 PM"}
	a.messagepane.SetMessages([]messages.MessageItem{parent})
	a.sidebar.SelectByID("C1")
	a.threadPanel.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	if before := screenText(a); !strings.Contains(before, "See #unknown") {
		t.Fatalf("precondition: unresolved channel mention not visible:\n%s", before)
	}
	_ = a.threadPanel.View(20, 80)

	a.Update(ConversationOpenedMsg{
		TeamID:     "T1",
		Item:       sidebar.ChannelItem{ID: "C9", Name: "public-news", Type: "channel"},
		FinderItem: core.ChannelFinderItem{ID: "C9", Name: "public-news", Type: "channel", Joined: true},
	})

	if after := screenText(a); !strings.Contains(after, "See #public-news") || strings.Contains(after, "See #unknown") {
		t.Errorf("known public channel still renders as #unknown:\n%s", after)
	}
	if after := ansi.Strip(a.threadPanel.View(20, 80)); !strings.Contains(after, "Reply in #public-news") || strings.Contains(after, "#unknown") {
		t.Errorf("thread channel mentions were not refreshed:\n%s", after)
	}
	selected, ok := a.messagepane.SelectedMessage()
	if a.sidebar.SelectedID() != "C1" || !ok || selected.TS != parent.TS {
		t.Error("opening a conversation moved the sidebar or message selection")
	}
}

func TestChannelMention_ResolvedRefreshesForwardedContent(t *testing.T) {
	a := newTestApp(t, withSize(160, 40), withActiveTeam("T1"))
	parent := messages.MessageItem{
		TS: "1.0", UserID: "U1", UserName: "Alice", Timestamp: "1:00 PM",
		LegacyAttachments: []blocks.LegacyAttachment{{Text: "Parent: see <#C9>"}},
	}
	reply := messages.MessageItem{
		TS: "2.0", UserID: "U1", UserName: "Alice", Timestamp: "1:01 PM",
		LegacyAttachments: []blocks.LegacyAttachment{{Text: "Reply: see <#C9>"}},
	}
	a.messagepane.SetMessages([]messages.MessageItem{parent})
	a.threadPanel.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	if got := screenText(a); !strings.Contains(got, "Parent: see #unknown") {
		t.Fatalf("precondition: missing unresolved forwarded channel mention:\n%s", got)
	}
	before := ansi.Strip(a.threadPanel.View(20, 80))
	for _, label := range []string{"Parent", "Reply"} {
		if !strings.Contains(before, label+": see #unknown") {
			t.Fatalf("precondition: missing unresolved forwarded thread mention:\n%s", before)
		}
	}

	a.Update(ChannelResolvedMsg{TeamID: "T1", ChannelID: "C9", Name: "public-news"})

	if got := screenText(a); !strings.Contains(got, "Parent: see #public-news") || strings.Contains(got, "#unknown") {
		t.Errorf("forwarded channel mention did not refresh:\n%s", got)
	}
	after := ansi.Strip(a.threadPanel.View(20, 80))
	for _, label := range []string{"Parent", "Reply"} {
		if !strings.Contains(after, label+": see #public-news") {
			t.Errorf("forwarded thread mention did not refresh:\n%s", after)
		}
	}
	if strings.Contains(after, "#unknown") {
		t.Errorf("forwarded thread mention remains unresolved:\n%s", after)
	}
}

func TestChannelMention_RefreshReachesEveryConsumer(t *testing.T) {
	for _, opened := range []bool{false, true} {
		name := "resolved"
		if opened {
			name = "conversation-opened"
		}
		t.Run(name, func(t *testing.T) {
			a, w1, w2 := twoWindowApp(t)
			msg := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "Alice", Text: "See <#C9>", Timestamp: "1:00 PM"}
			a.winModels[w1].SetMessages([]messages.MessageItem{msg})
			a.winModels[w2].SetMessages([]messages.MessageItem{msg})
			a.threadPanel.SetThread(msg, []messages.MessageItem{msg}, "C1", msg.TS)
			a.threadsView.SetSummaries([]cache.ThreadSummary{{
				ChannelID: "C1", ChannelName: "general", ThreadTS: "1.0",
				ParentTS: "1.0", ParentUserID: "U1", ParentText: msg.Text, ReplyCount: 1,
			}})
			a.activityView.SetItems([]core.ActivityItem{{
				Key: "k1", Type: "at_user", FeedTS: "1.0", ChannelID: "C1", TS: "1.0", AuthorID: "U1",
			}})
			a.activityView.SetBodies(map[string]core.ActivityMessage{
				core.ActivityMsgKey("C1", "1.0"): {Text: msg.Text, UserID: "U1"},
			})
			views := []struct {
				name string
				view func(int, int) string
			}{
				{"first window", a.winModels[w1].View},
				{"second window", a.winModels[w2].View},
				{"thread", a.threadPanel.View},
				{"Threads list", a.threadsView.View},
				{"Activity", a.activityView.View},
			}
			for _, v := range views {
				if got := ansi.Strip(v.view(20, 100)); !strings.Contains(got, "See #unknown") {
					t.Fatalf("precondition: %s missing unresolved mention:\n%s", v.name, got)
				}
			}
			if opened {
				a.Update(ConversationOpenedMsg{TeamID: a.activeTeamID, Item: sidebar.ChannelItem{ID: "C9", Name: "public-news", Type: "channel"}})
			} else {
				a.Update(ChannelResolvedMsg{TeamID: a.activeTeamID, ChannelID: "C9", Name: "public-news"})
			}
			for _, v := range views {
				if got := ansi.Strip(v.view(20, 100)); !strings.Contains(got, "See #public-news") || strings.Contains(got, "See #unknown") {
					t.Errorf("%s did not refresh its channel mention:\n%s", v.name, got)
				}
			}
		})
	}
}

func TestChannelMention_NonMemberSurvivesMembershipRefresh(t *testing.T) {
	a := newTestApp(t, withSize(160, 40), withActiveTeam("T1"))
	items := []sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "U1", DMUserID: "U1", Type: "dm"},
	}
	a.SetChannels(items)
	a.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", UserName: "Alice", Text: "See <#C9>"}})
	a.Update(ChannelResolvedMsg{TeamID: "T1", ChannelID: "C9", Name: "public-news"})
	for _, refresh := range []struct {
		name string
		run  func()
	}{
		{"SetChannels", func() { a.SetChannels(items) }},
		{"sections", func() { a.Update(SectionsRefreshedMsg{TeamID: "T1", Channels: items}) }},
		{"DM name", func() { a.Update(DMNameResolvedMsg{ChannelID: "D1", DisplayName: "Alice"}) }},
	} {
		t.Run(refresh.name, func(t *testing.T) {
			refresh.run()
			if got := screenText(a); !strings.Contains(got, "See #public-news") {
				t.Errorf("membership refresh lost a learned mention name:\n%s", got)
			}
			if len(a.sidebar.Items()) != 2 {
				t.Fatalf("name resolution changed sidebar membership: %+v", a.sidebar.Items())
			}
			for _, item := range a.sidebar.Items() {
				if item.ID == "C9" {
					t.Error("resolved non-member channel appeared in sidebar")
				}
			}
		})
	}
}

func TestChannelMention_WorkspaceSnapshotsReplaceNames(t *testing.T) {
	for _, ready := range []bool{true, false} {
		name := "switch"
		if ready {
			name = "ready"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp(t, withSize(160, 40), withActiveTeam("T0"))
			a.Update(ChannelResolvedMsg{TeamID: "T0", ChannelID: "COLD", Name: "old-workspace"})
			names := map[string]string{"C9": "public-news", "C1": "stale-sidebar"}
			items := []sidebar.ChannelItem{
				{ID: "C1", Name: "general", Type: "channel"},
				{ID: "D2", Name: "Gone", Type: "dm", DMUserID: "U2"},
			}
			if ready {
				a.Update(WorkspaceReadyMsg{TeamID: "T1", TeamName: "One", InitialActive: true, Channels: items, ChannelNames: names, DeactivatedUsers: map[string]bool{"U2": true}})
			} else {
				a.Update(WorkspaceSwitchedMsg{TeamID: "T1", TeamName: "One", Channels: items, ChannelNames: names, DeactivatedUsers: map[string]bool{"U2": true}})
			}
			for panel, picks := range map[string][]channelpicker.Channel{
				"channel": a.compose.ChannelPicks(),
				"thread":  a.threadCompose.ChannelPicks(),
			} {
				if len(picks) != 1 || picks[0].ID != "C1" || picks[0].Name != "general" {
					t.Errorf("%s composer suggestions = %+v, want only joined channel C1 (general)", panel, picks)
				}
			}
			// Incoming snapshots are copied; neither UI events nor the
			// snapshot's owner may mutate the other side's map.
			names["C9"] = "sender-mutated"
			a.Update(ChannelResolvedMsg{TeamID: "T1", ChannelID: "C1", Name: "delayed-stale"})
			a.Update(ChannelResolvedMsg{TeamID: "T0", ChannelID: "COLD", Name: "late-old"})
			a.Update(ConversationOpenedMsg{TeamID: "T0", Item: sidebar.ChannelItem{ID: "COLD", Name: "late-joined", Type: "channel"}})
			a.Update(WorkspaceReadyMsg{TeamID: "T-background", TeamName: "Background", ChannelNames: map[string]string{"C9": "background"}})
			msg := messages.MessageItem{TS: "1.0", UserName: "Alice", Text: "Joined <#C1>; learned <#C9>; old <#COLD>"}
			a.messagepane.SetMessages([]messages.MessageItem{msg})
			if got := screenText(a); !strings.Contains(got, "Joined #general; learned #public-news; old #unknown") {
				t.Errorf("workspace activation did not isolate snapshot and sidebar names:\n%s", got)
			}
			a.Update(ChannelResolvedMsg{TeamID: "T1", ChannelID: "C9", Name: "renamed"})
			if names["C9"] != "sender-mutated" {
				t.Error("UI name update mutated the incoming snapshot")
			}
			a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Two", ChannelNames: map[string]string{"C9": "other-news"}})
			a.Update(ChannelResolvedMsg{TeamID: "T1", ChannelID: "C1", Name: "late-general"})
			a.messagepane.SetMessages([]messages.MessageItem{msg})
			if got := screenText(a); !strings.Contains(got, "Joined #unknown; learned #other-news; old #unknown") {
				t.Errorf("switch retained names from the previous workspace:\n%s", got)
			}
			a.Update(WorkspaceSwitchedMsg{TeamID: "T3", TeamName: "Three"})
			a.messagepane.SetMessages([]messages.MessageItem{msg})
			if got := ansi.Strip(a.messagepane.View(20, 100)); !strings.Contains(got, "Joined #unknown; learned #unknown; old #unknown") {
				t.Errorf("nil incoming snapshot did not reset the pane's names:\n%s", got)
			}
		})
	}
}

func TestChannelMention_UnchangedResolutionIsNoOp(t *testing.T) {
	a, w1, w2 := twoWindowApp(t)
	a.Update(ChannelResolvedMsg{TeamID: a.activeTeamID, ChannelID: "C9", Name: "public-news"})
	versions := func() [5]int64 {
		return [5]int64{
			a.winModels[w1].Version(), a.winModels[w2].Version(),
			a.threadPanel.Version(), a.threadsView.Version(), a.activityView.Version(),
		}
	}
	before := versions()
	a.Update(ChannelResolvedMsg{TeamID: a.activeTeamID, ChannelID: "C9", Name: "public-news"})
	if after := versions(); after != before {
		t.Errorf("unchanged resolution invalidated consumers: %v -> %v", before, after)
	}
}
