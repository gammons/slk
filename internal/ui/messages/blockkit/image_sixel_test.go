package blockkit

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	imgpkg "github.com/gammons/slk/internal/image"
)

// stubImageFetcher satisfies core.ImageFetcher for the cached-path
// tests below: every key is "already fetched", so fetchOrPlaceholder
// runs its render step with no network, no disk cache, and no
// prerender entries.
type stubImageFetcher struct {
	img image.Image
}

func (f *stubImageFetcher) Fetch(_ context.Context, _ imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{Img: f.img}, nil
}

func (f *stubImageFetcher) Cached(string, image.Point) (image.Image, bool) {
	return f.img, true
}

func (f *stubImageFetcher) Prerendered(string, image.Point, imgpkg.Protocol) (imgpkg.Render, bool) {
	return imgpkg.Render{}, false
}

func (f *stubImageFetcher) ConfigurePrerender(imgpkg.Protocol) {}

func (f *stubImageFetcher) ConfigurePrerenderKitty(*imgpkg.KittyRenderer) {}

// stubImage renders as a 32x18 RGBA gradient: non-trivial content for
// the protocol encoders, small enough to keep the sixel encode fast.
func stubImage() image.Image {
	m := image.NewRGBA(image.Rect(0, 0, 32, 18))
	for y := 0; y < 18; y++ {
		for x := 0; x < 32; x++ {
			m.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 14), B: 0x40, A: 0xff})
		}
	}
	return m
}

func imageCtx(proto imgpkg.Protocol) Context {
	return Context{
		Protocol:   proto,
		Fetcher:    &stubImageFetcher{img: stubImage()},
		CellPixels: image.Pt(10, 20),
		MaxRows:    4,
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
}

// A sixel image block must come back as a SixelRows placement, never
// as a flush. The flush writes the raw DCS pixel stream to the
// terminal at whatever position the cursor happens to hold, and
// nothing tracks the painted pixels for erase - re-running it from
// every View smeared images across the screen while scrolling. The
// placement row hands the same bytes to the pane's painter, which
// positions, repaints and erases them like any other image.
func TestImageBlockSixelReturnsPlacementNotFlush(t *testing.T) {
	r := Render([]Block{ImageBlock{URL: "https://example.test/i.png"}}, imageCtx(imgpkg.ProtoSixel), 80)

	if len(r.Flushes) != 0 {
		t.Fatalf("sixel image block produced %d flushes; a sixel payload must ride the placement pipeline, not the side channel", len(r.Flushes))
	}
	if len(r.SixelRows) != 1 {
		t.Fatalf("sixel image block produced %d SixelRows entries, want 1", len(r.SixelRows))
	}
	for row, sx := range r.SixelRows {
		if row < 0 || row >= len(r.Lines) {
			t.Fatalf("SixelRows key %d outside rendered lines [0,%d)", row, len(r.Lines))
		}
		if row+sx.Height > len(r.Lines) {
			t.Fatalf("placement rows [%d,%d) exceed rendered lines %d", row, row+sx.Height, len(r.Lines))
		}
		if len(sx.Bytes) == 0 || sx.Bytes[0] != 0x1b || sx.Bytes[1] != 'P' {
			t.Fatalf("placement bytes do not start with a sixel DCS (ESC P), got %q", sx.Bytes[:min(8, len(sx.Bytes))])
		}
		if len(sx.Fallback) != sx.Height {
			t.Fatalf("fallback has %d rows, want %d (one per placement row)", len(sx.Fallback), sx.Height)
		}
	}
}

// A section's image accessory must key its placement at the rows the
// accessory actually occupies and carry the accessory's column, the
// same translation its hit gets. fetchOrPlaceholder runs before the
// side-by-side/stacked layout decision, so the merge happens after it;
// before that fix a section below another block keyed its image at row
// 0 and painted over the earlier block, and the painter placed it at
// the content column, over the section's own body text.
func TestSectionAccessorySixelMatchesHit(t *testing.T) {
	r := Render([]Block{
		HeaderBlock{Text: "a header block above"},
		SectionBlock{
			Text:      "section body text",
			Accessory: ImageAccessory{URL: "https://example.test/i.png"},
		},
	}, imageCtx(imgpkg.ProtoSixel), 80)

	var hit HitRect
	if len(r.Hits) == 1 {
		hit = r.Hits[0]
	} else {
		t.Fatalf("section accessory produced %d hits, want 1", len(r.Hits))
	}
	if len(r.SixelRows) != 1 {
		t.Fatalf("section accessory produced %d SixelRows entries, want 1", len(r.SixelRows))
	}
	for row, sx := range r.SixelRows {
		if row != hit.RowStart {
			t.Fatalf("SixelRows key %d does not match hit row %d; the placement and the click target disagree", row, hit.RowStart)
		}
		if row < 1 {
			t.Fatalf("SixelRows key %d ignores the header block rendered above the section", row)
		}
		if sx.Col != hit.ColStart {
			t.Fatalf("SixelRows Col %d does not match hit col %d; the image would paint over the section's body text", sx.Col, hit.ColStart)
		}
		// The fallback rows must stay bare halfblock: Col alone says
		// where the image sits, and the splice site grafts them over
		// the row's own body text. Baking the body text into the
		// fallback would draw it twice once the pane also keeps its
		// side of the graft.
		for i, fb := range sx.Fallback {
			if plain := ansi.Strip(fb); strings.Contains(plain, "section body text") || ansi.StringWidth(plain) != sx.Width {
				t.Fatalf("fallback row %d is not bare halfblock of width %d: %q", i, sx.Width, plain)
			}
		}
	}
}

// The kitty direction is unchanged: uploads are position-independent
// APC commands, so they keep riding the flush and never produce a
// placement row.
func TestImageBlockKittyStillFlushes(t *testing.T) {
	r := Render([]Block{ImageBlock{URL: "https://example.test/i.png"}}, imageCtx(imgpkg.ProtoKitty), 80)

	if len(r.Flushes) != 1 {
		t.Fatalf("kitty image block produced %d flushes, want 1", len(r.Flushes))
	}
	if len(r.SixelRows) != 0 {
		t.Fatalf("kitty image block produced %d SixelRows entries, want 0", len(r.SixelRows))
	}
}

// A sixel image in a legacy attachment's image_url must key its
// placement below the rows that precede the body (pretext), not at
// the body-relative row fetchOrPlaceholder stamped, and must carry
// the stripe column so the painted image lands inside the striped
// body instead of two columns left, covering the stripe. The hit gets
// the same translations; the placement and the hit must agree or
// clicks and pixels land on different rows. The fallback rows stay
// bare - the splice site grafts them over the row's own stripe, and a
// baked-in stripe would draw twice.
func TestLegacyAttachmentSixelRowIncludesPretextOffset(t *testing.T) {
	r := RenderLegacy([]LegacyAttachment{{
		Pretext:  "above the bar",
		ImageURL: "https://example.test/i.png",
	}}, imageCtx(imgpkg.ProtoSixel), 80)

	if len(r.SixelRows) != 1 {
		t.Fatalf("legacy image_url produced %d SixelRows entries, want 1", len(r.SixelRows))
	}
	var hit HitRect
	if len(r.Hits) == 1 {
		hit = r.Hits[0]
	} else {
		t.Fatalf("legacy image_url produced %d hits, want 1", len(r.Hits))
	}
	for row, sx := range r.SixelRows {
		if row != hit.RowStart {
			t.Fatalf("SixelRows key %d does not match hit row %d; the placement and the click target disagree", row, hit.RowStart)
		}
		if row < 1 {
			t.Fatalf("SixelRows key %d does not include the pretext row offset", row)
		}
		if sx.Col != hit.ColStart {
			t.Fatalf("SixelRows Col %d does not match hit col %d; the image would paint over the attachment stripe", sx.Col, hit.ColStart)
		}
		if sx.Col < stripeCol {
			t.Fatalf("SixelRows Col %d does not include the stripe prefix", sx.Col)
		}
		for i, fb := range sx.Fallback {
			if plain := ansi.Strip(fb); strings.Contains(plain, stripeGlyph) {
				t.Fatalf("fallback row %d bakes in the stripe; it must stay bare: %q", i, plain)
			}
		}
	}
}
