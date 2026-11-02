package ui

import (
	"reflect"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// deactivatedApp is an App in workspace T1 holding a person (U1), a
// deactivated person (U2) with a DM, a deactivated app (U3), a group DM
// and a channel.
func deactivatedApp() *App {
	a := NewApp()
	a.activeTeamID = "T1"
	a.currentUserID = "USELF"
	a.SetChannels([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1"},
		{ID: "D2", Name: "Gone", Type: "dm", DMUserID: "U2"},
		{ID: "A1", Name: "Gonebot", Type: "app", DMUserID: "U3"},
		{ID: "G1", Name: "gone, alice", Type: "group_dm"},
	})
	a.channelFinder.SetItems([]core.ChannelFinderItem{
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
		{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1", Joined: true},
		{ID: "D2", Name: "Gone", Type: "dm", DMUserID: "U2", Joined: true},
	})
	a.SetUserNames(map[string]string{"USELF": "Me", "U1": "Alice", "U2": "Gone", "U3": "Gonebot"})
	return a
}

// surfaces lists what each filtered surface currently offers.
type surfaces struct {
	sidebar, finder, picks, mentions, newMessage []string
}

func offered(a *App) surfaces {
	var s surfaces
	for _, it := range a.sidebar.VisibleItems() {
		s.sidebar = append(s.sidebar, it.ID)
	}
	a.channelFinder.Open()
	for _, it := range a.channelFinder.FilteredItems() {
		s.finder = append(s.finder, it.ID)
	}
	a.channelFinder.Close()
	for _, c := range a.compose.ChannelPicks() {
		s.picks = append(s.picks, c.ID)
	}
	for _, u := range a.compose.MentionUsers() {
		s.mentions = append(s.mentions, u.ID)
	}
	a.seedNewMessagePicker()
	for _, u := range a.newMessagePicker.Users() {
		s.newMessage = append(s.newMessage, u.ID)
	}
	for _, l := range []*[]string{&s.sidebar, &s.finder, &s.picks, &s.mentions, &s.newMessage} {
		slices.Sort(*l)
	}
	return s
}

func (s surfaces) has(id string) bool {
	for _, l := range [][]string{s.sidebar, s.finder, s.picks, s.mentions, s.newMessage} {
		if slices.Contains(l, id) {
			return true
		}
	}
	return false
}

func TestDeactivatedUsersAreHiddenEverywhere(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true, "U3": true}, 0)
	got := offered(a)

	for name, ids := range map[string][]string{"sidebar": got.sidebar, "finder": got.finder, "# picks": got.picks} {
		for _, id := range []string{"D2", "A1"} {
			if slices.Contains(ids, id) {
				t.Errorf("%s still offers %s: %v", name, id, ids)
			}
		}
		if !slices.Contains(ids, "D1") || !slices.Contains(ids, "C1") {
			t.Errorf("%s lost a visible conversation: %v", name, ids)
		}
	}
	for name, ids := range map[string][]string{"mentions": got.mentions, "new message": got.newMessage} {
		for _, id := range []string{"U2", "U3"} {
			if slices.Contains(ids, id) {
				t.Errorf("%s still offers %s: %v", name, id, ids)
			}
		}
		if !slices.Contains(ids, "U1") {
			t.Errorf("%s lost an active user: %v", name, ids)
		}
	}
	// Group DMs have no single peer and stay.
	if !slices.Contains(got.sidebar, "G1") || !slices.Contains(got.picks, "G1") {
		t.Errorf("a group DM was hidden: sidebar %v picks %v", got.sidebar, got.picks)
	}
	// Names are kept for rendering old messages.
	if a.userNames["U2"] != "Gone" || a.channelNames["D2"] != "Gone" {
		t.Error("the name maps lost the deactivated user")
	}
}

func TestHideDeactivatedUsersOptionOffShowsEveryone(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true, "U3": true}, 0)
	a.SetHideDeactivatedUsers(false)
	got := offered(a)
	for _, id := range []string{"D2", "A1", "U2", "U3"} {
		if !got.has(id) {
			t.Errorf("option off: %s is still hidden", id)
		}
	}
	// The set is kept, so turning the option back on hides them again.
	a.SetHideDeactivatedUsers(true)
	got = offered(a)
	for _, id := range []string{"D2", "A1", "U2", "U3"} {
		if got.has(id) {
			t.Errorf("option back on: %s is offered", id)
		}
	}
}

func TestSetDeactivatedUsersOnlyAdds(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true}, 0)
	a.SetDeactivatedUsers(map[string]bool{"U3": true}, 0)
	a.SetDeactivatedUsers(nil, 0)
	if !a.deactivatedUsers["U2"] || !a.deactivatedUsers["U3"] {
		t.Fatalf("a later snapshot undid a deactivation: %v", a.deactivatedUsers)
	}
	// A false entry is not a deactivation.
	a.SetDeactivatedUsers(map[string]bool{"U1": false}, 0)
	if a.deactivatedUsers["U1"] {
		t.Fatal("a false entry marked the user deactivated")
	}
}

// A workspace snapshot read before a live reactivation can arrive after
// it. It must not hide the reactivated user again, and the other users it
// carries still count.
func TestAnOlderSnapshotDoesNotUndoAReactivation(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true}, 1)
	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: false, Seq: 5})
	a.Update(WorkspaceReadyMsg{
		TeamID:           "T1",
		InitialActive:    true,
		DeactivatedUsers: map[string]bool{"U2": true, "U3": true},
		DeactivatedSeq:   4,
	})
	if a.deactivatedUsers["U2"] {
		t.Error("a snapshot older than the reactivation hid U2 again")
	}
	if !a.deactivatedUsers["U3"] {
		t.Error("the older snapshot's other deactivation was dropped")
	}
	// A switch snapshot read after the update is not older than it.
	a.Update(WorkspaceSwitchedMsg{TeamID: "T1", DeactivatedUsers: map[string]bool{"U2": true}, DeactivatedSeq: 5})
	if !a.deactivatedUsers["U2"] {
		t.Error("a snapshot as new as the update was ignored")
	}
}

// Sends from different goroutines can arrive out of order: an update older
// than the one applied for the user is dropped.
func TestAnOlderLiveUpdateIsDropped(t *testing.T) {
	a := deactivatedApp()
	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: false, Seq: 5})
	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: true, Seq: 3})
	if a.deactivatedUsers["U2"] {
		t.Error("an older deactivation overrode a newer reactivation")
	}
	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: true, Seq: 6})
	if !a.deactivatedUsers["U2"] {
		t.Error("a newer deactivation was dropped")
	}
}

// A snapshot that adds nothing must not refilter every list: each workspace
// message carries the whole cache set, almost always already known.
func TestSetDeactivatedUsersWithNothingNewDoesNotRepublish(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true}, 0)
	before := reflect.ValueOf(a.deactivatedUsers).Pointer()
	for _, users := range []map[string]bool{{"U2": true}, nil, {"U1": false}} {
		a.SetDeactivatedUsers(users, 0)
		if reflect.ValueOf(a.deactivatedUsers).Pointer() != before {
			t.Fatalf("SetDeactivatedUsers(%v) republished an unchanged set", users)
		}
	}
	a.SetDeactivatedUsers(map[string]bool{"U3": true}, 0)
	if reflect.ValueOf(a.deactivatedUsers).Pointer() == before || !a.deactivatedUsers["U3"] {
		t.Fatal("a new deactivation was not published")
	}
}

func TestSetDeactivatedUsersNeverWritesAMapItPublished(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true}, 0)
	held := a.deactivatedUsers // what the sidebar and finder keep
	a.setUserDeactivated("U3", true, 0)
	a.setUserDeactivated("U2", false, 0)
	if !held["U2"] || held["U3"] {
		t.Fatalf("a published map was written: %v", held)
	}
}

func TestUserDeactivatedMsgHidesAndReactivationRestores(t *testing.T) {
	// The message names no workspace: the account is deactivated in all of
	// them, so it applies to whichever one is on screen.
	a := deactivatedApp()
	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: true})
	got := offered(a)
	if got.has("D2") || got.has("U2") {
		t.Fatalf("still offered after UserDeactivatedMsg: %+v", got)
	}
	a.Update(UserDeactivatedMsg{UserID: "U2", Deactivated: false})
	got = offered(a)
	for name, ids := range map[string][]string{"sidebar": got.sidebar, "finder": got.finder, "# picks": got.picks} {
		if !slices.Contains(ids, "D2") {
			t.Errorf("reactivated DM missing from %s: %v", name, ids)
		}
	}
	if !slices.Contains(got.mentions, "U2") || !slices.Contains(got.newMessage, "U2") {
		t.Errorf("reactivated user missing from the people lists: %+v", got)
	}
}

func TestDeactivatedActiveConversationStaysInSidebar(t *testing.T) {
	a := deactivatedApp()
	a.sidebar.SetActiveChannelID("D2")
	a.SetDeactivatedUsers(map[string]bool{"U2": true}, 0)
	if !slices.Contains(offered(a).sidebar, "D2") {
		t.Fatal("the open conversation left the sidebar")
	}
}

// The workspace messages carry the set into the lists they build. It does not
// pin the order the handlers apply it in: the lists are refiltered when the
// set is published either way, so a later SetChannels or SetUserNames would
// be filtered too.
func TestWorkspaceMessagesCarryDeactivatedUsersIntoTheLists(t *testing.T) {
	items := []sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1"},
		{ID: "D2", Name: "Gone", Type: "dm", DMUserID: "U2"},
	}
	finder := []core.ChannelFinderItem{
		{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1", Joined: true},
		{ID: "D2", Name: "Gone", Type: "dm", DMUserID: "U2", Joined: true},
	}
	names := map[string]string{"U1": "Alice", "U2": "Gone"}
	for name, msg := range map[string]any{
		"ready": WorkspaceReadyMsg{TeamID: "T1", InitialActive: true, Channels: items, FinderItems: finder,
			UserNames: names, DeactivatedUsers: map[string]bool{"U2": true}},
		"switched": WorkspaceSwitchedMsg{TeamID: "T1", Channels: items, FinderItems: finder,
			UserNames: names, DeactivatedUsers: map[string]bool{"U2": true}},
	} {
		t.Run(name, func(t *testing.T) {
			a := NewApp()
			a.Update(msg)
			got := offered(a)
			if got.has("D2") || got.has("U2") {
				t.Fatalf("a deactivated user was built into a list: %+v", got)
			}
			if !got.has("D1") {
				t.Fatalf("an active DM is missing: %+v", got)
			}
		})
	}
}

// Opening a hidden deactivated DM from the finder puts the sidebar cursor
// on it: the row only becomes visible once it is the active conversation.
// During an upload the switch is refused, so the sidebar must not move:
// the hidden DM stays hidden and the cursor stays where it was.
func TestOpeningFromTheFinderDuringAnUploadLeavesTheSidebar(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true}, 0)
	a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	a.sidebar.SelectByID("C1")
	a.setUploaderForTest(func(_, _, _ string, _ []core.PendingAttachment) tea.Cmd { return nil })
	a.compose.AddAttachment(core.PendingAttachment{Filename: "shot.png", Bytes: []byte("x")})
	a.submitWithAttachments(&a.compose)
	if !a.compose.Uploading() {
		t.Fatal("precondition: compose not uploading")
	}

	cmd := a.openChannel(channelfinder.ChannelResult{ID: "D2", Name: "Gone", Type: "dm", Joined: true})
	if cmd != nil {
		a.Update(cmd())
	}
	if a.activeChannelID != "C1" {
		t.Fatalf("active channel = %q, want the switch refused", a.activeChannelID)
	}
	if got := a.sidebar.SelectedID(); got != "C1" {
		t.Errorf("sidebar cursor on %q, want C1", got)
	}
	if offered(a).has("D2") {
		t.Error("the refused switch made the deactivated DM the sidebar's active row")
	}
}

func TestOpeningAHiddenDMFromTheFinderSelectsItsRow(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true}, 0)
	a.sidebar.SelectByID("C1")
	cmd := a.openChannel(channelfinder.ChannelResult{ID: "D2", Name: "Gone", Type: "dm", Joined: true})
	if cmd != nil {
		a.Update(cmd())
	}
	if got := a.sidebar.SelectedID(); got != "D2" {
		t.Fatalf("sidebar cursor on %q, want the opened DM D2", got)
	}
}

func TestForwardPickerSkipsADeactivatedDMWithTheOptionOff(t *testing.T) {
	a := deactivatedApp()
	a.SetDeactivatedUsers(map[string]bool{"U2": true}, 0)
	a.SetHideDeactivatedUsers(false)
	a.channelFinder.OpenForForwarding()
	for _, r := range "gone" {
		a.channelFinder.HandleKey(string(r))
	}
	for _, it := range a.channelFinder.FilteredItems() {
		if it.ID == "D2" {
			t.Fatal("the forward picker offers a deactivated person's DM with the option off")
		}
	}
}
