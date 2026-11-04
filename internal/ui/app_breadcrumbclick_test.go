package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestMouseClick_OnBreadcrumbChannelClosesThread pins the up-level
// navigation: with a thread open, a click inside the breadcrumb's channel
// label closes the thread and returns to the channel, the mouse
// equivalent of the hint's esc. Clicking the separator or the Thread
// text does not.
func TestMouseClick_OnBreadcrumbChannelClosesThread(t *testing.T) {
	a := newTestApp(t, append(normalOpts(), withWindowSize(200, 30))...)
	focusMessages(t, a)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	if !a.threadVisible {
		t.Fatal("precondition: Enter did not open the thread")
	}
	_ = a.View()
	// The breadcrumb row is the thread pane's first row under its top
	// border: screen y=1. The channel label starts at the pane's left
	// border: pane-local col 0, so x = msgEnd+1 lands on the glyph.
	x := a.layout.msgEnd + 3 // inside "# general"
	_, cmd := a.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	_ = cmd
	if a.threadVisible {
		t.Error("thread still visible after clicking the breadcrumb channel label")
	}
}
func TestMouseClick_BreadcrumbLeavesNormalMode(t *testing.T) {
	a := newTestApp(t, append(normalOpts(), withWindowSize(200, 30))...)
	focusMessages(t, a)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	// Composing a thread reply: insert mode with the thread compose focused.
	a.SetMode(ModeInsert)
	_ = a.View()
	_, cmd := a.Update(tea.MouseClickMsg{X: a.layout.msgEnd + 3, Y: 1, Button: tea.MouseLeft})
	_ = cmd
	if a.threadVisible {
		t.Fatal("precondition: click did not close the thread")
	}
	if a.mode != ModeNormal {
		t.Errorf("mode = %v; want ModeNormal after the click closed the thread", a.mode)
	}
}

// TestMouseClick_BreadcrumbBorderDoesNotClose pins the coordinate
// frame: a click on the pane border left of the label (pane-local col
// -1) must not close the thread.
func TestMouseClick_BreadcrumbBorderDoesNotClose(t *testing.T) {
	a := newTestApp(t, append(normalOpts(), withWindowSize(200, 30))...)
	focusMessages(t, a)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	_ = a.View()
	// x = msgEnd lands on the pane's left border: px = -1, py = 0.
	_, cmd := a.Update(tea.MouseClickMsg{X: a.layout.msgEnd, Y: 1, Button: tea.MouseLeft})
	_ = cmd
	if !a.threadVisible {
		t.Error("border click closed the thread; the span must not accept negative columns")
	}
}

// TestMouseClick_BreadcrumbUploadInFlightRefused pins the upload guard:
// while an upload is in flight, clicking the breadcrumb's channel label
// is refused with the standard toast, leaving the thread open and the
// mode untouched - moving SetMode above the guard would drop the user
// out of insert mode with the thread still open.
func TestMouseClick_BreadcrumbUploadInFlightRefused(t *testing.T) {
	a, _, _ := uploadGuardApp(t)
	// uploadGuardApp leaves the App in normal mode; compose a thread
	// reply first so a guardless click WOULD move the mode, and the
	// assertion can catch it.
	a.SetMode(ModeInsert)
	mode := a.mode
	_ = a.View()
	_, cmd := a.Update(tea.MouseClickMsg{X: a.layout.msgEnd + 3, Y: 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Error("click during an upload must toast, not fall through to selection")
	}
	if !a.threadVisible {
		t.Error("thread closed during an upload; the guard must refuse the whole sequence")
	}
	if a.mode != mode {
		t.Errorf("mode moved from %v to %v during an upload; the guard must leave it untouched", mode, a.mode)
	}
}
