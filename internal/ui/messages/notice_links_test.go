package messages

import (
	"strings"
	"testing"
)

// A link in a system notice stays an OSC 8 span, so WrapRows can give it
// a click rectangle.
func TestNoticeKeepsLinkAsOsc8Span(t *testing.T) {
	msg := MessageItem{TS: "1.000000", Subtype: "channel_join", Text: "joined <https://example.com/x|the docs>"}
	got := RenderMessage(msg, 80, &RenderContext{}, RenderOptions{})
	spans := ScanOsc8Spans(strings.Split(got.Content, "\n"))
	if len(spans) != 1 || spans[0].URL != "https://example.com/x" {
		t.Fatalf("spans = %+v, want one for https://example.com/x", spans)
	}
}
