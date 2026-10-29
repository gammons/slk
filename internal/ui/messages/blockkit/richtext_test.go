package blockkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// helper: build a rich_text block with a single rich_text_section
// containing the given inline elements.
func rtSection(elements ...slack.RichTextSectionElement) RichTextBlock {
	return RichTextBlock{
		Elements: []slack.RichTextElement{
			&slack.RichTextSection{
				Type:     slack.RTESection,
				Elements: elements,
			},
		},
	}
}

func TestRichTextToMrkdwn_Empty(t *testing.T) {
	got := RichTextToMrkdwn(RichTextBlock{})
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestRichTextToMrkdwn_PlainText(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionTextElement{
		Type: slack.RTSEText,
		Text: "hello",
	})
	if got := RichTextToMrkdwn(rt); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestRichTextToMrkdwn_BoldText(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionTextElement{
		Type:  slack.RTSEText,
		Text:  "bold",
		Style: &slack.RichTextSectionTextStyle{Bold: true},
	})
	if got := RichTextToMrkdwn(rt); got != "*bold*" {
		t.Errorf("got %q, want %q", got, "*bold*")
	}
}

func TestRichTextToMrkdwn_ItalicText(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionTextElement{
		Type:  slack.RTSEText,
		Text:  "ital",
		Style: &slack.RichTextSectionTextStyle{Italic: true},
	})
	if got := RichTextToMrkdwn(rt); got != "_ital_" {
		t.Errorf("got %q, want %q", got, "_ital_")
	}
}

func TestRichTextToMrkdwn_StrikeText(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionTextElement{
		Type:  slack.RTSEText,
		Text:  "gone",
		Style: &slack.RichTextSectionTextStyle{Strike: true},
	})
	if got := RichTextToMrkdwn(rt); got != "~gone~" {
		t.Errorf("got %q, want %q", got, "~gone~")
	}
}

func TestRichTextToMrkdwn_CodeText(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionTextElement{
		Type:  slack.RTSEText,
		Text:  "x",
		Style: &slack.RichTextSectionTextStyle{Code: true},
	})
	if got := RichTextToMrkdwn(rt); got != "`x`" {
		t.Errorf("got %q, want %q", got, "`x`")
	}
}

func TestRichTextToMrkdwn_BoldAndItalicNest(t *testing.T) {
	// Bold + italic in one element: nesting order doesn't matter to
	// Slack's mrkdwn parser (both *_x_* and _*x*_ work). We emit
	// bold outermost so the output composes cleanly with bare
	// _italic_ runs in the same message.
	rt := rtSection(&slack.RichTextSectionTextElement{
		Type:  slack.RTSEText,
		Text:  "bi",
		Style: &slack.RichTextSectionTextStyle{Bold: true, Italic: true},
	})
	got := RichTextToMrkdwn(rt)
	if got != "*_bi_*" {
		t.Errorf("got %q, want %q", got, "*_bi_*")
	}
}

func TestRichTextToMrkdwn_StandaloneNewlinePreserved(t *testing.T) {
	// The whole point of this package: a standalone {type:"text",
	// text:"\n"} element must round-trip as a literal newline in
	// the output so multi-line block-kit messages don't collapse.
	rt := rtSection(
		&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "line1"},
		&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "\n"},
		&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "line2"},
	)
	want := "line1\nline2"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_DoubleNewlinePreserved(t *testing.T) {
	rt := rtSection(
		&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "para1"},
		&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "\n\n"},
		&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "para2"},
	)
	want := "para1\n\npara2"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_NewlineDoesNotGetStyled(t *testing.T) {
	// A standalone "\n" element that happens to carry a style flag
	// must not be wrapped in style markers — `\n` would round-trip
	// the literal asterisks into the output and confuse the
	// downstream mrkdwn renderer.
	rt := rtSection(
		&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "x"},
		&slack.RichTextSectionTextElement{
			Type:  slack.RTSEText,
			Text:  "\n",
			Style: &slack.RichTextSectionTextStyle{Bold: true},
		},
		&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "y"},
	)
	want := "x\ny"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_LinkWithLabel(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionLinkElement{
		Type: slack.RTSELink,
		URL:  "https://example.com",
		Text: "click",
	})
	want := "<https://example.com|click>"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_LinkWithoutLabel(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionLinkElement{
		Type: slack.RTSELink,
		URL:  "https://example.com",
	})
	want := "<https://example.com>"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_LinkLabelEqualsURL(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionLinkElement{
		Type: slack.RTSELink,
		URL:  "https://example.com",
		Text: "https://example.com",
	})
	// When the label is identical to the URL, the bare <url> form
	// is more compact and renders the same on the receiving side.
	want := "<https://example.com>"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_UserMention(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionUserElement{
		Type:   slack.RTSEUser,
		UserID: "U12345",
	})
	if got := RichTextToMrkdwn(rt); got != "<@U12345>" {
		t.Errorf("got %q, want %q", got, "<@U12345>")
	}
}

func TestRichTextToMrkdwn_ChannelMention(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionChannelElement{
		Type:      slack.RTSEChannel,
		ChannelID: "C9999",
	})
	if got := RichTextToMrkdwn(rt); got != "<#C9999>" {
		t.Errorf("got %q, want %q", got, "<#C9999>")
	}
}

func TestRichTextToMrkdwn_UsergroupMention(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionUserGroupElement{
		Type:        slack.RTSEUserGroup,
		UsergroupID: "S12345",
	})
	if got := RichTextToMrkdwn(rt); got != "<!subteam^S12345>" {
		t.Errorf("got %q, want %q", got, "<!subteam^S12345>")
	}
}

func TestRichTextToMrkdwn_BroadcastHere(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionBroadcastElement{
		Type:  slack.RTSEBroadcast,
		Range: "here",
	})
	if got := RichTextToMrkdwn(rt); got != "<!here>" {
		t.Errorf("got %q, want %q", got, "<!here>")
	}
}

func TestRichTextToMrkdwn_BroadcastChannel(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionBroadcastElement{
		Type:  slack.RTSEBroadcast,
		Range: "channel",
	})
	if got := RichTextToMrkdwn(rt); got != "<!channel>" {
		t.Errorf("got %q, want %q", got, "<!channel>")
	}
}

func TestRichTextToMrkdwn_Emoji(t *testing.T) {
	rt := rtSection(&slack.RichTextSectionEmojiElement{
		Type: slack.RTSEEmoji,
		Name: "smile",
	})
	if got := RichTextToMrkdwn(rt); got != ":smile:" {
		t.Errorf("got %q, want %q", got, ":smile:")
	}
}

func TestRichTextToMrkdwn_EmojiWithSkinTone(t *testing.T) {
	// Slack mrkdwn renders the skin-tone as an adjacent sibling
	// shortcode, e.g. :wave::skin-tone-3:. The downstream emoji
	// renderer reads each shortcode independently and composes the
	// final glyph.
	rt := rtSection(&slack.RichTextSectionEmojiElement{
		Type:     slack.RTSEEmoji,
		Name:     "wave",
		SkinTone: 3,
	})
	if got := RichTextToMrkdwn(rt); got != ":wave::skin-tone-3:" {
		t.Errorf("got %q, want %q", got, ":wave::skin-tone-3:")
	}
}

func TestRichTextToMrkdwn_EmojiWithTonePerPerson(t *testing.T) {
	// A two-person emoji with a tone per person has no skin_tone; the
	// tones are only in unicode. Slack's text field writes it as
	// :name::skin-tone-A-B:, even when A and B match. Elements are as
	// Slack sent them.
	cases := []struct {
		name string
		el   slack.RichTextSectionEmojiElement
		want string
	}{
		{"two tones",
			slack.RichTextSectionEmojiElement{Name: "people_holding_hands", Unicode: "1f9d1-1f3fb-200d-1f91d-200d-1f9d1-1f3ff"},
			":people_holding_hands::skin-tone-2-6:"},
		{"same tone twice",
			slack.RichTextSectionEmojiElement{Name: "people_holding_hands", Unicode: "1f9d1-1f3fb-200d-1f91d-200d-1f9d1-1f3fb"},
			":people_holding_hands::skin-tone-2-2:"},
		{"handshake",
			slack.RichTextSectionEmojiElement{Name: "handshake", Unicode: "1faf1-1f3fb-200d-1faf2-1f3fe"},
			":handshake::skin-tone-2-5:"},
		{"skin_tone wins",
			slack.RichTextSectionEmojiElement{Name: "handshake", SkinTone: 2, Unicode: "1f91d-1f3fb"},
			":handshake::skin-tone-2:"},
		{"untoned", slack.RichTextSectionEmojiElement{Name: "tada", Unicode: "1f389"}, ":tada:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.el.Type = slack.RTSEEmoji
			if got := RichTextToMrkdwn(rtSection(&c.el)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestRichTextToMrkdwn_MultipleSectionsJoinedByNewline(t *testing.T) {
	// Two adjacent rich_text_section blocks render as two visual
	// paragraphs, so a newline boundary is required between them.
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextSection{
			Type: slack.RTESection,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "A"},
			},
		},
		&slack.RichTextSection{
			Type: slack.RTESection,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "B"},
			},
		},
	}}
	want := "A\nB"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_UnorderedList(t *testing.T) {
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextList{
			Type:  slack.RTEList,
			Style: slack.RTEListBullet,
			Elements: []slack.RichTextElement{
				&slack.RichTextSection{Type: slack.RTESection, Elements: []slack.RichTextSectionElement{
					&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "apple"},
				}},
				&slack.RichTextSection{Type: slack.RTESection, Elements: []slack.RichTextSectionElement{
					&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "pear"},
				}},
			},
		},
	}}
	want := "• apple\n• pear"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_OrderedList(t *testing.T) {
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextList{
			Type:  slack.RTEList,
			Style: slack.RTEListOrdered,
			Elements: []slack.RichTextElement{
				&slack.RichTextSection{Type: slack.RTESection, Elements: []slack.RichTextSectionElement{
					&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "first"},
				}},
				&slack.RichTextSection{Type: slack.RTESection, Elements: []slack.RichTextSectionElement{
					&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "second"},
				}},
			},
		},
	}}
	want := "1. first\n2. second"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_IndentedList(t *testing.T) {
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextList{
			Type:   slack.RTEList,
			Style:  slack.RTEListBullet,
			Indent: 2,
			Elements: []slack.RichTextElement{
				&slack.RichTextSection{Type: slack.RTESection, Elements: []slack.RichTextSectionElement{
					&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "deep"},
				}},
			},
		},
	}}
	// Indent of 2 means two levels deep: 4 spaces of leading
	// padding before the bullet.
	want := "    • deep"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_Preformatted(t *testing.T) {
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextPreformatted{
			Type: slack.RTEPreformatted,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "console.log('hi')"},
			},
		},
	}}
	want := "```\nconsole.log('hi')\n```"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_Quote(t *testing.T) {
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextQuote{
			Type: slack.RTEQuote,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "to be"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "\n"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "or not to be"},
			},
		},
	}}
	want := "> to be\n> or not to be"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_UnknownElementSkippedSafely(t *testing.T) {
	rt := RichTextBlock{Elements: []slack.RichTextElement{
		&slack.RichTextSection{
			Type: slack.RTESection,
			Elements: []slack.RichTextSectionElement{
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: "before "},
				&slack.RichTextSectionUnknownElement{Type: "weird"},
				&slack.RichTextSectionTextElement{Type: slack.RTSEText, Text: " after"},
			},
		},
	}}
	// Unknown elements should not crash the conversion or smear
	// adjacent text together — they emit nothing and the surrounding
	// text passes through cleanly.
	want := "before  after"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_MessageMentionFixture(t *testing.T) {
	// End-to-end check on the shape Slack really sends for a message
	// that links to another message. The fixture is parsed through
	// slack-go's own rich-text decoder, so the test exercises the
	// RichTextSectionUnknownElement path rather than a hand-built
	// struct: slack-go has no type for `message_mention` and hands the
	// element back raw.
	p := loadFixture(t, "message_mention.json")
	blocks := Parse(p.Blocks)
	if len(blocks) == 0 {
		t.Fatal("fixture produced zero blocks")
	}
	rt, ok := blocks[0].(RichTextBlock)
	if !ok {
		t.Fatalf("first block = %T, want RichTextBlock", blocks[0])
	}
	sec, ok := rt.Elements[0].(*slack.RichTextSection)
	if !ok {
		t.Fatalf("first element = %T, want *slack.RichTextSection", rt.Elements[0])
	}
	if len(sec.Elements) != 4 {
		t.Fatalf("section has %d elements, want 4", len(sec.Elements))
	}
	if _, ok := sec.Elements[2].(*slack.RichTextSectionUnknownElement); !ok {
		t.Fatalf("third inline element = %T, want *slack.RichTextSectionUnknownElement", sec.Elements[2])
	}

	want := "<!subteam^S0000TEST1> <https://example.slack.com/archives/C0000TEST1/p1700000000000100> - see the earlier message"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_MessageMentionVariants(t *testing.T) {
	mention := func(raw string) *slack.RichTextSectionUnknownElement {
		return &slack.RichTextSectionUnknownElement{Type: "message_mention", Raw: raw}
	}
	cases := []struct {
		name string
		elem slack.RichTextSectionElement
		want string
	}{
		{
			name: "url with different label",
			elem: mention(`{"type":"message_mention","url":"https://x.slack.com/archives/C1/p1","text":"the other message"}`),
			want: "<https://x.slack.com/archives/C1/p1|the other message>",
		},
		{
			name: "url without label",
			elem: mention(`{"type":"message_mention","url":"https://x.slack.com/archives/C1/p1"}`),
			want: "<https://x.slack.com/archives/C1/p1>",
		},
		{
			name: "thread permalink labelled with itself",
			elem: mention(`{"type":"message_mention","url":"https://x.slack.com/archives/C1/p2?thread_ts=1.0&cid=C1","text":"https://x.slack.com/archives/C1/p2?thread_ts=1.0&cid=C1"}`),
			want: "<https://x.slack.com/archives/C1/p2?thread_ts=1.0&cid=C1>",
		},
		{
			name: "label escaped",
			elem: mention(`{"type":"message_mention","url":"https://x.slack.com/archives/C1/p1","text":"A -> B & <C>"}`),
			want: "<https://x.slack.com/archives/C1/p1|A -&gt; B &amp; &lt;C&gt;>",
		},
		{
			name: "label without url",
			elem: mention(`{"type":"message_mention","text":"just a label"}`),
			want: "",
		},
		{
			name: "malformed raw json",
			elem: mention(`{not json`),
			want: "",
		},
		{
			name: "other unknown type",
			elem: &slack.RichTextSectionUnknownElement{
				Type: "something_else",
				Raw:  `{"type":"something_else","url":"https://x.slack.com/archives/C1/p1"}`,
			},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RichTextToMrkdwn(rtSection(tc.elem)); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRichTextToMrkdwn_MessageMentionInPreformatted(t *testing.T) {
	rt := richTextFromJSON(t, `{"type":"rich_text","elements":[{"type":"rich_text_preformatted","elements":[`+
		`{"type":"message_mention","url":"https://x.slack.com/archives/C1/p1","text":"the other message"}]}]}`)
	want := "```\nhttps://x.slack.com/archives/C1/p1\n```"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_GitHubPendingReviewFixture(t *testing.T) {
	// End-to-end check using a real GitHub "Pending review on …"
	// message captured from cache.db. The full reconstructed
	// mrkdwn must contain one line per PR plus a blank-line
	// header separator. We assert the structural markers rather
	// than the exact string so the fixture can be regenerated
	// without re-encoding every label.
	p := loadFixture(t, "github_pending_review.json")
	blocks := Parse(p.Blocks)
	if len(blocks) == 0 {
		t.Fatal("fixture produced zero blocks")
	}
	rt, ok := blocks[0].(RichTextBlock)
	if !ok {
		t.Fatalf("first block = %T, want RichTextBlock", blocks[0])
	}
	got := RichTextToMrkdwn(rt)

	// 1. The header is bold.
	if !strings.HasPrefix(got, "*Pending review on userevidence/musashi*") {
		t.Errorf("output does not start with bold header; got:\n%s", got)
	}
	// 2. The header line is followed by a blank line then PR rows.
	if !strings.Contains(got, "\n\n[#4726]") {
		t.Errorf("missing blank-line separator between header and first PR; got:\n%s", got)
	}
	// 3. At least one PR per cached row — 7 PRs in this fixture.
	for _, pr := range []string{"[#4726]", "[#4743]", "[#4775]", "[#4823]", "[#4837]", "[#4852]", "[#4875]"} {
		if !strings.Contains(got, pr) {
			t.Errorf("missing %s in reconstructed mrkdwn", pr)
		}
	}
	// 4. Newlines actually separate PR rows — the bug we're fixing.
	// Count "[#" occurrences vs lines that start with "[#".
	lines := strings.Split(got, "\n")
	prLines := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "[#") {
			prLines++
		}
	}
	if prLines < 7 {
		t.Errorf("expected >=7 lines beginning with '[#', got %d. Full output:\n%s", prLines, got)
	}
	// 5. The user-mention element materialised as wire form.
	if !strings.Contains(got, "<@U015PNUF6DT>") {
		t.Errorf("missing user mention wire form in output:\n%s", got)
	}
	// 6. Links materialised as wire form (one URL is enough).
	if !strings.Contains(got, "<https://github.com/userevidence/musashi/pull/4726|") {
		t.Errorf("missing link wire form in output:\n%s", got)
	}
}

// richTextFromJSON decodes a rich_text block the way a received message's
// blocks are decoded, so element kinds slack-go does not know arrive as
// *slack.RichTextSectionUnknownElement with their raw JSON.
func richTextFromJSON(t *testing.T, raw string) RichTextBlock {
	t.Helper()
	var rt slack.RichTextBlock
	if err := json.Unmarshal([]byte(raw), &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return RichTextBlock{Elements: rt.Elements}
}

// attachmentMentionJSON is the shape of an app's link chip (a Work
// Object) in a rich_text block, as Slack's web client writes it when a
// pasted link unfurls into a chip (captured from drafts.list).
const attachmentMentionJSON = `{"type":"attachment_mention",` +
	`"url":"https://linear.app/acme/issue/ENG-42/fix-the-thing",` +
	`"text":"Fix the thing","app_id":"A0123456789",` +
	`"entity_id":"W:A0123456789:lin1:issue","icon_name":"brand-linear-filled",` +
	`"product_name":"Linear"}`

func TestRichTextToMrkdwn_AttachmentMentionRendersTitle(t *testing.T) {
	rt := richTextFromJSON(t, `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[`+
		attachmentMentionJSON+`]}]}`)
	want := "<https://linear.app/acme/issue/ENG-42/fix-the-thing|Fix the thing>"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_AttachmentMentionInsideText(t *testing.T) {
	rt := richTextFromJSON(t, `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[`+
		`{"type":"text","text":"see "},`+attachmentMentionJSON+`,{"type":"text","text":" please"}]}]}`)
	want := "see <https://linear.app/acme/issue/ENG-42/fix-the-thing|Fix the thing> please"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_AttachmentMentionWithoutTitle(t *testing.T) {
	rt := richTextFromJSON(t, `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[`+
		`{"type":"attachment_mention","url":"https://example.com/x"}]}]}`)
	want := "<https://example.com/x>"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A title is app-supplied text inside <url|label>: a ">" in it would end
// the link early, so it is entity-escaped like Slack's own text field.
func TestRichTextToMrkdwn_AttachmentMentionTitleEscaped(t *testing.T) {
	rt := richTextFromJSON(t, `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[`+
		`{"type":"attachment_mention","url":"https://example.com/x","text":"A -> B & <C>"}]}]}`)
	want := "<https://example.com/x|A -&gt; B &amp; &lt;C&gt;>"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_AttachmentMentionWithoutURLSkipped(t *testing.T) {
	rt := richTextFromJSON(t, `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[`+
		`{"type":"text","text":"a "},{"type":"attachment_mention","text":"orphan"},{"type":"text","text":" b"}]}]}`)
	want := "a  b"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRichTextToMrkdwn_AttachmentMentionInPreformatted(t *testing.T) {
	rt := richTextFromJSON(t, `{"type":"rich_text","elements":[{"type":"rich_text_preformatted","elements":[`+
		attachmentMentionJSON+`]}]}`)
	want := "```\nhttps://linear.app/acme/issue/ENG-42/fix-the-thing\n```"
	if got := RichTextToMrkdwn(rt); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
