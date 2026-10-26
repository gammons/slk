// internal/ui/messages/links.go
//
// ExtractLinks pulls http(s)/mailto links out of a message's mrkdwn
// text using the same regexes the renderer uses (render.go), so the
// "open link" keybinding sees exactly the links the user sees.
package messages

import (
	"sort"
	"strings"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/slackurl"
)

// Link is one link found in a message's text.
type Link struct {
	URL   string
	Label string // empty for bare <url> links
}

// ExtractLinks returns the links in text in order of appearance,
// deduplicated by URL (first occurrence wins). Returns nil when text
// has no links. Labels are entity-decoded (&amp; &lt; &gt;) the way the
// renderer shows them, so the link picker matches the message.
func ExtractLinks(text string) []Link {
	type posLink struct {
		start int
		link  Link
	}
	var found []posLink
	for _, m := range linkWithLabelRe.FindAllStringSubmatchIndex(text, -1) {
		found = append(found, posLink{
			start: m[0],
			link:  Link{URL: text[m[2]:m[3]], Label: slackEntityDecoder.Replace(text[m[4]:m[5]])},
		})
	}
	for _, m := range linkBareRe.FindAllStringSubmatchIndex(text, -1) {
		url := text[m[2]:m[3]]
		// linkBareRe also matches the labeled form (its [^>]+ spans
		// the "|label" part); those were already captured above.
		if strings.Contains(url, "|") {
			continue
		}
		found = append(found, posLink{start: m[0], link: Link{URL: url}})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].start < found[j].start })
	var out []Link
	seen := make(map[string]bool, len(found))
	for _, f := range found {
		if seen[f.link.URL] {
			continue
		}
		seen[f.link.URL] = true
		out = append(out, f.link)
	}
	return out
}

// MessageLinks returns the links the "open link" keybinding offers
// for msg, in the order they are drawn: ExtractLinks of the rendered
// body row when one exists, then links in the remaining top-level
// Block Kit blocks (sections and contexts), then the links inside
// each legacy attachment (mrkdwn text, pretext, title_link, fields,
// and nested blocks — Jira-style link-unfurl notifications carry the
// whole visible body in attachment blocks while Title/Text/Fields are
// empty), then the permalink of each message it shares. The body row
// is drawn only when the blocks do not already carry the body
// (render_message.go: hasBody := !BlocksCarryBody(msg)); its content
// is MessageTextSource — the first non-empty rich_text reconstruction
// when one exists, else msg.Text. When the blocks carry the body,
// msg.Text is not drawn at all, so its links stay out of the picker
// (a hidden fallback label must not shadow a visible block link's
// label in the dedup). A message shared via Slack's "Share message" /
// forward action shows as an embedded preview whose permalink exists
// only in the attachment's from_url, never in the text. Non-Slack
// from_urls (ordinary unfurls) are skipped; their URL is already in
// the text or the block body. Deduplicated by URL, first occurrence
// wins.
func MessageLinks(msg MessageItem) []Link {
	var out []Link
	if !BlocksCarryBody(msg) {
		out = ExtractLinks(MessageTextSource(msg))
	}
	out = append(out, linksFromBlocks(msg.Blocks)...)
	for _, att := range msg.LegacyAttachments {
		if att.Pretext != "" {
			out = append(out, ExtractLinks(att.Pretext)...)
		}
		// The renderer only draws a title (and its OSC-8 link) when
		// Title is set (blockkit attachments.go), so a TitleLink
		// without a Title is a link the user never sees; skip it.
		if att.Title != "" && att.TitleLink != "" {
			out = append(out, Link{URL: att.TitleLink, Label: att.Title})
		}
		if att.Text != "" {
			out = append(out, ExtractLinks(att.Text)...)
		}
		for _, f := range att.Fields {
			if f.Value != "" {
				out = append(out, ExtractLinks(f.Value)...)
			}
		}
		out = append(out, linksFromBlocks(att.Blocks)...)
		if att.FromURL == "" {
			continue
		}
		if _, ok := slackurl.Parse(att.FromURL); !ok {
			continue
		}
		dup := false
		for _, l := range out {
			if l.URL == att.FromURL {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		label := "Shared message"
		if att.AuthorName != "" {
			label = "Message from " + att.AuthorName
		}
		out = append(out, Link{URL: att.FromURL, Label: label})
	}
	return dedupeLinks(out)
}

// linksFromBlocks collects the links in a Block Kit block slice, in
// render order, by walking the text-bearing blocks: section text and
// fields, and context elements. Rich text never contributes here: a
// top-level rich_text block is the message body and arrives through
// MessageTextSource (its links are collected before this walk), and
// rich text nested inside an attachment renders no lines at all
// (blockkit.Render emits nothing for it, a pre-existing gap), so its
// links stay out of the picker until that rendering exists. Header
// text is plain, image URLs are images, actions elements carry no
// URL, and unknown blocks render as placeholders, so none of them
// contribute.
func linksFromBlocks(bs []blocks.Block) []Link {
	var out []Link
	for _, b := range bs {
		switch v := b.(type) {
		case blocks.SectionBlock:
			if v.Text != "" {
				out = append(out, ExtractLinks(v.Text)...)
			}
			for _, f := range v.Fields {
				if f != "" {
					out = append(out, ExtractLinks(f)...)
				}
			}
		case blocks.ContextBlock:
			for _, e := range v.Elements {
				if e.Text != "" {
					out = append(out, ExtractLinks(e.Text)...)
				}
			}
		}
	}
	return out
}

// dedupeLinks drops entries whose URL was already seen, keeping the
// first occurrence.
func dedupeLinks(in []Link) []Link {
	var out []Link
	seen := make(map[string]bool, len(in))
	for _, l := range in {
		if seen[l.URL] {
			continue
		}
		seen[l.URL] = true
		out = append(out, l)
	}
	return out
}
