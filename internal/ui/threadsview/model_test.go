package threadsview

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

func sampleSummaries() []cache.ThreadSummary {
	return []cache.ThreadSummary{
		{
			ChannelID: "C1", ChannelName: "general", ChannelType: "channel",
			ThreadTS: "1.000000", ParentUserID: "U1", ParentText: "hello world",
			ParentTS: "1.000000", ReplyCount: 3, LastReplyTS: "5.000000", LastReplyBy: "U2",
			Unread: true,
		},
		{
			ChannelID: "C2", ChannelName: "design", ChannelType: "channel",
			ThreadTS: "2.000000", ParentUserID: "U2", ParentText: "spec review",
			ParentTS: "2.000000", ReplyCount: 1, LastReplyTS: "4.000000", LastReplyBy: "USELF",
			Unread: false,
		},
	}
}

func TestNew_StartsAtTop(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("SelectedIndex = %d, want 0", got)
	}
}

func TestMoveDown_ClampsAtBottom(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	m.MoveDown()
	if m.SelectedIndex() != 1 {
		t.Errorf("after MoveDown SelectedIndex = %d, want 1", m.SelectedIndex())
	}
	m.MoveDown()
	if m.SelectedIndex() != 1 {
		t.Errorf("MoveDown past end should clamp; got %d, want 1", m.SelectedIndex())
	}
}

func TestSelected_ReturnsChannelAndThread(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	m.MoveDown()
	chID, threadTS, ok := m.Selected()
	if !ok || chID != "C2" || threadTS != "2.000000" {
		t.Errorf("Selected = (%q, %q, %v); want (C2, 2.000000, true)", chID, threadTS, ok)
	}
}

func TestSetSummaries_PreservesSelectionByThreadTS(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	m.MoveDown() // selected: thread 2

	// Re-rank: thread 2 moves to position 0, thread 1 to position 1.
	reranked := []cache.ThreadSummary{sampleSummaries()[1], sampleSummaries()[0]}
	m.SetSummaries(reranked)

	if m.SelectedIndex() != 0 {
		t.Errorf("after re-rank SelectedIndex should follow thread 2 to index 0, got %d", m.SelectedIndex())
	}
	chID, _, _ := m.Selected()
	if chID != "C2" {
		t.Errorf("Selected channel should still be C2, got %s", chID)
	}
}

func TestVersion_BumpsOnMutation(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	v0 := m.Version()
	m.SetSummaries(sampleSummaries())
	v1 := m.Version()
	if v1 == v0 {
		t.Errorf("Version did not bump on SetSummaries (v0=%d v1=%d)", v0, v1)
	}
	m.MoveDown()
	v2 := m.Version()
	if v2 == v1 {
		t.Errorf("Version did not bump on MoveDown")
	}
}

func TestView_RendersChannelAndPreview(t *testing.T) {
	m := New(map[string]string{"U1": "alice", "U2": "bob"}, "USELF")
	m.SetSummaries(sampleSummaries())
	out := m.View(60, 40)
	if !strings.Contains(out, "general") {
		t.Errorf("View output missing channel name 'general':\n%s", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("View output missing parent preview 'hello world':\n%s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("View output missing resolved author 'alice':\n%s", out)
	}
}

func TestView_EmptyState(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	out := m.View(60, 40)
	if !strings.Contains(strings.ToLower(out), "no threads") {
		t.Errorf("empty View output should mention 'no threads', got:\n%s", out)
	}
}

func TestUnreadCount(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	if got := m.UnreadCount(); got != 1 {
		t.Errorf("UnreadCount = %d, want 1", got)
	}
}
