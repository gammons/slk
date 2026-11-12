package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/wintree"
)

// assertFullscreen checks that the last View() drew pane alone across
// the whole terminal (no rail, no other pane), read from the hit-test
// bands it stored, and that the frame is exactly the terminal's size.
func assertFullscreen(t *testing.T, a *App, pane Panel) {
	t.Helper()
	l := a.layout
	drawn := map[Panel]int{
		PanelSidebar:  l.sidebarEnd - l.railWidth,
		PanelMessages: l.msgEnd - l.sidebarEnd,
		PanelThread:   l.threadEnd - l.msgEnd,
	}
	for p, w := range drawn {
		want := 0
		if p == pane {
			want = a.width
		}
		if w != want {
			t.Fatalf("panel %v drawn %d cols, want %d (bands %+v)", p, w, want, *l)
		}
	}
	if l.railWidth != 0 {
		t.Fatalf("rail drawn %d cols in full screen, want 0", l.railWidth)
	}
	lines := strings.Split(a.View().Content, "\n")
	if len(lines) != a.height {
		t.Fatalf("frame has %d lines, want %d", len(lines), a.height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != a.width {
			t.Fatalf("line %d is %d cols, want %d", i, w, a.width)
		}
	}
}

// assertPaned checks that the last View() drew the normal layout: the
// rail is back.
func assertPaned(t *testing.T, a *App) {
	t.Helper()
	if a.layout.railWidth == 0 {
		t.Fatalf("rail not drawn; still full screen? bands %+v", *a.layout)
	}
}

// fullscreenThreadApp is a 200-wide App (wide enough for the thread
// beside the channel) with a thread open and focused.
func fullscreenThreadApp(t *testing.T) *App {
	t.Helper()
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	assertFront(t, a, true, true)
	if a.focusedPanel != PanelThread {
		t.Fatalf("precondition: focus=%v, want PanelThread", a.focusedPanel)
	}
	return a
}

func TestFullscreen_FTogglesTheThread(t *testing.T) {
	a := fullscreenThreadApp(t)

	updateAndRender(t, a, keyPress('f'))
	assertFullscreen(t, a, PanelThread)

	updateAndRender(t, a, keyPress('f'))
	assertPaned(t, a)
	assertFront(t, a, true, true)
	if !a.threadVisible || a.focusedPanel != PanelThread {
		t.Fatalf("after f f: threadVisible=%v focus=%v, want the thread open and focused", a.threadVisible, a.focusedPanel)
	}
}

// Only f leaves full screen; Esc keeps its usual meaning. Closing the
// full-screen thread returns to the panes.
func TestFullscreen_EscClosesAFullscreenThread(t *testing.T) {
	a := fullscreenThreadApp(t)
	updateAndRender(t, a, keyPress('f'))

	updateAndRender(t, a, keyCode(tea.KeyEscape))
	if a.threadVisible {
		t.Fatal("Esc did not close the full-screen thread")
	}
	assertPaned(t, a)
}

func TestFullscreen_EscKeepsAFullscreenChannel(t *testing.T) {
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyPress('f'))

	updateAndRender(t, a, keyCode(tea.KeyEscape))
	assertFullscreen(t, a, PanelMessages)
}

func TestFullscreen_Messages(t *testing.T) {
	a := stackedApp(t, 120)
	updateAndRender(t, a, keyPress('f'))
	assertFullscreen(t, a, PanelMessages)

	if got, want := a.windowBounds(), (wintree.Rect{W: 120, H: 29}); got != want {
		t.Errorf("windowBounds = %+v, want %+v (the full-screen area)", got, want)
	}

	updateAndRender(t, a, keyPress('f'))
	assertPaned(t, a)
	assertFront(t, a, true, false)
}

func TestFullscreen_Sidebar(t *testing.T) {
	a := stackedApp(t, 120)
	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // channel → sidebar
	if a.focusedPanel != PanelSidebar {
		t.Fatalf("precondition: focus=%v, want PanelSidebar", a.focusedPanel)
	}

	updateAndRender(t, a, keyPress('f'))
	assertFullscreen(t, a, PanelSidebar)
	if screen := ansi.Strip(a.View().Content); !strings.Contains(screen, "Threads") || !strings.Contains(screen, "Channels") {
		t.Errorf("full-screen sidebar does not show its rows:\n%s", screen)
	}

	updateAndRender(t, a, keyPress('f'))
	assertPaned(t, a)
	if a.layout.sidebarEnd-a.layout.railWidth != testSidebarW+2 {
		t.Errorf("sidebar band = %d, want its normal %d", a.layout.sidebarEnd-a.layout.railWidth, testSidebarW+2)
	}
}

func TestFullscreen_FollowsFocus(t *testing.T) {
	a := fullscreenThreadApp(t)
	updateAndRender(t, a, keyPress('f'))
	assertFullscreen(t, a, PanelThread)

	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // thread → channel
	assertFullscreen(t, a, PanelMessages)

	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // channel → sidebar
	assertFullscreen(t, a, PanelSidebar)

	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // sidebar → thread
	assertFullscreen(t, a, PanelThread)
}

func TestFullscreen_EnterOnAMessageShowsTheThreadFullscreen(t *testing.T) {
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyPress('f'))
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	if !a.threadVisible || a.focusedPanel != PanelThread {
		t.Fatalf("Enter: threadVisible=%v focus=%v, want the thread open and focused", a.threadVisible, a.focusedPanel)
	}
	assertFullscreen(t, a, PanelThread)
}

func TestFullscreen_ClosingAFullscreenThreadLeavesFullscreen(t *testing.T) {
	a := fullscreenThreadApp(t)
	updateAndRender(t, a, keyPress('f'))

	updateAndRender(t, a, keyPress('q'))
	if a.threadVisible {
		t.Fatal("q did not close the thread")
	}
	assertPaned(t, a)
}

func TestFullscreen_ClosingAThreadBehindAFullscreenChannelStaysFullscreen(t *testing.T) {
	a := fullscreenThreadApp(t)
	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // thread → channel
	updateAndRender(t, a, keyPress('f'))
	assertFullscreen(t, a, PanelMessages)

	updateAndRender(t, a, keyPress('q'))
	if a.threadVisible {
		t.Fatal("q did not close the thread")
	}
	assertFullscreen(t, a, PanelMessages)
}

func TestFullscreen_InsertModeKeepsFullscreen(t *testing.T) {
	a := fullscreenThreadApp(t)
	updateAndRender(t, a, keyPress('f'))
	updateAndRender(t, a, keyPress('i'))
	if a.mode != ModeInsert || a.focusedPanel != PanelThread {
		t.Fatalf("i: mode=%v focus=%v, want insert in the thread", a.mode, a.focusedPanel)
	}
	assertFullscreen(t, a, PanelThread)

	updateAndRender(t, a, keyCode(tea.KeyEscape)) // insert → normal
	if a.mode != ModeNormal {
		t.Fatalf("Esc: mode=%v, want normal", a.mode)
	}
	assertFullscreen(t, a, PanelThread)
}

func TestFullscreen_TypingFInTheComposerTypesF(t *testing.T) {
	a := fullscreenThreadApp(t)
	updateAndRender(t, a, keyPress('i'))
	updateAndRender(t, a, keyPress('f'))
	assertPaned(t, a)
}
