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
