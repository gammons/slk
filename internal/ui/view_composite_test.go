package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/messages"
)

// TestJoinPanelsHorizontal_MatchesLipgloss verifies the zero-measurement
// row-concat join is byte-identical to lipgloss.JoinHorizontal for
// uniform-width, equal-height panels (the View invariant), and that it
// declines (ok=false) when heights differ so the caller can fall back.
func TestJoinPanelsHorizontal_MatchesLipgloss(t *testing.T) {
	mk := func(w, h int, fill rune) string {
		row := strings.Repeat(string(fill), w)
		rows := make([]string, h)
		for i := range rows {
			rows[i] = row
		}
		return strings.Join(rows, "\n")
	}

	// Equal heights, mixed widths -> must match lipgloss byte-for-byte.
	panels := []string{mk(3, 5, 'a'), mk(10, 5, 'b'), mk(2, 5, 'c')}
	got, ok := joinPanelsHorizontal(panels, 5)
	if !ok {
		t.Fatal("expected ok for equal-height panels")
	}
	want := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	if got != want {
		t.Fatalf("join mismatch:\n got=%q\nwant=%q", got, want)
	}

	// Mismatched height -> decline.
	if _, ok := joinPanelsHorizontal([]string{mk(3, 5, 'a'), mk(3, 4, 'b')}, 5); ok {
		t.Fatal("expected ok=false when a panel has the wrong row count")
	}
}

// buildViewPanels mirrors App.View's panel assembly so the composite
// helpers can be checked against lipgloss on real rendered panels.
func buildViewPanels(a *App) (panels []string, status string, contentHeight, width int) {
	themeVer := int64(0)
	railWidth := 0
	if a.workspacesVisible {
		railWidth = a.workspaceRail.Width()
	}
	frame := a.layout.Compute(a.width, a.height, railWidth, a.sidebar.Width(), a.sidebarVisible, a.threadVisible)
	if frame.RailWidth > 0 {
		panels = append(panels, a.renderRail(frame.RailWidth, frame.ContentHeight, themeVer))
	}
	if a.sidebarVisible {
		panels = append(panels, a.renderSidebar(frame.SidebarWidth, frame.SidebarBorder, frame.ContentHeight, themeVer))
	}
	if s := a.renderMessagesRegion(frame, themeVer, false); s != "" {
		panels = append(panels, s)
	}
	if a.threadVisible && frame.ThreadWidth > 0 {
		panels = append(panels, a.renderThreadRegion(frame, themeVer))
	}
	status = a.renderStatusRow(frame.RailWidth, a.width-frame.RailWidth, themeVer)
	return panels, status, frame.ContentHeight, a.width
}

// TestViewComposite_MatchesLipgloss checks the full screen composite
// (horizontal panel join + vertical status stack) is byte-identical to
// the lipgloss path across layout configurations (sidebar/thread on/off).
func TestViewComposite_MatchesLipgloss(t *testing.T) {
	newApp := func(sidebar, thread bool) *App {
		a := NewApp()
		_, _ = a.Update(tea.WindowSizeMsg{Width: 477, Height: 130})
		msgs := make([]messages.MessageItem, 40)
		for i := range msgs {
			msgs[i] = messages.MessageItem{TS: fmt.Sprintf("%d.0", 1700000000+i), UserName: "alice", UserID: "U1", Text: "hello world", Timestamp: "10:30 AM"}
		}
		a.messagepane.SetMessages(msgs)
		a.sidebarVisible = sidebar
		if thread {
			a.threadVisible = true
			a.threadPanel.SetThread(msgs[0], nil, "C1", msgs[0].TS)
		}
		a.focusedPanel = PanelMessages
		a.SetMode(ModeNormal)
		_ = a.View()
		return a
	}

	for _, sidebar := range []bool{true, false} {
		for _, thread := range []bool{false, true} {
			a := newApp(sidebar, thread)
			panels, status, ch, _ := buildViewPanels(a)

			gotH, ok := joinPanelsHorizontal(panels, ch)
			if !ok {
				t.Fatalf("[sidebar=%v thread=%v] fast join declined unexpectedly", sidebar, thread)
			}
			wantH := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
			if gotH != wantH {
				t.Fatalf("[sidebar=%v thread=%v] horizontal join differs from lipgloss", sidebar, thread)
			}

			gotV := stackContentStatus(gotH, status)
			wantV := lipgloss.JoinVertical(lipgloss.Left, wantH, status)
			if gotV != wantV {
				t.Fatalf("[sidebar=%v thread=%v] vertical stack differs from lipgloss", sidebar, thread)
			}
		}
	}
}
