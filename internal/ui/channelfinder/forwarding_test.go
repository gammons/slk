package channelfinder

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestForwardingFiltersAndRanks(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"", []string{"P1", "G1", "D1", "C1"}},
		{"eng", []string{"D1", "C1", "P1", "G1"}},
		{"eg", []string{"P1", "G1", "D1", "C1"}},
		{"threads", nil},
		{"browse", nil},
	} {
		t.Run("query="+tc.query, func(t *testing.T) {
			m := New()
			m.SetItems([]Item{
				{ID: "C1", Name: "engineering", Type: "channel", Joined: true, LastVisited: 10},
				{ID: "D1", Name: "Eng Person", Type: "dm", Joined: true, LastVisited: 20},
				{ID: "P1", Name: "team-engineering", Type: "private", Joined: true, LastVisited: 40},
				{ID: "G1", Name: "team-eng-group", Type: "group_dm", Joined: true, LastVisited: 30},
				{ID: "C2", Name: "eng-browse", Type: "channel"},
			})
			m.SetSyntheticItems([]Item{
				{ID: ThreadsViewID, Name: "eng-threads", Type: "threads", Joined: true},
				{ID: "other", Name: "eng-other", Type: "threads"},
			})
			m.OpenForForwarding()
			for _, r := range tc.query {
				m.HandleKey(string(r))
			}
			var got []string
			for _, item := range m.FilteredItems() {
				got = append(got, item.ID)
				if !item.Joined || item.Synthetic {
					t.Errorf("ineligible forwarding destination: %+v", item)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("filtered IDs = %v, want %v", got, tc.want)
			}
			box := m.View(100)
			if !strings.Contains(box, "Forward message to…") || strings.Contains(box, "Switch Channel") {
				t.Errorf("wrong forwarding title: %s", box)
			}
			for _, hidden := range []string{"eng-browse", "eng-threads", "eng-other"} {
				if strings.Contains(box, hidden) {
					t.Errorf("render contains hidden destination %q", hidden)
				}
			}
			if len(tc.want) == 0 && m.HandleKey("enter") != nil {
				t.Error("Enter returned a destination for an empty result list")
			}
		})
	}
}

func TestForwardingOpenResetAndNormalRestoration(t *testing.T) {
	for _, closeFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("closeFirst=%v", closeFirst), func(t *testing.T) {
			m := New()
			items := testItems()
			items[0].Joined = true
			items[1].Joined = true
			m.SetItems(items)
			m.SetSyntheticItems([]Item{{ID: ThreadsViewID, Name: "Threads", Type: "threads", Joined: true}})
			m.Open()
			originalItems := append([]Item(nil), m.Items()...)
			originalFiltered := m.FilteredItems()
			m.HandleKey("e")
			m.HandleKey("down")

			m.OpenForForwarding()
			if !m.IsVisible() || m.Query() != "" || m.selected != 0 {
				t.Fatalf("OpenForForwarding did not reset: visible=%v query=%q selected=%d", m.IsVisible(), m.Query(), m.selected)
			}
			m.HandleKey("e")
			m.HandleKey("down")
			m.OpenForForwarding()
			if m.Query() != "" || m.selected != 0 {
				t.Fatal("reopening forwarding did not reset query and selection")
			}
			m.HandleKey("e")
			m.HandleKey("down")
			if closeFirst {
				m.HandleKey("esc")
				if m.IsVisible() || m.View(100) != "" {
					t.Fatal("Escape did not hide forwarding finder")
				}
			}

			m.Open()
			if !m.IsVisible() || m.Query() != "" || m.selected != 0 {
				t.Fatal("normal Open did not reset state")
			}
			if !reflect.DeepEqual(m.Items(), originalItems) || !reflect.DeepEqual(m.FilteredItems(), originalFiltered) {
				t.Fatalf("normal Open lost or reordered original items: %+v", m.FilteredItems())
			}
			if box := m.View(100); !strings.Contains(box, "Switch Channel") || strings.Contains(box, "Forward message to…") {
				t.Errorf("wrong normal title: %s", box)
			}
			m.HandleKey("t")
			m.HandleKey("h")
			if result := m.HandleKey("enter"); result == nil || result.ID != ThreadsViewID {
				t.Errorf("synthetic destination not restored for query: %+v", result)
			}
			m.Open()
			for _, r := range "Alice" {
				m.HandleKey(string(r))
			}
			if result := m.HandleKey("enter"); result == nil || result.ID != "D1" || result.Joined {
				t.Errorf("nonjoined destination not restored for query: %+v", result)
			}
		})
	}
}

func TestForwardingNavigationAndClicks(t *testing.T) {
	m := New()
	var items []Item
	for i := 0; i < maxVisibleRows+3; i++ {
		items = append(items,
			Item{ID: fmt.Sprintf("C%02d", i), Name: fmt.Sprintf("channel-%02d", i), Type: "channel", Joined: true},
			Item{ID: fmt.Sprintf("B%02d", i), Name: fmt.Sprintf("browse-%02d", i), Type: "channel"},
		)
	}
	m.SetItems(items)
	m.SetSyntheticItems([]Item{{ID: ThreadsViewID, Name: "Threads", Type: "threads", Joined: true}})
	m.OpenForForwarding()
	for _, key := range []string{"down", "ctrl+n", "up", "ctrl+p", "up"} {
		m.HandleKey(key)
	}
	if result := m.HandleKey("enter"); result == nil || result.ID != "C00" {
		t.Fatalf("navigation at top returned %+v", result)
	}
	for i := 0; i < len(items); i++ {
		m.HandleKey("down")
	}
	if result := m.HandleKey("enter"); result == nil || result.ID != "C12" || !result.Joined || result.Type != "channel" || result.Name != "channel-12" {
		t.Fatalf("navigation at bottom returned %+v", result)
	}
	w, h := m.BoxSize(100, 30)
	box := m.View(100)
	if w != lipgloss.Width(box) || h != lipgloss.Height(box) {
		t.Fatalf("box size %dx%d differs from render %dx%d", w, h, lipgloss.Width(box), lipgloss.Height(box))
	}
	if m.ClickRow(100, 30, listTopOffset-1) || m.ClickRow(100, 30, listTopOffset+maxVisibleRows) {
		t.Fatal("click outside visible rows selected an item")
	}
	if !m.ClickRow(100, 30, listTopOffset) {
		t.Fatal("click on first scrolled row failed")
	}
	if result := m.HandleKey("enter"); result == nil || result.ID != "C03" {
		t.Errorf("click on first scrolled row returned %+v, want C03", result)
	}
}

func TestForwardingSetItemsRefresh(t *testing.T) {
	m := New()
	m.SetItems([]Item{
		{ID: "C1", Name: "one", Joined: true},
		{ID: "C2", Name: "two", Joined: true},
	})
	m.OpenForForwarding()
	m.HandleKey("down")
	m.SetItems([]Item{{ID: "C3", Name: "browse", Joined: false}})
	if len(m.FilteredItems()) != 0 || m.HandleKey("enter") != nil {
		t.Fatal("replacing items left a stale forwarding destination")
	}
}

func TestForwardingUpdates(t *testing.T) {
	for _, query := range []string{"", "eng"} {
		t.Run("query="+query, func(t *testing.T) {
			m := New()
			m.SetItems([]Item{
				{ID: "C1", Name: "eng-one", Type: "channel", Joined: true},
				{ID: "C2", Name: "eng-two", Type: "channel", Joined: true},
			})
			m.SetSyntheticItems([]Item{{ID: ThreadsViewID, Name: "eng-threads", Type: "threads", Joined: true}})
			m.OpenForForwarding()
			for _, r := range query {
				m.HandleKey(string(r))
			}
			m.HandleKey("down")
			m.SetBrowseable([]Item{{ID: "B1", Name: "eng-browse", Type: "channel"}})
			m.Upsert(Item{ID: "B2", Name: "eng-browse-two", Type: "channel"})
			m.Upsert(Item{ID: ThreadsViewID, Name: "eng-updated-threads", Type: "threads", Joined: true})
			m.Upsert(Item{ID: "S2", Name: "eng-synthetic", Type: "threads", Joined: true, Synthetic: true})
			if got := m.FilteredItems(); len(got) != 2 || got[0].ID != "C1" || got[1].ID != "C2" {
				t.Fatalf("updates admitted ineligible items: %+v", got)
			}

			// Removing the selected destination must keep Enter and rendering safe.
			m.Upsert(Item{ID: "C2", Name: "eng-two", Type: "channel", Joined: false})
			if got := m.FilteredItems(); len(got) != 1 || got[0].ID != "C1" {
				t.Fatalf("departed channel remains visible: %+v", got)
			}
			if result := m.HandleKey("enter"); result == nil || result.ID != "C1" {
				t.Fatalf("selection after removal = %+v, want C1", result)
			}
			m.Upsert(Item{ID: "C1", Name: "eng-one", Type: "channel", Joined: false})
			if len(m.FilteredItems()) != 0 || m.HandleKey("enter") != nil || m.ClickRow(100, 30, listTopOffset) {
				t.Fatal("empty forwarding results remained selectable")
			}
			m.Upsert(Item{ID: "B1", Name: "eng-browse", Type: "channel", Joined: true})
			m.Upsert(Item{ID: "D1", Name: "eng-person", Type: "dm", Joined: true})
			if got := m.FilteredItems(); len(got) != 2 || got[0].ID != "B1" || got[1].ID != "D1" {
				t.Fatalf("joined/new destinations missing: %+v", got)
			}
			if m.Query() != query {
				t.Errorf("updates changed query to %q, want %q", m.Query(), query)
			}
			m.Open()
			if got := m.FilteredItems(); len(got) != 7 {
				t.Errorf("normal Open did not restore all updated items: %+v", got)
			}
		})
	}
}
