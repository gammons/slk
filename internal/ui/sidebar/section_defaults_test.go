package sidebar

import (
	"slices"
	"strings"
	"testing"
)

func sectionDefaultsItems() []ChannelItem {
	return []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "bob", Type: "dm"},
		{ID: "A1", Name: "github", Type: "app"},
		{ID: "C2", Name: "ops", Type: "channel", Section: "Alerts", SectionOrder: 2},
		{ID: "C3", Name: "deploys", Type: "channel", Section: "Engineering", SectionOrder: 1},
	}
}

func TestOrderedSectionsPlacesConfiguredBuiltins(t *testing.T) {
	items := sectionDefaultsItems()
	filtered := []int{0, 1, 2, 3, 4}

	got := orderedSectionsLegacy(items, filtered, map[string]SectionDefault{
		"Channels":    {Order: new(-1)},
		"Engineering": {Order: new(1)},
		"Alerts":      {Order: new(2)},
		"Apps":        {Order: new(3)},
	})

	want := []string{"Channels", "Engineering", "Alerts", "Apps", "Direct Messages"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestOrderedSectionsBuiltinWithoutOrderStaysLast(t *testing.T) {
	expanded := false
	items := sectionDefaultsItems()

	got := orderedSectionsLegacy(items, []int{0, 1, 2, 3, 4}, map[string]SectionDefault{
		"Channels": {Collapsed: &expanded},
	})

	want := []string{"Engineering", "Alerts", "Direct Messages", "Apps", "Channels"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestOrderedSectionsMergesBuiltinWithSameNamedCustom(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "pinned", Type: "channel", Section: "Channels", SectionOrder: 3},
		{ID: "C3", Name: "deploys", Type: "channel", Section: "Engineering", SectionOrder: 1},
	}

	got := orderedSectionsLegacy(items, []int{0, 1, 2}, map[string]SectionDefault{
		"Channels":    {Order: new(3)},
		"Engineering": {Order: new(1)},
	})

	want := []string{"Engineering", "Channels"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSectionDefaultsReorderView(t *testing.T) {
	m := New(sectionDefaultsItems())
	m.SetSectionDefaults(map[string]SectionDefault{"Channels": {Order: new(-1)}})

	view := m.View(30, 40)
	channels := strings.Index(view, "Channels")
	engineering := strings.Index(view, "Engineering")
	if channels < 0 || engineering < 0 || channels > engineering {
		t.Fatalf("Channels should be drawn above Engineering:\n%s", view)
	}
}

func TestSectionDefaultsCollapsed(t *testing.T) {
	expanded, collapsed := false, true
	m := New(sectionDefaultsItems())

	if !m.IsCollapsed("Channels") || !m.IsCollapsed("Apps") || m.IsCollapsed("Engineering") {
		t.Fatal("without defaults, Channels and Apps start collapsed and custom sections expanded")
	}

	m.SetSectionDefaults(map[string]SectionDefault{
		"Channels":    {Collapsed: &expanded},
		"Engineering": {Collapsed: &collapsed},
	})
	if m.IsCollapsed("Channels") {
		t.Error("collapsed = false should expand Channels")
	}
	if !m.IsCollapsed("Engineering") {
		t.Error("collapsed = true should collapse Engineering")
	}
	if !m.IsCollapsed("Apps") {
		t.Error("an entry-less Apps keeps its built-in default")
	}
	if view := m.View(30, 40); !strings.Contains(view, "general") || strings.Contains(view, "deploys") {
		t.Errorf("Channels rows should show and Engineering rows hide:\n%s", view)
	}

	m.ToggleCollapse("Channels")
	if !m.IsCollapsed("Channels") {
		t.Error("a toggle wins over the configured default")
	}
}

func TestSectionDefaultsCollapsedOverridesStartCollapsed(t *testing.T) {
	expanded := false
	m := New(sectionDefaultsItems())
	m.SetStartCollapsed(true)
	m.SetSectionDefaults(map[string]SectionDefault{"Direct Messages": {Collapsed: &expanded}})

	if m.IsCollapsed("Direct Messages") {
		t.Error("collapsed = false should win over start_collapsed")
	}
	if !m.IsCollapsed("Alerts") {
		t.Error("an entry-less section follows start_collapsed")
	}
}
