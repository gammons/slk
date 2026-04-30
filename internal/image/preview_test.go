package image

import (
	"image/color"
	"strings"
	"testing"
)

func TestPreview_RenderShape(t *testing.T) {
	p := NewPreview(PreviewInput{
		Name:   "screenshot.png",
		FileID: "F1",
		Img:    makeSolid(800, 600, color.RGBA{1, 2, 3, 255}),
	})
	out := p.View(60, 30, ProtoHalfBlock)
	if out == "" {
		t.Fatal("empty view")
	}
	if !strings.Contains(out, "screenshot.png") {
		t.Error("expected filename in caption")
	}
}

func TestPreview_Closed(t *testing.T) {
	p := Preview{}
	if !p.IsClosed() {
		t.Error("zero-value Preview should be closed")
	}
	p2 := NewPreview(PreviewInput{Name: "x", Img: makeSolid(2, 2, color.RGBA{0, 0, 0, 255})})
	if p2.IsClosed() {
		t.Error("constructed Preview should not be closed")
	}
}

func TestPreview_SiblingsShownInCaptionAndHint(t *testing.T) {
	// Single image: no (i/N) badge, no h/l hint.
	solo := NewPreview(PreviewInput{
		Name:   "solo.png",
		FileID: "F1",
		Img:    makeSolid(50, 50, color.RGBA{0, 0, 0, 255}),
	})
	out := solo.View(80, 30, ProtoHalfBlock)
	if strings.Contains(out, "(1/1)") {
		t.Error("solo preview should not show sibling counter")
	}
	if strings.Contains(out, "prev") || strings.Contains(out, "next") {
		t.Error("solo preview should not show h/l cycling hint")
	}

	// Multi image: caption shows (i/N) and hint includes prev/next.
	multi := NewPreview(PreviewInput{
		Name:         "multi.png",
		FileID:       "F2",
		Img:          makeSolid(50, 50, color.RGBA{0, 0, 0, 255}),
		SiblingCount: 4,
		SiblingIndex: 2,
	})
	out = multi.View(80, 30, ProtoHalfBlock)
	if !strings.Contains(out, "(3/4)") {
		t.Errorf("expected '(3/4)' in caption, got: %s", out)
	}
	if !strings.Contains(out, "prev") || !strings.Contains(out, "next") {
		t.Errorf("expected 'prev'/'next' in hint, got: %s", out)
	}
}

func TestPreview_SwapImageUpdatesIndex(t *testing.T) {
	p := NewPreview(PreviewInput{
		Name:         "first.png",
		Img:          makeSolid(10, 10, color.RGBA{0, 0, 0, 255}),
		SiblingCount: 3,
		SiblingIndex: 0,
	})
	if p.SiblingIndex() != 0 {
		t.Errorf("initial idx: got %d want 0", p.SiblingIndex())
	}
	p.SwapImage(PreviewInput{
		Name:         "second.png",
		Img:          makeSolid(10, 10, color.RGBA{0, 0, 0, 255}),
		SiblingCount: 3,
		SiblingIndex: 1,
	})
	if p.SiblingIndex() != 1 {
		t.Errorf("after swap idx: got %d want 1", p.SiblingIndex())
	}
	if p.SiblingCount() != 3 {
		t.Errorf("count should remain 3, got %d", p.SiblingCount())
	}
}
