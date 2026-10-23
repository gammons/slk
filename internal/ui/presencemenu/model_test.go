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
	// Filter to the unique "20 minutes" row rather than a hardcoded
	// cursor offset, so adding/removing preset rows above it can't
	// silently change which row this test exercises.
	for _, r := range "20 min" {
		m.HandleKey(string(r))
	}
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSnooze || r.SnoozeMinutes != 20 {
		t.Fatalf("expected 20m snooze, got %+v", r)
	}
}

func TestModel_SelectWFH(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	m.HandleKey("h")
	m.HandleKey("o")
	m.HandleKey("m")
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSetWFH {
		t.Fatalf("expected ActionSetWFH, got %+v", r)
	}
}

func TestModel_SelectVacation(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	m.HandleKey("v")
	m.HandleKey("a")
	m.HandleKey("c")
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionSetVacation {
		t.Fatalf("expected ActionSetVacation, got %+v", r)
	}
}

// TestModel_SetCurrentCustomStatus_MarksMatchingRowCurrent pins
// SetCurrentCustomStatus + OpenWith wiring the checkmark onto whichever
// preset row matches the status text currently applied. Presence and
// custom status are independent in real Slack, so Active stays
// checkmarked too — this is not a single-selection menu.
func TestModel_SetCurrentCustomStatus_MarksMatchingRowCurrent(t *testing.T) {
	m := New()
	m.SetCurrentCustomStatus("Working from home", "🏠")
	m.OpenWith("Workspace", "active", false, time.Time{})

	labels := m.currentItemLabels()
	if !containsLabel(labels, "Working from home") {
		t.Fatalf("expected \"Working from home\" marked current, got %v", labels)
	}
	if !containsLabel(labels, "Active") {
		t.Fatalf("expected \"Active\" to remain marked current alongside the status, got %v", labels)
	}
}

func TestModel_NoCustomStatus_NeitherPresetRowCurrent(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})

	labels := m.currentItemLabels()
	if containsLabel(labels, "Working from home") || containsLabel(labels, "On vacation") {
		t.Fatalf("expected no status preset marked current, got %v", labels)
	}
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestModel_ClearStatusOnlyVisibleWhenStatusSet(t *testing.T) {
	m := New()
	m.OpenWith("Workspace", "active", false, time.Time{})
	if m.hasClearStatusItem() {
		t.Error("Clear status should not appear when no status is set")
	}

	m2 := New()
	m2.SetCurrentCustomStatus("On vacation", "🌴")
	m2.OpenWith("Workspace", "active", false, time.Time{})
	if !m2.hasClearStatusItem() {
		t.Error("Clear status should appear when a status is set")
	}
}

// TestModel_ClearStatusVisibleForEmojiOnlyStatus pins a real bug found
// in review: Slack allows a status with only an emoji, no text (set
// from the real Slack client, say). Clear status must still appear so
// the user has a way to remove it from slk.
func TestModel_ClearStatusVisibleForEmojiOnlyStatus(t *testing.T) {
	m := New()
	m.SetCurrentCustomStatus("", "🎉")
	m.OpenWith("Workspace", "active", false, time.Time{})
	if !m.hasClearStatusItem() {
		t.Error("Clear status should appear for an emoji-only status")
	}
}

func TestModel_ClearStatusAction(t *testing.T) {
	m := New()
	m.SetCurrentCustomStatus("On vacation", "🌴")
	m.OpenWith("Workspace", "active", false, time.Time{})
	m.HandleKey("c")
	m.HandleKey("l")
	m.HandleKey("e")
	m.HandleKey("a")
	m.HandleKey("r")
	r := m.HandleKey("enter")
	if r == nil || r.Action != ActionClearStatus {
		t.Fatalf("expected ActionClearStatus, got %+v", r)
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
	for _, r := range "custom" {
		m.HandleKey(string(r))
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
