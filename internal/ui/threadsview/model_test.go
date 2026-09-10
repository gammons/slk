package threadsview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
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

// Selected rows must render with the green left-border (▌ in Accent),
// matching the messages/thread-panel selection convention. Non-selected
// rows reserve the same column with a background-colored (invisible)
// border for layout consistency.
func TestView_SelectedRowHasGreenLeftBorder(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	out := m.View(40, 60)

	// Find the line containing the first card's channel name. That line's
	// first column should be the ▌ glyph styled in Accent.
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "general") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("no line containing 'general' in output:\n%s", out)
	}
	if !strings.Contains(line, "▌") {
		t.Errorf("selected row should contain the ▌ left-border glyph; got %q", line)
	}
	// The green color is encoded as an ANSI escape in the rendered string.
	// styles.Accent's color contains the substring "C8" (default #50C878);
	// custom themes may differ, so just assert the glyph and the presence
	// of an ANSI color escape preceding it.
	if !strings.Contains(line, "\x1b[") {
		t.Errorf("expected ANSI escape (color) on selected row border; got %q", line)
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

func TestSetUserNames_IdempotentDoesNotBumpVersion(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	names := map[string]string{"U1": "alice", "U2": "bob"}
	m.SetUserNames(names)
	v0 := m.Version()
	m.SetUserNames(names) // same map -> should be a no-op
	if v1 := m.Version(); v1 != v0 {
		t.Errorf("SetUserNames(same map) bumped Version: v0=%d v1=%d", v0, v1)
	}
	// A genuinely different map MUST still bump.
	m.SetUserNames(map[string]string{"U1": "alice", "U2": "carol"})
	if v2 := m.Version(); v2 == v0 {
		t.Errorf("SetUserNames(different map) did NOT bump Version: v0=%d v2=%d", v0, v2)
	}
}

// TestVersion_StableAcrossIdenticalSetCalls is the regression guard for the
// app.go:4068-4093 cache-key bug: pushing the same userNames + selfUserID
// repeatedly must NOT bump Version, otherwise the panel cache can never hit.
func TestVersion_StableAcrossIdenticalSetCalls(t *testing.T) {
	names := map[string]string{"U1": "alice", "U2": "bob"}
	m := New(names, "U1")
	m.SetSummaries(sampleSummaries())
	v0 := m.Version()
	for i := 0; i < 5; i++ {
		m.SetUserNames(names)
		m.SetSelfUserID("U1")
	}
	if v1 := m.Version(); v1 != v0 {
		t.Errorf("Version drifted across identical Set calls: v0=%d v1=%d", v0, v1)
	}
}

func TestMarkByThreadTSUnread_FlipsFlagAndReturnsTrue(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", Unread: false},
		{ChannelID: "C1", ThreadTS: "P2", Unread: false},
	})

	if !m.MarkByThreadTSUnread("C1", "P2") {
		t.Fatal("expected return true when flag flipped")
	}

	for _, s := range m.Summaries() {
		if s.ThreadTS == "P2" && !s.Unread {
			t.Error("P2 should be Unread=true after MarkByThreadTSUnread")
		}
		if s.ThreadTS == "P1" && s.Unread {
			t.Error("P1 should remain Unread=false")
		}
	}
}

func TestMarkByThreadTSUnread_AlreadyUnread_ReturnsFalse(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "P1", Unread: true}})

	if m.MarkByThreadTSUnread("C1", "P1") {
		t.Error("expected false when flag was already true")
	}
}

func TestMarkByThreadTSUnread_NotFound_ReturnsFalse(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "P1", Unread: false}})

	if m.MarkByThreadTSUnread("C2", "P9") {
		t.Error("expected false when (channel, thread) not in summaries")
	}
}

func TestMarkByThreadTSUnread_EmptyArgs_ReturnsFalse(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{{ChannelID: "C1", ThreadTS: "P1", Unread: false}})

	if m.MarkByThreadTSUnread("", "P1") {
		t.Error("expected false for empty channelID")
	}
	if m.MarkByThreadTSUnread("C1", "") {
		t.Error("expected false for empty threadTS")
	}
}

// itoaU8 / fmtRGBBg / fmtRGBFg are local helpers used by the
// focus-dim test. They build the SGR fragments lipgloss/v2 emits
// for RGB foreground / background ("38;2;R;G;B" / "48;2;R;G;B"),
// so the test can substring-match against rendered output.
func itoaU8(v uint8) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func fmtRGBFg(r, g, b uint8) string { return "38;2;" + itoaU8(r) + ";" + itoaU8(g) + ";" + itoaU8(b) }
func fmtRGBBg(r, g, b uint8) string { return "48;2;" + itoaU8(r) + ";" + itoaU8(g) + ";" + itoaU8(b) }

// TestSelectedCardDimsWhenUnfocused asserts:
//  1. Focused selected row uses Accent for the border foreground and
//     SelectionTintColor(true) for the background.
//  2. Unfocused selected row uses TextMuted for the border foreground
//     and SelectionTintColor(false) for the background — i.e. the
//     bright Accent goes away when the panel loses focus.
func TestSelectedCardDimsWhenUnfocused(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())

	// Focused
	m.SetFocused(true)
	focusedOut := m.View(20, 60)

	ar, ag, ab, _ := styles.Accent.RGBA()
	wantAccent := fmtRGBFg(uint8(ar>>8), uint8(ag>>8), uint8(ab>>8))
	if !strings.Contains(focusedOut, wantAccent) {
		t.Fatalf("focused selected card missing Accent border fg %q", wantAccent)
	}
	fr, fg, fb, _ := styles.SelectionTintColor(true).RGBA()
	wantFocusedTint := fmtRGBBg(uint8(fr>>8), uint8(fg>>8), uint8(fb>>8))
	if !strings.Contains(focusedOut, wantFocusedTint) {
		t.Fatalf("focused selected card missing focused tint bg %q", wantFocusedTint)
	}

	// Unfocused
	m.SetFocused(false)
	unfocusedOut := m.View(20, 60)

	if strings.Contains(unfocusedOut, wantAccent) {
		t.Fatal("unfocused selected card still contains Accent border fg; should dim to TextMuted")
	}
	mr, mg, mb, _ := styles.TextMuted.RGBA()
	wantMuted := fmtRGBFg(uint8(mr>>8), uint8(mg>>8), uint8(mb>>8))
	if !strings.Contains(unfocusedOut, wantMuted) {
		t.Fatalf("unfocused selected card missing TextMuted border fg %q", wantMuted)
	}
	if strings.Contains(unfocusedOut, wantFocusedTint) {
		t.Fatal("unfocused selected card still contains focused tint bg; should use unfocused tint")
	}
}

func TestView_RendersBannerWhenSubscriptionsUnavailable(t *testing.T) {
	m := New(map[string]string{}, "U1")
	m.SetSubscriptionsAvailable(false)
	out := m.View(10, 80)
	if !strings.Contains(out, "Threads list unavailable") {
		t.Errorf("expected banner in view, got:\n%s", out)
	}
}

func TestView_NoBannerWhenSubscriptionsAvailable(t *testing.T) {
	m := New(map[string]string{}, "U1")
	// Default is true; no need to call setter.
	out := m.View(10, 80)
	if strings.Contains(out, "Threads list unavailable") {
		t.Errorf("did not expect banner, got:\n%s", out)
	}
}

func TestView_BannerVisibleWithEmptySummaries(t *testing.T) {
	m := New(map[string]string{}, "U1")
	m.SetSubscriptionsAvailable(false)
	out := m.View(10, 80)
	if !strings.Contains(out, "Threads list unavailable") {
		t.Errorf("expected banner with empty summaries, got:\n%s", out)
	}
}

func TestView_BannerVisibleWithSummaries(t *testing.T) {
	m := New(map[string]string{"U2": "alice"}, "U1")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ChannelName: "general", ThreadTS: "1.0", ParentText: "hi", LastReplyTS: "2.0", LastReplyBy: "U2"},
	})
	m.SetSubscriptionsAvailable(false)
	out := m.View(20, 80)
	if !strings.Contains(out, "Threads list unavailable") {
		t.Errorf("expected banner with summaries present, got:\n%s", out)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("expected summary content alongside banner, got:\n%s", out)
	}
}

// TestClickAt_SelectsCardOnCardRow guards Bug B: clicking on any of
// the three rows of a thread card must move the selection cursor to
// that card and return true so the caller can follow up with
// openSelectedThreadCmd. Layout is cardStride=4 (3 content rows +
// 1 separator row); card 0 occupies rows [0,1,2], separator at row
// 3, card 1 at rows [4,5,6], separator at row 7, card 2 at [8,9,10].
func TestClickAt_SelectsCardOnCardRow(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())

	// Click row 5 — middle row of card 1. Should select card 1.
	if !m.ClickAt(5) {
		t.Fatal("ClickAt(5) returned false; want true (row inside card 1)")
	}
	if got := m.SelectedIndex(); got != 1 {
		t.Errorf("after ClickAt(5) SelectedIndex = %d, want 1", got)
	}

	// Click row 0 — first row of card 0. Should select card 0.
	if !m.ClickAt(0) {
		t.Fatal("ClickAt(0) returned false; want true (row inside card 0)")
	}
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("after ClickAt(0) SelectedIndex = %d, want 0", got)
	}
}

// TestClickAt_SeparatorRowIsNoop ensures a click on the blank line
// between cards is ignored (no selection movement, returns false).
func TestClickAt_SeparatorRowIsNoop(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	m.MoveDown() // selected = 1

	// Row 3 is the separator between card 0 and card 1.
	if m.ClickAt(3) {
		t.Fatal("ClickAt(3) returned true on separator row; want false")
	}
	if got := m.SelectedIndex(); got != 1 {
		t.Errorf("separator click should not move selection; got %d, want 1", got)
	}
}

// TestClickAt_OutOfRangeIsNoop guards clicks on the blank-fill area
// below the last card and on the negative-y region. Both return
// false and leave selection alone.
func TestClickAt_OutOfRangeIsNoop(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	// Two cards: last card occupies up to row 6 (card 1 = rows 4,5,6).
	// Row 50 is well past the list.
	if m.ClickAt(50) {
		t.Errorf("ClickAt(50) returned true; want false (past last card)")
	}
	if m.ClickAt(-1) {
		t.Errorf("ClickAt(-1) returned true; want false (negative y)")
	}
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("out-of-range click moved selection: got %d, want 0", got)
	}
}

// TestClickAt_AccountsForBannerRow guards that the
// "Threads list unavailable" banner offsets the body by one row.
// With banner shown, paneY=0 is the banner (no-op) and paneY=1 maps
// to the first card row.
func TestClickAt_AccountsForBannerRow(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(sampleSummaries())
	m.SetSubscriptionsAvailable(false)
	m.MoveDown() // selected = 1, so the banner-aware test starts away from 0

	if m.ClickAt(0) {
		t.Errorf("ClickAt(0) on banner returned true; want false")
	}
	if got := m.SelectedIndex(); got != 1 {
		t.Errorf("banner click moved selection: got %d, want 1", got)
	}
	// With banner reserved, paneY=1 is the first row of card 0.
	if !m.ClickAt(1) {
		t.Fatal("ClickAt(1) with banner returned false; want true (row 0 of card 0)")
	}
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("after ClickAt(1) with banner SelectedIndex = %d, want 0", got)
	}
}

// TestClickAt_AccountsForYOffset guards that ClickAt respects the
// current viewport scroll: rowY refers to a position in the visible
// window, not an absolute line in the flat list. Driving yOffset via
// moving the selection cursor past the viewport bottom (View() will
// snap yOffset down to keep the selected card visible).
func TestClickAt_AccountsForYOffset(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	sums := []cache.ThreadSummary{
		{ChannelID: "C1", ChannelName: "g1", ThreadTS: "1.0", ParentText: "a", LastReplyTS: "2.0", LastReplyBy: "U2"},
		{ChannelID: "C2", ChannelName: "g2", ThreadTS: "2.0", ParentText: "b", LastReplyTS: "3.0", LastReplyBy: "U2"},
		{ChannelID: "C3", ChannelName: "g3", ThreadTS: "3.0", ParentText: "c", LastReplyTS: "4.0", LastReplyBy: "U2"},
	}
	m.SetSummaries(sums)
	// 3 cards = 11 flat lines; viewport height = 5 forces snap to
	// scroll once we move past card 0.
	m.MoveDown()
	m.MoveDown() // selected = 2 (last card)
	_ = m.View(5, 80)

	// yOffset is now non-zero (snapped to keep card 2 visible).
	// Card 2 starts at absolute line 8 with cardContentLines=3, so
	// to fit content end (line 10) inside a 5-row viewport,
	// yOffset = end - height = 11 - 5 = 6. Within the viewport,
	// card 2's first row is at viewport row 8 - 6 = 2.
	if !m.ClickAt(2) {
		t.Fatalf("ClickAt(2) on card 2 (with yOffset=%d) returned false; want true", m.yOffset)
	}
	if got := m.SelectedIndex(); got != 2 {
		t.Errorf("ClickAt(2) with yOffset set: SelectedIndex = %d, want 2", got)
	}

	// And the rowY=0 click should select card 1 (whose first row is
	// at absolute line 4, which maps to viewport row 4 - 6 = -2 —
	// so actually card 1 is OFF-screen; rowY=0 corresponds to
	// absolute line yOffset + 0 = 6, which lies inside card 1's
	// separator/footer region. Let's check by computing the actual
	// row mapping using cardStride math:
	//   absLine = yOffset + 0 = 6
	//   6 % 4 = 2 (< cardContentLines=3) → card row
	//   idx = 6 / 4 = 1 → card 1, row 2 (footer)
	if !m.ClickAt(0) {
		t.Fatalf("ClickAt(0) at yOffset=%d returned false; want true (card 1 footer)", m.yOffset)
	}
	if got := m.SelectedIndex(); got != 1 {
		t.Errorf("ClickAt(0) at yOffset=6 should select card 1; got %d", got)
	}
}

func TestMarkByThreadTSReadAt_CursorBehindLatestSetsUnread(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: false},
	})

	if !m.MarkByThreadTSReadAt("C1", "P1", "4.000000") {
		t.Fatal("want true when the flag flips to unread")
	}
	if !m.Summaries()[0].Unread {
		t.Error("cursor behind latest reply must render unread")
	}
}

func TestMarkByThreadTSReadAt_CursorAtLatestClearsUnread(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	if !m.MarkByThreadTSReadAt("C1", "P1", "5.000000") {
		t.Fatal("want true when the flag flips to read")
	}
	if m.Summaries()[0].Unread {
		t.Error("cursor at latest reply must render read")
	}
}

func TestMarkByThreadTSReadAt_NoChangeReturnsFalse(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	if m.MarkByThreadTSReadAt("C1", "P1", "4.000000") {
		t.Error("want false when the flag is already correct")
	}
}

func TestMarkByThreadTSReadAt_UnknownThreadReturnsFalse(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: true},
	})

	if m.MarkByThreadTSReadAt("C1", "P_MISSING", "9.000000") {
		t.Error("want false for a thread not in the list")
	}
	if m.MarkByThreadTSReadAt("", "P1", "9.000000") {
		t.Error("want false for an empty channelID")
	}
}

func TestMarkByThreadTSReadAt_BumpsVersionOnlyOnChange(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries([]cache.ThreadSummary{
		{ChannelID: "C1", ThreadTS: "P1", LastReplyTS: "5.000000", Unread: false},
	})

	before := m.Version()
	if !m.MarkByThreadTSReadAt("C1", "P1", "4.000000") {
		t.Fatal("want true when the flag flips to unread")
	}
	if m.Version() == before {
		t.Error("a changed flag must bump the version so the list re-renders")
	}

	after := m.Version()
	if m.MarkByThreadTSReadAt("C1", "P1", "4.000000") {
		t.Fatal("want false on the repeated no-op call")
	}
	if m.Version() != after {
		t.Error("a no-op must not bump the version")
	}
}
