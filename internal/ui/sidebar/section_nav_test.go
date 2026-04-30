package sidebar

import (
	"strings"
	"testing"
)

// TestRenderedSelectionMatchesNavigation walks j/k through every item and
// verifies that the rendered View shows the cursor (▌) on the line whose
// channel name matches SelectedID's display name. This catches any drift
// between visual order and selection order.
func TestRenderedSelectionMatchesNavigation(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "alerts", Type: "channel", Section: "Alerts", SectionOrder: 1},
		{ID: "C3", Name: "ops", Type: "channel", Section: "Alerts", SectionOrder: 1},
		{ID: "C4", Name: "deploys", Type: "channel", Section: "Engineering", SectionOrder: 2},
		{ID: "D1", Name: "alice", Type: "dm"},
		{ID: "D2", Name: "bob", Type: "dm"},
	}
	m := New(items)
	// Expand all sections so every channel row is rendered. The
	// default-collapsed "Channels" section would otherwise hide
	// "general" from the rendered output.
	m.ToggleCollapse("Channels")
	// Step off the synthetic Threads row.
	m.MoveDown()

	// Expected nav stops include section headers; the test asserts the
	// cursor lands on a line containing each name in order. Headers
	// share names with the visible section titles ("Engineering",
	// "Alerts", "Direct Messages", "Channels") so we list them too.
	// Custom sections are ordered by SectionOrder ascending, so Alerts
	// (order=1) precedes Engineering (order=2). Then DMs, then the
	// catch-all Channels section.
	expectedOrder := []string{
		"Alerts", "alerts", "ops",
		"Engineering", "deploys",
		"Direct Messages", "alice", "bob",
		"Channels", "general",
	}
	for i, name := range expectedOrder {
		view := m.View(40, 40)
		lines := strings.Split(view, "\n")
		var cursorLine string
		for _, l := range lines {
			if strings.Contains(l, "▌") {
				cursorLine = l
				break
			}
		}
		if !strings.Contains(cursorLine, name) {
			t.Fatalf("step %d: cursor on %q, want it on %q\nview:\n%s", i, cursorLine, name, view)
		}
		if i < len(expectedOrder)-1 {
			m.MoveDown()
		}
	}
}

func TestNavigationFollowsSectionOrder(t *testing.T) {
	// Items in Slack-response order: a default channel, then two custom-section
	// channels, then a DM. Expected display order: custom section first, then
	// "Direct Messages", then "Channels".
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "alerts", Type: "channel", Section: "Alerts", SectionOrder: 1},
		{ID: "C3", Name: "ops", Type: "channel", Section: "Alerts", SectionOrder: 1},
		{ID: "D1", Name: "alice", Type: "dm"},
	}
	m := New(items)
	// Expand the Channels section so its rows participate in nav.
	m.ToggleCollapse("Channels")

	// Walk through every channel ID, advancing j past section headers
	// as needed. Selection-by-channel-ID order: C2, C3 (Alerts), D1
	// (Direct Messages), C1 (Channels).
	want := []string{"C2", "C3", "D1", "C1"}
	stepDownToID := func(t *testing.T, want string) {
		t.Helper()
		for i := 0; i < 50; i++ {
			m.MoveDown()
			if m.SelectedID() == want {
				return
			}
		}
		t.Fatalf("never reached id %q (current=%q)", want, m.SelectedID())
	}
	stepUpToID := func(t *testing.T, want string) {
		t.Helper()
		for i := 0; i < 50; i++ {
			m.MoveUp()
			if m.SelectedID() == want {
				return
			}
		}
		t.Fatalf("never reached id %q on the way up (current=%q)", want, m.SelectedID())
	}
	for _, id := range want {
		stepDownToID(t, id)
	}
	for i := len(want) - 2; i >= 0; i-- {
		stepUpToID(t, want[i])
	}
}

func TestOrderedSectionsCustomFirstDMsLast(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "bob", Type: "dm"},
		{ID: "C2", Name: "ops", Type: "channel", Section: "Alerts", SectionOrder: 2},
		{ID: "C3", Name: "deploys", Type: "channel", Section: "Engineering", SectionOrder: 1},
	}
	filtered := []int{0, 1, 2, 3}
	got := orderedSections(items, filtered)
	want := []string{"Engineering", "Alerts", "Direct Messages", "Channels"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestOrderedSectionsAppsBetweenDMsAndChannels(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "bob", Type: "dm"},
		{ID: "A1", Name: "github", Type: "app"},
		{ID: "A2", Name: "pagerduty", Type: "app"},
	}
	filtered := []int{0, 1, 2, 3}
	got := orderedSections(items, filtered)
	want := []string{"Direct Messages", "Apps", "Channels"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestOrderedSectionsAppsOnlyOmitsOthers(t *testing.T) {
	// Apps section should appear even when there are no human DMs and
	// no public channels (e.g. a workspace where the user only talks
	// to bots).
	items := []ChannelItem{
		{ID: "A1", Name: "github", Type: "app"},
	}
	got := orderedSections(items, []int{0})
	if len(got) != 1 || got[0] != "Apps" {
		t.Fatalf("expected just [Apps], got %v", got)
	}
}

func TestSectionForApp(t *testing.T) {
	if got := sectionFor(ChannelItem{Type: "app"}); got != "Apps" {
		t.Errorf("expected Type=app -> Apps section, got %q", got)
	}
}
