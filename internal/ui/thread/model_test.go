package thread

import (
	"context"
	"fmt"
	stdimage "image"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestSetThread(t *testing.T) {
	m := New()

	parent := messages.MessageItem{
		TS:       "1700000001.000000",
		UserName: "alice",
		Text:     "parent message",
	}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "reply 1"},
		{TS: "1700000003.000000", UserName: "charlie", Text: "reply 2"},
	}

	m.SetThread(parent, replies, "C123", "1700000001.000000")

	if m.ThreadTS() != "1700000001.000000" {
		t.Errorf("expected thread ts 1700000001.000000, got %s", m.ThreadTS())
	}
	if m.IsEmpty() {
		t.Error("expected thread to not be empty after SetThread")
	}
	if m.ReplyCount() != 2 {
		t.Errorf("expected 2 replies, got %d", m.ReplyCount())
	}
}

func TestThreadViewUpdatesAuthorStatus(t *testing.T) {
	m := New()
	m.SetThread(
		messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent"},
		[]messages.MessageItem{{TS: "2.0", UserID: "U2", UserName: "bob", Text: "reply"}},
		"C1",
		"1.0",
	)

	m.PatchUserStatus("U2", peerstatus.Status{Emoji: ":calendar:"})
	if got := ansi.Strip(m.View(20, 60)); !strings.Contains(got, "bob "+emojiutil.CodeMap()[":calendar:"]) {
		t.Fatalf("thread reply header does not show the author's status:\n%s", got)
	}

	m.PatchUserStatus("U2", peerstatus.Status{Huddle: peerstatus.HuddleActive})
	if got := ansi.Strip(m.View(20, 60)); !strings.Contains(got, "bob "+peerstatus.HuddleGlyph) {
		t.Fatalf("thread reply header does not replace the status with the huddle glyph:\n%s", got)
	}
}

func TestClear(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	m.SetThread(parent, nil, "C123", "1700000001.000000")

	m.Clear()

	if !m.IsEmpty() {
		t.Error("expected thread to be empty after Clear")
	}
	if m.ThreadTS() != "" {
		t.Errorf("expected empty thread ts after Clear, got %s", m.ThreadTS())
	}
}

func TestAddReply(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	m.SetThread(parent, nil, "C123", "1700000001.000000")

	m.AddReply(messages.MessageItem{TS: "1700000002.000000", UserName: "bob", Text: "hey"})

	if m.ReplyCount() != 1 {
		t.Errorf("expected 1 reply, got %d", m.ReplyCount())
	}
}

// TestAddReply_AlwaysScrollsToBottom asserts that an incoming thread
// reply scrolls the thread panel to the bottom even when the user has
// scrolled up.
func TestAddReply_AlwaysScrollsToBottom(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "r1"},
		{TS: "1700000003.000000", UserName: "carol", Text: "r2"},
		{TS: "1700000004.000000", UserName: "dave", Text: "r3"},
	}
	m.SetThread(parent, replies, "C123", "1700000001.000000")

	// Move selection up so we're explicitly NOT at the bottom.
	m.MoveUp()
	m.MoveUp()
	if m.selected == m.ReplyCount()-1 {
		t.Fatalf("test setup: expected selection above bottom, got %d", m.selected)
	}

	m.AddReply(messages.MessageItem{TS: "1700000005.000000", UserName: "eve", Text: "r4"})

	wantIdx := m.ReplyCount() - 1
	if m.selected != wantIdx {
		t.Errorf("AddReply should scroll to bottom: selected=%d want=%d", m.selected, wantIdx)
	}
}

func TestNavigation(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1700000001.000000", UserName: "alice", Text: "hi"}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "r1"},
		{TS: "1700000003.000000", UserName: "charlie", Text: "r2"},
		{TS: "1700000004.000000", UserName: "dave", Text: "r3"},
	}
	m.SetThread(parent, replies, "C123", "1700000001.000000")

	// Should start at the bottom (newest reply) per SetThread's contract,
	// so opening a long thread lands the user on the latest activity.
	if m.selected != 2 {
		t.Errorf("expected selected=2 (bottom), got %d", m.selected)
	}

	m.GoToTop()
	if m.selected != 0 {
		t.Errorf("expected selected=0 after GoToTop, got %d", m.selected)
	}

	m.MoveDown()
	if m.selected != 1 {
		t.Errorf("expected selected=1, got %d", m.selected)
	}

	m.MoveDown()
	m.MoveDown() // should not go past end
	if m.selected != 2 {
		t.Errorf("expected selected=2, got %d", m.selected)
	}
}

func TestViewRendersContent(t *testing.T) {
	m := New()
	parent := messages.MessageItem{
		TS:        "1700000001.000000",
		UserName:  "alice",
		Text:      "parent message",
		Timestamp: "10:30 AM",
	}
	replies := []messages.MessageItem{
		{TS: "1700000002.000000", UserName: "bob", Text: "reply one", Timestamp: "10:31 AM"},
	}
	m.SetThread(parent, replies, "C123", "1700000001.000000")

	view := m.View(20, 40)

	if !strings.Contains(view, "Thread") {
		t.Error("expected view to contain 'Thread'")
	}
	if !strings.Contains(view, "alice") {
		t.Error("expected view to contain parent username 'alice'")
	}
	if !strings.Contains(view, "bob") {
		t.Error("expected view to contain reply username 'bob'")
	}
}

// TestViewInsertsDateSeparatorAcrossDays asserts that the thread view
// inserts a centered date-divider row above a reply whose local day
// differs from the parent's, and that no divider is emitted when every
// reply lands on the parent's day. Mirrors the channel pane's
// day-boundary divider so threads that span days are legible at a
// glance.
func TestViewInsertsDateSeparatorAcrossDays(t *testing.T) {
	const parentTS = "1700000000.000000" // 2023-11-14 22:13:20 UTC
	// 48 hours later — guaranteed to land on a different LOCAL day in
	// every timezone, so the assertion is timezone-independent.
	const dayLaterTS = "1700172800.000000"
	// Same minute as the parent — same local day in every timezone.
	const sameDayTS = "1700000060.000000"

	parentDateLabel := messages.FormatDateSeparator(messages.DateFromTS(parentTS))
	laterDateLabel := messages.FormatDateSeparator(messages.DateFromTS(dayLaterTS))
	if parentDateLabel == laterDateLabel {
		t.Fatalf("test fixture broken: parent (%q) and later (%q) resolved to the same label", parentDateLabel, laterDateLabel)
	}

	// Case 1: reply spans into a new day -> divider for the later day
	// must appear; the parent's own day must NOT appear as a divider
	// (the parent sits in its own chrome block, not a divider row).
	m := New()
	parent := messages.MessageItem{TS: parentTS, UserName: "alice", Text: "parent", Timestamp: "10:30 PM"}
	replies := []messages.MessageItem{
		{TS: sameDayTS, UserName: "bob", Text: "same-day reply", Timestamp: "10:31 PM"},
		{TS: dayLaterTS, UserName: "carol", Text: "next-day reply", Timestamp: "10:33 PM"},
	}
	m.SetThread(parent, replies, "C1", parentTS)

	plain := ansi.Strip(m.View(40, 80))
	if !strings.Contains(plain, laterDateLabel) {
		t.Errorf("expected view to contain day-divider label %q for reply on a later day; got:\n%s", laterDateLabel, plain)
	}
	if strings.Contains(plain, "── "+parentDateLabel+" ──") {
		t.Errorf("did not expect a date divider for the parent's own day (%q); got:\n%s", parentDateLabel, plain)
	}
	// Sanity: the centered divider is bracketed by box-drawing dashes.
	if !strings.Contains(plain, "── "+laterDateLabel+" ──") {
		t.Errorf("expected centered divider \"── %s ──\"; got:\n%s", laterDateLabel, plain)
	}

	// Case 2: every reply on the parent's day -> no divider at all.
	m2 := New()
	m2.SetThread(parent, []messages.MessageItem{
		{TS: sameDayTS, UserName: "bob", Text: "same-day reply", Timestamp: "10:31 PM"},
	}, "C1", parentTS)
	plain2 := ansi.Strip(m2.View(40, 80))
	if strings.Contains(plain2, "── "+parentDateLabel+" ──") || strings.Contains(plain2, "── "+laterDateLabel+" ──") {
		t.Errorf("did not expect any date divider when all replies share the parent's day; got:\n%s", plain2)
	}
}

func TestUpdateMessageInPlace_Found(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "P1", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "R1", Text: "old reply"},
		{TS: "R2", Text: "other"},
	}
	m.SetThread(parent, replies, "C1", "P1")

	if !m.UpdateMessageInPlace("R1", "new reply") {
		t.Fatal("expected true updating R1")
	}
	if m.replies[0].Text != "new reply" {
		t.Errorf("text not updated: %q", m.replies[0].Text)
	}
	if !m.replies[0].IsEdited {
		t.Error("IsEdited not set")
	}
	if m.replies[1].Text != "other" {
		t.Error("other reply should be untouched")
	}
}

func TestUpdateMessageInPlace_NotFound(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "P1"}, []messages.MessageItem{
		{TS: "R1", Text: "a"},
	}, "C1", "P1")
	if m.UpdateMessageInPlace("nope", "x") {
		t.Error("expected false for missing TS")
	}
}

func TestRemoveMessageByTS_Middle(t *testing.T) {
	m := New()
	replies := []messages.MessageItem{
		{TS: "R1", Text: "a"},
		{TS: "R2", Text: "b"},
		{TS: "R3", Text: "c"},
	}
	m.SetThread(messages.MessageItem{TS: "P1"}, replies, "C1", "P1")
	if !m.RemoveMessageByTS("R2") {
		t.Fatal("expected true")
	}
	if len(m.replies) != 2 || m.replies[0].TS != "R1" || m.replies[1].TS != "R3" {
		t.Errorf("unexpected replies: %+v", m.replies)
	}
}

func TestRemoveMessageByTS_NotFound(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "P1"}, []messages.MessageItem{
		{TS: "R1", Text: "a"},
	}, "C1", "P1")
	if m.RemoveMessageByTS("nope") {
		t.Error("expected false for missing TS")
	}
	if len(m.replies) != 1 {
		t.Error("replies should be unchanged")
	}
}

func TestRemoveMessageByTS_LastBecomesEmpty(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "P1"}, []messages.MessageItem{
		{TS: "R1", Text: "only"},
	}, "C1", "P1")
	if !m.RemoveMessageByTS("R1") {
		t.Fatal("expected true")
	}
	if len(m.replies) != 0 {
		t.Error("expected empty replies")
	}
	// With no replies left the cursor falls back to the parent row
	// (parentSelected) so message ops still have a target.
	sel := m.SelectedReply()
	if sel == nil || sel.TS != "P1" {
		t.Errorf("SelectedReply = %+v, want parent (TS P1) when replies empty", sel)
	}
}

func TestRemoveMessageByTS_RemovesSelected(t *testing.T) {
	m := New()
	replies := []messages.MessageItem{
		{TS: "R1", Text: "a"},
		{TS: "R2", Text: "b"},
		{TS: "R3", Text: "c"},
	}
	m.SetThread(messages.MessageItem{TS: "P1"}, replies, "C1", "P1")
	// SetThread sets selected = 0, so explicitly select the last reply
	// to mirror the messages.Model test setup.
	for m.SelectedReply() == nil || m.SelectedReply().TS != "R3" {
		m.MoveDown()
	}
	if !m.RemoveMessageByTS("R3") {
		t.Fatal("expected true")
	}
	// Removing the selected (last) item should clamp selected to len-1 = 1.
	if m.selected != 1 {
		t.Errorf("expected selected=1 after removing last selected reply, got %d", m.selected)
	}
}

func TestUpdateParentInPlace_Match(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "P1", Text: "parent original"}
	m.SetThread(parent, nil, "C1", "P1")
	if !m.UpdateParentInPlace("P1", "parent edited") {
		t.Fatal("expected true")
	}
	if m.ParentMsg().Text != "parent edited" {
		t.Errorf("parent text not updated: %q", m.ParentMsg().Text)
	}
	if !m.ParentMsg().IsEdited {
		t.Error("parent IsEdited not set")
	}
}

func TestUpdateParentInPlace_NoMatch(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "P1", Text: "parent"}, nil, "C1", "P1")
	if m.UpdateParentInPlace("OTHER", "x") {
		t.Error("expected false when TS does not match parent")
	}
	if m.ParentMsg().Text != "parent" {
		t.Error("parent should be unchanged when TS does not match")
	}
}

// itoaU8 / fmtRGBBg are local helpers used by the tint-background test.
// Build the SGR fragment lipgloss/v2 emits for an RGB background
// ("48;2;R;G;B"), so the test can substring-match against rendered
// output without depending on terminal dimensions.
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

func fmtRGBBg(r, g, b uint8) string {
	return "48;2;" + itoaU8(r) + ";" + itoaU8(g) + ";" + itoaU8(b)
}

// TestSelectedReplyContainsTintBackground asserts that the rendered
// thread output for a selected reply contains the SelectionTintColor
// as an ANSI background code. Mirror of the messages-pane test.
func TestSelectedReplyContainsTintBackground(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	m := New()
	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "1.001", UserID: "U2", UserName: "bob", Text: "reply one"},
		{TS: "1.002", UserID: "U3", UserName: "carol", Text: "reply two"},
	}
	m.SetThread(parent, replies, "C123", "1.0")
	m.SetFocused(true)
	// Walk the selection to the second reply (index 1). The thread's
	// initial selection is implementation-defined — moving deterministically
	// avoids depending on it.
	m.MoveDown()
	m.MoveDown()

	out := m.View(20 /*height*/, 60 /*width*/)

	r, g, b, _ := styles.SelectionTintColor(true).RGBA()
	want := fmtRGBBg(uint8(r>>8), uint8(g>>8), uint8(b>>8))
	if !strings.Contains(out, want) {
		t.Fatalf("expected selected reply to contain tint bg %q\nout=%q", want, out)
	}
}

// TestThreadRendersInlineImagePlaceholder asserts that when a reply has
// an image attachment and the renderer's ImageContext has a non-Off
// Protocol with a fetcher, the thread panel emits a reserved-height
// placeholder block (multiple lines of "Loading…" or similar) instead
// of the legacy single-line "[Image] <url>" text.
//
// Uses a real *imgpkg.Fetcher with an empty tempdir cache so Cached()
// returns false and RenderBlock takes the placeholder path without
// needing to mock the (concrete-typed) Fetcher.
func TestThreadRendersInlineImagePlaceholder(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)

	m := New()
	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent"}
	reply := messages.MessageItem{
		TS:       "1.001",
		UserID:   "U2",
		UserName: "bob",
		Text:     "look",
		Attachments: []messages.Attachment{{
			Kind:   "image",
			Name:   "screenshot.png",
			FileID: "F123",
			URL:    "https://example.com/x.png",
			Thumbs: []messages.ThumbSpec{{URL: "https://example.com/x-720.png", W: 320, H: 240}},
		}},
	}
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", "1.0")

	m.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoHalfBlock,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
		// SendMsg deliberately nil: we only need the synchronous
		// Cached() == false branch; the spawned fetch goroutine will
		// no-op since there's no real HTTP client and SendMsg is nil.
	})

	out := ansi.Strip(m.View(20, 60))

	if !strings.Contains(out, "Loading") {
		t.Fatalf("expected reserved-height placeholder for unfetched image, got:\n%s", out)
	}
	// Inline rendering active: the legacy text fallback prefix MUST be absent.
	if strings.Contains(out, "[Image]") && strings.Contains(out, "https://example.com/x.png") {
		t.Fatalf("thread fell back to text rendering; should use inline placeholder. got:\n%s", out)
	}
}

// TestThread_LegacyTextFallback_WhenImageContextOff asserts that without
// SetImageContext (zero-valued context, ProtoOff), the thread panel
// falls back to the legacy "[Image] <url>" text rendering rather than
// silently dropping the attachment. This pins the safe default during
// app startup before SetImageContext has been called.
func TestThread_LegacyTextFallback_WhenImageContextOff(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	m := New()
	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent"}
	reply := messages.MessageItem{
		TS:       "1.001",
		UserID:   "U2",
		UserName: "bob",
		Attachments: []messages.Attachment{{
			Kind:   "image",
			Name:   "x.png",
			FileID: "F123",
			URL:    "https://example.com/x.png",
			Thumbs: []messages.ThumbSpec{{URL: "https://example.com/x-720.png", W: 320, H: 240}},
		}},
	}
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", "1.0")

	// No SetImageContext call — zero-valued context falls back to text.
	out := ansi.Strip(m.View(20, 60))

	if !strings.Contains(out, "[Image]") {
		t.Fatalf("expected [Image] legacy text fallback when no ImageContext set; got:\n%s", out)
	}
}

func TestHasReply(t *testing.T) {
	m := New()

	// Empty thread: HasReply always false.
	if m.HasReply("anything") {
		t.Error("HasReply on empty thread must return false")
	}

	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "p"}
	replies := []messages.MessageItem{
		{TS: "1.001", UserID: "U2", UserName: "bob", Text: "r1"},
		{TS: "1.002", UserID: "U3", UserName: "carol", Text: "r2"},
	}
	m.SetThread(parent, replies, "C1", "1.0")

	// HasReply might be false until View() builds the index — call
	// View once so replyIDToIdx is populated.
	_ = m.View(20, 60)

	if !m.HasReply("1.001") {
		t.Error("expected HasReply(1.001) true after View()")
	}
	if !m.HasReply("1.002") {
		t.Error("expected HasReply(1.002) true after View()")
	}
	if m.HasReply("1.999") {
		t.Error("expected HasReply(1.999) false; not in thread")
	}
}

func TestThreadPatchUserName_UpdatesMatchingRowsAndUserNamesMap(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "U1", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "1.001", UserID: "U1", UserName: "U1", Text: "first reply"},
		{TS: "1.002", UserID: "U2", UserName: "alice", Text: "other reply"},
		{TS: "1.003", UserID: "U1", UserName: "U1", Text: "third reply"},
	}
	m.SetThread(parent, replies, "C1", "1.0")

	verBefore := m.Version()

	m.PatchUserName("U1", "bob")

	if m.parent.UserName != "bob" {
		t.Errorf("parent.UserName = %q, want bob", m.parent.UserName)
	}
	if m.replies[0].UserName != "bob" {
		t.Errorf("replies[0].UserName = %q, want bob", m.replies[0].UserName)
	}
	if m.replies[1].UserName != "alice" {
		t.Errorf("replies[1].UserName should not have changed; got %q", m.replies[1].UserName)
	}
	if m.replies[2].UserName != "bob" {
		t.Errorf("replies[2].UserName = %q, want bob", m.replies[2].UserName)
	}
	if got := m.userNames["U1"]; got != "bob" {
		t.Errorf("userNames[U1] = %q, want bob", got)
	}
	if m.Version() <= verBefore {
		t.Error("Version should bump after PatchUserName")
	}
}

func TestThreadPatchUserName_NoOpWhenUnchanged(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserID: "U2", UserName: "alice", Text: "p"}
	replies := []messages.MessageItem{
		{TS: "1.001", UserID: "U1", UserName: "U1", Text: "hi"},
	}
	m.SetThread(parent, replies, "C1", "1.0")

	m.PatchUserName("U1", "bob") // prime the userNames map
	verBefore := m.Version()

	m.PatchUserName("U1", "bob") // second call, identical

	if m.Version() != verBefore {
		t.Error("Version should NOT bump on no-op PatchUserName")
	}
}

func TestThreadPatchUserName_InvalidatesCacheEvenWithNoMatchingRows(t *testing.T) {
	// Renders happen with userNames consulted at render time; mention
	// text in other-authored replies goes stale when userNames
	// changes. PatchUserName must invalidate the cache even if no
	// reply's UserID == userID.
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserID: "alice", UserName: "alice", Text: "p"}
	replies := []messages.MessageItem{
		{TS: "1.001", UserID: "alice", UserName: "alice", Text: "hello <@U99>"},
	}
	m.SetThread(parent, replies, "C1", "1.0")

	// Prime the render cache by calling View.
	_ = m.View(20, 80)
	if m.cache == nil {
		t.Fatal("expected cache populated after View(); harness assumption failed")
	}

	verBefore := m.userNamesV

	m.PatchUserName("U99", "carol")

	if m.cache != nil {
		t.Error("PatchUserName should have invalidated m.cache so the mention re-resolves")
	}
	if m.userNamesV <= verBefore {
		t.Errorf("userNamesV should bump after PatchUserName (chromeCache depends on it); before=%d after=%d", verBefore, m.userNamesV)
	}
}

// TestHitTestReaction_OnPill asserts that the thread panel records a
// hit rect for every rendered reaction pill, and that HitTestReaction
// returns the correct (replyIdx, emoji) when a click lands inside the
// pill's pane-local coordinates (which include the chromeHeight offset
// for the thread chrome above the replies).
func TestHitTestReaction_OnPill(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserID: "alice", UserName: "alice", Text: "p"}
	replies := []messages.MessageItem{
		{
			TS:        "1.001",
			UserID:    "bob",
			UserName:  "bob",
			Text:      "hello",
			Timestamp: "10:30 AM",
			Reactions: []messages.ReactionItem{
				{Emoji: "thumbsup", Count: 1, HasReacted: false},
				{Emoji: "tada", Count: 2, HasReacted: true},
			},
		},
	}
	m.SetThread(parent, replies, "C1", "1.0")
	// Render at a generous size so the reactions fit without wrap.
	_ = m.View(30, 80)

	if len(m.lastReactionHits) != 2 {
		t.Fatalf("expected 2 reaction hit rects, got %d", len(m.lastReactionHits))
	}

	h := m.lastReactionHits[0]
	if h.rowEnd <= h.rowStart || h.colEnd <= h.colStart {
		t.Fatalf("reaction hit rect is degenerate: rows=[%d,%d) cols=[%d,%d)", h.rowStart, h.rowEnd, h.colStart, h.colEnd)
	}
	// Rows should be at or below the chromeHeight (replies area).
	if h.rowStart < m.chromeHeight {
		t.Errorf("reaction hit rowStart=%d should be >= chromeHeight=%d", h.rowStart, m.chromeHeight)
	}

	rowMid := (h.rowStart + h.rowEnd) / 2
	colMid := (h.colStart + h.colEnd) / 2
	replyIdx, emoji, ok := m.HitTestReaction(rowMid, colMid)
	if !ok {
		t.Fatalf("HitTestReaction(%d,%d) returned ok=false inside recorded rect", rowMid, colMid)
	}
	if emoji != "thumbsup" {
		t.Errorf("emoji got %q want %q", emoji, "thumbsup")
	}
	if replyIdx != 0 {
		t.Errorf("replyIdx got %d want 0", replyIdx)
	}

	// Click on a column outside the pill must not register.
	if _, _, ok := m.HitTestReaction(rowMid, 0); ok {
		t.Error("HitTestReaction at col=0 (border) should return ok=false")
	}
}

// TestHitTestReaction_NoHitsWithoutReactions asserts that a thread
// reply without reactions records zero reaction hits.
func TestHitTestReaction_NoHitsWithoutReactions(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserID: "alice", UserName: "alice", Text: "p"}
	replies := []messages.MessageItem{
		{TS: "1.001", UserID: "bob", UserName: "bob", Text: "no reactions"},
	}
	m.SetThread(parent, replies, "C1", "1.0")
	_ = m.View(20, 80)
	if len(m.lastReactionHits) != 0 {
		t.Errorf("expected 0 reaction hits, got %d", len(m.lastReactionHits))
	}
	if _, _, ok := m.HitTestReaction(0, 0); ok {
		t.Error("HitTestReaction with no reactions should always return ok=false")
	}
}

// Parent message now lives at the top of the scrollable viewContent (not
// pinned in chrome). This means: (1) chromeHeight no longer counts the
// parent rows, (2) the parent shows up in viewContent above the replies,
// (3) ScrollDown moves the parent out of view, leaving the replies. Pins
// the issue-#23 follow-up: a long parent must not block the reply area.
func TestThreadView_ParentScrollsWithReplies(t *testing.T) {
	m := New()
	parent := messages.MessageItem{
		TS:       "1.0",
		UserName: "alice",
		// A long parent body that spans multiple wrapped lines.
		Text:      strings.Repeat("LONGPARENT ", 30),
		Timestamp: "10:30 AM",
	}
	replies := []messages.MessageItem{
		{TS: "2.0", UserName: "bob", Text: "REPLY_MARKER one", Timestamp: "10:31 AM"},
		{TS: "3.0", UserName: "carol", Text: "REPLY_MARKER two", Timestamp: "10:32 AM"},
	}
	m.SetThread(parent, replies, "C1", "1.0")
	_ = m.View(40, 60)

	// Chrome is just header + separator now -- exactly 2 visual rows.
	if m.chromeHeight != 2 {
		t.Errorf("chromeHeight = %d, want 2 (header + separator only)", m.chromeHeight)
	}

	// viewContent must start with the parent block (LONGPARENT) BEFORE
	// the first reply marker. Find the first occurrence of each and
	// assert ordering inside viewContent.
	pIdx := strings.Index(m.viewContent, "LONGPARENT")
	rIdx := strings.Index(m.viewContent, "REPLY_MARKER")
	if pIdx < 0 {
		t.Fatal("viewContent missing parent body")
	}
	if rIdx < 0 {
		t.Fatal("viewContent missing reply marker")
	}
	if pIdx >= rIdx {
		t.Errorf("parent (idx %d) must precede first reply (idx %d) in viewContent", pIdx, rIdx)
	}

	// entryOffsets[0] is now offset by the parent block's height -- not 0
	// as in the old layout. Defends the math used by snap + reaction-hit
	// translation + ClickAt.
	if len(m.entryOffsets) == 0 || m.entryOffsets[0] == 0 {
		t.Errorf("entryOffsets[0] = %v, want > 0 (parent block precedes first reply)", m.entryOffsets)
	}
}

// Thread reply area gets a scrollbar overlay when content exceeds the
// visible height -- matches the messages-pane convention. Defends the
// fix for the "replies view has no scrollbar" report.
func TestThreadView_HasScrollbarWhenOverflowing(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserName: "alice", Text: "p"}
	// Many short replies so the total content easily exceeds a 10-row pane.
	var replies []messages.MessageItem
	for i := 0; i < 30; i++ {
		replies = append(replies, messages.MessageItem{
			TS:       fmt.Sprintf("%d.0", i+2),
			UserName: "bob",
			Text:     fmt.Sprintf("reply %d", i),
		})
	}
	m.SetThread(parent, replies, "C1", "1.0")

	view := m.View(40, 10)
	// Scrollbar uses '│' (track) and '█' (thumb). With 30 replies in a
	// 10-row pane, totalLines >> pane height, so scrollbar.Overlay
	// definitely draws.
	if !strings.ContainsRune(view, '│') && !strings.ContainsRune(view, '█') {
		t.Fatalf("expected scrollbar glyph in overflowing thread view; got:\n%s", view)
	}
}

// TestThreadModel_SetEmojiContext_InvalidatesCache asserts that
// SetEmojiContext on the thread Model bumps the cache version so any
// View()-output consumer (App's panel-output cache) re-renders with
// the new emoji-image context. Mirrors the messages-pane behavior
// added in Phase 6.
func TestThreadModel_SetEmojiContext_InvalidatesCache(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserName: "alice", Text: "hi"}
	m.SetThread(parent, []messages.MessageItem{
		{TS: "1.1", UserName: "alice", UserID: "U1", Text: "hi"},
	}, "C1", "1.0")
	_ = m.View(80, 24)

	startVersion := m.Version()
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emojiutil.PlaceContext{},
		Cells:    2,
		Customs:  nil,
	})
	if m.Version() == startVersion {
		t.Errorf("SetEmojiContext did not bump thread cache version")
	}
}

// TestThreadModel_HandleEmojiImageReady_BumpsVersion asserts that an
// emoji image landing (EmojiImageReadyMsg) invalidates the thread
// render cache so the next View() picks up the now-warm placement.
func TestThreadModel_HandleEmojiImageReady_BumpsVersion(t *testing.T) {
	m := New()
	parent := messages.MessageItem{TS: "1.0", UserName: "alice", Text: "hi"}
	m.SetThread(parent, []messages.MessageItem{
		{TS: "1.1", UserName: "alice", UserID: "U1", Text: "hi"},
	}, "C1", "1.0")
	_ = m.View(80, 24)

	v0 := m.Version()
	m.HandleEmojiImageReady("https://example.com/x.png")
	if m.Version() == v0 {
		t.Errorf("HandleEmojiImageReady did not bump thread cache version")
	}
}

// TestThreadModel_RenderReplyWithImageEmoji_WarmCache wires a fake
// emoji prerender cache and asserts that a thread reply's body text
// containing :thumbsup: renders the kitty placeholder rune sequence
// (warm path), not the literal shortcode text. Mirrors the
// integration test in the messages pane (Phase 6 Task 6.10).
func TestThreadModel_RenderReplyWithImageEmoji_WarmCache(t *testing.T) {
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })

	thumbURL := emojiutil.CDNBaseURL + "1f44d.png"

	ff := newFakePlaceFetcher()
	ff.setPrerendered(emojiutil.EmojiCacheKey(thumbURL), stdimage.Pt(2, 1), imgpkg.Render{
		Cells: stdimage.Pt(2, 1),
		Lines: []string{"\U0010EEEE\U0010EEEE"},
	})

	m := New()
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emojiutil.PlaceContext{Fetcher: ff},
		Cells:    2,
		Customs:  nil,
	})
	parent := messages.MessageItem{TS: "1.0", UserName: "alice", Text: "p"}
	m.SetThread(parent, []messages.MessageItem{
		{TS: "1.1", UserName: "alice", UserID: "U1", Text: "reply :thumbsup:",
			Reactions: []messages.ReactionItem{{Emoji: "thumbsup", Count: 1}},
		},
	}, "C1", "1.0")

	out := m.View(80, 24)
	if !strings.Contains(out, "\U0010EEEE") {
		t.Errorf("thread view does not contain kitty placeholder runes; image mode appears inactive\noutput=%q", out)
	}
	if strings.Contains(out, ":thumbsup:") {
		t.Errorf("thread view contains literal :thumbsup: text; image mode did not replace it\noutput=%q", out)
	}
}

// fakePlaceFetcher is a test fake for emojiutil.PlaceFetcher. v1
// duplicates the one in internal/ui/messages/render_test.go (rather
// than factoring into a shared testutil) — see polish list for the
// follow-up factor-out item.
type fakePlaceFetcher struct {
	prerender map[string]imgpkg.Render
}

func newFakePlaceFetcher() *fakePlaceFetcher {
	return &fakePlaceFetcher{prerender: map[string]imgpkg.Render{}}
}

func (f *fakePlaceFetcher) setPrerendered(key string, t stdimage.Point, r imgpkg.Render) {
	f.prerender[fmt.Sprintf("%s|%dx%d", key, t.X, t.Y)] = r
}

func (f *fakePlaceFetcher) Prerendered(key string, t stdimage.Point, proto imgpkg.Protocol) (imgpkg.Render, bool) {
	r, ok := f.prerender[fmt.Sprintf("%s|%dx%d", key, t.X, t.Y)]
	return r, ok
}

func (f *fakePlaceFetcher) Fetch(ctx context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{}, nil
}

// Compile-time guard that `io` (used by the fake's Render flush type)
// is referenced; renderer flushes are `func(io.Writer) error`.
var _ = io.Discard

func TestThreadUpdateReactionMaintainsUserIDs(t *testing.T) {
	m := New()
	// SetThread(parent, replies, channelID, threadTS); the cursor defaults to
	// the last reply, so the single reply below becomes the selected one.
	m.SetThread(
		messages.MessageItem{TS: "199.0", Text: "parent"},
		[]messages.MessageItem{{TS: "200.0", Text: "reply"}},
		"C1", "199.0",
	)

	m.UpdateReaction("200.0", "eyes", "U1", false)
	reply := m.SelectedReply()
	if reply == nil || len(reply.Reactions) != 1 {
		t.Fatalf("want 1 reaction on selected reply")
	}
	if got := reply.Reactions[0].UserIDs; len(got) != 1 || got[0] != "U1" {
		t.Fatalf("want UserIDs [U1], got %v", got)
	}

	// Add a second user to the same emoji -> appends to the group.
	m.UpdateReaction("200.0", "eyes", "U2", false)
	reply = m.SelectedReply()
	if got := reply.Reactions[0].UserIDs; len(got) != 2 || got[1] != "U2" {
		t.Fatalf("want UserIDs [U1 U2], got %v", got)
	}

	// Remove one of many -> group remains with U2.
	m.UpdateReaction("200.0", "eyes", "U1", true)
	reply = m.SelectedReply()
	if reply == nil || len(reply.Reactions) != 1 {
		t.Fatalf("want 1 reaction remaining after removing one of two")
	}
	if got := reply.Reactions[0].UserIDs; len(got) != 1 || got[0] != "U2" {
		t.Fatalf("want UserIDs [U2] after removing U1, got %v", got)
	}

	// Remove the last user -> group disappears.
	m.UpdateReaction("200.0", "eyes", "U2", true)
	reply = m.SelectedReply()
	if reply != nil && len(reply.Reactions) != 0 {
		t.Fatalf("want 0 reactions after remove, got %d", len(reply.Reactions))
	}
}
