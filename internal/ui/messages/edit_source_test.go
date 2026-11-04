package messages

import (
	"testing"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/slack/mrkdwn"
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

// A rich_text link whose target a bare typed URL would cut short (non-ASCII
// or a space) is seeded in explicit form, so saving keeps the whole target.
func TestEditSource_LinkWithUnbareableTargetSurvivesSave(t *testing.T) {
	for _, url := range []string{"file:///tmp/résumé.txt", "file:///tmp/my file.txt"} {
		msg := MessageItem{Blocks: []blocks.Block{blockkit.RichTextBlock{Elements: []slack.RichTextElement{
			&slack.RichTextSection{Type: slack.RTESection, Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionLinkElement{Type: slack.RTSELink, URL: url},
			}},
		}}}}
		src := EditSource(msg)
		_, block := mrkdwn.Convert(src)
		if block == nil || len(block.Elements) != 1 {
			t.Fatalf("Convert(%q) gave %v", src, block)
		}
		sec, _ := block.Elements[0].(*slack.RichTextSection)
		var got []string
		for _, e := range sec.Elements {
			if l, ok := e.(*slack.RichTextSectionLinkElement); ok {
				got = append(got, l.URL)
			}
		}
		if len(got) != 1 || got[0] != url {
			t.Errorf("seed %q saved targets %q, want %q", src, got, url)
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
