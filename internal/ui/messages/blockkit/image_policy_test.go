package blockkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	imgpkg "github.com/gammons/slk/internal/image"
)

// An image the load policy holds back is neither drawn from the cache
// nor fetched: each image path draws a "not loaded" line instead, and a
// blocked one (image_loading = "off") stays a link.
func TestImagesHeldBackByPolicy(t *testing.T) {
	const ext = "https://tracker.example/pixel.png"
	render := func(mode imgpkg.LoadMode) string {
		ctx := imageCtx(imgpkg.ProtoHalfBlock)
		ctx.Policy = imgpkg.NewLoadPolicy(mode, nil)
		r := Render([]Block{
			ImageBlock{URL: ext},
			SectionBlock{Text: "body", Accessory: ImageAccessory{URL: ext, AltText: "logo"}},
		}, ctx, 80)
		if len(r.Hits) != 0 || len(r.SixelRows) != 0 || len(r.Flushes) != 0 {
			t.Fatalf("%s: held image still drawn: %d hits, %d sixel rows, %d flushes",
				mode, len(r.Hits), len(r.SixelRows), len(r.Flushes))
		}
		l := RenderLegacy([]LegacyAttachment{{Title: "t", ImageURL: ext}}, ctx, 80)
		return ansi.Strip(strings.Join(append(r.Lines, l.Lines...), "\n"))
	}

	got := render(imgpkg.LoadTrusted)
	if n := strings.Count(got, NotLoadedLabel("tracker.example")); n != 2 {
		t.Errorf("trusted: %d not-loaded lines (image block + legacy image_url), want 2:\n%s", n, got)
	}
	if !strings.Contains(got, "[image: logo · not loaded]") {
		t.Errorf("trusted: accessory not marked not loaded:\n%s", got)
	}

	got = render(imgpkg.LoadOff)
	if strings.Contains(got, "not loaded") || !strings.Contains(got, "[image] "+ext) {
		t.Errorf("off: want plain links, got:\n%s", got)
	}
}

// A Slack-hosted image still draws in trusted mode.
func TestTrustedImageStillDraws(t *testing.T) {
	ctx := imageCtx(imgpkg.ProtoHalfBlock)
	ctx.Policy = imgpkg.NewLoadPolicy(imgpkg.LoadTrusted, nil)
	r := Render([]Block{ImageBlock{URL: "https://files.slack.com/files-pri/T1-F1/i.png"}}, ctx, 80)
	if len(r.Hits) != 1 {
		t.Fatalf("trusted image: %d hits, want 1", len(r.Hits))
	}
}
