package sidebar

import "testing"

func TestCursorIdentityAcrossSectionsProviderReorders(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "one", Type: "channel", Section: "A"},
		{ID: "C2", Name: "two", Type: "channel", Section: "B"},
	})
	m.SetSectionsProvider(&fakeProvider{
		ready: true,
		sections: []SectionMeta{
			{ID: "A", Name: "First", Type: "standard"},
			{ID: "B", Name: "Second", Type: "standard"},
		},
	})
	m.SelectByID("C2")
	if m.SelectedID() != "C2" {
		t.Fatalf("initial selection = %s, want C2", m.SelectedID())
	}
	oldCursor := m.cursor

	m.SetSectionsProvider(&fakeProvider{
		ready: true,
		sections: []SectionMeta{
			{ID: "B", Name: "Second", Type: "standard"},
			{ID: "A", Name: "First", Type: "standard"},
		},
	})
	if m.SelectedID() != "C2" {
		t.Fatalf("provider reorder selected %s, want C2", m.SelectedID())
	}
	if m.cursor == oldCursor {
		t.Fatal("provider reorder did not move the selected row's index")
	}
}

func TestCursorIdentityAcrossUpsertInsert(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "one", Type: "channel"},
		{ID: "C2", Name: "two", Type: "channel"},
	})
	m.SelectByID("C2")
	if m.SelectedID() != "C2" {
		t.Fatalf("initial selection = %s, want C2", m.SelectedID())
	}
	oldCursor := m.cursor

	// A newly opened DM sorts ahead of the existing Channels section.
	m.UpsertItem(ChannelItem{ID: "D1", Name: "alice", Type: "dm"})
	if m.items[m.filtered[0]].ID != "D1" {
		t.Fatalf("first item = %s, want newly inserted D1", m.items[m.filtered[0]].ID)
	}
	if m.SelectedID() != "C2" {
		t.Fatalf("upsert insert selected %s, want C2", m.SelectedID())
	}
	if m.cursor == oldCursor {
		t.Fatal("upsert insert did not move the selected row's index")
	}
}

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
