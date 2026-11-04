package thread

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// A reply or the parent that grows in place keeps a thread pane that
// was at the bottom at the bottom.

// unfurl is the change echo that adds a link preview to msg, its last
// row marked "previewtail".
func unfurl(msg messages.MessageItem) messages.MessageItem {
	msg.LegacyAttachments = []blockkit.LegacyAttachment{{
		Title: "Preview title",
		Text:  "preview one\npreview two\npreview three\npreviewtail",
	}}
	return msg
}

// assertShows checks that want is on screen.
func assertShows(t *testing.T, view, want string) {
	t.Helper()
	if text := strings.Join(p3Rows(view), "\n"); !strings.Contains(text, want) {
		t.Errorf("%q not on screen:\n%s", want, text)
	}
}

func TestUpdateMessageInPlaceUnfurlKeepsBottom(t *testing.T) {
	m := p3Thread(t, 40)
	_ = m.View(20, 80)
	if !m.UpdateMessageInPlace(unfurl(m.Replies()[39])) {
		t.Fatal("last reply not found")
	}
	assertShows(t, m.View(20, 80), "previewtail")
}

func TestUpdateMessageInPlaceScrolledUpKeepsTopRow(t *testing.T) {
	m := p3Thread(t, 40)
	_ = m.View(20, 80)
	m.ScrollUp(30)
	before := p3Rows(m.View(20, 80))
	m.UpdateMessageInPlace(unfurl(m.Replies()[39]))
	after := p3Rows(m.View(20, 80))
	top := m.chromeHeight
	if before[top] != after[top] {
		t.Errorf("an unfurl below the window moved it:\n before %q\n after  %q", before[top], after[top])
	}
}

func TestUpdateParentInPlaceKeepsBottom(t *testing.T) {
	m := p3Thread(t, 40)
	before := p3Rows(m.View(20, 80))
	parent := unfurl(messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"})
	if !m.UpdateParentInPlace(parent) {
		t.Fatal("parent not updated")
	}
	after := p3Rows(m.View(20, 80))
	if b, a := before[len(before)-1], after[len(after)-1]; a != b {
		t.Errorf("the parent growing moved the bottom row:\n before %q\n after  %q", b, a)
	}
}

func TestUpdateReactionFirstPillOnLastReplyKeepsBottom(t *testing.T) {
	m := p3Thread(t, 40)
	_ = m.View(20, 80)
	y0 := m.list.YOffset()
	m.UpdateReaction(m.Replies()[39].TS, "tada", "U2", false)
	assertShows(t, m.View(20, 80), "🎉 1")
	if m.list.YOffset() <= y0 {
		t.Errorf("YOffset %d, want more than %d (the pill row)", m.list.YOffset(), y0)
	}
}

// The authoritative copy of a reply the user just sent replaces the
// optimistic one in place; when it is taller (an unfurl that came with
// it), the pane stays at the bottom.
func TestSelfSentReplyReplacedTallerKeepsBottom(t *testing.T) {
	sent := messages.MessageItem{TS: "1800000000.000000", UserName: "me", Text: "mark999 see link", Timestamp: "11:00 AM"}
	for _, tc := range []struct {
		name    string
		add     messages.MessageItem
		replace func(m *Model) bool
	}{
		{"SwapLocalSentReply", messages.MessageItem{TS: "local:1", UserName: "me", Text: "mark999 see link", Timestamp: "11:00 AM"},
			func(m *Model) bool { return m.SwapLocalSentReply("local:1", unfurl(sent)) }},
		{"UpsertSelfSentReply", sent,
			func(m *Model) bool { m.UpsertSelfSentReply(unfurl(sent)); return len(m.Replies()) == 41 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := p3Thread(t, 40)
			_ = m.View(20, 80)
			m.AddReply(tc.add)
			_ = m.View(20, 80)
			if !tc.replace(m) {
				t.Fatal("reply not replaced")
			}
			assertShows(t, m.View(20, 80), "previewtail")
		})
	}
}
