package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

func pressD(app *App) tea.Cmd {
	return app.handleNormalMode(tea.KeyPressMsg{Code: 'd', Text: "d"})
}

func fileAtt(name string) messages.Attachment {
	return messages.Attachment{
		Kind:        "file",
		Name:        name,
		URL:         "https://team.slack.com/files/U/F",
		DownloadURL: "https://files.slack.com/files-pri/T-F/" + name,
	}
}

func TestDownloadKey_NoFiles_Toasts(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.0", Text: "plain"}})
	cmd := pressD(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if _, ok := cmd().(ToastMsg); !ok {
		t.Errorf("expected ToastMsg, got %#v", cmd())
	}
}

func TestDownloadKey_SingleFile_DispatchesDownloadFileMsg(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{fileAtt("a.csv")}},
	})
	cmd := pressD(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(DownloadFileMsg)
	if !ok {
		t.Fatalf("expected DownloadFileMsg, got %#v", cmd())
	}
	if msg.Attachment.Name != "a.csv" {
		t.Errorf("attachment = %q", msg.Attachment.Name)
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal (no modal for single file)", app.mode)
	}
}

func TestDownloadKey_MultipleFiles_OpensPicker(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{fileAtt("a.csv"), fileAtt("b.pdf")}},
	})
	cmd := pressD(app)
	if cmd != nil {
		t.Errorf("expected nil cmd (modal opens), got %#v", cmd())
	}
	if app.mode != ModeLinkPicker {
		t.Fatalf("mode = %v, want ModeLinkPicker", app.mode)
	}
	if !app.linkPicker.IsVisible() {
		t.Fatal("picker not visible")
	}
	if app.linkPicker.Title() != "Download file" {
		t.Errorf("title = %q", app.linkPicker.Title())
	}
	items := app.linkPicker.Items()
	if len(items) != 2 || items[0].Label != "a.csv" || items[1].Label != "b.pdf" {
		t.Errorf("items = %#v", items)
	}
}

func TestFilePickerMode_EnterDispatchesDownloadFileMsg(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{fileAtt("a.csv"), fileAtt("b.pdf")}},
	})
	pressD(app)
	app.handleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	cmd := app.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd from enter")
	}
	msg, ok := cmd().(DownloadFileMsg)
	if !ok {
		t.Fatalf("expected DownloadFileMsg, got %#v", cmd())
	}
	if msg.Attachment.Name != "b.pdf" {
		t.Errorf("attachment = %q", msg.Attachment.Name)
	}
	if app.mode != ModeNormal {
		t.Errorf("mode = %v after choose", app.mode)
	}
}

func TestFilePickerMode_EscCloses(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{fileAtt("a.csv"), fileAtt("b.pdf")}},
	})
	pressD(app)
	cmd := app.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Errorf("expected nil cmd, got %#v", cmd())
	}
	if app.mode != ModeNormal || app.linkPicker.IsVisible() {
		t.Errorf("mode=%v visible=%v after esc", app.mode, app.linkPicker.IsVisible())
	}
}

func TestDownloadKey_FromThreadPanel(t *testing.T) {
	app := NewApp()
	parent := messages.MessageItem{TS: "1.0", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "1.0", Text: "parent"},
		{TS: "2.0", Text: "x", Attachments: []messages.Attachment{fileAtt("t.csv")}},
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
	cmd := pressD(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg, ok := cmd().(DownloadFileMsg)
	if !ok || msg.Attachment.Name != "t.csv" {
		t.Errorf("got %#v", cmd())
	}
}

// Images are excluded: they already have the preview flow (O/v).
func TestDownloadKey_SkipsImageAttachments(t *testing.T) {
	app := NewApp()
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", Text: "x", Attachments: []messages.Attachment{
			{Kind: "image", Name: "p.png", DownloadURL: "https://files.slack.com/x"},
		}},
	})
	cmd := pressD(app)
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if _, ok := cmd().(ToastMsg); !ok {
		t.Errorf("expected ToastMsg for image-only message, got %#v", cmd())
	}
}
