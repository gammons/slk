package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// renderedFor builds a model with a single message, runs buildCache
// at the given width, and returns the joined plain-text rendering of
// the first cache entry. Mirrors the existing test-helper pattern in
// plain_test.go and selection_test.go.
func renderedFor(t *testing.T, msg MessageItem, width int) string {
	t.Helper()
	m := New([]MessageItem{msg}, "general")
	m.buildCache(width)
	if len(m.cache) == 0 {
		t.Fatal("buildCache produced no entries")
	}
	var lines []string
	for _, e := range m.cache {
		if e.msgIdx == 0 {
			lines = e.linesNormal
			break
		}
	}
	if lines == nil {
		t.Fatal("no entry with msgIdx 0 in cache")
	}
	return ansi.Strip(strings.Join(lines, "\n"))
}

func TestRenderMessagePlainEmitsBlockKitContent(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "github",
		UserID:    "U-BOT",
		Text:      "PR opened",
		Timestamp: "1:23 PM",
		Blocks: []blockkit.Block{
			blockkit.HeaderBlock{Text: "Pull Request opened"},
			blockkit.SectionBlock{Text: "Pay system: bug fix for retry logic"},
		},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "PR opened") {
		t.Errorf("missing message body Text: %q", plain)
	}
	if !strings.Contains(plain, "Pull Request opened") {
		t.Errorf("missing header block: %q", plain)
	}
	if !strings.Contains(plain, "Pay system: bug fix for retry logic") {
		t.Errorf("missing section block: %q", plain)
	}
}

func TestRenderMessagePlainEmitsLegacyAttachment(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "pagerduty",
		UserID:    "U-BOT",
		Text:      "alert",
		Timestamp: "1:23 PM",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Color: "danger",
			Title: "Service down",
			Text:  "checkout-svc 5xx > 1%",
		}},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "Service down") {
		t.Errorf("missing legacy title: %q", plain)
	}
	if !strings.Contains(plain, "█") {
		t.Errorf("missing color stripe glyph: %q", plain)
	}
}

// TestRenderMessagePlainPreservesPlainTextRendering guards against
// regressions: a message with no blocks/attachments renders exactly
// as before this task (text body present, no extra spacing).
func TestRenderMessagePlainPreservesPlainTextRendering(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "alice",
		Text:      "hello world",
		Timestamp: "1:00 PM",
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "hello world") {
		t.Errorf("plain text body missing: %q", plain)
	}
}
