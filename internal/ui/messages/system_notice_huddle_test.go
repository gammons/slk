package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/slack-go/slack"
)

// textBlock is a rich_text block holding one section with one text run.
func textBlock(text string) blockkit.Block {
	return blockkit.RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextSection{
			Type: slack.RTESection,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: text},
			},
		},
	}}
}

// A huddle message has an EMPTY top-level text and the words only in its
// rich_text block. It must draw as a muted notice from the block text,
// not as a Slackbot bubble.
func TestHuddleThreadRendersAsSystemNotice(t *testing.T) {
	m := New([]MessageItem{{
		TS: "1.0", UserID: "USLACKBOT", UserName: "Slackbot",
		Timestamp: "10:30 AM", Subtype: "huddle_thread",
		Blocks: []blockkit.Block{textBlock("A huddle started")},
	}}, "general")

	got := ansi.Strip(m.View(20, 60))
	if strings.Contains(got, "Slackbot") {
		t.Errorf("huddle message shows the system bot's name instead of a notice:\n%s", got)
	}
	if !strings.Contains(got, "A huddle started") {
		t.Errorf("huddle notice text missing:\n%s", got)
	}
}

// With nothing readable in text or blocks a canvas update gets a fixed
// label; no name or duration is invented.
func TestCanvasUpdateFallsBackToLabel(t *testing.T) {
	msg := MessageItem{
		UserID: "USLACKBOT", UserName: "Slackbot", Timestamp: "10:30 AM",
		Subtype: "tabbed_canvas_updated",
		Blocks:  []blockkit.Block{textBlock(" ")},
	}
	got := ansi.Strip(RenderMessage(msg, 60, &RenderContext{}, RenderOptions{}).Content)
	if !strings.Contains(got, "A canvas was updated") || strings.Contains(got, "Slackbot") {
		t.Errorf("canvas notice = %q, want the fallback label and no author", got)
	}

	// Readable block text wins over the label.
	msg.Blocks = []blockkit.Block{textBlock("Canvas edited")}
	got = ansi.Strip(RenderMessage(msg, 60, &RenderContext{}, RenderOptions{}).Content)
	if !strings.Contains(got, "Canvas edited") || strings.Contains(got, "A canvas was updated") {
		t.Errorf("canvas notice = %q, want the block text", got)
	}
}

// A huddle message is a thread parent: the notice keeps the reply count
// line in the channel pane, and not in the thread pane's options.
func TestHuddleNoticeKeepsReplyAffordance(t *testing.T) {
	msg := MessageItem{
		UserID: "USLACKBOT", Timestamp: "10:30 AM", Subtype: "huddle_thread",
		Blocks: []blockkit.Block{textBlock("A huddle started")}, ReplyCount: 3,
	}
	with := ansi.Strip(RenderMessage(msg, 60, &RenderContext{}, RenderOptions{ReplyAffordance: true}).Content)
	if !strings.Contains(with, "[3 replies ->]") {
		t.Errorf("reply line missing from the huddle notice:\n%s", with)
	}
	without := ansi.Strip(RenderMessage(msg, 60, &RenderContext{}, RenderOptions{}).Content)
	if strings.Contains(without, "replies") {
		t.Errorf("reply line drawn without the affordance option:\n%s", without)
	}

	m := New([]MessageItem{msg}, "general")
	if got := ansi.Strip(m.View(20, 60)); !strings.Contains(got, "[3 replies ->]") {
		t.Errorf("channel pane lost the huddle's reply line:\n%s", got)
	}
}

// A normal message with empty text and no subtype is not a notice and
// gets no fallback label.
func TestEmptyTextOrdinaryMessageIsNotANotice(t *testing.T) {
	m := New([]MessageItem{{
		TS: "1.0", UserID: "U000", UserName: "alice", Timestamp: "10:30 AM",
	}}, "general")
	got := ansi.Strip(m.View(20, 60))
	if !strings.Contains(got, "alice") {
		t.Errorf("ordinary message lost its author line:\n%s", got)
	}
	if strings.Contains(got, "huddle") || strings.Contains(got, "canvas") {
		t.Errorf("fallback label leaked onto an ordinary message:\n%s", got)
	}
}

func TestIsSystemNoticeSubtypeHuddleAndCanvas(t *testing.T) {
	for _, s := range []string{"huddle_thread", "tabbed_canvas_updated"} {
		if !IsSystemNoticeSubtype(s) {
			t.Errorf("IsSystemNoticeSubtype(%q) = false, want true", s)
		}
	}
}
