package image

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// Transparent pixels are encoded as the configured surface colour, not
// black, so an emoji's transparent margin does not read as a black box.
func TestSetEncodeBackgroundSameColorKeepsVersion(t *testing.T) {
	SetEncodeBackground(color.RGBA{R: 4, G: 5, B: 6, A: 255})
	before := EncodeBackgroundVersion()
	SetEncodeBackground(color.RGBA{R: 4, G: 5, B: 6, A: 255})
	if got := EncodeBackgroundVersion(); got != before {
		t.Errorf("version = %d, want unchanged %d for the same colour", got, before)
	}
}

func TestEncodeBackgroundFillsTransparentPixels(t *testing.T) {
	SetEncodeBackground(color.RGBA{R: 1, G: 2, B: 3, A: 255})
	t.Cleanup(func() { SetEncodeBackground(color.RGBA{A: 255}) })

	src := image.NewRGBA(image.Rect(0, 0, 4, 4)) // fully transparent
	out := HalfBlockRenderer{}.Render(src, image.Pt(2, 1))
	if len(out.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(out.Lines))
	}
	if !strings.Contains(out.Lines[0], "48;2;1;2;3") {
		t.Errorf("transparent background was not filled: %q", out.Lines[0])
	}
}
