package sidebar

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
)

func deactivatedFixture() []ChannelItem {
	return []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1"},
		{ID: "D2", Name: "Gone", Type: "dm", DMUserID: "U2"},
		{ID: "A1", Name: "Gonebot", Type: "app", DMUserID: "U3"},
		{ID: "G1", Name: "gone, alice", Type: "group_dm"},
	}
}

func visibleIDs(m *Model) []string {
	var ids []string
	for _, it := range m.VisibleItems() {
		ids = append(ids, it.ID)
	}
	// Sections order the rows; only membership matters here.
	sort.Strings(ids)
	return ids
}

func TestDeactivatedDMsAreHidden(t *testing.T) {
	m := New(deactivatedFixture())
	m.SetDeactivatedUsers(map[string]bool{"U2": true, "U3": true})
	// DM and app rows go; channels and group DMs (no single peer) stay.
	if got, want := visibleIDs(&m), []string{"C1", "D1", "G1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
	// Nil means show everyone again, which is how the option is turned off.
	m.SetDeactivatedUsers(nil)
	if got := visibleIDs(&m); len(got) != 5 {
		t.Fatalf("visible after clearing = %v, want all 5", got)
	}
}

func TestDeactivatedActiveConversationStays(t *testing.T) {
	m := New(deactivatedFixture())
	m.SetActiveChannelID("D2")
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	got := map[string]bool{}
	for _, id := range visibleIDs(&m) {
		got[id] = true
	}
	if !got["D2"] {
		t.Fatal("the active conversation was hidden")
	}
	m.SetActiveChannelID("C1")
	if got, want := visibleIDs(&m), []string{"A1", "C1", "D1", "G1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after leaving the deactivated DM visible = %v, want %v", got, want)
	}
}

// Starred, section-pinned and unread DMs get no exemption: Slack hides
// them too.
func TestDeactivatedHidesStarredAndUnreadDMs(t *testing.T) {
	items := deactivatedFixture()
	items[2].Section = "ST"
	m := New(items)
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{"D2": {HasUnread: true, MentionCount: 2}}
	})
	m.SetSectionsProvider(&fakeProvider{ready: true, sections: []SectionMeta{{ID: "ST", Type: "stars"}}})
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	for _, id := range visibleIDs(&m) {
		if id == "D2" {
			t.Fatal("a starred DM with unreads stayed visible")
		}
	}
}

func TestDeactivatedKeepsCursorOnTheSameItem(t *testing.T) {
	for _, selected := range []string{"D1", "G1"} {
		m := New(deactivatedFixture())
		m.SelectByID(selected)
		m.SetDeactivatedUsers(map[string]bool{"U2": true, "U3": true})
		if m.SelectedID() != selected {
			t.Fatalf("selected %s after refilter, want %s", m.SelectedID(), selected)
		}
		m.SetDeactivatedUsers(nil)
		if m.SelectedID() != selected {
			t.Fatalf("selected %s after showing everyone, want %s", m.SelectedID(), selected)
		}
	}
	// A cursor on the row that disappears falls back like any other
	// removed row.
	m := New(deactivatedFixture())
	m.SelectByID("D2")
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	if m.SelectedID() == "D2" {
		t.Fatal("cursor stayed on a hidden row")
	}
}

// In Slack-sections mode (the default) the rendered sidebar leaves out a
// deactivated DM in the Direct Messages section, not just VisibleItems.
func TestDeactivatedDMIsNotRenderedInSlackSections(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", Section: "CH"},
		{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1", Section: "DMS"},
		{ID: "D2", Name: "Ghosted", Type: "dm", DMUserID: "U2", Section: "DMS"},
	}
	m := New(items)
	m.SetSectionsProvider(&fakeProvider{ready: true, sections: []SectionMeta{
		{ID: "CH", Name: "Channels", Type: "channels"},
		{ID: "DMS", Name: "Direct Messages", Type: "direct_messages"},
	}})
	if view := ansi.Strip(m.View(30, 40)); !strings.Contains(view, "Ghosted") {
		t.Fatalf("fixture: the DM is not rendered before hiding:\n%s", view)
	}
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	view := ansi.Strip(m.View(30, 40))
	if strings.Contains(view, "Ghosted") {
		t.Fatalf("deactivated DM still rendered in Slack-sections mode:\n%s", view)
	}
	if !strings.Contains(view, "Alice") {
		t.Fatalf("the other DM disappeared too:\n%s", view)
	}
}

// A deactivated person's DM stays hidden when it has unread messages:
// revealing hidden rows that turned unread is for rows hidden as stale,
// not for rows hidden on purpose.
func TestDeactivatedDMStaysHiddenWhenUnread(t *testing.T) {
	readState := map[string]cache.ReadState{}
	m := New([]ChannelItem{{ID: "C-general", Name: "general", Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState { return readState })
	m.UpsertItem(ChannelItem{ID: "D-gone", Name: "gone", Type: "dm", DMUserID: "U2"})
	m.SetDeactivatedUsers(map[string]bool{"U2": true})

	readState["D-gone"] = cache.ReadState{HasUnread: true}
	m.Invalidate()
	for _, it := range m.VisibleItems() {
		if it.ID == "D-gone" {
			t.Fatal("the deactivated DM came back after it turned unread")
		}
	}
}

// With Slack style and Active only on, a deactivated person's DM with
// recent unread activity stays hidden, including on the refilter that
// Active only runs on every read-state change.
func TestDeactivatedDMStaysHiddenWithActiveOnly(t *testing.T) {
	readState := map[string]cache.ReadState{"D-gone": {HasUnread: true}}
	m := New([]ChannelItem{
		{ID: "C-general", Name: "general", Type: "channel"},
		{ID: "D-gone", Name: "gone", Type: "dm", DMUserID: "U2"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState { return readState })
	m.SetSlackStyle(true)
	m.SetActiveOnly(true)
	if got := visibleIDs(&m); !reflect.DeepEqual(got, []string{"C-general", "D-gone"}) {
		t.Fatalf("fixture: visible = %v, want the DM shown before hiding", got)
	}

	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	if got := visibleIDs(&m); !reflect.DeepEqual(got, []string{"C-general"}) {
		t.Fatalf("visible = %v, want the deactivated DM hidden", got)
	}
	m.Invalidate()
	if got := visibleIDs(&m); !reflect.DeepEqual(got, []string{"C-general"}) {
		t.Fatalf("visible after Invalidate = %v, want the deactivated DM hidden", got)
	}
}

// The window title's count matches the dots drawn: a hidden deactivated
// DM with unreads has no row, so it is not counted, unless it is the
// conversation on screen.
func TestUnreadCountSkipsHiddenDeactivatedDMs(t *testing.T) {
	m := New(deactivatedFixture())
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"D1": {HasUnread: true},
			"D2": {HasUnread: true},
			"A1": {HasUnread: true},
		}
	})
	if got := m.UnreadChannelCount(); got != 3 {
		t.Fatalf("before any deactivation: count = %d, want 3", got)
	}
	m.SetDeactivatedUsers(map[string]bool{"U2": true, "U3": true})
	if got := m.UnreadChannelCount(); got != 1 {
		t.Errorf("count = %d, want 1 (only Alice's DM is shown)", got)
	}
	m.SetActiveChannelID("D2")
	if got := m.UnreadChannelCount(); got != 2 {
		t.Errorf("with D2 open: count = %d, want 2", got)
	}
}
