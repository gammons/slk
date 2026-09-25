// internal/image/kitty_cellpx_test.go
//
// Kitty places by cell (c=<cols>,r=<rows>), so the cell size does not
// affect WHERE an image lands — but it decides how many pixels the
// terminal has to scale across those cells. Encoding against a cell
// smaller than the real one hands the terminal a low-resolution source
// which it then upscales. See measuredCellPixels.
package image

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	imgpng "image/png"
	"testing"
)

// decodedPayloadSize renders key at target and returns the pixel
// dimensions of the PNG the renderer would transmit.
func decodedPayloadSize(t *testing.T, k *KittyRenderer, key string, target image.Point) (int, int) {
	t.Helper()
	k.RenderKey(key, target)

	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.payloads) != 1 {
		t.Fatalf("expected exactly one cached payload, got %d", len(k.payloads))
	}
	for _, b64 := range k.payloads {
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatalf("payload is not valid base64: %v", err)
		}
		cfg, err := imgpng.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("payload is not a decodable PNG: %v", err)
		}
		return cfg.Width, cfg.Height
	}
	return 0, 0
}

func solidTestImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	return img
}

func TestKittyPayload_EncodesAtMeasuredCellSize(t *testing.T) {
	resetCellPixels(t)
	SetCellPixels(14, 34)
	t.Cleanup(func() { resetCellPixels(t) })

	k := NewKittyRenderer(NewRegistry())
	src := solidTestImage(1024, 328)
	k.SetSource("k", src)

	target := image.Pt(40, 10)
	w, h := decodedPayloadSize(t, k, "k", target)

	if wantW, wantH := 40*14, 10*34; w != wantW || h != wantH {
		t.Errorf("payload = %dx%d, want %dx%d (target cells x measured cell size)", w, h, wantW, wantH)
	}
}

// With no measurement the 8x16 fallback keeps the previous behaviour,
// so terminals that answer nothing render exactly as before.
func TestKittyPayload_FallsBackTo8x16(t *testing.T) {
	resetCellPixels(t)

	k := NewKittyRenderer(NewRegistry())
	src := solidTestImage(1024, 328)
	k.SetSource("k", src)

	target := image.Pt(40, 10)
	w, h := decodedPayloadSize(t, k, "k", target)

	if wantW, wantH := 40*8, 10*16; w != wantW || h != wantH {
		t.Errorf("payload = %dx%d, want the %dx%d fallback", w, h, wantW, wantH)
	}
}

// The regression this fixes: on a HiDPI cell the old hardcode supplied
// materially fewer pixels than the box could display.
func TestKittyPayload_HiDPIBeatsTheOldHardcode(t *testing.T) {
	target := image.Pt(40, 10)
	src := solidTestImage(1024, 328)

	resetCellPixels(t)
	k1 := NewKittyRenderer(NewRegistry())
	k1.SetSource("k", src)
	oldW, oldH := decodedPayloadSize(t, k1, "k", target)

	SetCellPixels(14, 34)
	t.Cleanup(func() { resetCellPixels(t) })
	k2 := NewKittyRenderer(NewRegistry())
	k2.SetSource("k", src)
	newW, newH := decodedPayloadSize(t, k2, "k", target)

	if newW <= oldW || newH <= oldH {
		t.Errorf("hidpi payload %dx%d is not sharper than the old %dx%d", newW, newH, oldW, oldH)
	}
}
