package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/ui/styles"
)

// selectionTintSGR is the background SGR of the selected-message highlight.
func selectionTintSGR(focused bool) string {
	r, g, b, _ := styles.SelectionTintColor(focused).RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

// rawLineContaining returns the ANSI line whose stripped text contains
// text, so a test can see whether that message row carries the highlight.
func rawLineContaining(view, text string) string {
	raw := strings.Split(view, "\n")
	plain := strings.Split(ansi.Strip(view), "\n")
	for i, line := range plain {
		if strings.Contains(line, text) && i < len(raw) {
			return raw[i]
		}
	}
	return ""
}

// TestInsertModeHidesSelectionHighlight: normal mode highlights the
// message the cursor is on, in the channel pane and the thread pane.
// Insert mode drops that highlight in both and keeps the same cursor.
// Normal mode paints it again on that same message.
func TestInsertModeHidesSelectionHighlight(t *testing.T) {
	pinRenderGlobals(t)
	a := newTestApp(t,
		withSize(200, 40),
		withMessages(testMessageItems(3)...),
		withActiveChannel("C1"),
	)
	focusMessages(t, a)
	focusThreadPanel(t, a)
	// focusThreadPanel moves focus to the thread. Put it back on the
	// channel so this case is "composing in the channel, thread open":
	// the channel highlight is the bright one, the thread's is dimmed,
	// and insert mode has to clear both.
	a.focusedPanel = PanelMessages

	channelSel, ok := a.messagepane.SelectedMessage()
	if !ok {
		t.Fatal("channel pane has no selected message")
	}
	threadSel := a.threadPanel.SelectedReply()
	if threadSel == nil {
		t.Fatal("thread pane has no selected reply")
	}

	_ = a.View()
	channel := a.messagepane.View(24, 70)
	thread := a.threadPanel.View(24, 70)
	channelRow := rawLineContaining(channel, channelSel.Text)
	threadRow := rawLineContaining(thread, threadSel.Text)
	if channelRow == "" || threadRow == "" {
		t.Fatalf("selected text missing from the panes\nchannel %q\nthread %q",
			ansi.Strip(channel), ansi.Strip(thread))
	}
	if !strings.Contains(channelRow, selectionTintSGR(true)) {
		t.Fatalf("normal mode: channel row %q is not highlighted", channelSel.Text)
	}
	if !strings.Contains(threadRow, selectionTintSGR(false)) {
		t.Fatalf("normal mode: unfocused thread row %q is not highlighted", threadSel.Text)
	}

	a.SetMode(ModeInsert)
	_ = a.View()
	if got, _ := a.messagepane.SelectedMessage(); got.TS != channelSel.TS {
		t.Fatalf("insert mode moved the channel cursor from %s to %s", channelSel.TS, got.TS)
	}
	if got := a.threadPanel.SelectedReply(); got == nil || got.TS != threadSel.TS {
		t.Fatalf("insert mode moved the thread cursor from %s", threadSel.TS)
	}
	channel = a.messagepane.View(24, 70)
	thread = a.threadPanel.View(24, 70)
	channelRow = rawLineContaining(channel, channelSel.Text)
	threadRow = rawLineContaining(thread, threadSel.Text)
	if channelRow == "" || threadRow == "" {
		t.Fatal("insert mode dropped the selected message text")
	}
	for _, row := range []string{channelRow, threadRow} {
		if strings.Contains(row, selectionTintSGR(true)) || strings.Contains(row, selectionTintSGR(false)) {
			t.Fatalf("insert mode still highlights a selected row:\n%s", row)
		}
	}

	a.SetMode(ModeNormal)
	a.focusedPanel = PanelMessages
	_ = a.View()
	if got, _ := a.messagepane.SelectedMessage(); got.TS != channelSel.TS {
		t.Fatalf("leaving insert mode moved the channel cursor from %s to %s", channelSel.TS, got.TS)
	}
	if got := a.threadPanel.SelectedReply(); got == nil || got.TS != threadSel.TS {
		t.Fatalf("leaving insert mode moved the thread cursor from %s", threadSel.TS)
	}
	channelRow = rawLineContaining(a.messagepane.View(24, 70), channelSel.Text)
	threadRow = rawLineContaining(a.threadPanel.View(24, 70), threadSel.Text)
	if !strings.Contains(channelRow, selectionTintSGR(true)) {
		t.Fatal("normal mode did not restore the channel highlight on the same message")
	}
	if !strings.Contains(threadRow, selectionTintSGR(false)) {
		t.Fatal("normal mode did not restore the thread highlight on the same reply")
	}
}

// TestInsertModeHidesThreadHighlightWhenComposingThere is the same
// round trip with the thread pane focused, so its own bright highlight
// (not only the dimmed one) disappears while replying and comes back
// on the same reply.
func TestInsertModeHidesThreadHighlightWhenComposingThere(t *testing.T) {
	pinRenderGlobals(t)
	a := newTestApp(t,
		withSize(200, 40),
		withMessages(testMessageItems(3)...),
		withActiveChannel("C1"),
	)
	focusThreadPanel(t, a)
	sel := a.threadPanel.SelectedReply()
	if sel == nil {
		t.Fatal("thread pane has no selected reply")
	}

	_ = a.View()
	row := rawLineContaining(a.threadPanel.View(24, 70), sel.Text)
	if !strings.Contains(row, selectionTintSGR(true)) {
		t.Fatalf("normal mode: focused thread row %q is not highlighted", sel.Text)
	}

	a.SetMode(ModeInsert)
	_ = a.View()
	if got := a.threadPanel.SelectedReply(); got == nil || got.TS != sel.TS {
		t.Fatalf("insert mode moved the thread cursor from %s", sel.TS)
	}
	row = rawLineContaining(a.threadPanel.View(24, 70), sel.Text)
	if strings.Contains(row, selectionTintSGR(true)) || strings.Contains(row, selectionTintSGR(false)) {
		t.Fatalf("insert mode still highlights the thread reply:\n%s", row)
	}

	a.SetMode(ModeNormal)
	a.focusedPanel = PanelThread
	_ = a.View()
	if got := a.threadPanel.SelectedReply(); got == nil || got.TS != sel.TS {
		t.Fatalf("leaving insert mode moved the thread cursor from %s", sel.TS)
	}
	row = rawLineContaining(a.threadPanel.View(24, 70), sel.Text)
	if !strings.Contains(row, selectionTintSGR(true)) {
		t.Fatal("normal mode did not restore the thread highlight on the same reply")
	}
}

// TestInsertModeHidesThreadParentHighlight: the thread parent's highlight
// is drawn on its own path, not from the reply cache. Insert mode hides
// that too, and normal mode puts it back on the parent.
func TestInsertModeHidesThreadParentHighlight(t *testing.T) {
	pinRenderGlobals(t)
	a := newTestApp(t,
		withSize(200, 40),
		withMessages(testMessageItems(3)...),
		withActiveChannel("C1"),
	)
	focusThreadPanel(t, a)
	a.threadPanel.MoveUp()
	a.threadPanel.MoveUp()
	sel := a.threadPanel.SelectedReply()
	if sel == nil || sel.Text != "parent" {
		t.Fatalf("cursor = %v, want the parent", sel)
	}

	_ = a.View()
	row := rawLineContaining(a.threadPanel.View(24, 70), "parent")
	if !strings.Contains(row, selectionTintSGR(true)) {
		t.Fatal("normal mode: thread parent is not highlighted")
	}

	a.SetMode(ModeInsert)
	_ = a.View()
	if got := a.threadPanel.SelectedReply(); got == nil || got.TS != sel.TS {
		t.Fatalf("insert mode moved the cursor off the parent %s", sel.TS)
	}
	row = rawLineContaining(a.threadPanel.View(24, 70), "parent")
	if strings.Contains(row, selectionTintSGR(true)) || strings.Contains(row, selectionTintSGR(false)) {
		t.Fatalf("insert mode still highlights the thread parent:\n%s", row)
	}

	a.SetMode(ModeNormal)
	a.focusedPanel = PanelThread
	_ = a.View()
	row = rawLineContaining(a.threadPanel.View(24, 70), "parent")
	if !strings.Contains(row, selectionTintSGR(true)) {
		t.Fatal("normal mode did not restore the parent highlight")
	}
}
