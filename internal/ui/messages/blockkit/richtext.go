package blockkit

import (
	"strconv"
	"strings"

	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/emoji"
)

// RichTextToMrkdwn reconstructs Slack mrkdwn from a parsed rich_text
// block. It is the inverse of internal/slack/mrkdwn.Convert (which
// goes mrkdwn → rich_text on the send path); this direction is used
// on the receive path to recover the newline structure that Slack's
// `text` fallback throws away for any rich_text-originated message.
//
// The output is Slack mrkdwn wire form — *bold*, _italic_, ~strike~,
// `code`, <url|label>, <@U…>, <#C…>, <!subteam^S…>, <!here>, :emoji: —
// which the host can pass to RenderSlackMarkdown unchanged.
func RichTextToMrkdwn(rt RichTextBlock) string {
	if len(rt.Elements) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rt.Elements))
	for _, e := range rt.Elements {
		switch v := e.(type) {
		case *slack.RichTextSection:
			parts = append(parts, sectionToMrkdwn(v.Elements))
		case *slack.RichTextList:
			parts = append(parts, listToMrkdwn(v))
		case *slack.RichTextPreformatted:
			parts = append(parts, preformattedToMrkdwn(v.Elements))
		case *slack.RichTextQuote:
			parts = append(parts, quoteToMrkdwn(v.Elements))
			// RichTextUnknown is silently dropped — we can't render
			// content we don't understand, but we don't want it to
			// crash the conversion either.
		}
	}
	return strings.Join(parts, "\n")
}

// sectionToMrkdwn walks the inline elements of a rich_text_section
// and emits a single mrkdwn string preserving every standalone "\n"
// text element as a literal newline.
func sectionToMrkdwn(elements []slack.RichTextSectionElement) string {
	var b strings.Builder
	for _, e := range elements {
		b.WriteString(inlineToMrkdwn(e))
	}
	return b.String()
}

// inlineToMrkdwn renders one rich_text_section element. Returns "" for
// unknown / unsupported element kinds so adjacent elements still
// flow together cleanly.
func inlineToMrkdwn(e slack.RichTextSectionElement) string {
	switch v := e.(type) {
	case *slack.RichTextSectionTextElement:
		return textToMrkdwn(v.Text, v.Style)
	case *slack.RichTextSectionLinkElement:
		return linkToMrkdwn(v.URL, v.Text)
	case *slack.RichTextSectionUserElement:
		return "<@" + v.UserID + ">"
	case *slack.RichTextSectionChannelElement:
		return "<#" + v.ChannelID + ">"
	case *slack.RichTextSectionUserGroupElement:
		return "<!subteam^" + v.UsergroupID + ">"
	case *slack.RichTextSectionBroadcastElement:
		return "<!" + v.Range + ">"
	case *slack.RichTextSectionEmojiElement:
		return emojiToMrkdwn(v.Name, v.SkinTone, v.Unicode)
	case *slack.RichTextSectionTeamElement:
		// Team mentions are rare in chat surface; emit the Slack
		// wire form so the downstream renderer can do whatever it
		// wants (or fall through as a literal token).
		return "<!team^" + v.TeamID + ">"
	case *slack.RichTextSectionDateElement:
		if v.Fallback != nil && *v.Fallback != "" {
			return *v.Fallback
		}
		return strconv.FormatInt(int64(v.Timestamp), 10)
	case *slack.RichTextSectionColorElement:
		return v.Value
	}
	return ""
}

// textToMrkdwn applies style flags around a literal text run. Bare
// "\n" text elements (used by Slack to separate sibling paragraphs
// inside a single rich_text_section) MUST NOT be wrapped in style
// markers: the markers would round-trip into the rendered output and
// the downstream mrkdwn parser would either treat them as literal
// asterisks or open a styled run that never closes.
func textToMrkdwn(text string, style *slack.RichTextSectionTextStyle) string {
	if text == "" {
		return ""
	}
	// Whitespace-only runs are also treated as plain text — wrapping
	// "  " in *…* would mean Slack sees "* *" which it ignores.
	if style == nil || strings.TrimSpace(text) == "" {
		return text
	}
	// Order matters for nested style markers: bold outermost, then
	// italic, then strike, then code. The downstream Slack-mrkdwn
	// parser accepts any nesting order, but emitting them in a
	// stable order makes the output predictable for tests and
	// human inspection.
	out := text
	if style.Code {
		out = "`" + out + "`"
	}
	if style.Strike {
		out = "~" + out + "~"
	}
	if style.Italic {
		out = "_" + out + "_"
	}
	if style.Bold {
		out = "*" + out + "*"
	}
	return out
}

// linkToMrkdwn emits Slack's wire-form link. The bare <url> form is
// used when the label is empty or identical to the URL; otherwise
// <url|label> preserves the user-facing label.
func linkToMrkdwn(url, label string) string {
	if label == "" || label == url {
		return "<" + url + ">"
	}
	return "<" + url + "|" + label + ">"
}

// emojiToMrkdwn renders an emoji element as one or two adjacent
// shortcodes. A non-zero SkinTone is emitted as a sibling
// :skin-tone-N: shortcode — Slack web does the same, and the
// downstream emoji renderer composes them at display time.
//
// A two-person emoji with a tone per person has no SkinTone; its two
// modifiers are only in unicode ("1f9d1-1f3fb-200d-1f91d-200d-1f9d1-1f3ff").
// It is emitted as :skin-tone-A-B:, the form Slack's text field uses,
// even when A and B match.
func emojiToMrkdwn(name string, skinTone int, unicode string) string {
	out := ":" + name + ":"
	if skinTone > 0 {
		return out + ":skin-tone-" + strconv.Itoa(skinTone) + ":"
	}
	var tones []string
	for _, cp := range strings.Split(unicode, "-") {
		n, err := strconv.ParseInt(cp, 16, 32)
		if err != nil {
			continue
		}
		if tone, ok := emoji.SlackSkinToneNumber(rune(n)); ok {
			tones = append(tones, strconv.Itoa(tone))
		}
	}
	if len(tones) == 2 {
		out += ":skin-tone-" + tones[0] + "-" + tones[1] + ":"
	}
	return out
}

// listToMrkdwn emits a rich_text_list as one line per item. Bulleted
// lists use "• ", ordered lists use "N. " starting from Offset+1.
// Nested lists (encoded via the Indent field rather than nesting)
// get two leading spaces per indent level.
func listToMrkdwn(l *slack.RichTextList) string {
	lines := make([]string, 0, len(l.Elements))
	pad := strings.Repeat("  ", l.Indent)
	for i, child := range l.Elements {
		body := ""
		if sec, ok := child.(*slack.RichTextSection); ok {
			body = sectionToMrkdwn(sec.Elements)
		}
		var marker string
		if l.Style == slack.RTEListOrdered {
			marker = strconv.Itoa(l.Offset+i+1) + ". "
		} else {
			marker = "• "
		}
		lines = append(lines, pad+marker+body)
	}
	return strings.Join(lines, "\n")
}

// preformattedToMrkdwn wraps the section's inline elements in a
// triple-backtick fence. Inline style flags on text elements inside
// preformatted blocks are ignored (Slack ignores them on render too).
func preformattedToMrkdwn(elements []slack.RichTextSectionElement) string {
	var b strings.Builder
	for _, e := range elements {
		switch v := e.(type) {
		case *slack.RichTextSectionTextElement:
			b.WriteString(v.Text)
		case *slack.RichTextSectionLinkElement:
			b.WriteString(v.URL)
		}
	}
	return "```\n" + b.String() + "\n```"
}

// quoteToMrkdwn emits a rich_text_quote as one or more "> " lines.
// Each "\n" inside the section becomes a line boundary that gets
// its own "> " prefix.
func quoteToMrkdwn(elements []slack.RichTextSectionElement) string {
	body := sectionToMrkdwn(elements)
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = "> " + l
	}
	return strings.Join(lines, "\n")
}
