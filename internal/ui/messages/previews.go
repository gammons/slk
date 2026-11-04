package messages

// RemovablePreview is a link preview on a message that its author can
// remove (chat.deleteAttachment): a legacy attachment that unfurls a URL.
type RemovablePreview struct {
	// AttachmentID is what Slack's removal call names the attachment by:
	// its own id, or its 1-based position when Slack sent none.
	AttachmentID int
	// URL is the link the attachment unfurls (from_url, else original_url).
	URL string
	// Title is the card's title; empty when it has none.
	Title string
}

// Label is what a prompt names the preview by: its title, else its URL.
func (p RemovablePreview) Label() string {
	if p.Title != "" {
		return p.Title
	}
	return p.URL
}

// RemovablePreviews lists msg's link previews in attachment order. A
// legacy attachment counts when it carries a URL it unfurls (FromURL or
// OriginalURL); bot cards without either are left alone. A message the
// user shared (forwarded) carries from_url too and is listed: it cannot
// be told apart, and Slack refuses what it does not allow.
func RemovablePreviews(msg MessageItem) []RemovablePreview {
	var out []RemovablePreview
	for i, att := range msg.LegacyAttachments {
		url := att.FromURL
		if url == "" {
			url = att.OriginalURL
		}
		if url == "" {
			continue
		}
		id := att.ID
		if id == 0 {
			id = i + 1
		}
		out = append(out, RemovablePreview{AttachmentID: id, URL: url, Title: att.Title})
	}
	return out
}
