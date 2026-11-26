package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Dragging text in an app dialog copies it on release, like a message
// pane's selection.
func TestAppViewDragSelectionCopies(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	var copied string
	a.SetClipboardWriter(func(text string) tea.Cmd {
		copied = text
		return nil
	})
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	frame := ansi.Strip(a.View().Content)
	row, col := -1, -1
	for y, line := range strings.Split(frame, "\n") {
		if i := strings.Index(line, "Budget: $400"); i >= 0 {
			row, col = y, ansi.StringWidth(line[:i])
			break
		}
	}
	if row < 0 {
		t.Fatal("the dialog does not draw its body text")
	}

	a.Update(tea.MouseClickMsg{X: col, Y: row, Button: tea.MouseLeft})
	if !a.appView.HasSelection() {
		t.Fatal("the press did not start a selection")
	}
	a.Update(tea.MouseMotionMsg{X: col + len("Budget"), Y: row, Button: tea.MouseLeft})
	_, cmd := a.Update(tea.MouseReleaseMsg{X: col + len("Budget"), Y: row, Button: tea.MouseLeft})
	for _, m := range drainBatch(cmd) {
		if m != nil {
			a.Update(m)
		}
	}
	if copied != "Budget" {
		t.Errorf("clipboard = %q, want the dragged text", copied)
	}
	if status := statusbarText(a); !strings.Contains(status, "Copied") {
		t.Errorf("status bar = %q, want the copy notice", status)
	}
}
