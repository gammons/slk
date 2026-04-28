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
