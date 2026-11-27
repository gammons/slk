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
// BOTH encoders need them, for different reasons.
//
// Sixel has no cell-aware placement: the terminal paints one sixel pixel
// per device pixel and advances the cursor by the image's own height, so
// the encoded pixel dimensions ARE the on-screen size and must come from
// the real cell metrics or the image won't line up with the rows the
// layout reserved for it.
//
// Kitty does place by cell — c=<cols>,r=<rows> — so its LAYOUT is correct
// whatever we encode at. Its resolution is not. The terminal scales the
// transmitted image across the cell box, so encoding against a cell that
// is smaller than the real one hands it fewer pixels than the box can
// show and it upscales the difference. On a HiDPI terminal reporting a
// 14x34 cell, the old hardcoded 8x16 supplied 57% of the horizontal and
// 47% of the vertical resolution available — which reads as a soft,
// low-resolution image, most visibly on screenshots of text.
//
// Set once at startup from CellPixels() before any rendering goroutine
// runs; atomics keep that safe against the prerender worker pool.
var (
	cellPxW atomic.Int32
	cellPxH atomic.Int32
)

// SetCellPixels records the terminal's cell size for image encoding.
// Non-positive values are ignored, so a terminal that reports nothing
// keeps the 8x16 default. Call once during startup.
func SetCellPixels(w, h int) {
	if w <= 0 || h <= 0 {
		debuglog.ImgRender("image.SetCellPixels: ignoring non-positive cell size %dx%d", w, h)
		return
	}
	cellPxW.Store(int32(w))
	cellPxH.Store(int32(h))
	debuglog.ImgRender("image.SetCellPixels: cell_w=%d cell_h=%d", w, h)
}

// measuredCellPixels returns the cell size to encode against.
func measuredCellPixels() (int, int) {
	w, h := int(cellPxW.Load()), int(cellPxH.Load())
	if w <= 0 || h <= 0 {
		return defaultCellPxW, defaultCellPxH
	}
	return w, h
}

// MeasuredCellPixels returns the effective cell size encoders are
// working against: SetCellPixels' values, or the 8x16 default when the
// terminal never reported one. Hosts re-measuring the terminal compare
// against this, not against what they last passed to SetCellPixels.
func MeasuredCellPixels() (int, int) { return measuredCellPixels() }

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

	cw, ch := measuredCellPixels()
	pxW := target.X * cw
	pxH := target.Y * ch
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)
	// Bilinear scaling blends edge pixels with the empty canvas they
	// land on, so a transparent image's border arrives half-drawn at a
	// dark blended color. Threshold the alpha back to fully opaque or
	// fully transparent: go-sixel skips palette entries with zero
	// alpha, which the terminal leaves unpainted.
	thresholdAlpha(resized)

	var sx bytes.Buffer
	enc := gosixel.NewEncoder(&sx)
	if err := enc.Encode(resized); err != nil {
		debuglog.ImgRender("sixel.Render: target=(%d,%d) encode_err=%v fallback=halfblock",
			target.X, target.Y, err)
		return HalfBlockRenderer{}.Render(img, target)
	}
	// go-sixel hardcodes the DECSIXEL introducer P2=0, which tells the
	// terminal to paint every pixel the data leaves unset with its
	// background color — a black box behind any transparent image. Patch
	// our own output to P2=1 (transparent background): unset pixels keep
	// the cells' text background, so an emoji sits on the pill tint, the
	// selection tint, or the pane background. The raster attributes that
	// follow (SixelPixelSize reads them) are unaffected.
	payload := bytes.Replace(sx.Bytes(), []byte("\x1bP0;0;8q"), []byte("\x1bP0;1;8q"), 1)
	sixelBytes := payload
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

// thresholdAlpha clamps every pixel to fully opaque or fully
// transparent at the halfway point, in place. Scaling a transparent
// image blends its edge with the canvas, producing half-covered pixels
// that quantize to dark colors on the image's border; hard edges keep
// those pixels out of the paint entirely.
//
// The kept pixels are un-premultiplied first: image.RGBA stores colors
// multiplied by alpha, so a half-covered edge pixel holds a dimmed
// color, and forcing its alpha to 255 alone would keep the dim as the
// very dark fringe this exists to remove.
func thresholdAlpha(img *image.RGBA) {
	b := img.Pix
	for i := 0; i < len(b); i += 4 {
		a := b[i+3]
		switch {
		case a < 0x80:
			b[i], b[i+1], b[i+2], b[i+3] = 0, 0, 0, 0
		case a < 0xFF:
			// c ≤ a holds for premultiplied colors, so c*0xFF/a ≤ 0xFF
			// and needs no clamp.
			b[i] = uint8(uint16(b[i]) * 0xFF / uint16(a))
			b[i+1] = uint8(uint16(b[i+1]) * 0xFF / uint16(a))
			b[i+2] = uint8(uint16(b[i+2]) * 0xFF / uint16(a))
			b[i+3] = 0xFF
		}
	}
}

// SixelPixelSize reports the pixel dimensions a sixel payload claims in
// its raster attributes (DECGRA, " Pan;Pad;Ph;Pv), which go-sixel emits
// right after the introducer. ok is false when the payload carries no
// RA with an explicit size. Sixel paints one payload pixel per device
// pixel, so these are the on-screen dimensions regardless of what the
// cell layout reserved -- which is what lets an erase cover a payload
// painted before a cell-size change (see paintedExtent).
func SixelPixelSize(payload []byte) (w, h int, ok bool) {
	// The first 'q' in the stream is the introducer's terminator; sixel
	// data bytes come after it and cannot precede it.
	q := bytes.IndexByte(payload, 'q')
	if q < 0 {
		return 0, 0, false
	}
	rest := payload[q+1:]
	if len(rest) == 0 || rest[0] != '"' {
		return 0, 0, false
	}
	var params [4]int
	n := 0
	cur := 0
	for _, c := range rest[1:] {
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
		case c == ';':
			if n < len(params) {
				params[n] = cur
			}
			n++
			cur = 0
		default:
			if n < len(params) {
				params[n] = cur
			}
			n++
			if n < 4 {
				return 0, 0, false
			}
			return params[2], params[3], params[2] > 0 && params[3] > 0
		}
	}
	// Ran off the end of the payload mid-RA: flush the final param.
	if n < len(params) {
		params[n] = cur
	}
	n++
	if n < 4 {
		return 0, 0, false
	}
	return params[2], params[3], params[2] > 0 && params[3] > 0
}
