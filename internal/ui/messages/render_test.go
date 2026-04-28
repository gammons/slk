package messages

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestLabeledLinkShowsURLAndOSC8 asserts that a Slack-style labeled link
// (<URL|label>) renders the URL alongside the label and emits an OSC 8
// hyperlink escape so the label is clickable in modern terminals.
func TestLabeledLinkShowsURLAndOSC8(t *testing.T) {
	in := "see <https://example.com/doc|the document> for details"
	out := RenderSlackMarkdown(in, nil)
	plain := ansi.Strip(out)

	if !strings.Contains(plain, "the document") {
		t.Errorf("expected label %q in plain output, got %q", "the document", plain)
	}
	if !strings.Contains(plain, "https://example.com/doc") {
		t.Errorf("expected URL in plain output for copy/paste, got %q", plain)
	}
	// OSC 8 hyperlink: \x1b]8;;URL\x1b\\LABEL\x1b]8;;\x1b\\
	if !strings.Contains(out, "\x1b]8;;https://example.com/doc") {
		t.Error("expected OSC 8 hyperlink escape for clickable label")
	}
}

// TestBareLinkOSC8 asserts that a bare <URL> link gets wrapped in an OSC 8
// hyperlink escape so it's clickable.
func TestBareLinkOSC8(t *testing.T) {
	in := "go to <https://example.com>"
	out := RenderSlackMarkdown(in, nil)
	plain := ansi.Strip(out)

	if !strings.Contains(plain, "https://example.com") {
		t.Errorf("expected URL in plain output, got %q", plain)
	}
	if !strings.Contains(out, "\x1b]8;;https://example.com") {
		t.Error("expected OSC 8 hyperlink escape on bare link")
	}
}

// TestChannelMentionStillRendersWithHash guards against the regex-ordering
// regression noted in render.go: linkWithLabelRe must not consume
// <#CHANNEL_ID|name> and reduce it to just "name". We tighten it to require
// https?:// so channel mentions fall through to channelMentionRe.
func TestChannelMentionStillRendersWithHash(t *testing.T) {
	in := "see <#C123|general>"
	out := ansi.Strip(RenderSlackMarkdown(in, nil))

	if !strings.Contains(out, "#general") {
		t.Errorf("expected '#general' in output (channel mention should keep #), got %q", out)
	}
}

// TestUserMentionResolvesAndKeepsAt confirms user mentions resolve via the
// userNames map and retain their @ prefix.
func TestUserMentionResolvesAndKeepsAt(t *testing.T) {
	in := "hi <@U99>"
	out := ansi.Strip(RenderSlackMarkdown(in, map[string]string{"U99": "alice"}))
	if !strings.Contains(out, "@alice") {
		t.Errorf("expected '@alice' in output, got %q", out)
	}
}

// TestUnlabeledNonHTTPLinkSurvives confirms that <url|text> patterns where the
// URL is NOT http(s) don't get gobbled by linkWithLabelRe. (Slack uses
// <!subteam^S123|@team> for groups, etc.)
func TestNonHTTPBracketedSurvives(t *testing.T) {
	// Should not panic, should not render as a link. We only assert it doesn't
	// crash and the output is non-empty.
	out := RenderSlackMarkdown("ping <!subteam^S123|@team> please", nil)
	if out == "" {
		t.Error("expected non-empty output")
	}
}

// TestRenderAttachmentsImageMarker asserts that an Image attachment renders
// with an [Image] marker, the URL (visible for copy-paste), and an OSC 8
// hyperlink for clickability. Filenames are intentionally omitted to keep
// attachment lines short enough to fit in narrow panes.
func TestRenderAttachmentsImageMarker(t *testing.T) {
	got := RenderAttachments([]Attachment{
		{Kind: "image", Name: "uniquefile12345.png", URL: "https://files.slack.com/abc/xyz.png"},
	})
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "[Image]") {
		t.Errorf("expected [Image] marker, got %q", plain)
	}
	if strings.Contains(plain, "uniquefile12345.png") {
		t.Errorf("filename should be omitted from attachment line, got %q", plain)
	}
	if !strings.Contains(plain, "https://files.slack.com") {
		t.Errorf("expected URL visible in plain output, got %q", plain)
	}
	if !strings.Contains(got, "\x1b]8;;https://files.slack.com") {
		t.Error("expected OSC 8 hyperlink escape on attachment line")
	}
}

// TestRenderAttachmentsFileMarker confirms non-image attachments use [File]
// and omit the filename.
func TestRenderAttachmentsFileMarker(t *testing.T) {
	got := ansi.Strip(RenderAttachments([]Attachment{
		{Kind: "file", Name: "design.pdf", URL: "https://files.slack.com/x.pdf"},
	}))
	if !strings.Contains(got, "[File]") {
		t.Errorf("expected [File] marker, got %q", got)
	}
	if strings.Contains(got, "design.pdf") {
		t.Errorf("filename should be omitted, got %q", got)
	}
	if !strings.Contains(got, "https://files.slack.com/x.pdf") {
		t.Errorf("expected URL visible, got %q", got)
	}
}

// TestRenderAttachmentsEmpty returns empty string for no attachments.
func TestRenderAttachmentsEmpty(t *testing.T) {
	if got := RenderAttachments(nil); got != "" {
		t.Errorf("expected empty string for nil attachments, got %q", got)
	}
}

// TestRenderAttachmentsWrappedFitsLimit asserts that running attachment
// output through WordWrap produces lines that all fit the wrap limit.
// Attachment lines contain a long URL that has no whitespace, so without
// hard-break support the terminal would soft-wrap them and offset the
// surrounding layout.
func TestRenderAttachmentsWrappedFitsLimit(t *testing.T) {
	const limit = 60
	rendered := RenderAttachments([]Attachment{
		{Kind: "file", Name: "design.pdf", URL: "https://userevidence.slack.com/files/U05AZM7KJ1H/F0ATTEVCLUC/specright_roi_-_final_data_-_704193"},
	})
	wrapped := WordWrap(rendered, limit)
	for i, line := range strings.Split(wrapped, "\n") {
		if w := lipgloss.Width(line); w > limit {
			t.Errorf("attachment line %d width=%d exceeds limit=%d: plain=%q",
				i, w, limit, ansi.Strip(line))
		}
	}
}

// TestWordWrapBareURLEachLineFitsLimit asserts that wrapping a message
// containing a long bare URL — which RenderSlackMarkdown wraps in an OSC 8
// hyperlink escape — produces lines whose display width never exceeds the
// limit. Without proper hard-break of OSC-wrapped tokens, the terminal
// would soft-wrap the long line on its own and offset the rest of the
// thread layout.
func TestWordWrapBareURLEachLineFitsLimit(t *testing.T) {
	const limit = 50
	in := "see <https://userevidence.slack.com/files/U05AZM7KJ1H/F0ATTEVCLUC/specright_roi_-_final_data_-_704193> please"
	rendered := RenderSlackMarkdown(in, nil)
	got := WordWrap(rendered, limit)
	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > limit {
			t.Errorf("line %d width=%d exceeds limit=%d: plain=%q raw=%q",
				i, w, limit, ansi.Strip(line), line)
		}
	}
}

// TestWordWrapHardBreaksOverlongTokens guards against the layout bug where
// a single unbroken token (e.g. a long URL) wider than the wrap limit was
// emitted on one line, causing the terminal to soft-wrap it on its own.
// That extra terminal-side wrapping pushed the thread compose box over the
// last reply because lipgloss height arithmetic counted the overlong line
// as 1, not the multiple rows it actually consumed.
//
// Every output line must measure <= limit cells.
func TestWordWrapHardBreaksOverlongTokens(t *testing.T) {
	const limit = 40
	cases := []struct {
		name string
		in   string
	}{
		{"long URL alone", "https://userevidence.slack.com/files/U05AZM7KJ1H/F0ATTEVCLUC/specright_roi_-_final_data_-_704193"},
		{"long URL in sentence", "see https://example.com/this/is/a/very/long/path/that/cannot/break/at/word/boundaries for details"},
		{"giant identifier", "abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WordWrap(tc.in, limit)
			for i, line := range strings.Split(got, "\n") {
				if w := lipgloss.Width(line); w > limit {
					t.Errorf("line %d width=%d exceeds limit=%d: %q", i, w, limit, line)
				}
			}
		})
	}
}
