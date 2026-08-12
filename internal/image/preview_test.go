package image

import (
	"bytes"
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

// TestPreview_SixelPaint_RoutedOutOfBand pins the fix for the tiled/
// duplicated preview images: for ProtoSixel, View() must NOT write the
// image bytes anywhere the caller can see (they'd get re-emitted every
// frame with no way to erase them — see internal/ui/sixelpaint.go for
// why that's a bug, not just a style choice) and must instead expose
// them via SixelPaint() for the caller's out-of-band painter.
func TestPreview_SixelPaint_RoutedOutOfBand(t *testing.T) {
	p := NewPreview(PreviewInput{
		Name: "screenshot.png",
		Img:  makeSolid(200, 100, color.RGBA{10, 20, 30, 255}),
	})
	out := p.View(60, 30, ProtoSixel)

	sp := p.SixelPaint()
	if sp == nil {
		t.Fatal("SixelPaint() is nil after a ProtoSixel render with a real image")
	}
	if len(sp.Bytes) == 0 {
		t.Error("SixelPaint().Bytes is empty")
	}
	if sp.Rows <= 0 || sp.Cols <= 0 {
		t.Errorf("SixelPaint() footprint = %dx%d, want positive", sp.Cols, sp.Rows)
	}
	if sp.Key == "" {
		t.Error("SixelPaint().Key is empty; painter needs it to detect content changes")
	}
	// The DCS payload itself must not appear in the returned string —
	// only the sentinel placeholder line does.
	if strings.Contains(out, string(sp.Bytes)) {
		t.Error("sixel bytes leaked into the View() string; must be out-of-band only")
	}
}

// TestPreview_SixelPaint_NilForOtherProtocols asserts SixelPaint() is
// only ever populated for ProtoSixel — kitty keeps its existing eager
// OnFlush-to-KittyOutput path, and halfblock has nothing to paint
// out-of-band.
func TestPreview_SixelPaint_NilForOtherProtocols(t *testing.T) {
	for _, proto := range []Protocol{ProtoHalfBlock, ProtoKitty} {
		p := NewPreview(PreviewInput{
			Name: "screenshot.png",
			Img:  makeSolid(200, 100, color.RGBA{10, 20, 30, 255}),
		})
		p.View(60, 30, proto)
		if sp := p.SixelPaint(); sp != nil {
			t.Errorf("proto=%v: SixelPaint() = %+v, want nil", proto, sp)
		}
	}
}

// TestPreview_SixelPaint_NilWhileLoadingOrClosed guards the reset at the
// top of View(): a stale placement from a previous displaying frame
// must not survive into a loading or closed frame, since the caller
// would otherwise keep painting an image that's no longer being shown.
func TestPreview_SixelPaint_NilWhileLoadingOrClosed(t *testing.T) {
	p := NewPreview(PreviewInput{
		Name: "screenshot.png",
		Img:  makeSolid(200, 100, color.RGBA{10, 20, 30, 255}),
	})
	p.View(60, 30, ProtoSixel)
	if p.SixelPaint() == nil {
		t.Fatal("precondition: expected a populated SixelPaint after a displaying sixel render")
	}

	loading := NewLoadingPreview("screenshot.png", 1, 0)
	loading.View(60, 30, ProtoSixel)
	if sp := loading.SixelPaint(); sp != nil {
		t.Errorf("loading preview: SixelPaint() = %+v, want nil", sp)
	}

	p.Close()
	p.View(60, 30, ProtoSixel)
	if sp := p.SixelPaint(); sp != nil {
		t.Errorf("closed preview: SixelPaint() = %+v, want nil", sp)
	}
}

// TestPreview_SixelPaint_Key_DifferentFileIDsDiffer pins the preview
// side of the collision regression: two same-size solid images with
// different non-empty file IDs must produce different placement keys
// regardless of encoded byte length.
func TestPreview_SixelPaint_Key_DifferentFileIDsDiffer(t *testing.T) {
	red := NewPreview(PreviewInput{Name: "red.png", FileID: "FRED-720", Img: makeSolid(200, 100, color.RGBA{200, 0, 0, 255})})
	blue := NewPreview(PreviewInput{Name: "blue.png", FileID: "FBLUE-720", Img: makeSolid(200, 100, color.RGBA{0, 0, 200, 255})})
	red.View(60, 30, ProtoSixel)
	blue.View(60, 30, ProtoSixel)
	kr, kb := red.SixelPaint().Key, blue.SixelPaint().Key
	if kr == "" || kb == "" {
		t.Fatal("empty placement keys")
	}
	if kr == kb {
		t.Fatalf("different file IDs collided: %q", kr)
	}
}

// TestPreview_SixelPaint_Key_DifferentPayloadsDiffer is the digest
// fallback: with no file ID, two same-size different-color images encode
// to the same byte length but must still differ.
func TestPreview_SixelPaint_Key_DifferentPayloadsDiffer(t *testing.T) {
	red := NewPreview(PreviewInput{Name: "red.png", Img: makeSolid(200, 100, color.RGBA{200, 0, 0, 255})})
	blue := NewPreview(PreviewInput{Name: "blue.png", Img: makeSolid(200, 100, color.RGBA{0, 0, 200, 255})})
	red.View(60, 30, ProtoSixel)
	blue.View(60, 30, ProtoSixel)
	if len(red.SixelPaint().Bytes) != len(blue.SixelPaint().Bytes) {
		t.Fatal("precondition: encoded payload lengths must match for this regression")
	}
	kr, kb := red.SixelPaint().Key, blue.SixelPaint().Key
	if kr == "" || kb == "" {
		t.Fatal("empty placement keys")
	}
	if kr == kb {
		t.Fatalf("equal-length different payloads collided: %q", kr)
	}
}

// TestPreview_ReusesEncodedRenderAcrossViews pins the memo that keeps
// the overlay usable. App.View() runs after every bubbletea message and
// never memoizes overlay frames, so without this the preview re-encodes
// its image on every keystroke and mouse motion — ~1.4s per encode at
// full-pane size, which showed up as a multi-second lag opening and
// closing the preview.
//
// The image is swapped behind the memo's back (a direct field write, no
// SwapImage) so a re-encode would be visible as different payload bytes.
func TestPreview_ReusesEncodedRenderAcrossViews(t *testing.T) {
	p := NewPreview(PreviewInput{
		Name:   "screenshot.png",
		FileID: "F01",
		Img:    makeSolid(200, 100, color.RGBA{10, 20, 30, 255}),
	})
	p.View(60, 30, ProtoSixel)
	first := append([]byte(nil), p.SixelPaint().Bytes...)

	// Same file, same target, same protocol: the memo must win even
	// though the underlying pixels now differ.
	p.img = makeSolid(200, 100, color.RGBA{200, 40, 90, 255})
	p.View(60, 30, ProtoSixel)
	if !bytes.Equal(first, p.SixelPaint().Bytes) {
		t.Error("preview re-encoded an unchanged image; the render memo is not being hit")
	}

	// A different cell target is a different encode and must miss.
	p.View(40, 20, ProtoSixel)
	if bytes.Equal(first, p.SixelPaint().Bytes) {
		t.Error("memo served a stale render for a different target size")
	}
}

// TestPreview_SwapImageInvalidatesRenderMemo covers the case the memo
// key alone cannot: the loading→loaded swap and sibling cycling can
// reuse a file ID, so SwapImage has to invalidate explicitly or the
// overlay would keep showing the previous image.
func TestPreview_SwapImageInvalidatesRenderMemo(t *testing.T) {
	p := NewPreview(PreviewInput{
		Name:   "a.png",
		FileID: "F01",
		Img:    makeSolid(200, 100, color.RGBA{10, 20, 30, 255}),
	})
	p.View(60, 30, ProtoSixel)
	first := append([]byte(nil), p.SixelPaint().Bytes...)

	p.SwapImage(PreviewInput{
		Name:   "a.png",
		FileID: "F01", // deliberately the same ID
		Img:    makeSolid(200, 100, color.RGBA{200, 40, 90, 255}),
	})
	p.View(60, 30, ProtoSixel)
	if bytes.Equal(first, p.SixelPaint().Bytes) {
		t.Error("SwapImage did not invalidate the render memo; preview would show the old image")
	}
}
