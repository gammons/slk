package image

import (
	"bytes"
	"image"
	imgcolor "image/color"
	"strings"
	"testing"
)

func TestKitty_UploadEscapeFormat(t *testing.T) {
	src := makeSolid(64, 64, imgcolor.RGBA{1, 2, 3, 255})
	r := NewKittyRenderer(NewRegistry())
	out := r.Render(src, image.Pt(10, 5))

	if out.OnFlush == nil {
		t.Fatal("expected OnFlush set on first render")
	}
	if out.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	var buf bytes.Buffer
	if err := out.OnFlush(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.HasPrefix(s, "\x1b_G") {
		t.Errorf("expected \\e_G prefix, got %q", s[:minInt(20, len(s))])
	}
	if !strings.HasSuffix(s, "\x1b\\") {
		t.Errorf("expected \\e\\ suffix")
	}
	if !strings.Contains(s, "a=T") {
		t.Error("missing a=T (transmit-and-display, required for unicode-placeholder virtual placement)")
	}
	if !strings.Contains(s, "c=10") || !strings.Contains(s, "r=5") {
		t.Error("missing c=<cols>,r=<rows> for virtual placement footprint")
	}
	if !strings.Contains(s, "f=100") {
		t.Error("missing f=100 (PNG)")
	}
	if !strings.Contains(s, "U=1") {
		t.Error("missing U=1 (unicode placeholder)")
	}
}

func TestKitty_SecondRenderSameImageNoFlush(t *testing.T) {
	reg := NewRegistry()
	r := NewKittyRenderer(reg)
	src := makeSolid(8, 8, imgcolor.RGBA{1, 2, 3, 255})

	r.SetSource("test-key", src)
	out1 := r.RenderKey("test-key", image.Pt(4, 2))
	if out1.OnFlush == nil {
		t.Fatal("first render should flush")
	}

	out2 := r.RenderKey("test-key", image.Pt(4, 2))
	if out2.OnFlush != nil {
		t.Error("second render of same (key, size) should not flush")
	}
	if out2.ID != out1.ID {
		t.Error("ID should be stable across renders of same (key, size)")
	}
}

func TestKitty_PlaceholderRows(t *testing.T) {
	src := makeSolid(20, 20, imgcolor.RGBA{255, 255, 255, 255})
	r := NewKittyRenderer(NewRegistry())
	out := r.Render(src, image.Pt(10, 5))

	if len(out.Lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(out.Lines))
	}
	for i, line := range out.Lines {
		if !strings.Contains(line, "\U0010EEEE") {
			t.Errorf("line %d missing placeholder rune: %q", i, line[:minInt(30, len(line))])
		}
		if !strings.Contains(line, "\x1b[38;2;") {
			t.Errorf("line %d missing 24-bit SGR: %q", i, line[:minInt(30, len(line))])
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
