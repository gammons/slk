package image

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

// A transparent image must encode with the transparent-background
// introducer (P2=1), so the terminal keeps unset pixels on the text
// background instead of painting its own behind the image.
func TestSixelRender_TransparentHeader(t *testing.T) {
	SetCellPixels(10, 20)
	src := image.NewNRGBA(image.Rect(0, 0, 8, 8)) // all transparent
	r := NewSixelRenderer().Render(src, image.Pt(2, 1))
	var out bytes.Buffer
	if r.OnFlush == nil {
		t.Fatal("sixel render has no payload")
	}
	if err := r.OnFlush(&out); err != nil {
		t.Fatal(err)
	}
	payload := out.String()
	if !bytes.HasPrefix([]byte(payload), []byte("\x1bP0;1;8q")) {
		t.Fatalf("payload header = %q, want the P2=1 introducer", payload[:min(12, len(payload))])
	}
}

// Bilinear-blended edge pixels must not survive as half-drawn dark
// colors: thresholdAlpha clamps alpha to 0 or 255, and un-premultiplies
// the kept pixels — image.RGBA stores colors scaled by alpha, so
// raising the alpha alone would leave a dimmed, dark fringe.
func TestThresholdAlpha(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 1))
	// All values are premultiplied, as image.RGBA stores them.
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 10})                  // nearly transparent: dropped
	img.SetRGBA(1, 0, color.RGBA{R: 128, G: 128, B: 128, A: 128}) // white at 50%: opaque white
	img.SetRGBA(2, 0, color.RGBA{B: 200, A: 200})                 // blue premultiplied at 200: opaque blue
	img.SetRGBA(3, 0, color.RGBA{R: 40, G: 30, B: 20, A: 255})    // opaque: untouched
	thresholdAlpha(img)
	cases := []struct {
		x    int
		want color.RGBA
	}{
		{0, color.RGBA{A: 0}},
		{1, color.RGBA{R: 255, G: 255, B: 255, A: 255}},
		{2, color.RGBA{B: 255, A: 255}},
		{3, color.RGBA{R: 40, G: 30, B: 20, A: 255}}}
	for _, c := range cases {
		if got := img.RGBAAt(c.x, 0); got != c.want {
			t.Errorf("pixel at x=%d = %+v, want %+v", c.x, got, c.want)
		}
	}
}

// An opaque image encodes unchanged apart from the header: no alpha
// work can corrupt it.
func TestSixelRender_OpaqueStillEncodes(t *testing.T) {
	SetCellPixels(10, 20)
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.Set(x, y, color.RGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	r := NewSixelRenderer().Render(src, image.Pt(2, 1))
	var out bytes.Buffer
	_ = r.OnFlush(&out)
	payload := out.String()
	if !bytes.Contains([]byte(payload), []byte("#")) {
		t.Fatal("opaque image encoded no color bands")
	}
}
