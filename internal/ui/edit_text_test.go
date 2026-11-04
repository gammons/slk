package ui

import (
	"testing"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// listMessage is a message written in Slack's client: its text field
// has lost the line breaks and escaped the link, the block has both.
func listMessage(ts string) messages.MessageItem {
	text := func(s string) slack.RichTextSectionElement {
		return &slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: s}
	}
	section := func(el ...slack.RichTextSectionElement) *slack.RichTextSection {
		return &slack.RichTextSection{Type: slack.RTESection, Elements: el}
	}
	return messages.MessageItem{
		TS:     ts,
		UserID: "U_ME",
		Text:   "a list  • one • two in <#CUJL4QPU0>&lt;https://x.com/|a link&gt;",
		Blocks: []blocks.Block{blockkit.RichTextBlock{Elements: []slack.RichTextElement{
			section(text("a list\n")),
			&slack.RichTextList{Type: slack.RTEList, Style: slack.RTEListBullet, Elements: []slack.RichTextElement{
				section(text("one")),
				section(text("two in "), &slack.RichTextSectionChannelElement{Type: slack.RTSEChannel, ChannelID: "CUJL4QPU0"}),
			}},
			section(&slack.RichTextSectionLinkElement{Type: slack.RTSELink, URL: "https://x.com/", Text: "a link"}),
		}}},
	}
}

const listMessageEdit = "a list\n- one\n- two in #general\n\n[a link](https://x.com/)"

// SLK-330: the box shows the message as it was typed (lines, list,
// link, channel name), not Slack's flattened text field.
func TestBeginEditOfSelected_ShowsRichTextAsTyped(t *testing.T) {
	app := newTestApp(t,
		withActiveChannel("C1"),
		withChannels(sidebar.ChannelItem{ID: "CUJL4QPU0", Name: "general", Type: "channel"}),
		withMessages(listMessage("1.0")),
	)
	app.SetCurrentUserID("U_ME")
	app.focusedPanel = PanelMessages

	app.beginEditOfSelected()

	if got := app.compose.Value(); got != listMessageEdit {
		t.Errorf("compose = %q, want %q", got, listMessageEdit)
	}
}

func TestBeginEditOfSelected_ThreadPaneShowsRichTextAsTyped(t *testing.T) {
	app := newTestApp(t,
		withActiveChannel("C1"),
		withChannels(sidebar.ChannelItem{ID: "CUJL4QPU0", Name: "general", Type: "channel"}),
	)
	app.SetCurrentUserID("U_ME")
	app.threadPanel.SetThread(messages.MessageItem{TS: "P1", UserID: "U_OTHER", Text: "parent"},
		[]messages.MessageItem{listMessage("R1")}, "C1", "P1")
	app.threadVisible = true
	app.focusedPanel = PanelThread

	app.beginEditOfSelected()

	if got := app.threadCompose.Value(); got != listMessageEdit {
		t.Errorf("thread compose = %q, want %q", got, listMessageEdit)
	}
}
