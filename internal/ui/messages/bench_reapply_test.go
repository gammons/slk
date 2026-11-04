package messages

import (
	"fmt"
	"strings"
	"testing"
)

// reapplyBenchText is a mrkdwn body with many styled spans (bold, italic,
// strike, code, links, user and channel mentions), so every span ends in an
// SGR reset that ReapplyBgAfterResets has to patch.
func reapplyBenchText(lines int) string {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "*bold %d* and _italic_ with ~strike~ and `code` see <https://example.com/p/%d|the page> ping <@U1> and <@U2> in <#C1|general> then *more* _text_ `x`", i, i)
	}
	return sb.String()
}

var reapplyBenchOpts = RenderSlackMarkdownOpts{
	UserNames:    map[string]string{"U1": "alice", "U2": "bob"},
	ChannelNames: map[string]string{"C1": "general"},
}

// BenchmarkRenderSlackMarkdownSpans measures the markdown renderer on spans-
// heavy text; its final step is one ReapplyBgAfterResets over the output.
func BenchmarkRenderSlackMarkdownSpans(b *testing.B) {
	for _, n := range []int{1, 10} {
		text := reapplyBenchText(n)
		b.Run(fmt.Sprintf("lines=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = RenderSlackMarkdownWith(text, reapplyBenchOpts)
			}
		})
	}
}

// unpatchedBody returns the markdown body as it is just before the final
// ReapplyBgAfterResets pass in RenderSlackMarkdownWith: the rendered output
// with the style that pass appended after each reset removed. It fails the
// benchmark if re-applying the pass does not reproduce the rendered body.
func unpatchedBody(b *testing.B, style string) string {
	b.Helper()
	body := RenderSlackMarkdownWith(reapplyBenchText(10), reapplyBenchOpts)
	raw := strings.ReplaceAll(body, "\x1b[0m"+style, "\x1b[0m")
	raw = strings.ReplaceAll(raw, "\x1b[m"+style, "\x1b[m")
	if ReapplyBgAfterResets(raw, style) != body {
		b.Fatal("stripped body does not round-trip through ReapplyBgAfterResets")
	}
	return raw
}

// BenchmarkReapplyBgOnRenderedBody isolates the ReapplyBgAfterResets call
// RenderSlackMarkdownWith makes. The input is the body as that call sees it,
// before the pass has patched it (a patched body would hit the replacement
// path on every reset a second time, which production never does).
func BenchmarkReapplyBgOnRenderedBody(b *testing.B) {
	style := BgANSI() + FgANSI()
	body := unpatchedBody(b, style)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ReapplyBgAfterResets(body, style)
	}
}

// BenchmarkRenderMessageSpans is the whole message render for a span-heavy
// message, to put the Reapply calls in proportion.
func BenchmarkRenderMessageSpans(b *testing.B) {
	msg := MessageItem{
		TS:        "1700000000.0",
		UserName:  "alice",
		UserID:    "U1",
		Text:      reapplyBenchText(6),
		Timestamp: "10:30 AM",
	}
	m := New([]MessageItem{msg}, "general")
	rc := m.renderContext()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Zero RenderOptions: a plain-message baseline, not the channel
		// pane's options.
		_ = RenderMessage(msg, 100, rc, RenderOptions{})
	}
}

// BenchmarkWithBackground measures the Block Kit line path: one
// ReapplyBgAfterResets per line. Its input is a patched markdown body on
// purpose: Block Kit text is rendered through RenderSlackMarkdownWith
// (render_message.go RenderText), whose output has already been through the
// final reapply pass, and WithBackground then runs over those lines.
func BenchmarkWithBackground(b *testing.B) {
	body := RenderSlackMarkdownWith(reapplyBenchText(10), reapplyBenchOpts)
	lines := strings.Split(body, "\n")
	bg := BgANSI()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WithBackground(lines, bg)
	}
}
