package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestApp_PasteInsertsIntoCompose verifies that bracketed-paste content
// arriving as tea.PasteMsg is forwarded to the channel compose textarea
// while the App is in insert mode.
func TestApp_PasteInsertsIntoCompose(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	_ = a.compose.Focus()
	_, _ = a.Update(tea.PasteMsg{Content: "hello pasted"})
	if !strings.Contains(a.compose.Value(), "hello pasted") {
		t.Fatalf("expected pasted text in compose; got %q", a.compose.Value())
	}
}

// TestApp_PasteIgnoredInNormalMode pins that paste only flows to compose
// in insert mode — otherwise it would write into a buffer the user can't
// see, and the next 'i' keystroke would surface mystery text.
func TestApp_PasteIgnoredInNormalMode(t *testing.T) {
	a := newTestAppWithMessages(t)
	// Don't enter insert mode.
	_, _ = a.Update(tea.PasteMsg{Content: "hello pasted"})
	if strings.Contains(a.compose.Value(), "hello pasted") {
		t.Fatal("paste must not modify compose when not in insert mode")
	}
}

// TestApp_PasteRoutesToThreadCompose pins the routing decision: when
// the thread panel is open and focused, paste lands in the thread
// compose, not the channel compose.
func TestApp_PasteRoutesToThreadCompose(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.threadVisible = true
	a.focusedPanel = PanelThread
	a.SetMode(ModeInsert)
	_ = a.threadCompose.Focus()
	_, _ = a.Update(tea.PasteMsg{Content: "hello thread"})
	if !strings.Contains(a.threadCompose.Value(), "hello thread") {
		t.Fatalf("expected pasted text in thread compose; got %q", a.threadCompose.Value())
	}
	if strings.Contains(a.compose.Value(), "hello thread") {
		t.Fatal("paste leaked into channel compose when thread was focused")
	}
}
