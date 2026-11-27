package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/emoji"
)

func openEmojiPopup(t *testing.T) (*App, int) {
	t.Helper()
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.compose.SetEmojiEntries([]emoji.EmojiEntry{
		{Name: "rock", Display: "🪨"},
		{Name: "rocket", Display: "🚀"},
		{Name: "rose", Display: "🌹"},
	})
	_ = a.compose.Focus()
	for _, r := range ":ro" {
		a.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !a.compose.IsEmojiActive() {
		t.Fatal("precondition: emoji popup did not open")
	}
	_ = a.View()
	top, rows := a.layout.MsgPopup()
	if rows == 0 {
		t.Fatal("precondition: popup geometry was not recorded")
	}
	return a, top
}

func TestEmojiPopupClickInsertsRow(t *testing.T) {
	a, top := openEmojiPopup(t)
	a.Update(tea.MouseClickMsg{
		X: a.layout.SidebarEnd() + 2, Y: top + 2, Button: tea.MouseLeft,
	})
	if got := a.compose.Value(); !strings.Contains(got, ":rock: ") {
		t.Errorf("value = %q, want the clicked :rock:", got)
	}
	if a.compose.IsEmojiActive() {
		t.Error("popup stayed open after the click")
	}
}

func TestEmojiPopupWheelMovesHighlight(t *testing.T) {
	a, top := openEmojiPopup(t)
	a.Update(tea.MouseWheelMsg{
		X: a.layout.SidebarEnd() + 2, Y: top + 2, Button: tea.MouseWheelDown,
	})
	a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := a.compose.Value(); !strings.Contains(got, ":rocket: ") {
		t.Errorf("value = %q, want the second match :rocket:", got)
	}
}
