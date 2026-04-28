package messages

import (
	"fmt"
	"strings"
	"testing"
)

// TestView_ScrollbarAppearsOnOverflow verifies that when the rendered
// content exceeds the message-area height, View() decorates each visible
// row with a scrollbar gutter character on the right.
func TestView_ScrollbarAppearsOnOverflow(t *testing.T) {
	msgs := make([]MessageItem, 30)
	for i := range msgs {
		msgs[i] = MessageItem{
			TS:        fmt.Sprintf("%d.0", i+1),
			UserName:  "u",
			UserID:    "U1",
			Text:      fmt.Sprintf("message %d", i),
			Timestamp: "1:00 PM",
		}
	}
	m := New(msgs, "general")
	out := m.View(15, 60)
	if !strings.Contains(out, "\u2588") && !strings.Contains(out, "\u2502") {
		t.Fatalf("expected scrollbar glyph (█ or │) in overflow render; got %q", out)
	}
}

// TestView_NoScrollbarWhenContentFits verifies the gutter is omitted when
// content does not overflow.
func TestView_NoScrollbarWhenContentFits(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hi", Timestamp: "1:00 PM"},
	}
	m := New(msgs, "general")
	out := m.View(40, 60)
	if strings.Contains(out, "\u2588") || strings.Contains(out, "\u2502") {
		t.Fatalf("did not expect scrollbar glyph in non-overflow render; got %q", out)
	}
}
