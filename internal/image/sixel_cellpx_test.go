package image

import (
	"image"
	"strings"
	"testing"
)

// Sixel has no cell-aware placement: the encoded pixel size is the
// on-screen size. An image the layout gave N rows must therefore be
// encoded N*cellHeight pixels tall, or it won't cover those rows and
// the cursor advance won't match the reserved space.

func resetCellPixels(t *testing.T) {
	t.Helper()
	cellPxW.Store(0)
	cellPxH.Store(0)
}

func TestSixelCellPixels_DefaultsTo8x16(t *testing.T) {
	resetCellPixels(t)
	w, h := sixelCellPixels()
	if w != 8 || h != 16 {
		t.Errorf("sixelCellPixels() = %dx%d, want 8x16 default", w, h)
	}
}

func TestSetCellPixels_UsesMeasuredMetrics(t *testing.T) {
	resetCellPixels(t)
	SetCellPixels(14, 33)
	t.Cleanup(func() { resetCellPixels(t) })

	if w, h := sixelCellPixels(); w != 14 || h != 33 {
		t.Errorf("sixelCellPixels() = %dx%d, want 14x33", w, h)
	}
}

func TestSetCellPixels_IgnoresNonPositive(t *testing.T) {
	resetCellPixels(t)
	SetCellPixels(0, 33)
	SetCellPixels(14, -1)
	if w, h := sixelCellPixels(); w != 8 || h != 16 {
		t.Errorf("sixelCellPixels() = %dx%d, want the 8x16 default to survive bad input", w, h)
	}
}

// The encoded raster must scale with the reported cell height: a taller
// cell means more pixels for the same row count.
func TestSixelRender_ScalesWithCellHeight(t *testing.T) {
	resetCellPixels(t)
	t.Cleanup(func() { resetCellPixels(t) })

	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	target := image.Pt(10, 5)

	SetCellPixels(8, 16)
	small := renderSixelBytes(t, src, target)

	SetCellPixels(16, 32)
	large := renderSixelBytes(t, src, target)

	if len(large) <= len(small) {
		t.Errorf("doubling the cell size did not grow the sixel raster: small=%d large=%d bytes",
			len(small), len(large))
	}
}

func renderSixelBytes(t *testing.T, img image.Image, target image.Point) []byte {
	t.Helper()
	out := (&SixelRenderer{}).Render(img, target)
	if out.OnFlush == nil {
		t.Fatal("Render returned no OnFlush; expected sixel bytes")
	}
	var sb strings.Builder
	if err := out.OnFlush(&sb); err != nil {
		t.Fatalf("OnFlush: %v", err)
	}
	return []byte(sb.String())
}
