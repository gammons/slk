package image

import (
	"image"
	"image/color"
	"image/draw"
	"sync"
	"sync/atomic"
)

// encodeBackground is the colour transparent pixels are composited
// against before sixel/half-block encoding. Both formats ignore alpha;
// without this, a transparent custom emoji or avatar margin encodes as
// black, which reads as a black box larger than the artwork.
var (
	encodeBackgroundMu  sync.RWMutex
	encodeBackground    color.Color
	encodeBackgroundSeq atomic.Uint64
)

// SetEncodeBackground records the surface colour to use behind
// transparent pixels. Call it after applying a theme; nil leaves the
// previous colour in place. The version it bumps is part of every
// encoded-image cache key, so a theme switch cannot reuse pixels
// composited against the old background.
func SetEncodeBackground(c color.Color) {
	if c == nil {
		return
	}
	encodeBackgroundMu.Lock()
	if encodeBackground != nil && sameColor(encodeBackground, c) {
		encodeBackgroundMu.Unlock()
		return
	}
	encodeBackground = c
	encodeBackgroundMu.Unlock()
	encodeBackgroundSeq.Add(1)
}

func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// EncodeBackgroundVersion identifies the current compositing background.
func EncodeBackgroundVersion() uint64 { return encodeBackgroundSeq.Load() }

// fillEncodeBackground paints dst with the configured background before
// an alpha-preserving image is scaled over it.
func fillEncodeBackground(dst *image.RGBA) {
	encodeBackgroundMu.RLock()
	c := encodeBackground
	encodeBackgroundMu.RUnlock()
	if c == nil {
		return
	}
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
}
