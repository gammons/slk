package messages

import (
	"testing"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// Without a rich_text block the text field is all there is: its links
// read as markdown and Slack's escapes are decoded.
func TestEditSource_TextOnly(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"plain", "plain"},
		{"a &lt; b &amp;&amp; c &gt; d", "a < b && c > d"},
		{"see <https://x.com/a?b=1&amp;c=2|the docs> now", "see [the docs](https://x.com/a?b=1&c=2) now"},
		{"bare <https://x.com/>", "bare https://x.com/"},
		{"mail <mailto:a@b.com|a@b.com>", "mail a@b.com"},
		{"get <ftp://x.example/f|label>", "get [label](ftp://x.example/f)"},
		{"open <file:///tmp/a|label>", "open [label](file:///tmp/a)"},
		{"bare <file:///tmp/a>", "bare file:///tmp/a"},
		{"hi <@U1> in <#C2>", "hi <@U1> in <#C2>"},
		{"&lt;https://x.com/|not a link&gt;", "<https://x.com/|not a link>"},
		{"&lt;!here&gt; is text, <!here> is not", "&lt;!here&gt; is text, <!here> is not"},
		{"&lt;@U1&gt; vs <@U1>", "&lt;@U1&gt; vs <@U1>"},
	} {
		if got := EditSource(MessageItem{Text: tc.text}); got != tc.want {
			t.Errorf("EditSource(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

// Every rich_text block is kept, so saving does not drop the later ones.
func TestEditSource_AllRichTextBlocks(t *testing.T) {
	block := func(s string) blocks.Block {
		return blockkit.RichTextBlock{Elements: []slack.RichTextElement{
			&slack.RichTextSection{Type: slack.RTESection, Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: s},
			}},
		}}
	}
	msg := MessageItem{Text: "first second", Blocks: []blocks.Block{block("first"), block("second")}}
	if got, want := EditSource(msg), "first\n\nsecond"; got != want {
		t.Errorf("EditSource = %q, want %q", got, want)
	}
}
