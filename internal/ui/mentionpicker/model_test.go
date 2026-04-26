package mentionpicker

import "testing"

func TestFilterByDisplayNamePrefix(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
		{ID: "U2", DisplayName: "Bob", Username: "bob"},
		{ID: "U3", DisplayName: "Alicia", Username: "alicia.j"},
	})
	m.Open()
	m.SetQuery("ali")
	if len(m.Filtered()) != 2 {
		t.Fatalf("expected 2 filtered users, got %d", len(m.Filtered()))
	}
	if m.Filtered()[0].ID != "U1" {
		t.Errorf("expected Alice first, got %s", m.Filtered()[0].DisplayName)
	}
}

func TestFilterByUsernamePrefix(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice Smith", Username: "asmith"},
		{ID: "U2", DisplayName: "Bob Jones", Username: "bjones"},
	})
	m.Open()
	m.SetQuery("asm")
	if len(m.Filtered()) != 1 {
		t.Fatalf("expected 1 filtered user, got %d", len(m.Filtered()))
	}
	if m.Filtered()[0].ID != "U1" {
		t.Errorf("expected Alice Smith, got %s", m.Filtered()[0].DisplayName)
	}
}

func TestFilterCaseInsensitive(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.Open()
	m.SetQuery("ALI")
	if len(m.Filtered()) != 1 {
		t.Fatalf("expected 1 filtered user, got %d", len(m.Filtered()))
	}
}

func TestFilterEmptyQueryShowsAll(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
		{ID: "U2", DisplayName: "Bob", Username: "bob"},
		{ID: "U3", DisplayName: "Carol", Username: "carol"},
		{ID: "U4", DisplayName: "Dave", Username: "dave"},
		{ID: "U5", DisplayName: "Eve", Username: "eve"},
		{ID: "U6", DisplayName: "Frank", Username: "frank"},
	})
	m.Open()
	m.SetQuery("")
	// Empty query shows specials (3) + first users up to MaxVisible=5 total
	if len(m.Filtered()) != 5 {
		t.Fatalf("expected 5 filtered users (max), got %d", len(m.Filtered()))
	}
}

func TestFilterSpecialMentions(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Henry", Username: "henry"},
	})
	m.Open()
	m.SetQuery("he")
	filtered := m.Filtered()
	foundHere := false
	for _, u := range filtered {
		if u.ID == "special:here" {
			foundHere = true
		}
	}
	if !foundHere {
		t.Error("expected @here in filtered results")
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("expected not visible initially")
	}
	m.Open()
	if !m.IsVisible() {
		t.Error("expected visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Error("expected not visible after Close")
	}
}

func TestMoveUpDown(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
		{ID: "U2", DisplayName: "Bob", Username: "bob"},
	})
	m.Open()
	m.SetQuery("")
	if m.Selected() != 0 {
		t.Errorf("expected selected=0, got %d", m.Selected())
	}
	m.MoveDown()
	if m.Selected() != 1 {
		t.Errorf("expected selected=1, got %d", m.Selected())
	}
	m.MoveUp()
	if m.Selected() != 0 {
		t.Errorf("expected selected=0, got %d", m.Selected())
	}
	m.MoveUp()
	if m.Selected() != 0 {
		t.Errorf("expected selected=0 (clamped), got %d", m.Selected())
	}
}

func TestSelectReturnsResult(t *testing.T) {
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.Open()
	m.SetQuery("alice")
	result := m.Select()
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.UserID != "U1" {
		t.Errorf("expected U1, got %s", result.UserID)
	}
	if result.DisplayName != "Alice" {
		t.Errorf("expected Alice, got %s", result.DisplayName)
	}
}

func TestSelectEmptyReturnsNil(t *testing.T) {
	m := New()
	m.SetUsers([]User{})
	m.Open()
	m.SetQuery("zzz")
	result := m.Select()
	if result != nil {
		t.Error("expected nil result for empty filtered list")
	}
}
