package image

import (
	"bytes"
	"image"
	imgcolor "image/color"
	"strings"
	"testing"
)

func TestSixel_RenderShape(t *testing.T) {
	src := makeSolid(40, 20, imgcolor.RGBA{200, 100, 50, 255})
	r := NewSixelRenderer()
	out := r.Render(src, image.Pt(20, 5))

	if out.Cells != image.Pt(20, 5) {
		t.Errorf("Cells: got %v, want (20,5)", out.Cells)
	}
	if len(out.Lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(out.Lines))
	}
	// The placeholder rows are plain spaces reserving the footprint; the
	// messages pane drives emission from the placement pipeline, not from
	// any marker in the line content.
	for i, l := range out.Lines {
		if w := strings.Count(l, " "); w != 20 {
			t.Errorf("row %d width = %d, want 20 cells of space", i, w)
		}
	}
	if len(out.Fallback) != 5 {
		t.Errorf("Fallback len got %d, want 5", len(out.Fallback))
	}
}

func TestSixel_OnFlushWritesSixelBytes(t *testing.T) {
	src := makeSolid(20, 20, imgcolor.RGBA{0, 255, 0, 255})
	r := NewSixelRenderer()
	out := r.Render(src, image.Pt(10, 5))

	if out.OnFlush == nil {
		t.Fatal("expected OnFlush set")
	}
	var buf bytes.Buffer
	if err := out.OnFlush(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.HasPrefix(s, "\x1bP") {
		t.Errorf("expected sixel DCS prefix \\eP, got %q", s[:minInt(20, len(s))])
	}
	if !strings.HasSuffix(s, "\x1b\\") {
		t.Errorf("expected ST suffix")
	}
}
