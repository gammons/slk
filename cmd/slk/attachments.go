package main

import (
	"strings"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/slack-go/slack"
)

// extractAttachments converts slack-go File entries into the UI's
// Attachment representation.
//
// URL preference depends on the kind:
//   - For images we use an unauthenticated thumbnail URL (files.slack.com/...)
//     when available so the link opens the picture directly in a browser
//     instead of bouncing through Slack's auth flow / launching the desktop
//     client. We pick a reasonably large thumbnail (1024 -> 720 -> 480 ->
//     360 -> 160 -> 80 -> 64) and fall back to PermalinkPublic, Permalink,
//     and finally URLPrivate.
//   - For non-images (PDFs, etc.) we use Permalink, since those files are
//     intentionally gated by Slack auth and opening the workspace UI is the
//     correct flow.
//
// Title is used for the display name when present (Slack lets users set a
// title separate from the original filename); otherwise we fall back to
// the filename. Image mimetypes get the "image" kind so the renderer can
// show [Image]; everything else gets "file" -> [File].
func extractAttachments(files []slack.File) []messages.Attachment {
	if len(files) == 0 {
		return nil
	}
	out := make([]messages.Attachment, 0, len(files))
	for _, f := range files {
		kind := "file"
		if strings.HasPrefix(f.Mimetype, "image/") {
			kind = "image"
		}
		name := f.Title
		if name == "" {
			name = f.Name
		}
		att := messages.Attachment{Kind: kind, Name: name, URL: pickAttachmentURL(f, kind)}
		att.DownloadURL = f.URLPrivate
		att.Size = int64(f.Size)
		if kind == "image" {
			att.FileID = f.ID
			att.Mime = f.Mimetype
			att.Thumbs = collectThumbs(f)
		}
		out = append(out, att)
	}
	return out
}

// extractBlocks converts a slack.Blocks value to our typed block
// slice for storage on a MessageItem. Empty input returns nil.
func extractBlocks(b slack.Blocks) []blockkit.Block {
	return blockkit.Parse(b)
}

// extractLegacyAttachments converts slack-go Attachment slice into
// our LegacyAttachment type. Empty input returns nil.
func extractLegacyAttachments(a []slack.Attachment) []blockkit.LegacyAttachment {
	return blockkit.ParseAttachments(a)
}

// collectThumbs builds a slice of ThumbSpec from a slack.File's thumb_*
// fields. Tiers with an empty URL or non-positive dimensions are skipped.
// The slice is ordered smallest-to-largest, matching the order Slack
// returns them in the file metadata.
func collectThumbs(f slack.File) []messages.ThumbSpec {
	var out []messages.ThumbSpec
	add := func(url string, w, h int) {
		if url != "" && w > 0 && h > 0 {
			out = append(out, messages.ThumbSpec{URL: url, W: w, H: h})
		}
	}
	add(f.Thumb360, f.Thumb360W, f.Thumb360H)
	add(f.Thumb480, f.Thumb480W, f.Thumb480H)
	add(f.Thumb720, f.Thumb720W, f.Thumb720H)
	add(f.Thumb960, f.Thumb960W, f.Thumb960H)
	add(f.Thumb1024, f.Thumb1024W, f.Thumb1024H)
	return out
}

// pickAttachmentURL chooses the best URL for a slack.File based on its kind.
// See extractAttachments for the rationale.
func pickAttachmentURL(f slack.File, kind string) string {
	if kind == "image" {
		// Try thumbnails from largest to smallest -- these are direct image
		// bytes hosted at files.slack.com and openable without auth.
		for _, u := range []string{f.Thumb1024, f.Thumb720, f.Thumb480, f.Thumb360, f.Thumb160, f.Thumb80, f.Thumb64} {
			if u != "" {
				return u
			}
		}
		if f.PermalinkPublic != "" {
			return f.PermalinkPublic
		}
	}
	if f.Permalink != "" {
		return f.Permalink
	}
	return f.URLPrivate
}
