package image

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// makeSolid returns a w×h image filled with c.
func makeSolid(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestHalfBlock_OutputShape(t *testing.T) {
	hb := HalfBlockRenderer{}
	src := makeSolid(8, 16, color.RGBA{255, 0, 0, 255})
	r := hb.Render(src, image.Pt(4, 2))

	if r.Cells != image.Pt(4, 2) {
		t.Errorf("Cells: got %v, want (4,2)", r.Cells)
	}
	if len(r.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(r.Lines))
	}
	for i, line := range r.Lines {
		if !strings.Contains(line, "▀") {
			t.Errorf("line %d missing ▀: %q", i, line)
		}
		if !strings.Contains(line, "\x1b[38;2;255;0;0m") {
			t.Errorf("line %d missing red fg ANSI: %q", i, line)
		}
	}
	if r.OnFlush != nil {
		t.Error("halfblock should not have OnFlush")
	}
	if r.ID != 0 {
		t.Error("halfblock should have ID=0")
	}
	// Fallback equals Lines for halfblock.
	if len(r.Fallback) != len(r.Lines) {
		t.Error("halfblock Fallback should equal Lines")
	}
}

func TestHalfBlock_TopBottomColors(t *testing.T) {
	// Top half red, bottom half blue. After half-block rendering, fg=red, bg=blue.
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	for y := 2; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	hb := HalfBlockRenderer{}
	r := hb.Render(src, image.Pt(4, 2))

	// Row 0 of cells = pixel rows 0-1 (both red). Row 1 of cells = pixel rows 2-3 (both blue).
	if !strings.Contains(r.Lines[0], "\x1b[38;2;255;0;0m") {
		t.Error("row 0 fg should be red")
	}
	if !strings.Contains(r.Lines[1], "\x1b[38;2;0;0;255m") {
		t.Error("row 1 fg should be blue")
	}
}
