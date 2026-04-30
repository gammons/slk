package image

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/draw"
)

// HalfBlockRenderer encodes images via Unicode upper-half-block characters
// (▀) with 24-bit fg/bg colors. Two pixel rows per terminal cell row.
type HalfBlockRenderer struct{}

// Render satisfies the Renderer interface.
func (HalfBlockRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		return Render{Cells: target}
	}
	pxW, pxH := target.X, target.Y*2
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	lines := make([]string, target.Y)
	for cellY := 0; cellY < target.Y; cellY++ {
		var b strings.Builder
		for x := 0; x < pxW; x++ {
			top := rgbaAt(resized, x, cellY*2)
			bot := rgbaAt(resized, x, cellY*2+1)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bot.R, bot.G, bot.B)
		}
		b.WriteString("\x1b[0m")
		lines[cellY] = b.String()
	}
	return Render{
		Cells:    target,
		Lines:    lines,
		Fallback: lines, // half-block is its own fallback
	}
}

func rgbaAt(img image.Image, x, y int) color.RGBA {
	r, g, b, _ := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255}
}
