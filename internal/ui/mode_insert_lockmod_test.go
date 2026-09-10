package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestHandleInsertMode_CtrlUIgnoresNumLock guards the regression: with
// NumLock's Mod bit set, Ctrl+U must still clear the whole compose box
// rather than falling through to the textarea's own default binding
// (delete-to-line-start).
func TestHandleInsertMode_CtrlUIgnoresNumLock(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	_ = a.compose.Focus()
	a.compose.SetValue("some draft text")

	_ = a.handleKey(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl | tea.ModNumLock})

	if a.compose.Value() != "" {
		t.Fatalf("want compose fully cleared, got %q", a.compose.Value())
	}
}

// TestHandleInsertMode_PasteIgnoresCapsLock mirrors the above for
// Ctrl+V: with CapsLock set, it must still dispatch to smartPaste
// (inserting the clipboard's text) rather than falling through to the
// textarea's own default paste binding.
func TestHandleInsertMode_PasteIgnoresCapsLock(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	a.SetClipboardAvailable(true)
	a.SetClipboardReader(fakeClipboard(nil, []byte("pasted via ctrl+v")))
	_ = a.compose.Focus()

	_ = a.handleKey(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl | tea.ModCapsLock})

	if !strings.Contains(a.compose.Value(), "pasted via ctrl+v") {
		t.Fatalf("want clipboard text pasted, got compose value %q", a.compose.Value())
	}
}
