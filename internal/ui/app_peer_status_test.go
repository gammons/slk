package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/wintree"
)

func newPeerStatusTestApp(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm", DMUserID: "U1"},
		{ID: "C1", Name: "general", Type: "channel"},
	})
	a.channelFinder.SetItems([]channelfinder.Item{
		{ID: "D1", Name: "alice", Type: "dm", Joined: true},
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
	})
	return a
}

func sidebarStatus(t *testing.T, a *App, channelID string) peerstatus.Status {
	t.Helper()
	for _, it := range a.sidebar.Items() {
		if it.ID == channelID {
			return it.Status
		}
	}
	t.Fatalf("no sidebar item %s", channelID)
	return peerstatus.Status{}
}

func finderStatus(t *testing.T, a *App, channelID string) peerstatus.Status {
	t.Helper()
	for _, it := range a.channelFinder.Items() {
		if it.ID == channelID {
			return a.channelFinder.StatusFor(channelID)
		}
	}
	t.Fatalf("no finder item %s", channelID)
	return peerstatus.Status{}
}

func TestUserStatusChangeMsg_ReachesSidebarFinderAndStartsOneTick(t *testing.T) {
	a := newPeerStatusTestApp(t)
	exp := time.Now().Add(time.Hour)

	cmd, handled := a.presence.Handle(a, UserStatusChangeMsg{UserID: "U1", Emoji: ":calendar:", Text: "In a meeting", Expires: exp})
	if !handled {
		t.Fatal("UserStatusChangeMsg not handled by the presence reducer")
	}
	if cmd == nil {
		t.Error("a status that expires must start the expiry tick")
	}
	if st := sidebarStatus(t, a, "D1"); st.Emoji != ":calendar:" || !st.Expires.Equal(exp) {
		t.Errorf("sidebar status = %+v", st)
	}
	if st := finderStatus(t, a, "D1"); st.Emoji != ":calendar:" {
		t.Errorf("finder status = %+v", st)
	}

	cmd, _ = a.presence.Handle(a, UserStatusChangeMsg{UserID: "U1", Emoji: ":calendar:", Expires: exp.Add(time.Hour)})
	if cmd != nil {
		t.Error("a second expiring status started another tick chain while one is running")
	}
}

func TestUserDNDChangeMsg_KeepsCustomStatus(t *testing.T) {
	a := newPeerStatusTestApp(t)
	a.presence.Handle(a, UserStatusChangeMsg{UserID: "U1", Emoji: ":palm_tree:", Text: "Vacation"})
	if _, handled := a.presence.Handle(a, UserDNDChangeMsg{UserID: "U1", Enabled: true}); !handled {
		t.Fatal("UserDNDChangeMsg not handled by the presence reducer")
	}
	for name, st := range map[string]peerstatus.Status{
		"sidebar": sidebarStatus(t, a, "D1"),
		"finder":  finderStatus(t, a, "D1"),
		"peers":   a.presence.peers["U1"],
	} {
		if st.Emoji != ":palm_tree:" || !st.DND {
			t.Errorf("%s status = %+v; want the custom status plus DND", name, st)
		}
	}
}

func TestPeerStatusTick_ClearsExpiredAndStopsWhenNothingPends(t *testing.T) {
	a := newPeerStatusTestApp(t)
	now := time.Now()
	a.presence.Handle(a, UserStatusChangeMsg{UserID: "U1", Emoji: ":calendar:", Expires: now.Add(time.Minute)})
	if !a.presence.peerTickerOn {
		t.Fatal("expiring status did not claim the ticker")
	}

	if cmd := a.presence.expirePeers(a, now); cmd == nil {
		t.Error("tick stopped while a status still had a deadline")
	}
	if cmd := a.presence.expirePeers(a, now.Add(2*time.Minute)); cmd != nil {
		t.Error("tick kept running with nothing left to expire")
	}
	if a.presence.peerTickerOn {
		t.Error("ticker claim not released after the chain stopped")
	}
	if st := sidebarStatus(t, a, "D1"); st.Emoji != "" {
		t.Errorf("sidebar kept an expired status: %+v", st)
	}
	if st := finderStatus(t, a, "D1"); st.Emoji != "" {
		t.Errorf("finder kept an expired status: %+v", st)
	}
	if st := a.presence.peers["U1"]; st.Emoji != "" {
		t.Errorf("peers kept an expired status: %+v", st)
	}
}

func TestDMTopicFor(t *testing.T) {
	a := newPeerStatusTestApp(t)
	a.presence.Handle(a, UserStatusChangeMsg{UserID: "U1", Emoji: ":calendar:", Text: "In a meeting"})

	want := sidebarStatus(t, a, "D1").Summary(time.Now(), dmTopicLayout)
	if want == "" {
		t.Fatal("fixture has no summary")
	}
	if got := a.presence.dmTopicFor(a, "D1"); got != want {
		t.Errorf("dmTopicFor(D1) = %q; want %q", got, want)
	}
	if got := a.presence.dmTopicFor(a, "C1"); got != "" {
		t.Errorf("dmTopicFor(C1) = %q; want empty for a channel", got)
	}
}

func TestChannelSelectedAndLivePeerChangesUpdateDMHeader(t *testing.T) {
	a := newPeerStatusTestApp(t)
	a.activeTeamID = "T1"
	_, _ = a.Update(UserStatusChangeMsg{
		TeamID: "T1", UserID: "U1", Emoji: ":calendar:", Text: "In a meeting",
	})
	_, _ = a.Update(ChannelSelectedMsg{ID: "D1", Name: "alice", Type: "dm"})
	if got := a.messagepane.View(10, 80); !strings.Contains(got, "In a meeting") {
		t.Fatalf("selecting a DM did not put the peer status in its header:\n%s", got)
	}

	_, _ = a.Update(UserStatusChangeMsg{
		TeamID: "T1", UserID: "U1", Emoji: ":palm_tree:", Text: "Vacation",
	})
	_, _ = a.Update(UserDNDChangeMsg{TeamID: "T1", UserID: "U1", Enabled: true})
	got := a.messagepane.View(10, 80)
	if !strings.Contains(got, "Vacation") || !strings.Contains(got, "Do not disturb") {
		t.Fatalf("live peer changes did not update the open DM header:\n%s", got)
	}
	if strings.Contains(got, "In a meeting") {
		t.Fatalf("open DM header kept the superseded status:\n%s", got)
	}
}

// TestUnfocusedSplitWindowGetsLivePeerStatusHeader: a live status
// change updates every window's header, not just the focused one.
func TestUnfocusedSplitWindowGetsLivePeerStatusHeader(t *testing.T) {
	a := newPeerStatusTestApp(t)
	a.activeTeamID = "T1"
	_, _ = a.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	_, _ = a.Update(ChannelSelectedMsg{ID: "D1", Name: "alice", Type: "dm"})

	oldWin := a.focusedWin
	oldModel := a.winModels[oldWin]
	if oldModel == nil {
		t.Fatal("no model for the pre-split focused window")
	}
	if a.splitWindow(wintree.SplitSideBySide) != nil {
		t.Fatal("split refused; want room at 200x60")
	}
	if a.focusedWin == oldWin {
		t.Fatal("split did not move focus to the new window")
	}

	_, _ = a.Update(UserStatusChangeMsg{
		TeamID: "T1", UserID: "U1", Emoji: ":calendar:", Text: "In a meeting",
	})

	if got := oldModel.View(10, 80); !strings.Contains(got, "In a meeting") {
		t.Fatalf("unfocused split window kept a stale DM header:\n%s", got)
	}
}

// TestUnfocusedSplitWindowStatusExpiryUpdatesHeader: a status expiry
// clears every window's header, not just the focused one.
func TestUnfocusedSplitWindowStatusExpiryUpdatesHeader(t *testing.T) {
	a := newPeerStatusTestApp(t)
	a.activeTeamID = "T1"
	_, _ = a.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	_, _ = a.Update(ChannelSelectedMsg{ID: "D1", Name: "alice", Type: "dm"})

	oldWin := a.focusedWin
	oldModel := a.winModels[oldWin]
	if a.splitWindow(wintree.SplitSideBySide) != nil {
		t.Fatal("split refused; want room at 200x60")
	}

	now := time.Now()
	a.presence.Handle(a, UserStatusChangeMsg{
		TeamID: "T1", UserID: "U1", Emoji: ":calendar:", Text: "In a meeting", Expires: now.Add(time.Minute),
	})
	if got := oldModel.View(10, 80); !strings.Contains(got, "In a meeting") {
		t.Fatalf("unfocused split window did not get the live status:\n%s", got)
	}

	a.presence.expirePeers(a, now.Add(2*time.Minute))
	if got := oldModel.View(10, 80); strings.Contains(got, "In a meeting") {
		t.Fatalf("unfocused split window kept an expired status:\n%s", got)
	}
}

func TestPeerChangesFromInactiveWorkspaceAreDropped(t *testing.T) {
	a := newPeerStatusTestApp(t)
	a.activeTeamID = "T1"

	_, _ = a.Update(UserStatusChangeMsg{
		TeamID: "T2", UserID: "U1", Emoji: ":calendar:", Text: "Wrong workspace",
	})
	_, _ = a.Update(UserDNDChangeMsg{TeamID: "T2", UserID: "U1", Enabled: true})

	if st := sidebarStatus(t, a, "D1"); st != (peerstatus.Status{}) {
		t.Fatalf("inactive workspace changed the active sidebar: %+v", st)
	}
}

func TestSetPeers_StartsTickOnlyForExpiringStatuses(t *testing.T) {
	a := newPeerStatusTestApp(t)
	if cmd := a.presence.SetPeers(a, map[string]peerstatus.Status{"U1": {Emoji: ":palm_tree:"}}); cmd != nil {
		t.Error("statuses that never expire started the tick")
	}
	cmd := a.presence.SetPeers(a, map[string]peerstatus.Status{"U2": {Emoji: ":calendar:", Expires: time.Now().Add(time.Hour)}})
	if cmd == nil {
		t.Error("an expiring seeded status did not start the tick")
	}
	if _, ok := a.presence.peers["U1"]; ok {
		t.Error("SetPeers kept a previous workspace's peer")
	}
}
