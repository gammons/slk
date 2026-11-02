package channelfinder

import (
	"slices"
	"testing"

	"github.com/gammons/slk/internal/core"
)

func deactivatedItems() []core.ChannelFinderItem {
	return []core.ChannelFinderItem{
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
		{ID: "D1", Name: "alice", Type: "dm", DMUserID: "U1", Joined: true},
		{ID: "D2", Name: "gone", Type: "dm", DMUserID: "U2", Joined: true},
		{ID: "G1", Name: "gone, alice", Type: "group_dm", Joined: true},
	}
}

func filteredIDs(m *Model) []string {
	var ids []string
	for _, it := range m.FilteredItems() {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestDeactivatedUsersAreAbsentFromTheFinder(t *testing.T) {
	// The rows that must still be offered, per mode and query. "gone"
	// matches the deactivated DM's name and the group DM's.
	for _, tc := range []struct {
		forwarding bool
		query      string
		want       []string
	}{
		{false, "", []string{"C1", "D1", "G1"}},
		{true, "", []string{"C1", "D1", "G1"}},
		{false, "gone", []string{"G1"}},
		{true, "gone", []string{"G1"}},
	} {
		m := New()
		m.SetItems(deactivatedItems())
		m.SetDeactivatedUsers(map[string]bool{"U2": true})
		if tc.forwarding {
			m.OpenForForwarding()
		} else {
			m.Open()
		}
		for _, r := range tc.query {
			m.HandleKey(string(r))
		}
		got := filteredIDs(&m)
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("forwarding=%v query=%q: offered %v, want %v", tc.forwarding, tc.query, got, tc.want)
		}
	}
}

func TestDeactivatedUsersReturnWhenTheSetIsCleared(t *testing.T) {
	m := New()
	m.SetItems(deactivatedItems())
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	m.Open()
	if got := len(filteredIDs(&m)); got != 3 {
		t.Fatalf("filtered = %d rows, want 3", got)
	}
	// A live change refilters the open overlay.
	m.SetDeactivatedUsers(nil)
	if got := len(filteredIDs(&m)); got != 4 {
		t.Fatalf("filtered after clearing = %d rows, want 4", got)
	}
	// Group DMs and channels have no DMUserID and are never hidden.
	m.SetDeactivatedUsers(map[string]bool{"U2": true, "": true})
	if got, want := len(filteredIDs(&m)), 3; got != want {
		t.Fatalf("an empty user ID hid rows: %d, want %d", got, want)
	}
}

func TestDeactivatedRefilterKeepsSelectionInRange(t *testing.T) {
	m := New()
	m.SetItems(deactivatedItems())
	m.Open()
	for range 3 {
		m.HandleKey("down")
	}
	m.SetDeactivatedUsers(map[string]bool{"U1": true, "U2": true})
	if res := m.HandleKey("enter"); res == nil || res.ID == "D1" || res.ID == "D2" {
		t.Fatalf("enter selected %+v after the rows were hidden", res)
	}
}
