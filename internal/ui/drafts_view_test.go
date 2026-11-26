package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
)

func draftsSidebarApp(t *testing.T) *App {
	t.Helper()
	a := jumpMarkTestApp(t)
	a.now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	a.SetDraftService(core.NewDraftService(core.DraftServiceFuncs{}))
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	a.focusedPanel = PanelSidebar
	a.sidebar.SelectDraftsRow()
	return a
}

func openDraftsView(t *testing.T, a *App) {
	t.Helper()
	_, cmd := a.Update(keyCode(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on the Drafts row produced no command")
	}
	a.Update(cmd())
	if a.view != ViewDrafts || a.focusedPanel != PanelMessages {
		t.Fatalf("view %v focus %v, want the Drafts view with the list focused", a.view, a.focusedPanel)
	}
}

func TestDraftsSidebar_EnterOpensTheList(t *testing.T) {
	a := draftsSidebarApp(t)
	a.drafts.rows["T1"] = listDrafts()
	openDraftsView(t, a)
	if a.sidebar.DraftsCount() != 3 {
		t.Fatalf("badge = %d, want 3", a.sidebar.DraftsCount())
	}
	plain := ansi.Strip(a.View().Content)
	for _, want := range []string{"✎ Drafts", "hello there", "#C2-name", "5m"} {
		if !strings.Contains(plain, want) {
			t.Errorf("frame lacks %q", want)
		}
	}
	// The row is directly under Threads.
	threads := strings.Index(plain, "Threads")
	drafts := strings.Index(plain, "Drafts")
	activity := strings.Index(plain, "Activity")
	if threads < 0 || !(threads < drafts && drafts < activity) {
		t.Fatalf("sidebar order, threads %d drafts %d activity %d", threads, drafts, activity)
	}
}

func TestDraftsView_EmptyState(t *testing.T) {
	a := draftsSidebarApp(t)
	openDraftsView(t, a)
	if !strings.Contains(ansi.Strip(a.View().Content), "no drafts") {
		t.Fatal("an empty list should say no drafts, like the Threads view says no threads")
	}
	if strings.Contains(ansi.Strip(a.sidebar.View(10, 30)), "•") {
		t.Fatal("no drafts should hide the sidebar count")
	}
}

func TestDraftsView_EnterJumpsToTheDraft(t *testing.T) {
	a := draftsSidebarApp(t)
	a.drafts.rows["T1"] = listDrafts()
	openDraftsView(t, a)
	_, cmd := a.Update(keyPress('j'))
	if cmd != nil {
		t.Fatalf("j returned %v, the list should only move", cmd)
	}
	_, cmd = a.Update(keyCode(tea.KeyEnter))
	if a.pendingLinkNav == nil || a.pendingLinkNav.ChannelID != "C3" || a.pendingLinkNav.ThreadTS != "10.0" {
		t.Fatalf("pending nav = %+v, want the second draft (C3 thread 10.0)", a.pendingLinkNav)
	}
	driveJump(a, cmd)
	if a.view != ViewChannels {
		t.Fatalf("after the jump view = %v, want the channel", a.view)
	}
}

func TestDraftsView_DeleteRemovesTheRow(t *testing.T) {
	a := draftsSidebarApp(t)
	a.setDraftServiceForTest(core.DraftServiceFuncs{Delete: func(string, string) error { return nil }})
	a.drafts.rows["T1"] = listDrafts()[:1]
	openDraftsView(t, a)
	a.Update(keyPress('d'))
	if a.mode != ModeConfirm {
		t.Fatalf("mode = %v, want the delete confirm", a.mode)
	}
	_, cmd := a.Update(keyCode(tea.KeyEnter))
	for _, m := range deliveredMsgs[draftDeleteConfirmedMsg](t, cmd) {
		_, del := a.Update(m)
		a.Update(del())
	}
	if a.view != ViewDrafts {
		t.Fatalf("view = %v, want to stay on Drafts", a.view)
	}
	if a.sidebar.DraftsCount() != 0 {
		t.Fatalf("badge = %d, want 0", a.sidebar.DraftsCount())
	}
	if !strings.Contains(ansi.Strip(a.View().Content), "no drafts") {
		t.Fatal("the deleted draft should leave the empty state")
	}
}

func TestDraftsSidebar_SyncOffRefuses(t *testing.T) {
	a := jumpMarkTestApp(t)
	a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a.focusedPanel = PanelSidebar
	a.sidebar.SelectDraftsRow()
	_, cmd := a.Update(keyCode(tea.KeyEnter))
	if cmd != nil {
		a.Update(cmd())
	}
	if a.view != ViewChannels || !strings.Contains(statusbarText(a), "sync_drafts") {
		t.Fatalf("view %v toast %q, want to stay put and say sync is off", a.view, statusbarText(a))
	}
}

func TestDraftsSidebar_CountFollowsALoad(t *testing.T) {
	a := draftsSidebarApp(t)
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: listDrafts()})
	if a.sidebar.DraftsCount() != 3 {
		t.Fatalf("badge = %d, want 3 before the view is opened", a.sidebar.DraftsCount())
	}
	if a.view != ViewChannels {
		t.Fatalf("a load opened the view: %v", a.view)
	}
}
