package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// sharedPermalink is the from_url of a message shared with Slack's
// "Share message" / forward action.
const sharedPermalink = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139"

func sharedAttachment() blockkit.LegacyAttachment {
	return blockkit.LegacyAttachment{
		FromURL:    sharedPermalink,
		AuthorName: "Jenny Leyva",
		Text:       "adding here for vis",
	}
}

// A shared message carries its permalink only in the attachment, so
// `o` must find it there even when the outer text has no links.
func TestOpenLinkKey_SharedMessage_DispatchesPermalink(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{
		TS:                "1.0",
		Text:              "lmk if the MCP work solves for this",
		LegacyAttachments: []blockkit.LegacyAttachment{sharedAttachment()},
	}})
	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(OpenLinkMsg)
	if !ok {
		t.Fatalf("expected OpenLinkMsg, got %#v", cmd())
	}
	if msg.URL != sharedPermalink {
		t.Errorf("URL = %q, want %q", msg.URL, sharedPermalink)
	}
}

func TestOpenLinkKey_TextLinkAndSharedMessage_OpensPicker(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{
		TS:                "1.0",
		Text:              "see <https://example.com/docs|docs>",
		LegacyAttachments: []blockkit.LegacyAttachment{sharedAttachment()},
	}})
	if cmd := pressO(app); cmd != nil {
		t.Errorf("expected nil cmd (modal opens), got %#v", cmd())
	}
	if app.mode != ModeLinkPicker {
		t.Fatalf("mode = %v, want ModeLinkPicker", app.mode)
	}
	items := app.linkPicker.Items()
	if len(items) != 2 {
		t.Fatalf("items = %#v, want 2", items)
	}
	if items[0].URL != "https://example.com/docs" {
		t.Errorf("items[0].URL = %q", items[0].URL)
	}
	if items[1].URL != sharedPermalink {
		t.Errorf("items[1].URL = %q", items[1].URL)
	}
	if items[1].Label != "Message from Jenny Leyva" {
		t.Errorf("items[1].Label = %q", items[1].Label)
	}
}

// A pasted permalink stays in the text and Slack unfurls it into an
// attachment with the same from_url; that is still one link.
func TestOpenLinkKey_PastedPermalinkAndUnfurl_Dedupes(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{
		TS:                "1.0",
		Text:              "<" + sharedPermalink + ">",
		LegacyAttachments: []blockkit.LegacyAttachment{sharedAttachment()},
	}})
	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(OpenLinkMsg)
	if !ok || msg.URL != sharedPermalink {
		t.Errorf("got %#v, want OpenLinkMsg{%q}", cmd(), sharedPermalink)
	}
}

// Only Slack permalinks count: a non-Slack from_url (an ordinary link
// unfurl) is not offered, so bot cards don't grow extra links.
func TestOpenLinkKey_NonSlackFromURL_Ignored(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{
		TS:   "1.0",
		Text: "no links here",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			FromURL: "https://github.com/foo/bar",
			Title:   "foo/bar",
		}},
	}})
	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if _, ok := cmd().(ToastMsg); !ok {
		t.Errorf("expected ToastMsg, got %#v", cmd())
	}
}

func TestOpenLinkKey_SharedMessage_FromThreadPanel(t *testing.T) {
	app := NewApp()
	parent := messages.MessageItem{TS: "1.0", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "1.0", Text: "parent"},
		{TS: "2.0", Text: "fyi", LegacyAttachments: []blockkit.LegacyAttachment{sharedAttachment()}},
	}
	app.threadPanel.SetThread(parent, replies, "C1", "1.0")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	for i := 0; i < len(replies); i++ {
		if sel := app.threadPanel.SelectedReply(); sel != nil && sel.TS == "2.0" {
			break
		}
		app.threadPanel.MoveDown()
	}
	cmd := pressO(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(OpenLinkMsg)
	if !ok || msg.URL != sharedPermalink {
		t.Errorf("got %#v, want OpenLinkMsg{%q}", cmd(), sharedPermalink)
	}
}
