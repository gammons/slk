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
	if a.pendingLinkNav == nil || a.pendingLinkNav.ChannelID != "C2" {
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

// Deleting a non-writable draft (scheduled, file-attached, …) through the
// :drafts dialog clears hands-off at its key, the same as a list that
// stops mentioning it.
func TestDraftList_DeletingTheNonWritableBlockerClearsHandsOff(t *testing.T) {
	a, calls, _ := draftSyncApp(t, core.ErrDraftHandsOff)
	// Mark C1 hands-off the way a real save attempt would.
	a.compose.SetValue("one")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	c1 := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	if !a.drafts.handsOff[c1] {
		t.Fatalf("setup: C1 is not hands-off (toast=%q)", statusbarText(a))
	}
	blocker := core.Draft{ID: "DrBlock", Key: c1, Scheduled: time.Unix(1_900_000_000, 0)}
	a.drafts.rows["T1"] = []core.Draft{blocker}
	a.setDraftServiceForTest(core.DraftServiceFuncs{
		Delete: func(teamID, id string) error {
			*calls = append(*calls, draftCall{op: "delete " + id})
			return nil
		},
	})
	a.Update(draftDeletedMsg{teamID: "T1", draft: blocker})
	if a.drafts.handsOff[c1] {
		t.Error("hands-off not cleared after its non-writable blocker was deleted")
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
	if a.pendingLinkNav == nil || a.pendingLinkNav.ChannelID != "C3" {
		t.Errorf("pending nav = %+v, want C3 (the first row now shown)", a.pendingLinkNav)
	}
}

// draftsClickApp opens the :drafts dialog over an 80x24 App with two
// drafts, and returns the box's top-left screen cell. Mirrors
// marksClickApp (marks_mode_test.go).
func draftsClickApp(t *testing.T) (*App, int, int) {
	t.Helper()
	a := jumpMarkTestApp(t)
	a.SetDraftService(core.NewDraftService(core.DraftServiceFuncs{}))
	a.width, a.height = 80, 24
	a.drafts.rows["T1"] = listDrafts()
	executeCommand(a, "drafts")
	if a.mode != ModeDrafts {
		t.Fatalf("mode = %v, want ModeDrafts", a.mode)
	}
	w, h := a.draftList.BoxSize(a.width, a.height)
	return a, max((a.width-w)/2, 0), max((a.height-h)/2, 0)
}

// Before ModeDrafts had a click target, reduceModalClick fell back to
// treating any click, even one inside the box, as Esc and dismissing the
// dialog. A click that lands inside the box but off a row (the title)
// must now be a no-op that leaves the dialog open, exactly like the
// marks overlay (TestMarksOverlay_ClickInsideOffRowKeepsListOpen).
func TestDraftsOverlay_ClickInsideOffRowKeepsListOpen(t *testing.T) {
	a, x, y := draftsClickApp(t)
	// Box-local row 2 is the title (border, padding, title).
	if _, cmd := a.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x + 3, Y: y + 2}); cmd != nil {
		t.Errorf("clicking the title returned a cmd: %v", cmd)
	}
	if a.mode != ModeDrafts || !a.draftList.IsVisible() {
		t.Errorf("the list must stay open: mode=%v visible=%v", a.mode, a.draftList.IsVisible())
	}
}

// A click directly on a row selects it and acts on it exactly as Enter
// would (the same activation path), rather than being swallowed as the
// blanket Esc the missing click target used to produce.
func TestDraftsOverlay_ClickRowJumps(t *testing.T) {
	a, x, y := draftsClickApp(t)
	// The list starts at box-local row 4: border, padding, title, blank.
	_, cmd := a.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x + 3, Y: y + 4 + 1})
	if cmd == nil {
		t.Fatal("clicking a row must act on it")
	}
	cmd()
	if a.pendingLinkNav == nil || a.pendingLinkNav.ChannelID != "C3" {
		t.Errorf("pending nav = %+v, want a jump to C3 (the second row)", a.pendingLinkNav)
	}
	if a.mode != ModeNormal || a.draftList.IsVisible() {
		t.Errorf("the jump must close the list: mode=%v visible=%v", a.mode, a.draftList.IsVisible())
	}
}
