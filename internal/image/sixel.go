package image

import (
	"bytes"
	"image"
	"io"
	"strings"
	"sync/atomic"

	"github.com/gammons/slk/internal/debuglog"
	gosixel "github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
)

// Default cell geometry, used until SetCellPixels reports the real
// values. Matches the CellPixels fallback so behaviour is unchanged on
// terminals that don't answer TIOCGWINSZ.
const (
	defaultCellPxW = 8
	defaultCellPxH = 16
)

// cellPxW / cellPxH hold the terminal's measured cell size in pixels.
//
// Unlike kitty — which transmits c=<cols>,r=<rows> and lets the terminal
// scale the image into that cell box — sixel has no cell-aware placement:
// the terminal paints one sixel pixel per device pixel and advances the
// cursor by the image's own height. So the encoded pixel dimensions ARE
// the on-screen size, and they must be derived from the real cell metrics
// or the image won't line up with the rows the layout reserved for it.
//
// Set once at startup from CellPixels() before any rendering goroutine
// runs; atomics keep that safe against the prerender worker pool.
var (
	cellPxW atomic.Int32
	cellPxH atomic.Int32
)

// SetCellPixels records the terminal's cell size for sixel encoding.
// Non-positive values are ignored, so a terminal that reports nothing
// keeps the 8x16 default. Call once during startup.
func SetCellPixels(w, h int) {
	if w <= 0 || h <= 0 {
		debuglog.ImgRender("sixel.SetCellPixels: ignoring non-positive cell size %dx%d", w, h)
		return
	}
	cellPxW.Store(int32(w))
	cellPxH.Store(int32(h))
	debuglog.ImgRender("sixel.SetCellPixels: cell_w=%d cell_h=%d", w, h)
}

// sixelCellPixels returns the cell size to encode against.
func sixelCellPixels() (int, int) {
	w, h := int(cellPxW.Load()), int(cellPxH.Load())
	if w <= 0 || h <= 0 {
		return defaultCellPxW, defaultCellPxH
	}
	return w, h
}

// SixelRenderer encodes images as DEC sixel byte streams.
type SixelRenderer struct{}

// NewSixelRenderer returns a stateless sixel renderer.
func NewSixelRenderer() *SixelRenderer {
	return &SixelRenderer{}
}

// Render emits a Render whose Lines reserve the image's cell footprint
// with plain spaces, exactly like a text block of the same size — the
// messages pane treats them as ordinary content, reserves the rows, and
// drives the sixel byte stream from the placement pipeline (see
// internal/ui/messages and imgpkg.SixelPaint) rather than from the line
// content. Fallback carries the half-block equivalent used when the
// image is only partially visible.
func (s *SixelRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		debuglog.ImgRender("sixel.Render: target=(%d,%d) abort=zero_target", target.X, target.Y)
		return Render{Cells: target}
	}

	cw, ch := sixelCellPixels()
	pxW := target.X * cw
	pxH := target.Y * ch
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	var sx bytes.Buffer
	enc := gosixel.NewEncoder(&sx)
	if err := enc.Encode(resized); err != nil {
		debuglog.ImgRender("sixel.Render: target=(%d,%d) encode_err=%v fallback=halfblock",
			target.X, target.Y, err)
		return HalfBlockRenderer{}.Render(img, target)
	}
	sixelBytes := sx.Bytes()
	debuglog.ImgRender("sixel.Render: target=(%d,%d) cell=(%d,%d) px=(%d,%d) sixel_bytes=%d",
		target.X, target.Y, cw, ch, pxW, pxH, len(sixelBytes))

	hb := HalfBlockRenderer{}.Render(img, target)

	lines := make([]string, target.Y)
	for i := range lines {
		lines[i] = strings.Repeat(" ", target.X)
	}

	bs := sixelBytes
	return Render{
		Cells:    target,
		Lines:    lines,
		Fallback: hb.Lines,
		OnFlush: func(w io.Writer) error {
			_, err := w.Write(bs)
			return err
		},
	}
}
