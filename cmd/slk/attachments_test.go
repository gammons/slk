package main

import (
	"testing"

	"github.com/slack-go/slack"
)

// TestPickAttachmentURLImagePrefersThumbnail verifies that for image
// attachments we prefer the unauthenticated thumbnail URL over the
// auth-gated Permalink. Without this, clicking an image bounces through
// Slack's browser auth flow / launches the desktop client.
func TestPickAttachmentURLImagePrefersThumbnail(t *testing.T) {
	f := slack.File{
		Mimetype:        "image/png",
		Permalink:       "https://team.slack.com/files/U1/F1/image.png",
		PermalinkPublic: "https://slack-files.com/T-F-pubtoken",
		URLPrivate:      "https://files.slack.com/files-pri/T-F/image.png",
		Thumb480:        "https://files.slack.com/files-tmb/T-F/image_480.png",
		Thumb720:        "https://files.slack.com/files-tmb/T-F/image_720.png",
	}

	got := pickAttachmentURL(f, "image")
	if got != f.Thumb720 {
		t.Errorf("expected largest available thumbnail (720), got %q", got)
	}
}

// TestPickAttachmentURLImageFallsBackThroughThumbs ensures we walk the
// thumbnail-size ladder downward when larger sizes are missing.
func TestPickAttachmentURLImageFallsBackThroughThumbs(t *testing.T) {
	f := slack.File{
		Mimetype: "image/jpeg",
		Thumb360: "https://files.slack.com/files-tmb/.../small_360.jpg",
	}
	got := pickAttachmentURL(f, "image")
	if got != f.Thumb360 {
		t.Errorf("expected fall-through to Thumb360, got %q", got)
	}
}

// TestPickAttachmentURLImageFallsBackToPublicPermalink covers the case
// where no thumbnails are populated.
func TestPickAttachmentURLImageFallsBackToPublicPermalink(t *testing.T) {
	f := slack.File{
		Mimetype:        "image/gif",
		PermalinkPublic: "https://slack-files.com/pub",
		Permalink:       "https://team.slack.com/files/U/F",
	}
	got := pickAttachmentURL(f, "image")
	if got != f.PermalinkPublic {
		t.Errorf("expected PermalinkPublic, got %q", got)
	}
}

// TestPickAttachmentURLFileUsesPermalink confirms non-image files keep
// using the auth-gated Permalink (correct: those files aren't directly
// downloadable without Slack auth anyway).
func TestPickAttachmentURLFileUsesPermalink(t *testing.T) {
	f := slack.File{
		Mimetype:   "application/pdf",
		Permalink:  "https://team.slack.com/files/U/F/doc.pdf",
		URLPrivate: "https://files.slack.com/files-pri/.../doc.pdf",
	}
	got := pickAttachmentURL(f, "file")
	if got != f.Permalink {
		t.Errorf("expected Permalink for non-image, got %q", got)
	}
}

// TestExtractAttachmentsPopulatesDownloadFields confirms every
// attachment carries the auth-gated URLPrivate for the `d` download
// keybinding, plus the byte size for the picker row.
func TestExtractAttachmentsPopulatesDownloadFields(t *testing.T) {
	files := []slack.File{
		{
			ID:         "F1",
			Mimetype:   "text/csv",
			Title:      "report.csv",
			Permalink:  "https://team.slack.com/files/U/F1",
			URLPrivate: "https://files.slack.com/files-pri/T-F1/report.csv",
			Size:       1234,
		},
	}
	atts := extractAttachments(files)
	if len(atts) != 1 {
		t.Fatalf("got %d attachments", len(atts))
	}
	if atts[0].DownloadURL != files[0].URLPrivate {
		t.Errorf("DownloadURL = %q", atts[0].DownloadURL)
	}
	if atts[0].Size != 1234 {
		t.Errorf("Size = %d", atts[0].Size)
	}
}
