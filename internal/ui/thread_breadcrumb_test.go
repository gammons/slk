package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/cache"
)

// updateAndRender drives msg through the real Update chain and renders
// one frame, as the bubbletea runtime does after every message.
func updateAndRender(t *testing.T, a *App, msg tea.Msg) {
	t.Helper()
	_, _ = a.Update(msg)
	_ = a.View()
}

func TestThreadBreadcrumb_EnterNamesTheSidebarChannel(t *testing.T) {
	a := newTestApp(t, append(normalOpts(), withWindowSize(200, 30))...)
	focusMessages(t, a)
	updateAndRender(t, a, keyCode(tea.KeyEnter))

	screen := ansi.Strip(a.View().Content)
	if !strings.Contains(screen, "# general › Thread from alice") {
		t.Errorf("thread header does not name the channel and author:\n%s", screen)
	}
}

func TestThreadBreadcrumb_ThreadsViewUsesSummaryChannelType(t *testing.T) {
	// C2 is a plain "channel" in the sidebar; the summary says private.
	// The summary is the fresher source for a thread opened from the
	// list, so its type must win.
	sums := []cache.ThreadSummary{{
		ChannelID: "C2", ChannelName: "random", ChannelType: "private",
		ThreadTS: "50.0", ParentTS: "50.0", ParentUserID: "U1",
		ParentText: "hello", ReplyCount: 1,
	}}
	a := newTestApp(t, append(normalOpts(), withWindowSize(200, 30), withThreadsView(sums))...)
	updateAndRender(t, a, ThreadsViewActivatedMsg{})

	if !a.threadVisible {
		t.Fatal("precondition: activating the Threads view did not open the selected thread")
	}
	screen := ansi.Strip(a.View().Content)
	if !strings.Contains(screen, "◆ random › Thread") {
		t.Errorf("thread header does not show the summary's private glyph:\n%s", screen)
	}
}
