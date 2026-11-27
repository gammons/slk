package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/sidebar"
)

// Clicking the Find a DM row opens the workspace people search in its
// single-result variant.
func TestFindDMRowOpensWorkspaceSearch(t *testing.T) {
	a := newTestApp(t, withSize(100, 40))
	a.sidebarVisible = true
	a.SetUserNames(map[string]string{"U1": "Alice Chen"})
	a.SetChannels([]sidebar.ChannelItem{{ID: "D1", Name: "alice", Type: "dm"}})

	row := -1
	for y, line := range strings.Split(a.View().Content, "\n") {
		if strings.Contains(ansi.Strip(line), "Find a DM") {
			row = y
			break
		}
	}
	if row < 0 {
		t.Fatal("frame does not draw the Find a DM row")
	}

	a.Update(tea.MouseClickMsg{
		X: a.layout.RailWidth() + 1, Y: row, Button: tea.MouseLeft,
	})
	if a.mode != ModeNewMessage {
		t.Fatalf("mode = %v, want ModeNewMessage", a.mode)
	}
	if !a.newMessagePicker.IsFinder() {
		t.Error("the picker opened in new-message mode, not finder mode")
	}
}

// A click on a finder result opens that person; the multi-recipient
// picker keeps its space-toggle click.
func TestFinderResultClickActivates(t *testing.T) {
	a := newTestApp(t)
	a.mode = ModeNewMessage
	a.newMessagePicker.SetFinder(true)
	target, ok := a.activeModalClickTarget()
	if !ok {
		t.Fatal("no click target for the finder")
	}
	key, ok := target.activation.(tea.KeyPressMsg)
	if !ok || key.Code != tea.KeyEnter {
		t.Errorf("finder activation = %#v, want Enter", target.activation)
	}

	a.newMessagePicker.SetFinder(false)
	target, _ = a.activeModalClickTarget()
	key, ok = target.activation.(tea.KeyPressMsg)
	if !ok || key.Code != tea.KeySpace {
		t.Errorf("new-message activation = %#v, want Space", target.activation)
	}
}

func TestFinderSeedUsesDND(t *testing.T) {
	a := newTestApp(t)
	a.currentUserID = "SELF"
	a.SetUserNames(map[string]string{"U1": "Alice Chen"})
	a.sidebar.UpdateDNDByUser("U1", true, time.Now().Add(time.Hour))

	a.seedNewMessagePicker()
	for _, u := range a.newMessagePicker.Users() {
		if u.ID == "U1" {
			if u.Presence != "dnd" {
				t.Errorf("presence = %q, want dnd", u.Presence)
			}
			return
		}
	}
	t.Fatal("U1 not in the finder seed")
}
