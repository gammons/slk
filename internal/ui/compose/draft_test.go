package compose

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/mentionpicker"
)

// Draft ownership lives in SetDraftContext, keyed by (team, channel,
// thread). These tests pin the model-level invariants; the App-level
// wiring is covered separately in internal/ui.

func TestSetDraftContextKeepsIndependentChannelDrafts(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("draft one")
	m.SetDraftContext("T1", "C2", "")

	if got := m.Value(); got != "" {
		t.Fatalf("fresh conversation should show an empty composer, got %q", got)
	}
	m.SetValue("draft two")
	m.SetDraftContext("T1", "C1", "")
	if got := m.Value(); got != "draft one" {
		t.Errorf("returning to C1: value = %q, want %q", got, "draft one")
	}
	m.SetDraftContext("T1", "C2", "")
	if got := m.Value(); got != "draft two" {
		t.Errorf("returning to C2: value = %q, want %q", got, "draft two")
	}
}

func TestSetDraftContextSameKeyIsNoOp(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Alice"}})
	m.SetWidth(80)
	m.Focus()
	m.SetValue("hello ")
	m = typeText(m, "@")
	if !m.IsMentionActive() {
		t.Fatal("precondition: mention picker open")
	}

	m.SetDraftContext("T1", "C1", "")

	if got := m.Value(); got != "hello @" {
		t.Errorf("same-key call altered text: got %q", got)
	}
	if pos := m.cursorPosition(); pos != len("hello @") {
		t.Errorf("same-key call moved cursor to %d, want %d", pos, len("hello @"))
	}
	if !m.IsMentionActive() {
		t.Error("same-key call closed an open picker")
	}
}

func TestSetDraftContextEmptyChannelDetachesAndClears(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("channel draft")
	m.AddAttachment(core.PendingAttachment{Filename: "a.png", Size: 3})

	m.SetDraftContext("", "", "") // detached composer

	if got := m.Value(); got != "" {
		t.Errorf("detach should clear visible text, got %q", got)
	}
	if n := len(m.Attachments()); n != 0 {
		t.Errorf("detach should clear visible attachments, got %d", n)
	}
	if _, ok := m.drafts[draftKey{}]; ok {
		t.Error("a detached composer must not persist a draft under an empty channel")
	}

	// The prior valid conversation still owns its saved draft.
	m.SetDraftContext("T1", "C1", "")
	if got := m.Value(); got != "channel draft" {
		t.Errorf("returning after detach: value = %q, want %q", got, "channel draft")
	}
	if n := len(m.Attachments()); n != 1 {
		t.Errorf("returning after detach: attachments = %d, want 1", n)
	}
}

func TestSetDraftContextMovesAttachmentOwnership(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.AddAttachment(core.PendingAttachment{Filename: "private.txt", Size: 7})
	m.SetValue("see attached")

	m.SetDraftContext("T1", "C2", "")
	if n := len(m.Attachments()); n != 0 {
		t.Fatalf("attachments leaked into a different conversation: %d", n)
	}

	m.SetDraftContext("T1", "C1", "")
	att := m.Attachments()
	if len(att) != 1 || att[0].Filename != "private.txt" {
		t.Fatalf("attachments not restored: %+v", att)
	}
	// The visible composer owns the slice again; clearing it must not
	// leave a stale map entry behind.
	if got := m.Value(); got != "see attached" {
		t.Errorf("text not restored alongside attachments: %q", got)
	}
}

func TestSetDraftContextClosesAllPickersOnSwap(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetEmojiEntries(sampleEmojiEntries())
	m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Alice"}})
	m.SetWidth(80)
	m.Focus()
	m.SetDraftContext("T1", "C1", "")

	// Channel picker left open mid-query, the state that produced a
	// stale start-offset and a slice-bounds panic in insertChannel.
	m = typeText(m, "hi #")
	if !m.IsChannelActive() {
		t.Fatal("precondition: channel picker open")
	}

	m.SetDraftContext("T1", "C2", "")

	if m.IsChannelActive() {
		t.Error("channel picker survived a draft swap")
	}
	if m.IsMentionActive() {
		t.Error("mention picker survived a draft swap")
	}
	if m.IsEmojiActive() {
		t.Error("emoji picker survived a draft swap")
	}
	// Enter with the picker gone must not index the empty draft with the
	// old offset (the pre-fix panic).
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if strings.Contains(m.Value(), "general") {
		t.Errorf("stale channel completion leaked into the new draft: %q", m.Value())
	}

	// Emoji and mention sessions are closed too.
	m = typeText(m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: emoji picker open")
	}
	m.SetDraftContext("T1", "C3", "")
	if m.IsEmojiActive() {
		t.Error("emoji picker survived a draft swap")
	}
}

func TestSetDraftContextRestoreRecomputesHeightAndVersion(t *testing.T) {
	m := New("general")
	m.SetWidth(80)
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("line1\nline2\nline3")
	m.autoGrow()
	if m.input.Height() < 3 {
		t.Fatalf("precondition: height = %d, want >= 3", m.input.Height())
	}

	m.SetDraftContext("T1", "C2", "") // away: clears visible height
	if m.input.Height() != 1 {
		t.Fatalf("swap-away should collapse height, got %d", m.input.Height())
	}
	before := m.Version()
	m.SetDraftContext("T1", "C1", "") // back

	if m.input.Height() < 3 {
		t.Errorf("restore did not recompute visible height: got %d", m.input.Height())
	}
	if m.Version() == before {
		t.Error("restore must bump Version so the panel cache invalidates")
	}
}

func TestSetDraftContextIsolatesSameChannelAcrossTeams(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("team one")

	m.SetDraftContext("T2", "C1", "")
	if got := m.Value(); got != "" {
		t.Errorf("same channel ID in another workspace leaked: %q", got)
	}
	m.SetValue("team two")

	m.SetDraftContext("T1", "C1", "")
	if got := m.Value(); got != "team one" {
		t.Errorf("T1/C1 draft = %q, want %q", got, "team one")
	}
}

func TestSetDraftContextRebindsUnknownTeamStartup(t *testing.T) {
	m := New("general")
	// Startup binds the composer before the workspace is known.
	m.SetDraftContext("", "C1", "")
	m.SetValue("hello")
	m.AddAttachment(core.PendingAttachment{Filename: "a.png", Size: 3})

	// Learning the team rebinds in place and keeps the visible draft.
	m.SetDraftContext("T1", "C1", "")
	if got := m.Value(); got != "hello" {
		t.Fatalf("rebind lost startup text: %q", got)
	}
	if n := len(m.Attachments()); n != 1 {
		t.Fatalf("rebind lost startup attachment: %d", n)
	}

	// Known teams stay isolated after the rebind.
	m.SetDraftContext("T2", "C1", "")
	if got := m.Value(); got != "" {
		t.Errorf("same channel in another workspace leaked: %q", got)
	}
	m.SetDraftContext("T1", "C1", "")
	if got := m.Value(); got != "hello" {
		t.Errorf("T1/C1 draft = %q, want %q", got, "hello")
	}

	// A known team is never adopted into an unknown/empty team.
	m.SetDraftContext("", "C1", "")
	if got := m.Value(); got != "" {
		t.Errorf("known team migrated to empty team instead of detaching: %q", got)
	}
	if got := m.drafts[draftKey{teamID: "T1", channelID: "C1"}].text; got != "hello" {
		t.Errorf("T1/C1 draft not saved on detach: %q", got)
	}
}

func TestSetDraftContextKeepsThreadDraftsSeparate(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "100.0")
	m.SetValue("thread one")
	m.SetDraftContext("T1", "C1", "200.0")
	m.SetValue("thread two")

	m.SetDraftContext("T1", "C1", "100.0")
	if got := m.Value(); got != "thread one" {
		t.Errorf("thread 100.0 draft = %q, want %q", got, "thread one")
	}
	m.SetDraftContext("T1", "C1", "200.0")
	if got := m.Value(); got != "thread two" {
		t.Errorf("thread 200.0 draft = %q, want %q", got, "thread two")
	}
	// A channel-level draft (empty threadTS) is distinct from its threads.
	m.SetDraftContext("T1", "C1", "")
	if got := m.Value(); got != "" {
		t.Errorf("channel-level draft should be empty, got %q", got)
	}
}

func TestResetLeavesInactiveDraftsIntact(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "A", "")
	m.SetValue("keep A")
	m.SetDraftContext("T1", "B", "")
	m.SetValue("keep B")

	m.SetDraftContext("T1", "A", "") // restores "keep A", deletes its entry
	m.Reset()                        // send/clear of A

	if got := m.Value(); got != "" {
		t.Fatalf("Reset should clear the visible composer, got %q", got)
	}
	m.SetDraftContext("T1", "B", "")
	if got := m.Value(); got != "keep B" {
		t.Errorf("Reset wiped an unrelated inactive draft: got %q", got)
	}
}

func TestSentDraftDoesNotResurrect(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "A", "")
	m.SetValue("send me")
	m.SetDraftContext("T1", "B", "")
	m.SetDraftContext("T1", "A", "") // restored; entry deleted
	m.Reset()                        // user sends A, composer cleared

	m.SetDraftContext("T1", "B", "")
	m.SetDraftContext("T1", "A", "")
	if got := m.Value(); got != "" {
		t.Errorf("already-sent draft resurrected: %q", got)
	}
}

func TestSetActiveChannelDoesNotTouchDrafts(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("live draft")

	// Mention-membership context moves independently of draft ownership.
	m.SetActiveChannel("C1")
	m.SetActiveChannel("COTHER")
	m.SetActiveChannel("C1")

	if got := m.Value(); got != "live draft" {
		t.Errorf("SetActiveChannel altered the visible draft: %q", got)
	}
}
