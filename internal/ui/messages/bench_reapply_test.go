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

// BenchmarkReapplyBgOnRenderedBody isolates the ReapplyBgAfterResets call
// RenderSlackMarkdownWith makes, on a real rendered body.
func BenchmarkReapplyBgOnRenderedBody(b *testing.B) {
	body := RenderSlackMarkdownWith(reapplyBenchText(10), reapplyBenchOpts)
	style := BgANSI() + FgANSI()
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
		_ = RenderMessage(msg, 100, rc, RenderOptions{})
	}
}

// BenchmarkWithBackground measures the Block Kit line path: one
// ReapplyBgAfterResets per line.
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
