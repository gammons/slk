package thread

import (
	stdimage "image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
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
	if m.SelectedReply() != nil {
		t.Error("SelectedReply should be nil when empty")
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
