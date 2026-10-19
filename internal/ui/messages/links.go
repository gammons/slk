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
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// Link is one link found in a message's text.
type Link struct {
	URL   string
	Label string // empty for bare <url> links
}

// ExtractLinks returns the links in text in order of appearance,
// deduplicated by URL (first occurrence wins). Returns nil when text
// has no links.
func ExtractLinks(text string) []Link {
	type posLink struct {
		start int
		link  Link
	}
	var found []posLink
	for _, m := range linkWithLabelRe.FindAllStringSubmatchIndex(text, -1) {
		found = append(found, posLink{
			start: m[0],
			link:  Link{URL: text[m[2]:m[3]], Label: text[m[4]:m[5]]},
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
// for msg: ExtractLinks of its text and of its Block Kit blocks, then
// the links inside each legacy attachment (mrkdwn text, pretext,
// title_link, fields, and nested blocks — Jira-style link-unfurl
// notifications carry the whole visible body in attachment blocks
// while Title/Text/Fields are empty), then the permalink of each
// message it shares. A message shared via Slack's "Share message" /
// forward action shows as an embedded preview whose permalink exists
// only in the attachment's from_url, never in the text. Non-Slack
// from_urls (ordinary unfurls) are skipped; their URL is already in
// the text or the block body. Deduplicated by URL, first occurrence
// wins.
func MessageLinks(msg MessageItem) []Link {
	out := ExtractLinks(msg.Text)
	out = append(out, linksFromBlocks(msg.Blocks)...)
	for _, att := range msg.LegacyAttachments {
		if att.Pretext != "" {
			out = append(out, ExtractLinks(att.Pretext)...)
		}
		if att.TitleLink != "" {
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
// render order, by walking the text-bearing blocks. Only blocks that
// render readable mrkdwn are scanned: section text and fields,
// context elements, and rich_text blocks (reconstructed to mrkdwn by
// blockkit.RichTextToMrkdwn, whose link elements become <url|label).
// Header text is plain, image URLs are images, actions elements carry
// no URL, and unknown blocks render as placeholders, so none of them
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
		case blocks.RichTextBlock:
			if md := blockkit.RichTextToMrkdwn(v); md != "" {
				out = append(out, ExtractLinks(md)...)
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
