package compose

import (
	"sort"
	"testing"

	"github.com/gammons/slk/internal/core"
)

func slotSet(m *Model) map[string]Slot {
	out := map[string]Slot{}
	for _, s := range m.Slots() {
		out[s.ChannelID+"/"+s.ThreadTS] = s
	}
	return out
}

func TestSlots_VisibleEvenEmptyAndEveryStoredOne(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("one")
	m.SetDraftContext("T1", "C2", "")
	got := slotSet(&m)
	if len(got) != 2 {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Fatalf("slots = %v, want C1 and C2", keys)
	}
	if s := got["C1/"]; s.Text != "one" || s.Visible || s.TeamID != "T1" {
		t.Errorf("C1 = %+v", s)
	}
	if s := got["C2/"]; s.Text != "" || !s.Visible {
		t.Errorf("C2 = %+v", s)
	}
}

func TestSlots_DetachedComposerHasNoVisibleSlot(t *testing.T) {
	m := New("general")
	if len(m.Slots()) != 0 {
		t.Errorf("slots = %+v, want none", m.Slots())
	}
}

func TestSlotText(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("one")
	m.SetDraftContext("T1", "C2", "")
	m.SetValue("two")
	if txt, vis := m.SlotText("T1", "C1", ""); txt != "one" || vis {
		t.Errorf("C1 = %q, %v", txt, vis)
	}
	if txt, vis := m.SlotText("T1", "C2", ""); txt != "two" || !vis {
		t.Errorf("C2 = %q, %v", txt, vis)
	}
	if txt, vis := m.SlotText("T1", "C9", ""); txt != "" || vis {
		t.Errorf("C9 = %q, %v", txt, vis)
	}
}

func TestApplyRemoteDraft(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	if !m.ApplyRemoteDraft("T1", "C1", "", "theirs") || m.Value() != "theirs" {
		t.Errorf("empty visible: value = %q", m.Value())
	}
	if m.ApplyRemoteDraft("T1", "C1", "", "newer") || m.Value() != "theirs" {
		t.Errorf("non-empty visible must not be replaced: value = %q", m.Value())
	}
	if !m.ApplyRemoteDraft("T1", "C2", "", "stored") {
		t.Error("inactive slot not written")
	}
	m.SetDraftContext("T1", "C2", "")
	if m.Value() != "stored" {
		t.Errorf("restored C2 = %q", m.Value())
	}
	if !m.ApplyRemoteDraft("T1", "C1", "", "") {
		t.Error("clearing an inactive slot")
	}
	if txt, _ := m.SlotText("T1", "C1", ""); txt != "" {
		t.Errorf("C1 after clear = %q", txt)
	}
	if m.ApplyRemoteDraft("T1", "", "", "x") {
		t.Error("a slot without a channel is not a conversation")
	}
}

func TestApplyRemoteDraft_ClearKeepsAttachments(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("text")
	m.AddAttachment(core.PendingAttachment{Filename: "a.png"})
	m.SetDraftContext("T1", "C2", "")
	m.ApplyRemoteDraft("T1", "C1", "", "")
	m.SetDraftContext("T1", "C1", "")
	if m.Value() != "" || len(m.Attachments()) != 1 {
		t.Errorf("value %q, %d attachments", m.Value(), len(m.Attachments()))
	}
}

func TestClearSlot(t *testing.T) {
	m := New("general")
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("one")
	m.SetDraftContext("T1", "C2", "")
	m.SetValue("two")
	m.ClearSlot("T1", "C2", "")
	m.ClearSlot("T1", "C1", "")
	if m.Value() != "" {
		t.Errorf("visible = %q", m.Value())
	}
	if txt, _ := m.SlotText("T1", "C1", ""); txt != "" {
		t.Errorf("C1 = %q", txt)
	}
}
