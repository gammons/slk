package messages

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// A message that grows in place (an unfurl, an edit, a first reaction,
// a first reply) keeps a pane that was at the bottom at the bottom.

// unfurl is the change echo that adds a link preview to msg: a legacy
// attachment several rows tall, its last row marked "previewtail".
func unfurl(msg MessageItem) MessageItem {
	msg.LegacyAttachments = []blockkit.LegacyAttachment{{
		Title: "Preview title",
		Text:  "preview one\npreview two\npreview three\npreviewtail",
	}}
	return msg
}

// assertAtBottom checks the pane has no "-- more below --" row and shows want.
func assertAtBottom(t *testing.T, view, want string) {
	t.Helper()
	rows := p3MsgRows(view)
	text := strings.Join(rows, "\n")
	if strings.Contains(text, "more below") {
		t.Errorf("pane left the bottom (more below):\n%s", text)
	}
	if !strings.Contains(text, want) {
		t.Errorf("%q not on screen:\n%s", want, text)
	}
}

func TestUpdateMessageInPlaceUnfurlKeepsBottom(t *testing.T) {
	m := p3Messages(t, 40, 0)
	_ = m.View(20, 80)
	y0 := m.YOffset()
	last := m.Messages()[39]
	if !m.UpdateMessageInPlace(unfurl(last)) {
		t.Fatal("last message not found")
	}
	assertAtBottom(t, m.View(20, 80), "previewtail")
	if m.YOffset() <= y0 {
		t.Errorf("YOffset %d, want more than %d (the preview's rows)", m.YOffset(), y0)
	}
}

func TestUpdateMessageInPlaceEditKeepsBottom(t *testing.T) {
	m := p3Messages(t, 40, 0)
	_ = m.View(20, 80)
	edit := m.Messages()[39]
	edit.Text += "\nmore\nmore\nmore\nedittail"
	edit.IsEdited = true
	m.UpdateMessageInPlace(edit)
	assertAtBottom(t, m.View(20, 80), "edittail")
}

func TestUpdateMessageInPlaceScrolledUpKeepsTopRow(t *testing.T) {
	m := p3Messages(t, 40, 0)
	_ = m.View(20, 80)
	m.ScrollUp(30)
	before := p3MsgRows(m.View(20, 80))
	m.UpdateMessageInPlace(unfurl(m.Messages()[39]))
	after := p3MsgRows(m.View(20, 80))
	top := m.ChromeHeight()
	if before[top] != after[top] {
		t.Errorf("an unfurl below the window moved it:\n before %q\n after  %q", before[top], after[top])
	}
}

func TestUpdateMessageInPlaceUnknownTSLeavesThePane(t *testing.T) {
	m := p3Messages(t, 40, 0)
	before := m.View(20, 80)
	if m.UpdateMessageInPlace(unfurl(MessageItem{TS: "1.000000"})) {
		t.Fatal("unknown ts reported found")
	}
	if got := m.View(20, 80); got != before {
		t.Error("an echo for a message not shown changed the pane")
	}
}

func TestUpdateReactionFirstPillKeepsBottom(t *testing.T) {
	m := p3Messages(t, 40, 0)
	_ = m.View(20, 80)
	y0 := m.YOffset()
	m.UpdateReaction(m.Messages()[39].TS, "tada", "U2", false)
	assertAtBottom(t, m.View(20, 80), "🎉 1")
	if m.YOffset() <= y0 {
		t.Errorf("YOffset %d, want more than %d (the pill row)", m.YOffset(), y0)
	}
}

func TestIncrementReplyCountKeepsBottom(t *testing.T) {
	m := p3Messages(t, 40, 0)
	_ = m.View(20, 80)
	y0 := m.YOffset()
	m.IncrementReplyCount(m.Messages()[39].TS, "1800000000.000000")
	assertAtBottom(t, m.View(20, 80), "1 reply")
	if m.YOffset() <= y0 {
		t.Errorf("YOffset %d, want more than %d (the reply row)", m.YOffset(), y0)
	}
}
