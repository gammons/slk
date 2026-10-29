package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// svgAttachment is an image Slack made no thumbnail for, as it does for
// SVG uploads: Thumbs is empty but the file can still be downloaded.
func svgAttachment() messages.Attachment {
	return messages.Attachment{
		Kind:        "image",
		Name:        "diagram.svg",
		FileID:      "F0SVG",
		Mime:        "image/svg+xml",
		DownloadURL: "https://files.example.com/diagram.svg",
	}
}

func TestOKey_ImageWithoutThumbnailDownloadsAndOpens(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Attachments: []messages.Attachment{svgAttachment()}},
	})
	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd == nil {
		t.Fatal("O on an image without a thumbnail did nothing")
	}
	dl, ok := cmd().(DownloadFileMsg)
	if !ok || dl.Attachment.Name != "diagram.svg" {
		t.Fatalf("got %#v, want DownloadFileMsg for diagram.svg", cmd())
	}
}

func TestOKey_PreviewableImageWinsOverThumbless(t *testing.T) {
	_, _, _, png := imageBearingMessage(t)
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Attachments: []messages.Attachment{svgAttachment(), png.Attachments[0]}},
	})
	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd == nil {
		t.Fatal("expected a cmd")
	}
	op, ok := cmd().(messages.OpenImagePreviewMsg)
	if !ok || op.AttIdx != 1 {
		t.Fatalf("got %#v, want OpenImagePreviewMsg for the PNG (attachment 1)", cmd())
	}
}

func TestVKey_ThumblessImageInThreadDownloadsAndOpens(t *testing.T) {
	parent := messages.MessageItem{TS: "1.0", UserName: "bob", Text: "thread"}
	reply := messages.MessageItem{TS: "2.0", UserName: "alice", Attachments: []messages.Attachment{svgAttachment()}}
	app := NewApp()
	app.activeChannelID = "C123"
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.threadPanel.SetThread(parent, []messages.MessageItem{reply}, "C123", parent.TS)
	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if cmd == nil {
		t.Fatal("v on a thread reply's image without a thumbnail did nothing")
	}
	if dl, ok := cmd().(DownloadFileMsg); !ok || dl.Attachment.FileID != "F0SVG" {
		t.Fatalf("got %#v, want DownloadFileMsg for the reply's SVG", cmd())
	}
}

func TestOKey_ThumblessImageWithoutURLNoop(t *testing.T) {
	att := svgAttachment()
	att.DownloadURL = ""
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Attachments: []messages.Attachment{att}},
	})
	if cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'O', Text: "O"}); cmd != nil {
		t.Fatalf("O on an image with neither thumbnail nor download URL returned %#v", cmd())
	}
}
