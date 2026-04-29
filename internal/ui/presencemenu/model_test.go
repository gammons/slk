package presencemenu

import (
	"testing"
	"time"
)

func TestModel_SelectActive(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	if !m.IsVisible() {
		t.Fatal("expected visible")
	}
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSetActive {
		t.Fatalf("expected ActionSetActive, got %+v", r)
	}
}

func TestModel_SelectAway(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	m.HandleKey("j") // step to Away
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSetAway {
		t.Fatalf("expected ActionSetAway, got %+v", r)
	}
}

func TestModel_SnoozeOption(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	// Active(0), Away(1), Snooze 20m(2) — third item.
	m.HandleKey("j")
	m.HandleKey("j")
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSnooze || r.SnoozeMinutes != 20 {
		t.Fatalf("expected 20m snooze, got %+v", r)
	}
}

func TestModel_EndDNDOnlyVisibleWhenSnoozed(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	if m.hasEndDNDItem() {
		t.Error("End DND should not appear when not snoozed")
	}

	m2 := New()
	m2.OpenWith("Workspace", "active", true, time.Now().Add(time.Hour))
	if !m2.hasEndDNDItem() {
		t.Error("End DND should appear when snoozed")
	}
}

func TestModel_CustomSnoozeAction(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	// Walk down past Active, Away, and the 7 snooze durations to "Snooze custom..."
	for i := 0; i < 9; i++ {
		m.HandleKey("j")
	}
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionCustomSnooze {
		t.Fatalf("expected ActionCustomSnooze, got %+v", r)
	}
}

func TestModel_FilterByQuery(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	m.HandleKey("a")
	m.HandleKey("w")
	m.HandleKey("a")
	m.HandleKey("y")
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSetAway {
		t.Fatalf("expected filtered to Away, got %+v", r)
	}
}

func TestModel_EscapeCloses(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	if r := m.HandleKey("esc"); r != nil {
		t.Errorf("expected nil result on esc, got %+v", r)
	}
	if m.IsVisible() {
		t.Error("expected closed after esc")
	}
}
