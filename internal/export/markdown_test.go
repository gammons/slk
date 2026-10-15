package export

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

func TestThreadToMarkdown_ParentOnly(t *testing.T) {
	parent := messages.MessageItem{UserName: "alice", DateStr: "2026-05-18", Timestamp: "3:04 PM", Text: "hello world"}
	got := ThreadToMarkdown(parent, nil, nil, nil)
	if !strings.Contains(got, "**alice** — 2026-05-18 3:04 PM") {
		t.Errorf("missing header, got:\n%s", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("missing body, got:\n%s", got)
	}
}

func TestThreadToMarkdown_ParentAndReplies(t *testing.T) {
	parent := messages.MessageItem{UserName: "alice", DateStr: "2026-05-18", Timestamp: "3:04 PM", Text: "question?"}
	replies := []messages.MessageItem{
		{UserName: "bob", DateStr: "2026-05-18", Timestamp: "3:05 PM", Text: "answer!"},
	}
	got := ThreadToMarkdown(parent, replies, nil, nil)
	if strings.Count(got, "**alice**") != 1 || strings.Count(got, "**bob**") != 1 {
		t.Errorf("expected both users, got:\n%s", got)
	}
}

func TestThreadToMarkdown_Attachments(t *testing.T) {
	parent := messages.MessageItem{
		UserName: "alice", DateStr: "2026-05-18", Timestamp: "3:04 PM", Text: "see attached",
		Attachments: []messages.Attachment{
			{Kind: "image", Name: "screenshot.png", URL: "https://files.slack.com/img.png"},
			{Kind: "file", URL: "https://files.slack.com/doc.pdf"},
		},
	}
	got := ThreadToMarkdown(parent, nil, nil, nil)
	if !strings.Contains(got, "[screenshot.png](https://files.slack.com/img.png)") {
		t.Errorf("missing named image attachment, got:\n%s", got)
	}
	if !strings.Contains(got, "[File](https://files.slack.com/doc.pdf)") {
		t.Errorf("missing unnamed file attachment, got:\n%s", got)
	}
}

// A forwarded message carries its shared content in a legacy
// attachment's Text, with an empty top-level Text. The exported
// markdown must include it instead of an empty body line.
func TestThreadToMarkdown_ForwardedMessage(t *testing.T) {
	parent := messages.MessageItem{
		UserName: "alice", DateStr: "2026-05-18", Timestamp: "3:04 PM",
		LegacyAttachments: []blockkit.LegacyAttachment{
			{Text: "shared: check this out"},
		},
	}
	got := ThreadToMarkdown(parent, nil, nil, nil)
	if !strings.Contains(got, "shared: check this out") {
		t.Errorf("missing forwarded attachment text, got:\n%s", got)
	}
}

func TestThreadToMarkdown_Reactions(t *testing.T) {
	parent := messages.MessageItem{
		UserName: "alice", DateStr: "2026-05-18", Timestamp: "3:04 PM", Text: "great idea",
		Reactions: []messages.ReactionItem{
			{Emoji: "thumbsup", Count: 3},
		},
	}
	got := ThreadToMarkdown(parent, nil, nil, nil)
	if !strings.Contains(got, "3") {
		t.Errorf("missing reaction count, got:\n%s", got)
	}
}

func TestThreadToMarkdown_BoldConverted(t *testing.T) {
	parent := messages.MessageItem{UserName: "alice", DateStr: "2026-05-18", Timestamp: "3:04 PM", Text: "*important*"}
	got := ThreadToMarkdown(parent, nil, nil, nil)
	if !strings.Contains(got, "**important**") {
		t.Errorf("bold not converted, got:\n%s", got)
	}
}
