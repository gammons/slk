package channelfinder

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/peerstatus"
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

func TestDeactivatedUsersAreFoundOnlyBySearch(t *testing.T) {
	// The rows offered, per mode and query, in display order. "gone"
	// matches the deactivated DM's name and the group DM's: the
	// deactivated DM is found by a search, but only after every active
	// match, and never as a forwarding destination.
	for _, tc := range []struct {
		forwarding bool
		query      string
		want       []string
	}{
		{false, "", []string{"C1", "D1", "G1"}},
		{true, "", []string{"C1", "D1", "G1"}},
		{false, "gone", []string{"G1", "D2"}},
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
		if tc.query == "" {
			slices.Sort(got)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("forwarding=%v query=%q: offered %v, want %v", tc.forwarding, tc.query, got, tc.want)
		}
	}
}

func TestWhitespaceIsNoSearchForADeactivatedDM(t *testing.T) {
	m := New()
	m.SetItems([]core.ChannelFinderItem{
		{ID: "D2", Name: "Jane Gone", Type: "dm", DMUserID: "U2", Joined: true},
	})
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	m.Open()
	m.HandleKey(" ")
	if got := filteredIDs(&m); len(got) != 0 {
		t.Fatalf("a space offered %v", got)
	}
}

func TestDeactivatedDMSortsBelowNotJoinedChannels(t *testing.T) {
	m := New()
	m.SetItems([]core.ChannelFinderItem{
		{ID: "D2", Name: "gone", Type: "dm", DMUserID: "U2", Joined: true, LastVisited: 100},
	})
	m.SetBrowseable([]core.ChannelFinderItem{{ID: "C9", Name: "gone-fishing", Type: "channel"}})
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	m.Open()
	for _, r := range "gone" {
		m.HandleKey(string(r))
	}
	if got, want := filteredIDs(&m), []string{"C9", "D2"}; !slices.Equal(got, want) {
		t.Fatalf("offered %v, want %v", got, want)
	}
	if res := m.HandleKey("down"); res != nil {
		t.Fatalf("down returned %+v", res)
	}
	// Enter opens it like any DM.
	if res := m.HandleKey("enter"); res == nil || res.ID != "D2" || res.Type != "dm" || !res.Joined {
		t.Fatalf("enter = %+v, want the joined DM D2", res)
	}
}

func TestDeactivatedDMRowIsMarked(t *testing.T) {
	rowFor := func(t *testing.T, set map[string]bool, name string) string {
		t.Helper()
		m := New()
		m.SetItems([]core.ChannelFinderItem{
			{ID: "D2", Name: name, Type: "dm", DMUserID: "U2", Joined: true, Presence: "active"},
		})
		m.SetStatus("D2", peerstatus.Status{Emoji: ":palm_tree:", Text: "away"})
		m.SetDeactivatedUsers(set)
		m.Open()
		m.HandleKey("g")
		for _, line := range strings.Split(ansi.Strip(m.View(80)), "\n") {
			if strings.Contains(line, "○") || strings.Contains(line, "●") {
				return line
			}
		}
		t.Fatalf("no DM row in\n%s", ansi.Strip(m.View(80)))
		return ""
	}

	row := rowFor(t, map[string]bool{"U2": true}, "gone")
	if !strings.Contains(row, "gone") || !strings.Contains(row, "deactivated") {
		t.Errorf("deactivated row = %q, want the name and the tag", row)
	}
	if strings.Contains(row, "●") || strings.Contains(row, "🌴") {
		t.Errorf("deactivated row shows presence or status: %q", row)
	}

	// A long name is cut before the tag is lost.
	long := rowFor(t, map[string]bool{"U2": true}, "g"+strings.Repeat("x", 100))
	if !strings.Contains(long, "…") || !strings.Contains(long, "deactivated") {
		t.Errorf("long deactivated row = %q, want a truncated name and the tag", long)
	}

	// With the option off the App passes nil: a normal row.
	plain := rowFor(t, nil, "gone")
	if strings.Contains(plain, "deactivated") || !strings.Contains(plain, "●") {
		t.Errorf("row with no set = %q, want a normal DM row", plain)
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
