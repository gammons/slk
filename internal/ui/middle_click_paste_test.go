package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"golang.design/x/clipboard"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/wintree"
)

// clipboardApp is an App whose channel composer is on screen and whose
// clipboard holds text, for the middle-click paste tests.
func clipboardApp(t *testing.T, text string, opts ...testOpt) *App {
	t.Helper()
	base := []testOpt{withSize(120, 60), withActiveChannel("C1"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice", Text: "hello",
	})}
	a := newTestApp(t, append(base, opts...)...)
	a.clipboardAvailable = true
	a.setClipboardReaderForTest(func(f clipboard.Format) []byte {
		if f == clipboard.FmtText {
			return []byte(text)
		}
		return nil
	})
	_ = a.View()
	_ = a.View()
	return a
}

// threadClipboardApp also has the thread panel open with its own
// composer on screen.
func threadClipboardApp(t *testing.T, text string) *App {
	t.Helper()
	a := newTestApp(t, withWindowSize(200, 60), withActiveChannel("C1"), withMessages(messages.MessageItem{
		TS: "1.0", UserName: "alice", Text: "parent",
	}))
	a.clipboardAvailable = true
	a.setClipboardReaderForTest(func(f clipboard.Format) []byte {
		if f == clipboard.FmtText {
			return []byte(text)
		}
		return nil
	})
	a.openThreadPanel(messages.MessageItem{TS: "1.0"}, "C1", "1.0")
	_ = a.View()
	_ = a.View()
	return a
}

// threadMiddleClickPoint is a frame point inside the thread composer.
func threadMiddleClickPoint(t *testing.T, a *App) (x, y int) {
	t.Helper()
	if !a.threadVisible || a.layout.threadComposeTop <= 0 {
		t.Fatal("thread compose region not recorded after View()")
	}
	return a.layout.msgEnd + 5, 1 + a.threadPanel.ChromeHeight() + a.layout.threadComposeTop + 1
}

// middleClickPoint returns a frame point inside the channel compose box.
func middleClickPoint(t *testing.T, a *App) (x, y int) {
	t.Helper()
	top := a.layout.msgComposeTop
	if top <= 0 {
		t.Fatal("msgComposeTop not recorded after View()")
	}
	return a.layout.sidebarEnd + 5, 1 + a.messagepane.ChromeHeight() + top + 1
}

// A drag selection is copied on release, so a middle click pastes it
// into the composer the cursor is over, exactly as the terminal's own
// middle-click paste would.
func TestMiddleClickOnComposePastes(t *testing.T) {
	a := clipboardApp(t, "from the clipboard")
	x, y := middleClickPoint(t, a)

	a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if a.mode != ModeInsert || a.focusedPanel != PanelMessages {
		t.Fatalf("mode = %v panel = %v, want insert on the channel composer", a.mode, a.focusedPanel)
	}
	if got := a.compose.Value(); !strings.Contains(got, "from the clipboard") {
		t.Errorf("compose = %q, want the pasted text", got)
	}
}

func TestMiddleClickOnComposeAppendsInInsertMode(t *testing.T) {
	a := clipboardApp(t, " world")
	_ = a.beginInsertMode(false)
	a.compose.SetValue("hello")

	x, y := middleClickPoint(t, a)
	a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if got := a.compose.Value(); got != "hello world" {
		t.Errorf("compose = %q, want the paste appended", got)
	}
}

// Middle-clicking a message has no text field to paste into.
func TestMiddleClickOnMessageDoesNotPaste(t *testing.T) {
	a := clipboardApp(t, "from the clipboard")
	x := a.layout.sidebarEnd + 5
	y := 1 + a.messagepane.ChromeHeight() + 1 // first message row

	a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if a.mode == ModeInsert {
		t.Error("middle-clicking a message entered insert mode")
	}
	if got := a.compose.Value(); got != "" {
		t.Errorf("compose = %q, want it untouched", got)
	}
}

// Middle-clicking the sidebar or the rail does not paste either.
func TestMiddleClickOutsideAPaneDoesNotPaste(t *testing.T) {
	a := clipboardApp(t, "from the clipboard")
	a.Update(tea.MouseClickMsg{X: 1, Y: 5, Button: tea.MouseMiddle})
	if a.mode == ModeInsert || a.compose.Value() != "" {
		t.Errorf("mode = %v compose = %q, want an ignored middle click", a.mode, a.compose.Value())
	}
}

func TestMiddleClickRefusedWhileUploading(t *testing.T) {
	a := clipboardApp(t, "from the clipboard")
	a.compose.SetUploading(true)
	x, y := middleClickPoint(t, a)
	a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if a.mode == ModeInsert || a.compose.Value() != "" {
		t.Errorf("mode = %v compose = %q, want an ignored click during upload", a.mode, a.compose.Value())
	}
}

func TestMiddleClickRefusedWhileEitherComposerUploads(t *testing.T) {
	a := threadClipboardApp(t, "from the clipboard")
	a.threadCompose.SetUploading(true)
	x, y := middleClickPoint(t, a)
	a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if a.compose.Value() != "" {
		t.Errorf("compose = %q, want an ignored click while the thread uploads", a.compose.Value())
	}
}

// With split windows only the focused one has a composer; a click in
// the region must not paste into the wrong draft.
func TestMiddleClickSplitWindowIsIgnored(t *testing.T) {
	a := clipboardApp(t, "from the clipboard", withSize(160, 60))
	if cmd := a.splitWindow(wintree.SplitSideBySide); cmd != nil {
		t.Fatalf("split refused: %#v", cmd())
	}
	_ = a.View()
	if a.wins.Len() <= 1 {
		t.Fatal("precondition: the window tree has no split")
	}
	a.Update(tea.MouseClickMsg{X: a.layout.sidebarEnd + 5, Y: 10, Button: tea.MouseMiddle})
	if a.mode == ModeInsert || a.compose.Value() != "" {
		t.Errorf("mode = %v compose = %q, want an ignored split-window click", a.mode, a.compose.Value())
	}
}

func TestMiddleClickOnThreadComposePastes(t *testing.T) {
	a := threadClipboardApp(t, "thread clipboard")
	x, y := threadMiddleClickPoint(t, a)
	a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if a.focusedPanel != PanelThread || a.mode != ModeInsert {
		t.Errorf("panel = %v mode = %v, want the thread composer", a.focusedPanel, a.mode)
	}
	if got := a.threadCompose.Value(); !strings.Contains(got, "thread clipboard") {
		t.Errorf("thread compose = %q, want the pasted text", got)
	}
	if a.compose.Value() != "" {
		t.Errorf("channel compose = %q, want it untouched", a.compose.Value())
	}
}

// A disabled channel composer must not let the click fall through into
// the thread draft the user is already in.
func TestMiddleClickRefusedChannelLeavesThreadDraftAlone(t *testing.T) {
	a := threadClipboardApp(t, "from the clipboard")
	_ = a.beginInsertMode(true)
	a.composeDisabled = true
	x, y := middleClickPoint(t, a)
	a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseMiddle})
	if a.focusedPanel != PanelThread {
		t.Errorf("focusedPanel = %v, want the thread to keep focus", a.focusedPanel)
	}
	if a.threadCompose.Value() != "" {
		t.Errorf("thread compose = %q, want the refused click to paste nothing", a.threadCompose.Value())
	}
}
