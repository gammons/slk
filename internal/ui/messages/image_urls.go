package messages

import "github.com/gammons/slk/internal/core/blocks"

// ImageURLs returns every URL the panes may fetch to draw msg's images,
// in message order, each once: image file thumbnails (every size, since
// the one drawn depends on the pane width), Block Kit image blocks and
// section image accessories, and legacy attachment image_url, including
// blocks nested in a legacy attachment. Avatars and emoji are not
// included: Slack assigns their URLs.
func ImageURLs(msg MessageItem) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(u string) {
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	for _, att := range msg.Attachments {
		if att.Kind != "image" {
			continue
		}
		for _, t := range att.Thumbs {
			add(t.URL)
		}
	}
	blockImageURLs(msg.Blocks, add)
	for _, la := range msg.LegacyAttachments {
		add(la.ImageURL)
		blockImageURLs(la.Blocks, add)
	}
	return out
}

func blockImageURLs(bs []blocks.Block, add func(string)) {
	for _, b := range bs {
		switch b := b.(type) {
		case blocks.ImageBlock:
			add(b.URL)
		case blocks.SectionBlock:
			if acc, ok := b.Accessory.(blocks.ImageAccessory); ok {
				add(acc.URL)
			}
		}
	}
}
