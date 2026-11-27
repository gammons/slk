package newmessagepicker

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func openFinder(t *testing.T) Model {
	t.Helper()
	m := New()
	m.SetUsers([]User{
		{ID: "U1", DisplayName: "Alice Chen", Username: "alice", Presence: "active"},
		{ID: "U2", DisplayName: "Bob Singh", Username: "bob", Presence: "away"},
	})
	m.SetFinder(true)
	m.Open()
	return m
}

func TestFinderTitleAndPresenceRow(t *testing.T) {
	m := openFinder(t)
	view := ansi.Strip(m.View(80))
	if !strings.Contains(view, "Find a DM") {
		t.Errorf("view does not say Find a DM: %q", view)
	}
	if !strings.Contains(view, "A") || !strings.Contains(view, "●") {
		t.Errorf("view lacks the active person's initial/presence: %q", view)
	}
	if strings.Contains(view, "0 / 8") {
		t.Errorf("finder view still shows the recipient counter: %q", view)
	}
}

func TestFinderEnterSubmitsHighlightedPerson(t *testing.T) {
	m := openFinder(t)
	for _, r := range "bob" {
		m.HandleKey(string(r))
	}
	result := m.HandleKey("enter")
	if result == nil || len(result.UserIDs) != 1 || result.UserIDs[0] != "U2" {
		t.Fatalf("enter = %+v, want U2", result)
	}
}

// In finder mode a space is part of the query, not a pill toggle.
func TestFinderSpaceEntersQuery(t *testing.T) {
	m := openFinder(t)
	m.HandleKey("a")
	m.HandleKey(" ")
	m.HandleKey("l")
	if m.query != "a l" {
		t.Errorf("query = %q, want %q", m.query, "a l")
	}
	if len(m.selected) != 0 {
		t.Errorf("space selected a person: %+v", m.selected)
	}
}

func TestNewMessageModeStillTogglesOnSpace(t *testing.T) {
	src := openFinder(t)
	m := New()
	m.SetUsers(src.Users())
	m.SetFinder(false)
	m.Open()
	m.HandleKey(" ")
	if len(m.selected) != 1 {
		t.Errorf("space selected %d people, want 1", len(m.selected))
	}
}
