package image

import (
	"bytes"
	"image"
	"io"
	"strings"

	gosixel "github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
)

// SixelRenderer encodes images as DEC sixel byte streams.
type SixelRenderer struct{}

// NewSixelRenderer returns a stateless sixel renderer.
func NewSixelRenderer() *SixelRenderer {
	return &SixelRenderer{}
}

// Render emits a Render whose Lines contain a sentinel marker on row 0;
// the messages-pane line writer recognizes the sentinel and emits the
// sixel byte stream via OnFlush only when the image fits fully on screen.
// Otherwise, the Fallback (half-block) Lines are emitted instead.
func (s *SixelRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		return Render{Cells: target}
	}

	pxW := target.X * 8
	pxH := target.Y * 16
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	var sx bytes.Buffer
	enc := gosixel.NewEncoder(&sx)
	if err := enc.Encode(resized); err != nil {
		return HalfBlockRenderer{}.Render(img, target)
	}
	sixelBytes := sx.Bytes()

	hb := HalfBlockRenderer{}.Render(img, target)

	lines := make([]string, target.Y)
	rowSpaces := strings.Repeat(" ", target.X)
	if target.X >= 1 {
		lines[0] = string(SixelSentinel) + strings.Repeat(" ", target.X-1)
	} else {
		lines[0] = string(SixelSentinel)
	}
	for i := 1; i < target.Y; i++ {
		lines[i] = rowSpaces
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
