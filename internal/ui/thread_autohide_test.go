package ui

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
)

func appWithOpenThread(t *testing.T, width int) *App {
	t.Helper()
	a := NewApp()
	a.width, a.height = width, 40
	a.activeTeamID, a.activeChannelID = "T1", "C1"
	a.sidebar.SetItems([]sidebar.ChannelItem{{ID: "C1", Name: "newsletter", Type: "channel"}})
	a.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1750000000.000100", UserName: "Michał", Text: "Cześć", ReplyCount: 1},
	})
	a.focusedPanel = PanelMessages
	_ = a.View()
	a.handleEnter() // opens the thread
	return a
}

// A thread that opens and then gets auto-hidden for want of width used
// to leave the status bar reading "> Thread" with no thread panel on
// screen. The user was told they were in a thread and had nothing to
// look at, with no way to tell a failed open from a panel that simply
// did not fit.
func TestThreadAutoHidden_StatusBarStopsClaimingThread(t *testing.T) {
	a := appWithOpenThread(t, 100) // too narrow for the 35% share at minThreadW

	if !a.threadVisible {
		t.Fatal("precondition: thread did not open")
	}
	out := a.View().Content

	if a.threadVisible {
		t.Error("thread still marked visible after the panel was auto-hidden")
	}
	if strings.Contains(out, "> Thread") {
		t.Errorf("status bar still claims the thread is open:\n%s", lastLine(out))
	}
	if !strings.Contains(out, "nie mieści") {
		t.Errorf("no explanation shown for the missing panel:\n%s", lastLine(out))
	}
	if a.focusedPanel == PanelThread {
		t.Error("focus left on a panel that is not rendered")
	}
}

func TestThreadAutoHidden_WideTerminalKeepsThePanel(t *testing.T) {
	a := appWithOpenThread(t, 160)

	_ = a.View()
	if !a.threadVisible {
		t.Error("thread panel hidden on a terminal wide enough for it")
	}
	if !strings.Contains(a.View().Content, "Thread") {
		t.Error("status bar does not show the open thread")
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
