package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

func centreNotice() MessageItem {
	return MessageItem{
		TS: "1.0", UserID: "USLACKBOT", UserName: "Slackbot",
		Text: "<@U2> has joined the channel", Timestamp: "10:30 AM", Subtype: "channel_join",
	}
}

func leadingSpaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// A notice is centred and bold like a day divider: the same style, so
// the same row as DividerStyle gives the same text.
func TestNoticeIsCentredAndBoldLikeDateSeparator(t *testing.T) {
	rc := &RenderContext{UserNames: map[string]string{"U2": "alice"}}
	content := RenderMessage(centreNotice(), 60, rc, RenderOptions{}).Content
	plain := ansi.Strip(content)
	text := strings.TrimSpace(plain)
	if text != "10:30 AM  alice has joined the channel" && !strings.Contains(text, "has joined the channel") {
		t.Fatalf("notice text changed: %q", text)
	}
	if got, want := leadingSpaces(plain), (59-ansi.StringWidth(text))/2; got != want {
		t.Errorf("left pad = %d, want %d (centred in 59 columns)", got, want)
	}
	if content != DividerStyle(59).Render(text) {
		t.Errorf("notice does not use the date separator style:\n%q", content)
	}
	if !strings.Contains(content, "\x1b[1;") {
		t.Errorf("notice is not bold: %q", content)
	}
}

// The plain mirror (selection, copy) carries the notice text only, while
// keeping one column entry per display column.
func TestNoticePlainMirrorHasNoPadding(t *testing.T) {
	msg := centreNotice()
	r := RenderMessage(msg, 60, &RenderContext{}, RenderOptions{})
	rows := WrapRows(msg, r, 60, NewWrapStyles(true))
	if len(rows.Plain) != 1 {
		t.Fatalf("plain rows = %d, want 1", len(rows.Plain))
	}
	p := rows.Plain[0]
	if p.Text != strings.TrimSpace(p.Text) || !strings.Contains(p.Text, "has joined the channel") {
		t.Errorf("plain text = %q, want the notice without padding", p.Text)
	}
	if got := entrylist.DisplayWidth(p); got != 59 {
		t.Errorf("plain width = %d, want 59 (columns preserved)", got)
	}
	pad := leadingSpaces(ansi.Strip(rows.Lines[0])[len("▌"):])
	if got := entrylist.SliceColumns(p, pad, pad+ansi.StringWidth(p.Text)); got != p.Text {
		t.Errorf("column slice = %q, want %q", got, p.Text)
	}
}

// Text wider than the pane wraps at the 20-column floor and every line
// stays centred within the row.
func TestNoticeNarrowWidthWraps(t *testing.T) {
	msg := centreNotice()
	msg.Text = "<@U2> has joined the channel along with a wide emoji 😀 and more words"
	for _, w := range []int{10, 24, 30} {
		content := RenderMessage(msg, w, &RenderContext{}, RenderOptions{}).Content
		for _, line := range strings.Split(ansi.Strip(content), "\n") {
			if ansi.StringWidth(line) > 20 && ansi.StringWidth(line) > w-1 {
				t.Errorf("width %d: line %q is wider than the row", w, line)
			}
		}
	}
}

// The reply line of a huddle parent is centred under the notice.
func TestHuddleReplyLineCentred(t *testing.T) {
	msg := MessageItem{
		UserID: "USLACKBOT", Timestamp: "10:30 AM", Subtype: "huddle_thread",
		Blocks: []blockkit.Block{textBlock("A huddle started")}, ReplyCount: 3,
	}
	lines := strings.Split(ansi.Strip(RenderMessage(msg, 60, &RenderContext{}, RenderOptions{ReplyAffordance: true}).Content), "\n")
	last := lines[len(lines)-1]
	if got, want := leadingSpaces(last), (59-ansi.StringWidth("[3 replies ->]"))/2; got != want {
		t.Errorf("reply line pad = %d, want %d: %q", got, want, last)
	}
}

// An ordinary message stays left-aligned with its author line, and the
// date separator row is still the divider style.
func TestOrdinaryMessageNotCentred(t *testing.T) {
	msg := MessageItem{TS: "1.0", UserID: "U000", UserName: "alice", Text: "hi", Timestamp: "10:30 AM"}
	got := ansi.Strip(New([]MessageItem{msg}, "general").View(20, 60))
	found := false
	for _, line := range strings.Split(got, "\n") {
		if i := strings.Index(line, "alice"); i >= 0 {
			found = true
			if i > 6 {
				t.Errorf("ordinary message author at column %d, want left-aligned: %q", i, line)
			}
		}
	}
	if !found {
		t.Fatalf("author line missing:\n%s", got)
	}
	if got := ansi.Strip(DividerStyle(20).Render("── Today ──")); got != "    ── Today ──     " {
		t.Errorf("divider = %q", got)
	}
}
