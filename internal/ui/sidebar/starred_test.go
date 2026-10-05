package sidebar

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
)

// These fixtures deliberately interleave conversation types and use names
// whose alphabetical order differs from Slack's order.
func starredItems() []ChannelItem {
	return []ChannelItem{
		{ID: "D1", Name: "Zoe", Type: "dm", DMUserID: "U1", Section: "ST"},
		{ID: "C1", Name: "zebra", Type: "channel", Section: "ST"},
		{ID: "D2", Name: "Alice", Type: "dm", DMUserID: "U2", Section: "ST"},
		{ID: "C2", Name: "alpha", Type: "private", Section: "ST"},
	}
}

func TestStarredOrderUsesSectionType(t *testing.T) {
	for _, tc := range []struct {
		name, sectionType, label string
		ready, provider          bool
		want                     []string
	}{
		{"stars", "stars", "Starred", true, true, []string{"C1", "C2", "D1", "D2"}},
		{"empty label", "stars", "", true, true, []string{"C1", "C2", "D1", "D2"}},
		{"renamed", "stars", "Favorites", true, true, []string{"C1", "C2", "D1", "D2"}},
		{"custom Starred", "standard", "Starred", true, true, []string{"D1", "C1", "D2", "C2"}},
		{"not ready", "stars", "Starred", false, true, []string{"D1", "C1", "D2", "C2"}},
		{"config only", "", "", false, false, []string{"D1", "C1", "D2", "C2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(starredItems())
			if tc.provider {
				m.SetSectionsProvider(&fakeProvider{ready: tc.ready, sections: []SectionMeta{{ID: "ST", Name: tc.label, Type: tc.sectionType}}})
			}
			var got []string
			for _, idx := range m.filtered {
				got = append(got, m.items[idx].ID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStarredOrderBeforeChannelOrder(t *testing.T) {
	items := starredItems()
	items[0].ChannelOrder = 1
	items[2].ChannelOrder = 1 // equal priority must preserve Slack order
	items[1].ChannelOrder = 9
	items[3].ChannelOrder = 3
	items = append(items, ChannelItem{ID: "A1", Name: "app", Type: "app", Section: "ST"}, ChannelItem{ID: "G1", Name: "group", Type: "group_dm", Section: "ST"})
	m := New(items)
	m.SetSectionsProvider(&fakeProvider{ready: true, sections: []SectionMeta{{ID: "ST", Type: "stars"}}})
	var got []string
	for _, idx := range m.filtered {
		got = append(got, m.items[idx].ID)
	}
	want := []string{"C2", "C1", "D1", "D2", "A1", "G1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestStarredRenderingNavigationAndRefresh(t *testing.T) {
	m := New(starredItems())
	m.SetSectionsProvider(&fakeProvider{ready: true, sections: []SectionMeta{{ID: "ST", Type: "stars"}}})
	want := []string{"C1", "C2", "D1", "D2"}
	var got []string
	for i := 0; i < len(m.nav)-1; i++ {
		m.MoveDown()
		id := m.SelectedID()
		if id == "" {
			continue
		}
		got = append(got, id)
		selected, _ := m.SelectedItem()
		for _, line := range strings.Split(m.View(40, 30), "\n") {
			if strings.Contains(line, "▌") && !strings.Contains(line, selected.Name) {
				t.Fatalf("selected %s but rendered cursor on %q", id, line)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("navigation = %v, want %v", got, want)
	}
	for i := len(want) - 2; i >= 0; i-- {
		for attempts := 0; attempts < len(m.nav); attempts++ {
			m.MoveUp()
			if m.SelectedID() != "" {
				break
			}
		}
		if m.SelectedID() != want[i] {
			t.Fatalf("reverse navigation selected %s, want %s", m.SelectedID(), want[i])
		}
	}
	m.SelectByID("D1")
	// Reversing the incoming list moves D1's underlying index. Selection must
	// follow its conversation ID, not the old filtered index.
	items := starredItems()
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	m.SetItems(items)
	if m.SelectedID() != "D1" {
		t.Fatalf("SetItems selected %q, want D1", m.SelectedID())
	}
	m.UpdatePresenceByUser("U1", "active")
	item, _ := m.SelectedItem()
	item.Name = "Renamed"
	item.Type = "app"
	m.UpsertItem(item)
	if m.SelectedID() != "D1" {
		t.Fatalf("UpsertItem selected %q, want D1", m.SelectedID())
	}
	m.ToggleCollapse("ST")
	m.ToggleCollapse("ST")
	var order []string
	for _, n := range m.nav {
		if n.kind == navChannel {
			order = append(order, m.items[m.filtered[n.fi]].ID)
		}
	}
	if !reflect.DeepEqual(order, []string{"C2", "C1", "D2", "D1"}) {
		t.Fatalf("refresh order = %v", order)
	}
	// An explicit peer priority now moves D1 within its block; selection
	// must survive an Upsert that actually changes filtered positions.
	m.SelectByID("D1")
	item.ChannelOrder = 1
	m.UpsertItem(item)
	if m.SelectedID() != "D1" {
		t.Fatalf("reordered Upsert selected %q", m.SelectedID())
	}
}

func TestStarredUnreadMouseAndStaleness(t *testing.T) {
	m := New(starredItems())
	m.SetSectionsProvider(&fakeProvider{ready: true, sections: []SectionMeta{{ID: "ST", Type: "stars"}}})
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{
			"C1": {HasUnread: true}, "C2": {HasUnread: true},
			"D1": {HasUnread: true, MentionCount: 2},
			"D2": {LastReadTS: "1000000000.000000"}, // old, read, but starred
		}
	})
	m.SetStaleThreshold(24 * time.Hour)
	want := []string{"C1", "C2", "D1", "D2"}
	if len(m.filtered) != len(want) {
		t.Fatalf("staleness hid starred rows: %v", m.filtered)
	}
	for _, tc := range []struct {
		after string
		dir   int
		want  string
	}{
		{"", 1, "C1"}, {"C1", 1, "C2"}, {"C2", 1, "D1"},
		{"D1", 1, "C1"}, {"D1", -1, "C2"},
	} {
		if id, _, _, ok := m.NextUnread(tc.after, tc.dir); !ok || id != tc.want {
			t.Fatalf("NextUnread(%s,%d) = %s,%v, want %s", tc.after, tc.dir, id, ok, tc.want)
		}
	}
	view := m.View(40, 30)
	var clicked []string
	for y, line := range strings.Split(view, "\n") {
		for _, fixture := range starredItems() {
			if !strings.Contains(line, fixture.Name) {
				continue
			}
			item, ok := m.ClickAt(y)
			if !ok || item.ID != fixture.ID || m.SelectedID() != fixture.ID {
				t.Fatalf("click %q = %+v,%v; selected %s", line, item, ok, m.SelectedID())
			}
			clicked = append(clicked, item.ID)
		}
	}
	if !reflect.DeepEqual(clicked, want) {
		t.Fatalf("clicked visual order = %v, want %v", clicked, want)
	}
}

func TestStarredSingleBlockAndEmpty(t *testing.T) {
	for _, items := range [][]ChannelItem{nil, starredItems()[:1], starredItems()[1:2]} {
		m := New(items)
		m.SetSectionsProvider(&fakeProvider{ready: true, sections: []SectionMeta{{ID: "ST", Type: "stars"}}})
		if len(m.filtered) != len(items) {
			t.Fatalf("visible = %d, want %d", len(m.filtered), len(items))
		}
		if len(items) > 0 && !strings.Contains(m.View(40, 20), "Starred") {
			t.Fatal("missing Starred header")
		}
	}
}
