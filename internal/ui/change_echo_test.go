package ui

import (
	"testing"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/messages"
)

// withPreview is a message carrying one unfurl, one block, one file and
// one reaction.
func withPreview(ts string) messages.MessageItem {
	return messages.MessageItem{
		TS: ts, UserID: "U1", UserName: "alice", Text: "see https://example.com",
		ReplyCount: 2,
		Reactions:  []messages.ReactionItem{{Emoji: "tada", Count: 1}},
		Blocks:     []blocks.Block{blocks.DividerBlock{}},
		Attachments: []messages.Attachment{
			{Kind: "file", Name: "notes.txt", URL: "https://example.com/notes.txt"},
		},
		LegacyAttachments: []blocks.LegacyAttachment{
			{ID: 1, Title: "Example", FromURL: "https://example.com"},
		},
	}
}

// The message_changed echo Slack sends once the preview (and here the
// block and the file too) is gone: the text is the same, nobody edited
// anything.
func previewRemovedEcho(ts string) messages.MessageItem {
	return messages.MessageItem{TS: ts, UserID: "U1", UserName: "alice", Text: "see https://example.com"}
}

// Both panes, held to one contract by the contentPane interface, take a
// change echo the same way: content fields replaced, everything the echo
// does not carry kept.
func TestChangeEcho_AppliesToBothPanes(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"), withMessages(withPreview("1.0")))
	a.threadPanel.SetThread(withPreview("P1"), []messages.MessageItem{withPreview("R1")}, "C1", "P1")

	read := map[string]func() messages.MessageItem{
		"channel pane": func() messages.MessageItem { return a.messagepane.Messages()[0] },
		"thread reply": func() messages.MessageItem { return a.threadPanel.Replies()[0] },
	}
	for name, ts := range map[string]string{"channel pane": "1.0", "thread reply": "R1"} {
		t.Run(name, func(t *testing.T) {
			var pane contentPane = a.messagepane
			if name == "thread reply" {
				pane = a.threadPanel
			}
			if !pane.UpdateMessageInPlace(previewRemovedEcho(ts)) {
				t.Fatal("UpdateMessageInPlace = false for a message the pane holds")
			}
			got := read[name]()
			if len(got.LegacyAttachments) != 0 {
				t.Errorf("legacy attachments = %+v, want none after the preview was removed", got.LegacyAttachments)
			}
			if len(got.Blocks) != 0 || len(got.Attachments) != 0 {
				t.Errorf("blocks=%+v attachments=%+v, want both replaced by the echo's (none)", got.Blocks, got.Attachments)
			}
			if got.IsEdited {
				t.Error("IsEdited set by an echo without `edited`")
			}
			if len(got.Reactions) != 1 || got.ReplyCount != 2 {
				t.Errorf("reactions=%+v replyCount=%d, want both kept", got.Reactions, got.ReplyCount)
			}
		})
	}
}

// Driven through Update: the echo reaches the channel pane, the thread's
// replies and the thread's parent, and only `edited` marks "(edited)".
func TestChangeEcho_ThroughUpdate(t *testing.T) {
	setup := func() *App {
		a := newTestApp(t, withActiveChannel("C1"), withMessages(withPreview("P1"), withPreview("M2")))
		a.threadPanel.SetThread(withPreview("P1"), []messages.MessageItem{withPreview("R1")}, "C1", "P1")
		return a
	}
	for _, edited := range []bool{false, true} {
		a := setup()
		for _, ts := range []string{"P1", "R1", "M2"} {
			echo := previewRemovedEcho(ts)
			echo.IsEdited = edited
			a.Update(NewMessageMsg{ChannelID: "C1", Message: echo, Changed: true})
		}
		var got []messages.MessageItem
		got = append(got, a.messagepane.Messages()...)
		got = append(got, a.threadPanel.ParentMsg())
		got = append(got, a.threadPanel.Replies()...)
		if len(a.messagepane.Messages()) != 2 || len(a.threadPanel.Replies()) != 1 {
			t.Fatalf("a change echo appended: channel=%d replies=%d", len(a.messagepane.Messages()), len(a.threadPanel.Replies()))
		}
		for _, m := range got {
			if len(m.LegacyAttachments) != 0 {
				t.Errorf("edited=%v %s: preview still shown", edited, m.TS)
			}
			if m.IsEdited != edited {
				t.Errorf("edited=%v %s: IsEdited=%v", edited, m.TS, m.IsEdited)
			}
		}
	}
}

// An unfurl arriving on a message adds the preview live.
func TestChangeEcho_AddsPreview(t *testing.T) {
	plain := withPreview("1.0")
	plain.LegacyAttachments = nil
	a := newTestApp(t, withActiveChannel("C1"), withMessages(plain))
	a.Update(NewMessageMsg{ChannelID: "C1", Message: withPreview("1.0"), Changed: true})
	got := a.messagepane.Messages()[0]
	if len(got.LegacyAttachments) != 1 || got.IsEdited {
		t.Errorf("legacy=%+v edited=%v, want the unfurl shown and no edited mark", got.LegacyAttachments, got.IsEdited)
	}
}
