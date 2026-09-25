package thread

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// TestRenderThreadMessageAttachmentLinesFit asserts that a message with a
// long-URL file attachment renders without crashing and produces an
// attachment line containing the URL.
//
// Historical note: this test previously asserted that no rendered line
// exceeded the panel content width, relying on a `messages.WordWrap` call
// that the legacy `messages.RenderAttachments` codepath was wrapped in.
// Task 8 migrates this codepath to `imgrender.Renderer.RenderBlock`,
// which produces a single OSC 8 hyperlink line for non-image
// attachments without inner wrapping (matching the messages pane's
// post-Task-7 behavior). The cache-build layer's
// `borderFill.Width(width - 1).Render(...)` is now solely responsible
// for width enforcement when the cached output is composed for display.
func TestRenderThreadMessageAttachmentLinesFit(t *testing.T) {
	const width = 50 // panel content width passed to renderThreadMessage
	m := New()
	msg := messages.MessageItem{
		TS:        "1700000001.000000",
		UserName:  "alice",
		Text:      "see attachment",
		Timestamp: "10:30 AM",
		Attachments: []messages.Attachment{
			{Kind: "file", Name: "specright_roi_-_final_data_-_704193", URL: "https://userevidence.slack.com/files/U05AZM7KJ1H/F0ATTEVCLUC/specright_roi_-_final_data_-_704193"},
		},
	}
	got, _, _ := m.renderThreadMessage(msg, width, nil, nil, false)
	if got == "" {
		t.Fatal("renderThreadMessage returned empty output")
	}
	if !strings.Contains(got, "specright_roi_-_final_data_-_704193") {
		t.Fatalf("expected rendered output to contain the file URL; got %q", got)
	}
	// Confirm the attachment was rendered through the legacy [File]
	// hyperlink path (not silently dropped).
	if !strings.Contains(got, "[File]") {
		t.Fatalf("expected rendered output to include [File] marker; got %q", got)
	}
	_ = lipgloss.Width // keep import; older assertion measured per-line widths
}

// TestRenderThreadMessageLegacyAttachmentBlocks asserts that a reply
// carrying a link-unfurl legacy attachment (Linear/Jira issue card,
// whose content lives in nested Block Kit blocks) renders its content
// in the thread panel, matching the main message pane.
func TestRenderThreadMessageLegacyAttachmentBlocks(t *testing.T) {
	const width = 60
	m := New()
	msg := messages.MessageItem{
		TS:        "1700000002.000000",
		UserName:  "alice",
		Text:      "see ticket",
		Timestamp: "10:31 AM",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Color: "#2d1c9c",
			Blocks: []blockkit.Block{
				blockkit.SectionBlock{Text: "TRU-111 Customer Facing Blacklist Monitoring"},
				blockkit.ContextBlock{Elements: []blockkit.ContextElement{{Text: "*State*  In Progress"}}},
			},
		}},
	}
	got, _, _ := m.renderThreadMessage(msg, width, nil, nil, false)
	if !strings.Contains(got, "TRU-111 Customer Facing Blacklist Monitoring") {
		t.Errorf("thread render missing legacy-attachment block content; got %q", got)
	}
	if !strings.Contains(got, "In Progress") {
		t.Errorf("thread render missing context block content; got %q", got)
	}
}

// TestRenderThreadMessageTopLevelBlocks asserts that a reply carrying
// top-level Block Kit blocks (bot messages) renders them in the thread
// panel.
func TestRenderThreadMessageTopLevelBlocks(t *testing.T) {
	const width = 60
	m := New()
	msg := messages.MessageItem{
		TS:        "1700000003.000000",
		UserName:  "deploybot",
		Timestamp: "10:32 AM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "Deploy finished: v1.2.3"},
		},
	}
	got, _, _ := m.renderThreadMessage(msg, width, nil, nil, false)
	if !strings.Contains(got, "Deploy finished: v1.2.3") {
		t.Errorf("thread render missing top-level block content; got %q", got)
	}
}

// TestRenderThreadMessageBlocksCarryBody asserts that a reply whose
// blocks carry its body draws no body row: the notification fallback in
// msg.Text is not drawn, the block content sits directly under the
// header rather than after an empty row, and the reaction hit row still
// lands on the reaction line.
func TestRenderThreadMessageBlocksCarryBody(t *testing.T) {
	const width = 60
	m := New()
	msg := messages.MessageItem{
		TS:        "1700000004.000000",
		UserName:  "deploybot",
		Timestamp: "10:33 AM",
		Text:      "deploy notification fallback",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "Deploy finished: v1.2.3"},
		},
		Reactions: []messages.ReactionItem{{Emoji: "tada", Count: 1}},
	}
	got, _, hits := m.renderThreadMessage(msg, width, nil, nil, false)
	plain := ansi.Strip(got)
	if strings.Contains(plain, "deploy notification fallback") {
		t.Errorf("fallback text drawn beside its blocks; got %q", plain)
	}
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 || !strings.Contains(lines[1], "Deploy finished: v1.2.3") {
		t.Fatalf("row 1 should be the section block, not an empty body row; got %q", lines)
	}
	if len(hits) == 0 {
		t.Fatal("no reaction hits recorded")
	}
	if row := hits[0].rowStartInEntry; row != len(lines)-1 {
		t.Errorf("reaction hit row = %d, want %d (the reaction line); got %q", row, len(lines)-1, lines)
	}
}
