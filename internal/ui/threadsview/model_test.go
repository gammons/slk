package threadsview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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
	// Args: height=40, width=60.
	out := m.View(40, 60)
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
	out := m.View(40, 60)
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

// M7: Parent-not-loaded fallback renders the placeholder when both
// ParentText and ParentUserID are empty.
func TestView_ParentNotLoadedFallback(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{{
		ChannelID: "C1", ChannelName: "general", ChannelType: "channel",
		ThreadTS: "1.000000", ParentUserID: "", ParentText: "",
		ParentTS: "1.000000", ReplyCount: 0, LastReplyTS: "1.000000", LastReplyBy: "",
		Unread: false,
	}})
	out := m.View(40, 60)
	if !strings.Contains(out, "parent not loaded") {
		t.Errorf("View should render parent-not-loaded fallback, got:\n%s", out)
	}
}

// M8: Selecting a different row must produce different View() output --
// catches the case where selection styling silently no-ops.
func TestView_SelectionChangesOutput(t *testing.T) {
	m := New(map[string]string{"U1": "alice", "U2": "bob"}, "USELF")
	m.SetSummaries(sampleSummaries())
	before := m.View(40, 60)
	m.MoveDown()
	after := m.View(40, 60)
	if before == after {
		t.Errorf("View output unchanged after MoveDown; selection styling not applied")
	}
}

// M9: When the list overflows the viewport, MoveDown beyond the visible
// window must snap-to-selected so the active card stays on screen.
func TestView_SnapsToSelectedOnOverflow(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	// 10 summaries with distinct channel names so we can spot which one
	// is on-screen.
	var summaries []cache.ThreadSummary
	for i := 0; i < 10; i++ {
		summaries = append(summaries, cache.ThreadSummary{
			ChannelID:    "C" + string(rune('0'+i)),
			ChannelName:  "ch-" + string(rune('a'+i)),
			ChannelType:  "channel",
			ThreadTS:     "1.00000" + string(rune('0'+i)),
			ParentUserID: "U1",
			ParentText:   "msg-" + string(rune('a'+i)),
			ParentTS:     "1.00000" + string(rune('0'+i)),
			ReplyCount:   1,
			LastReplyTS:  "2.00000" + string(rune('0'+i)),
			LastReplyBy:  "U2",
		})
	}
	m.SetSummaries(summaries)

	// Total content lines = 10*3 + 9 separators = 39. With height=10 the
	// viewport holds ~2.5 cards. Walk cursor far enough that without
	// snapping, the selected card sits below the initial yOffset=0 window.
	// Args: height=10, width=40.
	for i := 0; i < 6; i++ {
		m.MoveDown()
	}
	out := m.View(10, 40)

	// Selected card name must be present (snap brought it into view).
	if !strings.Contains(out, "ch-g") {
		t.Errorf("selected card 'ch-g' not in viewport after MoveDown; snap not applied:\n%s", out)
	}
	// And the very first off-screen card should NOT be visible anymore.
	if strings.Contains(out, "ch-a") {
		t.Errorf("first card 'ch-a' should have scrolled off; snap clamped wrong:\n%s", out)
	}
}

// All rendered lines (including blank separator lines) must be exactly
// `width` columns wide so the panel composes cleanly with borders.
func TestView_AllLinesUniformWidth(t *testing.T) {
	m := New(map[string]string{"U1": "alice", "U2": "bob"}, "USELF")
	m.SetSummaries(sampleSummaries())
	const (
		height = 60
		width  = 40
	)
	out := m.View(height, width)
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != width {
			t.Errorf("line %d width = %d, want %d (line=%q)", i, w, width, line)
		}
	}
}
