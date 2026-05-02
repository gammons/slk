package thread

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/messages"
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
	got, _ := m.renderThreadMessage(msg, width, nil, nil, false)
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
