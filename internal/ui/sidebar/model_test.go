package sidebar

import (
	"strings"
	"testing"
)

func TestSidebarView(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", UnreadCount: 0},
		{ID: "C2", Name: "random", Type: "channel", UnreadCount: 3},
		{ID: "C3", Name: "alice", Type: "dm", Presence: "active"},
	}

	m := New(channels)
	view := m.View(20, 25) // height=20, width=25

	if !strings.Contains(view, "general") {
		t.Error("expected 'general' in view")
	}
	if !strings.Contains(view, "random") {
		t.Error("expected 'random' in view")
	}
}

func TestSidebarNavigation(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "eng", Type: "channel"},
	}

	m := New(channels)
	// Default selection is now the synthetic Threads row; step off it to
	// reach the first channel.
	m.MoveDown()
	if m.SelectedID() != "C1" {
		t.Error("expected C1 selected after stepping off the Threads row")
	}

	m.MoveDown()
	if m.SelectedID() != "C2" {
		t.Error("expected C2 after move down")
	}

	m.MoveDown()
	m.MoveDown() // should stop at bottom
	if m.SelectedID() != "C3" {
		t.Error("expected C3 at bottom")
	}

	m.MoveUp()
	if m.SelectedID() != "C2" {
		t.Error("expected C2 after move up")
	}
}

func TestThreadsItem_DefaultSelected(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "design", Type: "channel"},
	})
	if !m.IsThreadsSelected() {
		t.Errorf("expected Threads entry to be selected by default (top of list)")
	}
}

func TestThreadsItem_MoveDownLeavesIt(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "design", Type: "channel"},
	})
	m.MoveDown()
	if m.IsThreadsSelected() {
		t.Errorf("MoveDown should leave the Threads entry")
	}
	item, ok := m.SelectedItem()
	if !ok || item.ID != "C1" {
		t.Errorf("first channel should be selected, got %+v ok=%v", item, ok)
	}
}

func TestThreadsItem_MoveUpReturnsToIt(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
	})
	m.MoveDown()
	if m.IsThreadsSelected() {
		t.Fatalf("precondition: should be on a channel")
	}
	m.MoveUp()
	if !m.IsThreadsSelected() {
		t.Errorf("MoveUp from first channel should land on Threads entry")
	}
}

func TestThreadsItem_UnreadBadgeRenders(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	out := m.View(10, 30)
	if !strings.Contains(out, "Threads") {
		t.Errorf("View should contain 'Threads': %q", out)
	}
	// Find the line containing "Threads" and assert the badge glyph and count
	// appear together as the literal substring "•3".
	var threadsLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Threads") {
			threadsLine = line
			break
		}
	}
	if threadsLine == "" {
		t.Fatalf("no line containing 'Threads' in view: %q", out)
	}
	if !strings.Contains(threadsLine, "•3") {
		t.Errorf("Threads line should contain badge '•3', got %q", threadsLine)
	}
}

func TestThreadsItem_VisibleWhenNoChannels(t *testing.T) {
	m := New(nil)
	out := m.View(10, 30)
	if !strings.Contains(out, "Threads") {
		t.Errorf("View should contain 'Threads' even when there are no channels: %q", out)
	}
	if !m.IsThreadsSelected() {
		t.Errorf("Threads row should still be selected when there are no channels")
	}
}

func TestSetThreadsUnreadCount_NegativeClampsToZero(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(-5)
	if got := m.ThreadsUnreadCount(); got != 0 {
		t.Errorf("negative count should clamp to 0, got %d", got)
	}
}

func TestSetThreadsUnreadCount_NoChangeNoVersionBump(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	v1 := m.Version()
	m.SetThreadsUnreadCount(3) // identical -- no state change
	v2 := m.Version()
	if v1 != v2 {
		t.Errorf("identical SetThreadsUnreadCount should not bump version, got %d -> %d", v1, v2)
	}
}

func TestSetThreadsUnreadCount_ZeroRemovesBadge(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	out := m.View(10, 30)
	if !strings.Contains(out, "•3") {
		t.Fatalf("precondition: badge '•3' should be present, got %q", out)
	}
	m.SetThreadsUnreadCount(0)
	out = m.View(10, 30)
	if strings.Contains(out, "•") {
		t.Errorf("badge glyph '•' should be gone after setting count to 0, got %q", out)
	}
}

func TestThreadsItem_SelectedItemFalseWhenOnThreadsRow(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	if _, ok := m.SelectedItem(); ok {
		t.Errorf("SelectedItem should return ok=false when Threads row is selected")
	}
}

func TestThreadsItem_SelectByIDClearsThreadsSelection(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	if !m.IsThreadsSelected() {
		t.Fatal("precondition")
	}
	m.SelectByID("C1")
	if m.IsThreadsSelected() {
		t.Errorf("SelectByID should clear Threads selection")
	}
}

func TestMarkUnread_IncrementsCount(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", UnreadCount: 0},
		{ID: "C2", Name: "random", Type: "channel", UnreadCount: 2},
	})
	m.MarkUnread("C1")
	if got := m.Items()[0].UnreadCount; got != 1 {
		t.Errorf("MarkUnread should bump UnreadCount from 0 to 1, got %d", got)
	}
	m.MarkUnread("C2")
	if got := m.Items()[1].UnreadCount; got != 3 {
		t.Errorf("MarkUnread should bump existing count from 2 to 3, got %d", got)
	}
}

func TestMarkUnread_BumpsVersionAndInvalidatesCache(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	// Prime the cache.
	_ = m.View(10, 30)
	v1 := m.Version()
	m.MarkUnread("C1")
	v2 := m.Version()
	if v2 == v1 {
		t.Errorf("MarkUnread should bump version, got %d -> %d", v1, v2)
	}
}

func TestMarkUnread_UnknownChannelIsNoop(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	v1 := m.Version()
	m.MarkUnread("C-does-not-exist")
	v2 := m.Version()
	if v1 != v2 {
		t.Errorf("MarkUnread on unknown channel should not bump version, got %d -> %d", v1, v2)
	}
}

func TestMarkUnread_RendersDotAndBold(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel", UnreadCount: 0}})
	before := m.View(10, 30)
	// Find the line for "general" before bumping.
	var beforeLine string
	for _, line := range strings.Split(before, "\n") {
		if strings.Contains(line, "general") {
			beforeLine = line
			break
		}
	}
	m.MarkUnread("C1")
	after := m.View(10, 30)
	var afterLine string
	for _, line := range strings.Split(after, "\n") {
		if strings.Contains(line, "general") {
			afterLine = line
			break
		}
	}
	if beforeLine == afterLine {
		t.Errorf("expected sidebar render to change after MarkUnread; before=%q after=%q", beforeLine, afterLine)
	}
}

func TestSidebarFilter(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "eng", Type: "channel"},
	}

	m := New(channels)
	m.SetFilter("gen")

	visible := m.VisibleItems()
	if len(visible) != 1 {
		t.Errorf("expected 1 filtered result, got %d", len(visible))
	}
	if visible[0].Name != "general" {
		t.Errorf("expected 'general', got %q", visible[0].Name)
	}

	m.SetFilter("")
	visible = m.VisibleItems()
	if len(visible) != 3 {
		t.Errorf("expected 3 items after clear filter, got %d", len(visible))
	}
}
