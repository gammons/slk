package image

import (
	stdimage "image"
	"image/color"
	"testing"
)

// The prerender cache is keyed by (key, cellTarget, proto): a payload
// encoded for one cell-pixel size still matches that key after the
// terminal's pixels-per-cell change, so a rebuilt pane would reuse bytes
// that paint at the wrong size. InvalidatePrerender drops the whole
// cache; workers that started before it hold the old map and their
// stores are harmlessly GC'd.
func TestFetcher_InvalidatePrerender(t *testing.T) {
	resetCellPixels(t)
	t.Cleanup(func() { resetCellPixels(t) })
	SetCellPixels(9, 20)

	cache, err := NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	f := NewFetcher(cache, nil)
	f.ConfigurePrerender(ProtoSixel)

	// Stash an entry the way maybePrerender would: a Render whose
	// OnFlush carries sixel bytes encoded for the old pixel size.
	stale := RenderImage(ProtoSixel, stubPreviewImage(), stdimage.Pt(4, 2))
	f.prerendered.Store(prerenderKey("k", stdimage.Pt(4, 2), ProtoSixel), stale)

	if got, ok := f.Prerendered("k", stdimage.Pt(4, 2), ProtoSixel); !ok || got.OnFlush == nil {
		t.Fatal("Prerendered missed an entry the cache holds")
	}

	f.InvalidatePrerender()

	if _, ok := f.Prerendered("k", stdimage.Pt(4, 2), ProtoSixel); ok {
		t.Error("Prerendered answered after InvalidatePrerender: rebuilt panes would reuse bytes encoded at the old cell size")
	}

	// The reconfigure path is unaffected: a same-protocol
	// ConfigurePrerender keeps the cache, a protocol change still
	// resets it.
	f.ConfigurePrerender(ProtoSixel)
	if _, ok := f.Prerendered("k", stdimage.Pt(4, 2), ProtoSixel); ok {
		t.Error("same-protocol reconfigure resurrected an invalidated entry")
	}
	f.ConfigurePrerender(ProtoHalfBlock)
	f.prerendered.Store(prerenderKey("k2", stdimage.Pt(4, 2), ProtoHalfBlock), RenderImage(ProtoHalfBlock, stubPreviewImage(), stdimage.Pt(4, 2)))
	f.ConfigurePrerender(ProtoSixel)
	if _, ok := f.Prerendered("k2", stdimage.Pt(4, 2), ProtoHalfBlock); ok {
		t.Error("protocol change kept the other protocol's entry")
	}
}

// stubPreviewImage is a small opaque image for the encoders.
func stubPreviewImage() stdimage.Image {
	m := stdimage.NewRGBA(stdimage.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			m.Set(x, y, color.RGBA{uint8(x * 30), uint8(y * 30), 128, 255})
		}
	}
	return m
}

// The preview's memo is keyed by file, protocol and cell target: a
// cell-size change leaves it holding bytes sized for the old cells.
// InvalidateMemo drops it; the next renderImage re-encodes.
func TestPreview_InvalidateMemo(t *testing.T) {
	resetCellPixels(t)
	t.Cleanup(func() { resetCellPixels(t) })
	SetCellPixels(9, 20)

	p := NewPreview(PreviewInput{Img: stubPreviewImage(), Name: "x.png", FileID: "F1"})
	p.open = true

	p.renderImage(ProtoSixel, stdimage.Pt(10, 5))
	if !p.memoValid {
		t.Fatal("renderImage did not populate the memo")
	}

	p.InvalidateMemo()
	if p.memoValid {
		t.Error("memo still valid after InvalidateMemo")
	}
	p.renderImage(ProtoSixel, stdimage.Pt(10, 5))
	if !p.memoValid {
		t.Fatal("renderImage did not re-populate the memo")
	}
}
