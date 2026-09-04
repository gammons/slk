package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/slack-go/slack"
)

func drainForCopiedMsg(msg tea.Msg) (statusbar.CopiedMsg, bool) {
	switch v := msg.(type) {
	case statusbar.CopiedMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if cm, ok := drainForCopiedMsg(c()); ok {
				return cm, true
			}
		}
	}
	return statusbar.CopiedMsg{}, false
}

func TestCopyMessage_FromMessagesPane_Y(t *testing.T) {
	app := NewApp()
	var copied string
	app.SetClipboardWriter(func(text string) tea.Cmd {
		copied = text
		return nil
	})
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1700000001.000200", UserName: "alice", Text: "hello world 🔥"},
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from y key")
	}
	msg := cmd()
	cMsg, found := drainForCopiedMsg(msg)
	if !found {
		t.Fatalf("expected statusbar.CopiedMsg in batch, got %#v", msg)
	}
	if copied != "hello world 🔥" {
		t.Errorf("clipboard = %q, want 'hello world 🔥'", copied)
	}
	wantRunes := len([]rune("hello world 🔥"))
	if cMsg.N != wantRunes {
		t.Errorf("CopiedMsg.N = %d, want %d", cMsg.N, wantRunes)
	}
}

func TestCopyMessage_FromThreadPane(t *testing.T) {
	app := NewApp()
	var copied string
	app.SetClipboardWriter(func(text string) tea.Cmd {
		copied = text
		return nil
	})
	parent := messages.MessageItem{TS: "1700000000.000100", UserName: "alice", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "1700000000.000100", UserName: "alice", Text: "parent"},
		{TS: "1700000050.000400", UserName: "bob", Text: "reply message"},
	}
	app.threadPanel.SetThread(parent, replies, "C999", "1700000000.000100")
	app.threadVisible = true
	app.focusedPanel = PanelThread
	for i := 0; i < len(replies); i++ {
		sel := app.threadPanel.SelectedReply()
		if sel != nil && sel.TS == "1700000050.000400" {
			break
		}
		app.threadPanel.MoveDown()
	}

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from y key")
	}
	msg := cmd()
	cMsg, found := drainForCopiedMsg(msg)
	if !found {
		t.Fatalf("expected statusbar.CopiedMsg in batch, got %#v", msg)
	}
	if copied != "reply message" {
		t.Errorf("clipboard = %q, want 'reply message'", copied)
	}
	if cMsg.N != len("reply message") {
		t.Errorf("CopiedMsg.N = %d, want %d", cMsg.N, len("reply message"))
	}
}

func TestCopyMessage_NothingSelectedNoop(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatalf("expected nil cmd when nothing selected, got %#v", cmd)
	}
}

func TestCopyMessage_EmptyTextMessage(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: ""},
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for empty text message")
	}
	msg := cmd()
	toast, ok := msg.(ToastMsg)
	if !ok {
		t.Fatalf("expected ToastMsg, got %T (%#v)", msg, msg)
	}
	if toast.Text != "Message has no text" {
		t.Errorf("toast.Text = %q, want 'Message has no text'", toast.Text)
	}
}

func TestCopyMessage_RichTextBlockReconstructsMrkdwn(t *testing.T) {
	app := NewApp()
	var copied string
	app.SetClipboardWriter(func(text string) tea.Cmd {
		copied = text
		return nil
	})
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{
			TS:       "1.0",
			UserName: "bot",
			Text:     "lossy fallback",
			Blocks: []blockkit.Block{
				blockkit.RichTextBlock{
					Elements: []slack.RichTextElement{
						&slack.RichTextSection{
							Type: slack.RTESection,
							Elements: []slack.RichTextSectionElement{
								&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "Line 1"},
								&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "\n"},
								&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "Line 2"},
							},
						},
					},
				},
			},
		},
	})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from y key")
	}
	msg := cmd()
	_, found := drainForCopiedMsg(msg)
	if !found {
		t.Fatalf("expected statusbar.CopiedMsg in batch, got %#v", msg)
	}
	want := "Line 1\nLine 2"
	if copied != want {
		t.Errorf("clipboard = %q, want %q", copied, want)
	}
}

func TestDefaultKeyMap_CopyMessage(t *testing.T) {
	km := DefaultKeyMap()
	if km.CopyMessage.Help().Key != "y" {
		t.Errorf("CopyMessage key help = %q, want 'y'", km.CopyMessage.Help().Key)
	}
	if km.CopyMessage.Help().Desc != "copy message" {
		t.Errorf("CopyMessage desc help = %q, want 'copy message'", km.CopyMessage.Help().Desc)
	}
}
