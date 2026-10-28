package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/sidebar"
)

func dmPaneApp() *App {
	a := NewApp()
	a.width = 160
	a.height = 40
	a.activeTeamID = "T1"
	a.sidebarVisible = true
	a.focusedPanel = PanelSidebar
	a.sidebar.SetItems([]sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "alice", Type: "dm"},
		{ID: "D2", Name: "bob", Type: "dm"},
	})
	a.SetSidebarDMPane(true)
	return a
}

// TestSidebarDMPane_TwoBoxesFillTheColumn: the sidebar column holds two
// bordered boxes, the second one the Direct Messages, and the frame keeps
// its height.
func TestSidebarDMPane_TwoBoxesFillTheColumn(t *testing.T) {
	a := dmPaneApp()
	lines := strings.Split(ansi.Strip(a.View().Content), "\n")
	if len(lines) != a.height {
		t.Fatalf("frame has %d lines, want %d", len(lines), a.height)
	}
	frame := a.computeFrame()
	mainRows, _, ok := a.sidebarSplit(frame.SidebarWidth, frame.ContentHeight)
	if !ok {
		t.Fatal("sidebarSplit reported no split with DMs and dm_pane on")
	}
	corners := 0
	for _, l := range lines {
		r := []rune(l)
		if len(r) > frame.RailWidth && strings.ContainsRune("╭┏", r[frame.RailWidth]) {
			corners++
		}
	}
	if corners != 2 {
		t.Errorf("sidebar column has %d box tops, want 2", corners)
	}
	if !strings.Contains(lines[mainRows+3], "Direct Messages") {
		t.Errorf("first row of the second box is %q, want the Direct Messages header", lines[mainRows+3])
	}
}

// TestSidebarDMPane_FocusCycle: Tab and Shift+Tab stop at the Direct
// Messages box, between the channel box and the messages pane.
func TestSidebarDMPane_FocusCycle(t *testing.T) {
	a := dmPaneApp()
	_ = a.View()
	type stop struct {
		panel Panel
		inDMs bool
	}
	at := func() stop { return stop{a.focusedPanel, a.sidebar.CursorInDMPane()} }
	if got := at(); got != (stop{PanelSidebar, false}) {
		t.Fatalf("start = %+v, want the channel box", got)
	}
	for i, want := range []stop{{PanelSidebar, true}, {PanelMessages, true}, {PanelSidebar, false}} {
		a.FocusNext()
		_ = a.View()
		if got := at(); got.panel != want.panel || (want.panel == PanelSidebar && got.inDMs != want.inDMs) {
			t.Errorf("Tab #%d: %+v, want %+v", i+1, got, want)
		}
	}
	for i, want := range []stop{{PanelMessages, false}, {PanelSidebar, true}, {PanelSidebar, false}} {
		a.FocusPrev()
		_ = a.View()
		if got := at(); got.panel != want.panel || (want.panel == PanelSidebar && got.inDMs != want.inDMs) {
			t.Errorf("Shift+Tab #%d: %+v, want %+v", i+1, got, want)
		}
	}
}

func TestSidebarDMPane_ClickOpensDM(t *testing.T) {
	a := dmPaneApp()
	_ = a.View()
	frame := a.computeFrame()
	mainRows, _, _ := a.sidebarSplit(frame.SidebarWidth, frame.ContentHeight)
	msgs := drainCmd(reduceMouseClick(a, tea.MouseClickMsg{
		Button: tea.MouseLeft, X: a.layout.RailWidth() + 1, Y: mainRows + 4, // header row + 1
	}))
	sel, ok := firstMsg(msgs).(ChannelSelectedMsg)
	if !ok || sel.ID != "D1" {
		t.Fatalf("click in the DM box produced %#v, want ChannelSelectedMsg for alice", firstMsg(msgs))
	}
}
