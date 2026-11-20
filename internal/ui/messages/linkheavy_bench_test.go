package messages

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/styles"
)

// linkHeavyWidth is the pane width from the #blacklists trace in issue 501.
const linkHeavyWidth = 378

// linkHeavyMessages is 50 messages, each with one legacy attachment of
// about 20 lines. Every line is a labelled link: the visible label is
// short and coloured by the renderer, and the OSC 8 target is long, which
// is the shape that made WrapRows spend ~690ms inside lipgloss.
func linkHeavyMessages() []MessageItem {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "<https://monitor.example.com/blacklist/%d/%s|blocked-%d.example>",
			i, strings.Repeat("x", 120), i)
	}
	text := b.String()
	msgs := make([]MessageItem, 50)
	for i := range msgs {
		msgs[i] = MessageItem{
			TS:        fmt.Sprintf("%d.000000", 1700000000+i),
			UserID:    "UBOT",
			UserName:  "blacklist-bot",
			Timestamp: "10:30 AM",
			Text:      "scan",
			LegacyAttachments: []blockkit.LegacyAttachment{{
				Color: "danger",
				Text:  text,
			}},
		}
	}
	return msgs
}

func pinBenchTheme(b *testing.B) {
	b.Helper()
	styles.Apply("dark", config.Theme{})
	b.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
}

// BenchmarkWrapRowsLinkHeavy times WrapRows alone on the rendered form of
// the link-heavy fixture, at the width from the trace.
func BenchmarkWrapRowsLinkHeavy(b *testing.B) {
	pinBenchTheme(b)
	msgs := linkHeavyMessages()
	m := New(msgs, "blacklists")
	rendered := make([]RenderedMessage, len(msgs))
	for i, msg := range msgs {
		rendered[i] = RenderMessage(msg, linkHeavyWidth, m.renderContext(), RenderOptions{})
	}
	wrap := NewWrapStyles(true, false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for n, msg := range msgs {
			_ = WrapRows(msg, rendered[n], linkHeavyWidth, wrap)
		}
	}
}

// BenchmarkBuildCacheLinkHeavy times a full messages.buildCache of the
// same fixture. That is the cost of opening the channel.
func BenchmarkBuildCacheLinkHeavy(b *testing.B) {
	pinBenchTheme(b)
	m := New(linkHeavyMessages(), "blacklists")
	_ = m.View(40, linkHeavyWidth)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.buildCache(linkHeavyWidth, false)
	}
}
