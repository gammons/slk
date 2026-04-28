package thread

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// TestRenderThreadMessageAttachmentLinesFit asserts that a message with a
// long-URL attachment renders such that no output line exceeds the panel
// content width. Without this guarantee, the terminal soft-wraps overlong
// attachment lines and offsets the rest of the thread layout (the compose
// box ends up sitting on top of the last visible reply).
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
	got := m.renderThreadMessage(msg, width, nil, false)
	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d width=%d exceeds width=%d: %q", i, w, width, line)
		}
	}
}
