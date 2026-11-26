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
		Save: func(k core.DraftKey, text string, edited time.Time, current func() bool) (core.DraftSaveResult, error) {
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
		Save: func(k core.DraftKey, text string, edited time.Time, current func() bool) (core.DraftSaveResult, error) {
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
	// gone (deleted here or on another device). issued is set to the
	// current seq so this list -- fetched after the hands-off save
	// answered -- is not treated as stale against it.
	a.Update(draftsLoadedMsg{teamID: "T1", issued: a.drafts.seq})
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

// A list issued before a send that still shows the sent draft (the
// request was in flight when the send completed) must not restore its
// text into the now-empty slot, and must not bring its row back.
func TestDraftsLoaded_StaleListDoesNotRestoreOrRowbackASentDraft(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	a.drafts.synced[key] = "hello"
	a.drafts.upsertRow(core.Draft{ID: "Dr1", Key: key, Text: "hello"})
	a.compose.SetValue("hello")

	issued := a.drafts.seq // the list was issued now, before the send
	a.noteDraftSent(key)   // the send: bumps touched[key] past issued
	a.compose.Reset()      // the send leaves the composer empty

	// The stale answer still shows the just-sent draft.
	a.Update(draftsLoadedMsg{teamID: "T1", issued: issued, drafts: []core.Draft{
		{ID: "Dr1", Key: key, Text: "hello"},
	}})

	if txt, _ := a.compose.SlotText("T1", "C1", ""); txt != "" {
		t.Errorf("the sent text was restored into the slot: %q", txt)
	}
	if rows := a.drafts.rows["T1"]; len(rows) != 0 {
		t.Errorf("the sent draft's row came back: %v", rows)
	}
}

// A list issued before an inactive (not visible) slot's save answered
// must not clear that slot, even though the list -- fetched before the
// save -- does not mention the key yet.
func TestDraftsLoaded_StaleListDoesNotClearASlotSavedDuringTheRequest(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C2"} // not the visible slot (C1)
	a.compose.SetValue("left behind")
	a.compose.SetDraftContext("T1", "C2", "")
	a.compose.SetValue("left behind")
	a.compose.SetDraftContext("T1", "C1", "") // move away: C2 is now inactive

	issued := a.drafts.seq // the list was issued now, before the save answers
	a.drafts.synced[key] = "left behind"
	a.drafts.upsertRow(core.Draft{ID: "Dr2", Key: key, Text: "left behind"})
	a.drafts.touch(key) // what the real save answer does on success

	// The stale list doesn't mention C2 at all yet.
	a.Update(draftsLoadedMsg{teamID: "T1", issued: issued})

	if txt, _ := a.compose.SlotText("T1", "C2", ""); txt != "left behind" {
		t.Errorf("the saved slot was cleared by the stale list: %q", txt)
	}
	if rows := a.drafts.rows["T1"]; len(rows) != 1 {
		t.Errorf("the saved draft's row was dropped by the stale list: %v", rows)
	}
}

// A hands-off answer touches its key but does not remove any stale
// writable row already recorded for it (the ruling: "keep the current
// rows as they are. The point is to avoid a duplicate, not to invent a
// row"). A list issued earlier that still shows the key's own
// non-writable (blocking) draft must not add a second row for the same
// conversation: mergeDraftRows must drop the list's row for a touched
// key regardless of kind, not just a writable one.
func TestDraftsLoaded_StaleListDoesNotDuplicateARowAfterHandsOff(t *testing.T) {
	a, _, _ := draftSyncApp(t, core.ErrDraftHandsOff)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	// A prior save had created a writable row at this key.
	a.drafts.upsertRow(core.Draft{ID: "Dr1", Key: key, Text: "old"})

	issued := a.drafts.seq // the list was issued now, before the hands-off save answers
	a.compose.SetValue("one")
	tick(t, a)
	tick(t, a) // the save answers ErrDraftHandsOff: sets handsOff[key], touches key

	if !a.drafts.handsOff[key] {
		t.Fatalf("setup: key not hands-off")
	}

	// The stale (older) list shows this destination's own non-writable
	// (blocking) draft.
	a.Update(draftsLoadedMsg{teamID: "T1", issued: issued, drafts: []core.Draft{
		{ID: "DrBlock", Key: key, Scheduled: time.Unix(1_900_000_000, 0)},
	}})

	if rows := a.drafts.rows["T1"]; len(rows) != 1 {
		t.Errorf("rows for the key = %d, want 1: %v", len(rows), rows)
	}
}

// Deleting a non-writable draft from :drafts must touch its key (not
// only a writable one), so a list issued before the delete, still
// showing the deleted draft, does not bring its row back.
func TestDraftsLoaded_StaleListDoesNotReviveADeletedNonWritableDraft(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C9"}
	blocker := core.Draft{ID: "DrBlock", Key: key, Scheduled: time.Unix(1_900_000_000, 0)}
	a.drafts.rows["T1"] = []core.Draft{blocker}

	issued := a.drafts.seq // the list was issued now, before the delete
	a.Update(draftDeletedMsg{teamID: "T1", draft: blocker})

	// The stale list still shows the just-deleted blocker.
	a.Update(draftsLoadedMsg{teamID: "T1", issued: issued, drafts: []core.Draft{blocker}})

	if rows := a.drafts.rows["T1"]; len(rows) != 0 {
		t.Errorf("the deleted draft's row came back: %v", rows)
	}
}

// An unaddressed draft (Key.ChannelID == "") has no real conversation
// key: deleting it must still keep a stale list from bringing it back,
// via deletedAt (by draft ID), since several unaddressed drafts would
// otherwise share the same degenerate key.
func TestDraftsLoaded_StaleListDoesNotReviveADeletedUnaddressedDraft(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	unaddressed := core.Draft{ID: "DrNew", Key: core.DraftKey{TeamID: "T1"}, Saved: true}
	a.drafts.rows["T1"] = []core.Draft{unaddressed}

	issued := a.drafts.seq
	a.Update(draftDeletedMsg{teamID: "T1", draft: unaddressed})

	a.Update(draftsLoadedMsg{teamID: "T1", issued: issued, drafts: []core.Draft{unaddressed}})

	if rows := a.drafts.rows["T1"]; len(rows) != 0 {
		t.Errorf("the deleted unaddressed draft's row came back: %v", rows)
	}
}

// reduceDraftSaved's ErrDraftStale branch is unreachable in practice
// (the gen check right above it already returns first for every real
// case: ErrDraftStale only happens when current() reported false,
// i.e. the generation was already bumped). Feed it directly with the
// CURRENT gen, so that check does not short-circuit, and assert
// nothing changes except inflight being cleared.
func TestDraftSync_ErrDraftStaleAnswerChangesNothingButInflight(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	a.drafts.inflight[key] = "pending"
	a.drafts.synced[key] = "synced text"
	a.drafts.rows["T1"] = []core.Draft{{ID: "Dr1", Key: key, Text: "synced text"}}

	gen := a.drafts.genPtr(key).Load() // the current gen: must not mismatch above
	a.reduceDraftSaved(draftSavedMsg{key: key, text: "whatever", gen: gen, err: core.ErrDraftStale})

	if _, busy := a.drafts.inflight[key]; busy {
		t.Error("inflight not cleared")
	}
	if a.drafts.synced[key] != "synced text" {
		t.Errorf("synced changed: %q", a.drafts.synced[key])
	}
	if a.drafts.handsOff[key] {
		t.Error("handsOff set")
	}
	if _, retried := a.drafts.retry[key]; retried {
		t.Error("a retry was scheduled")
	}
	if len(a.drafts.rows["T1"]) != 1 {
		t.Errorf("rows changed: %v", a.drafts.rows["T1"])
	}
}

// Save A, start saving B, change back to A, quit: the text at quit
// equals synced ("A"), but a save for B is still in flight and must
// not be left as Slack's last word, so UnsyncedDrafts must still
// include the key, with the current text (A).
func TestUnsyncedDrafts_IncludesAKeyWithARacingInflightSave(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.compose.SetValue("A")
	tick(t, a)
	tick(t, a) // A saved: synced[C1] = "A"

	a.compose.SetValue("B")
	a.syncDrafts(a.now())         // observe B
	cmds := a.syncDrafts(a.now()) // builds (and marks in flight) the save for B
	if len(cmds) != 1 {
		t.Fatalf("cmds = %d, want 1 (the in-flight save for B)", len(cmds))
	}
	// The in-flight save's answer has not landed: synced is still "A".

	a.compose.SetValue("A") // changed back before quitting

	got := a.UnsyncedDrafts()
	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	if len(got) != 1 || got[0].Key != key || got[0].Text != "A" {
		t.Errorf("UnsyncedDrafts = %+v, want one change for %v with text %q", got, key, "A")
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

// A slot left in workspace T1, then a switch to T2, must still be
// saved with T1's own mention names: the composer's live mention data
// belongs to the active workspace (T2) once the switch lands, and T2
// has a different name for the same user ID.
func TestDraftSync_MentionsTranslatedWithTheirOwnWorkspacesNames(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	a.SetUserNames(map[string]string{"U1": "Alice"})
	a.compose.SetValue("hi @Alice")
	a.compose.SetDraftContext("T1", "C2", "") // leave C1 (stored, not visible)

	a.Update(WorkspaceSwitchedMsg{TeamID: "T2", TeamName: "Two", UserID: "U9"})
	a.SetUserNames(map[string]string{"U1": "Bob"}) // T2's name for the same ID

	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hi <@U1>"` {
		t.Errorf("calls = %s, want the mention translated with T1's names", got)
	}
}

// A draft list for a workspace that is not active must not restore
// into the composers (which hold the active workspace's mention data)
// or clear anything; only that workspace's own loaded/rows bookkeeping
// changes.
func TestDraftsLoaded_InactiveWorkspaceListIsNotRestored(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	a.Update(draftsLoadedMsg{teamID: "T2", drafts: []core.Draft{
		{ID: "Dr9", Key: core.DraftKey{TeamID: "T2", ChannelID: "C9"}, Text: "hello"},
	}})
	if txt, _ := a.compose.SlotText("T2", "C9", ""); txt != "" {
		t.Errorf("T2 draft restored into a composer while T1 was active: %q", txt)
	}
	if !a.drafts.loaded["T2"] {
		t.Error("T2 not marked loaded")
	}
	if len(a.drafts.rows["T2"]) != 1 {
		t.Errorf("T2 rows = %v, want 1", a.drafts.rows["T2"])
	}
	if a.compose.Value() != "" {
		t.Errorf("T1's visible composer changed: %q", a.compose.Value())
	}
}

// Two loads for the same team issued with no local change between them
// share the same "issued" (d.seq), so the touched/issued freshness
// check alone cannot tell which response is newer when they land out
// of order. The ordinal (d.loadSeq, bumped at every loadDraftsCmd
// build) must: the older response landing after the newer one must not
// replace the newer text it already restored into an inactive slot,
// nor replace the newer list's rows.
func TestDraftsLoaded_OlderLoadDoesNotReplaceANewerOne(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C2"} // inactive: the visible slot is C1

	a.loadDraftsCmd("T1") // issued first: ordinal 1 (older)
	a.loadDraftsCmd("T1") // issued second: ordinal 2 (newer)

	newer := draftsLoadedMsg{teamID: "T1", ordinal: 2, drafts: []core.Draft{
		{ID: "DrNew", Key: key, Text: "new"},
	}}
	older := draftsLoadedMsg{teamID: "T1", ordinal: 1, drafts: []core.Draft{
		{ID: "DrOld", Key: key, Text: "old"},
	}}

	a.Update(newer) // the newer response lands first
	if txt, _ := a.compose.SlotText("T1", "C2", ""); txt != "new" {
		t.Fatalf("setup: C2 = %q, want %q", txt, "new")
	}

	a.Update(older) // the older response lands after

	if txt, _ := a.compose.SlotText("T1", "C2", ""); txt != "new" {
		t.Errorf("an older load replaced the newer one's text: %q", txt)
	}
	if rows := a.drafts.rows["T1"]; len(rows) != 1 || rows[0].ID != "DrNew" {
		t.Errorf("rows = %v, want just DrNew", rows)
	}
}

// A save built before a send must not recreate the draft the send just
// marked sent: the send bumps the key's generation before the save's
// service call runs, and the service call's own current (passed
// through from saveDraftCmd, the way the engine checks it under its
// lock) must see that bump and refuse before any API call is made.
func TestDraftSync_SaveBuiltBeforeSendDoesNotRecreateIt(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
	a.compose.SetDraftContext("T1", "C1", "")
	clock := time.Unix(1_800_000_000, 0)
	a.now = func() time.Time { return clock }
	var saveCalls int
	a.setDraftServiceForTest(core.DraftServiceFuncs{
		Save: func(k core.DraftKey, text string, edited time.Time, current func() bool) (core.DraftSaveResult, error) {
			if current != nil && !current() {
				return core.DraftSaveResult{}, core.ErrDraftStale
			}
			saveCalls++
			return core.DraftSaveResult{Draft: core.Draft{ID: "Dr-" + k.ChannelID, Key: k, Text: text}}, nil
		},
		Discard: func(k core.DraftKey) error { return nil },
	})
	a.Update(draftsLoadedMsg{teamID: "T1"})

	a.compose.SetValue("hello")
	tick(t, a)                    // first comparison: observes "hello", too soon to save (visible)
	cmds := a.syncDrafts(a.now()) // second comparison at the same instant: stable since observed, builds the save command

	if len(cmds) != 1 {
		t.Fatalf("cmds = %d, want 1 (the save built before the send)", len(cmds))
	}

	// The send: marks the slot sent and bumps gen before the save
	// command built above has run its service call.
	a.noteDraftSent(core.DraftKey{TeamID: "T1", ChannelID: "C1"})

	for _, c := range cmds {
		c()
	}

	if saveCalls != 0 {
		t.Errorf("the stale save reached the service %d times, want 0", saveCalls)
	}
}

// A save built before a delete is confirmed must not recreate the
// draft the delete just removed: confirming the delete bumps the
// key's generation before the delete call itself runs, and the save's
// own current (passed through the way the engine checks it under its
// lock) must see that bump and refuse before any API call is made.
func TestDraftDelete_QueuedSaveDoesNotRecreateIt(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withActiveChannel("C1"))
	a.compose.SetDraftContext("T1", "C1", "")
	var saveCalls int
	a.setDraftServiceForTest(core.DraftServiceFuncs{
		Save: func(k core.DraftKey, text string, edited time.Time, current func() bool) (core.DraftSaveResult, error) {
			if current != nil && !current() {
				return core.DraftSaveResult{}, core.ErrDraftStale
			}
			saveCalls++
			return core.DraftSaveResult{Draft: core.Draft{ID: "Dr-" + k.ChannelID, Key: k, Text: text}}, nil
		},
		Discard: func(k core.DraftKey) error { return nil },
	})
	a.Update(draftsLoadedMsg{teamID: "T1"})

	a.compose.SetValue("hello")
	tick(t, a)                    // observe
	cmds := a.syncDrafts(a.now()) // builds the save command, capturing gen

	if len(cmds) != 1 {
		t.Fatalf("cmds = %d, want 1 (the save built before the delete)", len(cmds))
	}

	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	a.Update(draftDeleteConfirmedMsg{teamID: "T1", draft: core.Draft{ID: "Dr1", Key: key, Text: "hello"}})

	for _, c := range cmds {
		c()
	}

	if saveCalls != 0 {
		t.Errorf("the stale save reached the service %d times, want 0", saveCalls)
	}
}

// While a confirmed delete is in flight, the tick must not queue a new
// save for its key: a save that raced the delete for the engine's
// per-conversation lock would only have to be answered with
// ErrDraftStale (or undone once the delete lands), for no benefit.
func TestDraftDelete_TicksIssueNoSaveWhileInFlight(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	a.Update(draftDeleteConfirmedMsg{teamID: "T1", draft: core.Draft{ID: "Dr1", Key: key, Text: "hello"}})

	a.compose.SetValue("typed while the delete is in flight")
	tick(t, a)
	tick(t, a)

	if len(*calls) != 0 {
		t.Errorf("a save was issued while the delete was in flight: %s", ops(*calls))
	}
}

// A failed delete must leave the slot as it is, so the next tick
// resyncs it instead of leaving the key stuck "deleting" forever.
func TestDraftDelete_FailureResumesSyncing(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	draft := core.Draft{ID: "Dr1", Key: key, Text: "hello"}
	a.Update(draftDeleteConfirmedMsg{teamID: "T1", draft: draft})

	a.Update(draftDeletedMsg{teamID: "T1", draft: draft, err: errors.New("boom")})

	if a.drafts.deleting[key] {
		t.Fatal("deleting not cleared after a failed delete")
	}

	a.compose.SetValue("resumed")
	tick(t, a)
	tick(t, a)

	if got := ops(*calls); got != `save C1/ "resumed"` {
		t.Errorf("calls = %s, want the resumed save", got)
	}
}

// A non-writable draft (scheduled, files, "Save as draft") can share a
// conversation key with the composer's own attached draft. Confirming
// its deletion must not bump the key's gen or pause the composer's
// sync: the tick still saves unsaved text, and UnsyncedDrafts still
// includes it, while that unrelated delete is notionally in flight.
func TestDraftDelete_NonWritableDeleteDoesNotPauseComposerSync(t *testing.T) {
	a, calls, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	blocker := core.Draft{ID: "DrBlock", Key: key, Scheduled: time.Unix(1_900_000_000, 0)}

	a.compose.SetValue("hello")
	a.Update(draftDeleteConfirmedMsg{teamID: "T1", draft: blocker})

	if a.drafts.deleting[key] {
		t.Fatal("deleting[key] was set for a non-writable draft's delete")
	}

	if got := a.UnsyncedDrafts(); len(got) != 1 || got[0].Key != key || got[0].Text != "hello" {
		t.Errorf("UnsyncedDrafts = %+v, want the composer's unsaved text", got)
	}

	tick(t, a)
	tick(t, a)
	if got := ops(*calls); got != `save C1/ "hello"` {
		t.Errorf("calls = %s, want the composer's text saved despite the non-writable delete", got)
	}
}

// A save already in flight when a non-writable draft's delete (at the
// same key) is confirmed must not be treated as stale: only the
// attached draft's own delete bumps the gen.
func TestDraftDelete_NonWritableDeleteDoesNotStaleASaveInFlight(t *testing.T) {
	a, _, _ := draftSyncApp(t, nil)
	key := core.DraftKey{TeamID: "T1", ChannelID: "C1"}
	blocker := core.Draft{ID: "DrBlock", Key: key, Scheduled: time.Unix(1_900_000_000, 0)}

	a.compose.SetValue("hello")
	tick(t, a)                    // observe
	cmds := a.syncDrafts(a.now()) // builds the save, captures gen

	if len(cmds) != 1 {
		t.Fatalf("cmds = %d, want 1", len(cmds))
	}

	a.Update(draftDeleteConfirmedMsg{teamID: "T1", draft: blocker}) // the unrelated, non-writable delete confirms

	for _, c := range cmds {
		a.Update(c())
	}

	if a.drafts.synced[key] != "hello" {
		t.Errorf("synced[key] = %q, want %q (the in-flight save's answer must not be dropped as stale)", a.drafts.synced[key], "hello")
	}
}

// A Load failure for a workspace that has never loaded leaves sync
// silently off unless it is retried. reduceDraftsLoaded must schedule
// a backed-off retry (DraftsStaleMsg, which re-issues the load) and
// grow the backoff on each further failure; a success resets it, and
// a failed reload of an already-loaded workspace keeps the old
// behaviour (log only, no retry).
func TestDraftsLoaded_FirstLoadFailureRetriesWithBackoff(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	orig := draftLoadRetryBase
	draftLoadRetryBase = 5 * time.Millisecond
	t.Cleanup(func() { draftLoadRetryBase = orig })

	cmd := a.reduceDraftsLoaded(draftsLoadedMsg{teamID: "T1", err: errors.New("boom")})
	if cmd == nil {
		t.Fatal("a load error for a not-yet-loaded workspace returned no retry command")
	}
	got := deliveredMsgs[DraftsStaleMsg](t, cmd)
	if len(got) != 1 || got[0].TeamID != "T1" {
		t.Fatalf("retry delivered = %+v, want one DraftsStaleMsg{T1}", got)
	}
	if a.drafts.loadRetry["T1"] != 1 {
		t.Errorf("loadRetry[T1] = %d, want 1", a.drafts.loadRetry["T1"])
	}

	// A second failure backs off further.
	a.reduceDraftsLoaded(draftsLoadedMsg{teamID: "T1", err: errors.New("boom again")})
	if a.drafts.loadRetry["T1"] != 2 {
		t.Errorf("loadRetry[T1] = %d, want 2", a.drafts.loadRetry["T1"])
	}

	// Success resets the backoff.
	a.reduceDraftsLoaded(draftsLoadedMsg{teamID: "T1"})
	if _, ok := a.drafts.loadRetry["T1"]; ok {
		t.Error("loadRetry[T1] not reset after a successful load")
	}

	// A failed reload of an already-loaded workspace: log only, no
	// retry scheduled (today's behaviour, unchanged).
	if cmd := a.reduceDraftsLoaded(draftsLoadedMsg{teamID: "T1", err: errors.New("later failure")}); cmd != nil {
		t.Error("a reload failure for an already-loaded workspace scheduled a retry")
	}
}

// A large n (reached after a long streak of failures) must not shift
// base past the point where it would overflow into a negative
// Duration -- which would make the next retry fire immediately
// instead of waiting, a retry storm. Both draft retry backoffs
// (the load retry and reduceDraftSaved's save retry) use the same
// helper, so test both call sites' base/max pairs.
func TestDraftBackoff_CapsWithoutOverflowAtLargeN(t *testing.T) {
	for _, tc := range []struct {
		name      string
		base, max time.Duration
	}{
		{"load retry", draftLoadRetryBase, draftLoadRetryMax},
		{"save retry", draftTickInterval, draftRetryMax},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wait := draftBackoff(tc.base, tc.max, 64)
			if wait != tc.max {
				t.Errorf("wait = %v, want the cap %v", wait, tc.max)
			}
			if wait <= 0 {
				t.Errorf("wait = %v, want positive", wait)
			}
		})
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
