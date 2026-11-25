package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
)

func listDrafts() []core.Draft {
	return []core.Draft{
		{ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C2"}, Text: "hello there", Updated: time.Unix(1_800_000_000-300, 0)},
		{ID: "Dr2", Key: core.DraftKey{TeamID: "T1", ChannelID: "C3", ThreadTS: "10.0"}, Text: "deploy", Updated: time.Unix(1_800_000_000-7200, 0),
			Scheduled: time.Date(2027, 12, 1, 9, 0, 0, 0, time.Local)},
		{ID: "Dr3", Key: core.DraftKey{TeamID: "T1"}, Text: "nobody yet", Saved: true, Updated: time.Unix(1_700_000_000, 0)},
	}
}

func TestCmdDrafts_OffRefuses(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	executeCommand(a, "drafts")
	if a.mode == ModeDrafts || !strings.Contains(statusbarText(a), "sync_drafts") {
		t.Errorf("mode %v, toast %q", a.mode, statusbarText(a))
	}
}

func TestCmdDrafts_OpensAndReloads(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.drafts.rows["T1"] = listDrafts()
	loads := 0
	a.setDraftServiceForTest(core.DraftServiceFuncs{Load: func(string) ([]core.Draft, error) { loads++; return nil, nil }})
	cmd := executeCommand(a, "drafts")
	if a.mode != ModeDrafts || !a.draftList.IsVisible() {
		t.Fatalf("mode %v visible %v", a.mode, a.draftList.IsVisible())
	}
	if cmd == nil {
		t.Fatal("no reload command")
	}
	cmd()
	if loads != 1 {
		t.Errorf("loads = %d", loads)
	}
	v := ansi.Strip(a.draftList.View(a.width, a.height))
	// Dr3 is saved but has no recipient: its label says so, no marker.
	for _, want := range []string{"hello there", "5m", "2h", "scheduled Dec 1 9:00 AM", "(no recipient)"} {
		if !strings.Contains(v, want) {
			t.Errorf("list lacks %q", want)
		}
	}
}

func TestDraftList_EnterJumps(t *testing.T) {
	a := jumpMarkTestApp(t)
	a.SetDraftService(core.NewDraftService(core.DraftServiceFuncs{}))
	a.drafts.rows["T1"] = listDrafts()
	executeCommand(a, "drafts")
	a.Update(keyCode(tea.KeyEnter))
	if a.mode != ModeNormal || a.draftList.IsVisible() {
		t.Errorf("mode %v, list visible %v", a.mode, a.draftList.IsVisible())
	}
	if a.pendingLinkNav == nil || a.pendingLinkNav.Location.ChannelID != "C2" {
		t.Errorf("pending nav = %+v, want C2", a.pendingLinkNav)
	}
}

func TestDraftList_NoRecipientDoesNotJump(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.drafts.rows["T1"] = listDrafts()[2:]
	executeCommand(a, "drafts")
	a.Update(keyCode(tea.KeyEnter))
	if !strings.Contains(statusbarText(a), "no recipient") {
		t.Errorf("toast = %q", statusbarText(a))
	}
}

func TestDraftList_DeleteConfirmsThenDeletesAndClearsTheSlot(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: []core.Draft{
		{ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "on screen"},
	}})
	executeCommand(a, "drafts")
	a.Update(keyPress('d'))
	if a.mode != ModeConfirm {
		t.Fatalf("mode = %v, want ModeConfirm", a.mode)
	}
	_, cmd := a.Update(keyCode(tea.KeyEnter)) // confirm
	for _, m := range deliveredMsgs[draftDeleteConfirmedMsg](t, cmd) {
		_, del := a.Update(m)
		a.Update(del())
	}
	if ops(*calls) != `delete Dr1 / ""` {
		t.Errorf("calls = %s", ops(*calls))
	}
	if a.compose.Value() != "" || len(a.drafts.rows["T1"]) != 0 {
		t.Errorf("value %q rows %v", a.compose.Value(), a.drafts.rows["T1"])
	}
	if !strings.Contains(statusbarText(a), "Draft deleted") {
		t.Errorf("toast = %q", statusbarText(a))
	}
}

// A reload while the list is open must not shift what Enter acts on.
func TestDraftList_ActsOnTheRowShown(t *testing.T) {
	a := jumpMarkTestApp(t)
	a.SetDraftService(core.NewDraftService(core.DraftServiceFuncs{}))
	a.drafts.rows["T1"] = listDrafts()
	executeCommand(a, "drafts")
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: listDrafts()[1:]}) // Dr1 gone
	a.Update(keyCode(tea.KeyEnter))
	if a.pendingLinkNav == nil || a.pendingLinkNav.Location.ChannelID != "C3" {
		t.Errorf("pending nav = %+v, want C3 (the first row now shown)", a.pendingLinkNav)
	}
}
