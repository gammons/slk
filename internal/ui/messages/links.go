// internal/ui/messages/links.go
//
// ExtractLinks pulls http(s)/mailto links out of a message's mrkdwn
// text using the same regexes the renderer uses (render.go), so the
// "open link" keybinding sees exactly the links the user sees.
package messages

import (
	"sort"
	"strings"

	"github.com/gammons/slk/internal/slackurl"
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
// for msg: ExtractLinks of its text, then the permalink of each
// message it shares. A message shared via Slack's "Share message" /
// forward action shows as an embedded preview whose permalink exists
// only in the attachment's from_url, never in the text. Non-Slack
// from_urls (ordinary unfurls) are skipped; their URL is already in
// the text. Deduplicated by URL, first occurrence wins.
func MessageLinks(msg MessageItem) []Link {
	out := ExtractLinks(msg.Text)
	for _, att := range msg.LegacyAttachments {
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
	return out
}
