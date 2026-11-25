package ui

import (
	"testing"

	"github.com/gammons/slk/internal/core/blocks"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
)

const (
	heldPixel = "https://tracker.example/pixel.png"
	heldThumb = "https://files.slack.com/files-tmb/T1-F1-x/cat_360.png"
)

// heldImagesMessage carries a link-preview image from a third party and
// a Slack-hosted file image.
func heldImagesMessage() messages.MessageItem {
	return messages.MessageItem{
		TS: "1.0", UserID: "U2", UserName: "bob", Text: "look",
		LegacyAttachments: []blocks.LegacyAttachment{{Title: "Preview", ImageURL: heldPixel}},
		Attachments: []messages.Attachment{{
			Kind: "image", FileID: "F1", Name: "cat.png",
			Thumbs: []messages.ThumbSpec{{URL: heldThumb, W: 360, H: 240}},
		}},
	}
}

// loadImagesApp is previewApp (first message selected, toasts kept) with
// a load policy in mode.
func loadImagesApp(t *testing.T, mode imgpkg.LoadMode) (*App, *imgpkg.LoadPolicy) {
	t.Helper()
	a, _ := previewApp(t, nil, heldImagesMessage())
	p := imgpkg.NewLoadPolicy(mode, nil)
	a.SetImageContext(imgrender.ImageContext{Policy: p})
	return a, p
}

// I lists the hosts it would contact and loads nothing until answered;
// "a" trusts those hosts for the session.
func TestLoadImagesPromptAllowsHosts(t *testing.T) {
	a, p := loadImagesApp(t, imgpkg.LoadTrusted)

	pressKeyAll(t, a, keyPress('I'))
	if a.mode != ModeConfirm || !dialogShows(a, "1 image from tracker.example") || !dialogShows(a, "IP address") {
		t.Fatalf("mode = %v; want the load prompt naming tracker.example:\n%s", a.mode, a.confirmPrompt.View())
	}
	if p.Decide(heldPixel) != imgpkg.Ask {
		t.Fatal("opening the prompt allowed the image")
	}

	pressKeyAll(t, a, keyPress('a'))
	if p.Decide(heldPixel) != imgpkg.Load || p.Decide("https://tracker.example/other.png") != imgpkg.Load {
		t.Error("\"a\" did not trust tracker.example for the session")
	}
}

// In confirm mode the Slack-hosted file is held too, and "y" allows just
// the listed URLs, without the third-party warning for Slack's own host.
func TestLoadImagesConfirmModeAllowsURLs(t *testing.T) {
	a, p := loadImagesApp(t, imgpkg.LoadConfirm)

	pressKeyAll(t, a, keyPress('I'))
	if !dialogShows(a, "2 images from files.slack.com,") || !dialogShows(a, "tracker.example.") ||
		!dialogShows(a, "Trust these hosts this session") {
		t.Fatalf("prompt:\n%s", a.confirmPrompt.View())
	}
	pressKeyAll(t, a, keyPress('y'))
	if p.Decide(heldPixel) != imgpkg.Load || p.Decide(heldThumb) != imgpkg.Load {
		t.Error("\"y\" did not allow the listed URLs")
	}
	if p.Decide("https://tracker.example/other.png") != imgpkg.Ask {
		t.Error("\"y\" allowed the whole host")
	}
}

// Opening the preview of a held file image asks instead of fetching.
func TestPreviewOfHeldImageAsks(t *testing.T) {
	a, _ := loadImagesApp(t, imgpkg.LoadConfirm)
	pressKeyAll(t, a, keyPress('v'))
	if a.preview.Active() || !dialogShows(a, "Load images?") {
		t.Fatalf("preview active = %v; want the load prompt", a.preview.Active())
	}
}

func TestLoadImagesWhenOffOrNothingHeld(t *testing.T) {
	a, _ := loadImagesApp(t, imgpkg.LoadOff)
	pressKeyAll(t, a, keyPress('I'))
	if a.mode == ModeConfirm || !toastShows(a, "Images are off") {
		t.Errorf("off: mode = %v, status %q", a.mode, statusbarText(a))
	}

	b, p := loadImagesApp(t, imgpkg.LoadTrusted)
	p.AllowHosts("tracker.example")
	pressKeyAll(t, b, keyPress('I'))
	if b.mode == ModeConfirm || !toastShows(b, "No images waiting") {
		t.Errorf("nothing held: mode = %v, status %q", b.mode, statusbarText(b))
	}
}
