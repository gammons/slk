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
	// Step off the synthetic Threads row so the cursor lands on the first channel.
	m.MoveDown()

	expectedOrder := []string{"alerts", "ops", "deploys", "general", "alice", "bob"}
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
	// "Channels", then "Direct Messages".
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "alerts", Type: "channel", Section: "Alerts", SectionOrder: 1},
		{ID: "C3", Name: "ops", Type: "channel", Section: "Alerts", SectionOrder: 1},
		{ID: "D1", Name: "alice", Type: "dm"},
	}
	m := New(items)
	// Step off the synthetic Threads row so navigation begins on the first channel.
	m.MoveDown()

	want := []string{"C2", "C3", "C1", "D1"}
	for i, id := range want {
		if got := m.SelectedID(); got != id {
			t.Fatalf("step %d: got selected %q, want %q", i, got, id)
		}
		if i < len(want)-1 {
			m.MoveDown()
		}
	}

	// And back up.
	for i := len(want) - 2; i >= 0; i-- {
		m.MoveUp()
		if got := m.SelectedID(); got != want[i] {
			t.Fatalf("up step %d: got %q, want %q", i, got, want[i])
		}
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
	want := []string{"Engineering", "Alerts", "Channels", "Direct Messages"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}
