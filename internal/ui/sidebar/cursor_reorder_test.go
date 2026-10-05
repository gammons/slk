package sidebar

import "testing"

func TestCursorIdentityAcrossItemReorders(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "one", Type: "channel"},
		{ID: "C2", Name: "two", Type: "channel"},
		{ID: "C3", Name: "three", Type: "channel"},
	})
	m.SelectByID("C2")
	m.SetItems([]ChannelItem{
		{ID: "C2", Name: "two", Type: "channel"},
		{ID: "C3", Name: "three", Type: "channel"},
		{ID: "C1", Name: "one", Type: "channel"},
	})
	if m.SelectedID() != "C2" {
		t.Fatalf("reordered input selected %s, want C2", m.SelectedID())
	}
	// Changing a sort key on an existing row also changes filtered indices.
	m.UpsertItem(ChannelItem{ID: "C3", Name: "three", Type: "channel", ChannelOrder: 1})
	if m.SelectedID() != "C2" {
		t.Fatalf("upsert reorder selected %s, want C2", m.SelectedID())
	}
	m.SetItems([]ChannelItem{{ID: "C1", Name: "one", Type: "channel"}})
	if !m.IsThreadsSelected() {
		t.Fatalf("removed target selected %s instead of Threads", m.SelectedID())
	}
}
