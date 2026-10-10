package ui

import (
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/ui/sidebar"
)

// This provider is deliberately view-only; workspace lifecycle regressions
// should exercise the UI port without importing Slack or its section store.
type starredTestProvider []sidebar.SectionMeta

func (p starredTestProvider) Ready() bool                                 { return true }
func (p starredTestProvider) OrderedSlackSections() []sidebar.SectionMeta { return p }

func TestWorkspaceStarredRefreshAndLateName(t *testing.T) {
	a := newTestApp(t)
	p1 := starredTestProvider{{ID: "S1", Type: "stars"}}
	items := []sidebar.ChannelItem{
		{ID: "D1", Name: "U1", DMUserID: "U1", Type: "dm", Section: "S1"},
		{ID: "C1", Name: "general", Type: "channel", Section: "S1"},
	}
	a.Update(WorkspaceSwitchedMsg{TeamID: "T1", Channels: items, SectionsProvider: p1})
	// Inspect through public navigation: the model's shared filtered order
	// must drive both rendering and keyboard selection after every reducer.
	assertOrder := func(want []string) {
		t.Helper()
		a.sidebar.SelectThreadsRow()
		var got []string
		for i := 0; i < 30 && len(got) < len(want); i++ {
			a.sidebar.MoveDown()
			if id := a.sidebar.SelectedID(); id != "" {
				got = append(got, id)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("navigation = %v, want %v", got, want)
		}
	}
	assertOrder([]string{"C1", "D1"})
	a.sidebar.SelectByID("D1")
	a.Update(DMNameResolvedMsg{ChannelID: "D1", DisplayName: "Deploy Bot", IsBot: true})
	if a.sidebar.SelectedID() != "D1" {
		t.Fatalf("late name changed selection to %s", a.sidebar.SelectedID())
	}
	item, _ := a.sidebar.SelectedItem()
	if item.Type != "app" || item.Section != "S1" || item.Name != "Deploy Bot" {
		t.Fatalf("late name item = %+v", item)
	}
	assertOrder([]string{"C1", "D1"})

	// A different workspace owns a people-only stars section. Its provider
	// must replace S1, not inherit the previous workspace's section IDs.
	p2 := starredTestProvider{{ID: "S2", Type: "stars"}}
	second := []sidebar.ChannelItem{{ID: "D2", Name: "Alice", DMUserID: "U2", Type: "dm", Section: "S2"}}
	a.Update(WorkspaceSwitchedMsg{TeamID: "T2", Channels: second, SectionsProvider: p2})
	assertOrder([]string{"D2"})
	a.Update(SectionsRefreshedMsg{TeamID: "T1", Channels: items})
	if got := a.sidebar.Items(); len(got) != 1 || got[0].ID != "D2" || got[0].Section != "S2" {
		t.Fatalf("inactive refresh leaked: %+v", got)
	}
	a.Update(WorkspaceSwitchedMsg{TeamID: "T1", Channels: items, SectionsProvider: p1})
	a.sidebar.SelectByID("D1")
	a.Update(SectionsRefreshedMsg{TeamID: "T1", Channels: []sidebar.ChannelItem{items[1], items[0]}})
	if a.sidebar.SelectedID() != "D1" {
		t.Fatalf("section refresh changed selection to %s", a.sidebar.SelectedID())
	}
	assertOrder([]string{"C1", "D1"})
}
