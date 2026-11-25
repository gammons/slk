package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
)

type draftCall struct {
	op     string
	key    core.DraftKey
	text   string
	edited time.Time
}

// draftSyncApp is an App on T1/C1 with draft sync wired to a recording
// service and a clock the test moves. saveErr answers every Save.
// T1's drafts have been listed (empty), so the comparison runs.
func draftSyncApp(t *testing.T, saveErr error) (*App, *[]draftCall, *time.Time) {
	t.Helper()
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
	a.compose.SetDraftContext("T1", "C1", "")
	clock := time.Unix(1_800_000_000, 0)
	a.now = func() time.Time { return clock }
	var calls []draftCall
	a.setDraftServiceForTest(core.DraftServiceFuncs{
		Save: func(k core.DraftKey, text string, edited time.Time) (core.DraftSaveResult, error) {
			calls = append(calls, draftCall{"save", k, text, edited})
			if saveErr != nil {
				return core.DraftSaveResult{}, saveErr
			}
			return core.DraftSaveResult{Draft: core.Draft{ID: "Dr-" + k.ChannelID, Key: k, Text: text}}, nil
		},
		Discard: func(k core.DraftKey) error {
			calls = append(calls, draftCall{op: "discard", key: k})
			return nil
		},
		Delete: func(teamID, id string) error {
			calls = append(calls, draftCall{op: "delete " + id})
			return nil
		},
	})
	a.Update(draftsLoadedMsg{teamID: "T1"})
	return a, &calls, &clock
}

// tick runs one comparison at the App's clock, runs its saves and feeds
// their answers back through Update.
func tick(t *testing.T, a *App) {
	t.Helper()
	for _, c := range a.syncDrafts(a.now()) {
		a.Update(c())
	}
}

func ops(calls []draftCall) string {
	var s []string
	for _, c := range calls {
		s = append(s, fmt.Sprintf("%s %s/%s %q", c.op, c.key.ChannelID, c.key.ThreadTS, c.text))
	}
	return strings.Join(s, "; ")
}

func TestDraftSync_VisibleTextSavedOnceItStaysTheSame(t *testing.T) {
	a, calls, clock := draftSyncApp(t, nil)
	a.compose.SetValue("hel")
	tick(t, a)
	first := *clock
	a.compose.SetValue("hello")
	*clock = clock.Add(2 * time.Second)
	tick(t, a)
	if len(*calls) != 0 {
		t.Fatalf("saved while typing: %s", ops(*calls))
	}
	seen := *clock
	*clock = clock.Add(2 * time.Second)
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Fatalf("calls = %s", got)
	}
	if !(*calls)[0].edited.Equal(seen) || seen.Equal(first) {
		t.Errorf("edited = %v, want %v (when the text was first seen)", (*calls)[0].edited, seen)
	}
	*clock = clock.Add(2 * time.Second)
	tick(t, a)
	if len(*calls) != 1 {
		t.Errorf("saved again without a change: %s", ops(*calls))
	}
}

func TestDraftSync_LeftSlotSavedAtTheNextTick(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("hello")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Errorf("calls = %s", got)
	}
}

func TestDraftSync_EmptiedSlotDiscards(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("hello")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	a.compose.SetDraftContext("T1", "C1", "")
	a.compose.SetValue("")
	a.compose.SetDraftContext("T1", "C2", "") // C1 now stores nothing at all
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"; discard C1/ ""` {
		t.Errorf("calls = %s", got)
	}
}

func TestDraftSync_WhitespaceIsEmpty(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("  \n ")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if len(*calls) != 0 {
		t.Errorf("calls = %s, want none", ops(*calls))
	}
}

func TestDraftSync_NothingBeforeFirstList(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetDraftContext("T2", "C5", "") // T2 never listed
	a.compose.SetValue("x")
	a.compose.SetDraftContext("", "C6", "") // team not known yet
	a.compose.SetValue("y")
	a.compose.SetDraftContext("", "", "")
	tick(t, a)
	tick(t, a)
	if len(*calls) != 0 {
		t.Errorf("calls = %s, want none", ops(*calls))
	}
}

func TestDraftSync_EditModeTextIsNotADraft(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.editing.Begin("C1", "1.0", PanelMessages, "")
	a.compose.SetValue("text of a sent message")
	tick(t, a)
	tick(t, a)
	if len(*calls) != 0 {
		t.Errorf("calls = %s, want none", ops(*calls))
	}
}

func TestDraftSync_SendIsNotADiscardOrAResave(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("hello")
	tick(t, a)
	tick(t, a) // saved
	a.SetMode(ModeInsert)
	a.compose.Focus()
	a.Update(keyCode(tea.KeyEnter))
	tick(t, a)
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Errorf("calls = %s, want only the save before the send", got)
	}
}

// A save answer that lands after the send must not bring the draft back.
func TestDraftSync_LateSaveAnswerAfterSendIsIgnored(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.compose.SetValue("hello")
	tick(t, a)
	saves := a.syncDrafts(a.now()) // in flight
	a.noteDraftSent(core.DraftKey{TeamID: "T1", ChannelID: "C1"})
	a.compose.Reset()
	for _, c := range saves {
		a.Update(c())
	}
	tick(t, a)
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Errorf("calls = %s", got)
	}
}

func TestDraftSync_HandsOffToastsOnceAndStops(t *testing.T) {
	a, calls, _ := draftSyncApp(t, core.ErrDraftHandsOff)
	a.compose.SetValue("one")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if !strings.Contains(statusbarText(a), "won't overwrite") {
		t.Errorf("toast = %q", statusbarText(a))
	}
	a.compose.SetDraftContext("T1", "C1", "")
	a.compose.SetValue("two")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if len(*calls) != 1 {
		t.Errorf("calls = %s, want one", ops(*calls))
	}
}

// A hands-off key recovers once its blocker is gone, even though the
// list never shows the key as writable: a list is a full snapshot, so a
// key no longer mentioned at all (deleted elsewhere) must resume sync.
func TestDraftSync_HandsOffResumesOnceTheBlockerIsGone(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	saveErr := core.ErrDraftHandsOff
	a.setDraftServiceForTest(core.DraftServiceFuncs{
		Save: func(k core.DraftKey, text string, edited time.Time) (core.DraftSaveResult, error) {
			*calls = append(*calls, draftCall{"save", k, text, edited})
			if saveErr != nil {
				return core.DraftSaveResult{}, saveErr
			}
			return core.DraftSaveResult{Draft: core.Draft{ID: "Dr-" + k.ChannelID, Key: k, Text: text}}, nil
		},
		Discard: func(k core.DraftKey) error {
			*calls = append(*calls, draftCall{op: "discard", key: k})
			return nil
		},
	})
	a.compose.SetValue("one")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	c1 := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	if !a.drafts.handsOff[c1] {
		t.Fatalf("setup: C1 is not hands-off (toast=%q)", statusbarText(a))
	}
	// A fresh list for T1 that doesn't mention C1 at all: the blocker is
	// gone (deleted here or on another device).
	a.Update(draftsLoadedMsg{teamID: "T1"})
	if a.drafts.handsOff[c1] {
		t.Fatal("hands-off was not cleared once the list stopped mentioning the key")
	}
	saveErr = nil
	a.compose.SetDraftContext("T1", "C1", "")
	a.compose.SetValue("two")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	if got := ops(*calls); !strings.Contains(got, `save C1/ "two"`) {
		t.Errorf("calls = %s, want a retried save for C1", got)
	}
}

func TestDraftSync_TransientErrorBacksOff(t *testing.T) {
	a, calls, clock := draftSyncApp(t, errors.New("timeout"))
	a.compose.SetValue("one")
	a.compose.SetDraftContext("T1", "C2", "")
	tick(t, a)
	tick(t, a)
	if len(*calls) != 1 {
		t.Fatalf("retried at once: %s", ops(*calls))
	}
	*clock = clock.Add(5 * time.Second)
	tick(t, a)
	if len(*calls) != 2 {
		t.Errorf("no retry after the backoff: %s", ops(*calls))
	}
}

func TestDraftsLoaded_RestoresWhereNothingWasTyped(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: []core.Draft{
		{ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "one"},
		{ID: "Dr2", Key: core.DraftKey{TeamID: "T1", ChannelID: "C2"}, Text: "two"},
		{ID: "Dr3", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1", ThreadTS: "10.0"}, Text: "three"},
		{ID: "Dr4", Key: core.DraftKey{TeamID: "T1", ChannelID: "C3"}, Text: "saved", Saved: true},
	}})
	if a.compose.Value() != "one" {
		t.Errorf("visible = %q", a.compose.Value())
	}
	if txt, _ := a.compose.SlotText("T1", "C2", ""); txt != "two" {
		t.Errorf("C2 = %q", txt)
	}
	if txt, _ := a.threadCompose.SlotText("T1", "C1", "10.0"); txt != "three" {
		t.Errorf("thread = %q", txt)
	}
	if txt, _ := a.compose.SlotText("T1", "C3", ""); txt != "" {
		t.Errorf("a saved draft was restored: %q", txt)
	}
	tick(t, a)
	tick(t, a)
	if len(*calls) != 0 {
		t.Errorf("restores were saved back: %s", ops(*calls))
	}
}

func TestDraftsLoaded_NeverReplacesTypedText(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.compose.SetValue("mine")
	a.compose.SetDraftContext("T1", "C2", "")
	a.compose.SetValue("mine too")
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: []core.Draft{
		{ID: "Dr1", Key: core.DraftKey{TeamID: "T1", ChannelID: "C1"}, Text: "theirs"},
		{ID: "Dr2", Key: core.DraftKey{TeamID: "T1", ChannelID: "C2"}, Text: "theirs"},
	}})
	if txt, _ := a.compose.SlotText("T1", "C1", ""); txt != "mine" {
		t.Errorf("C1 = %q", txt)
	}
	if a.compose.Value() != "mine too" {
		t.Errorf("visible = %q", a.compose.Value())
	}
}

func TestDraftsLoaded_VanishedDraftClearsAnUnchangedStoredSlot(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	k := core.DraftKey{TeamID: "T1", ChannelID: "C2"}
	a.Update(draftsLoadedMsg{teamID: "T1", drafts: []core.Draft{{ID: "Dr2", Key: k, Text: "two"}}})
	a.Update(draftsLoadedMsg{teamID: "T1"}) // sent from the phone
	if txt, _ := a.compose.SlotText("T1", "C2", ""); txt != "" {
		t.Errorf("C2 = %q, want cleared", txt)
	}
}

func TestUnsyncedDrafts(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.compose.SetValue("typed just before quitting")
	got := a.UnsyncedDrafts()
	if len(got) != 1 || got[0].Key != (core.DraftKey{TeamID: "T1", ChannelID: "C1"}) || got[0].Text != "typed just before quitting" {
		t.Errorf("UnsyncedDrafts = %+v", got)
	}
}

func TestDraftTick_ReschedulesItself(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	if _, cmd := a.Update(draftTickMsg{}); cmd == nil {
		t.Error("tick returned no command")
	}
}

func TestDraftSync_OffDoesNothing(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.compose.SetDraftContext("T1", "C1", "")
	a.compose.SetValue("x")
	if cmds := a.syncDrafts(a.now()); len(cmds) != 0 || a.UnsyncedDrafts() != nil {
		t.Error("sync ran with no draft service")
	}
	if _, cmd := a.Update(DraftsStaleMsg{TeamID: "T1"}); cmd != nil {
		t.Error("loaded drafts with no draft service")
	}
}
