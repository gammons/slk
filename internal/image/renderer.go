// Package image renders bitmap images for terminal display via three
// protocols: kitty graphics (preferred), sixel, and unicode half-block.
// The package also owns image fetching, decoding, downscaling, and the
// on-disk cache shared with the avatar subsystem.
package image

import (
	"image"
	"io"
)

// Protocol enumerates the rendering protocols this package can emit.
type Protocol int

const (
	// ProtoOff disables image rendering; consumers should fall back to text.
	ProtoOff Protocol = iota
	// ProtoHalfBlock uses the ▀ upper-half-block character with 24-bit color.
	ProtoHalfBlock
	// ProtoSixel uses the DEC sixel protocol.
	ProtoSixel
	// ProtoKitty uses the kitty graphics protocol with unicode placeholders.
	ProtoKitty
)

// String returns a human-readable protocol name (used in logs and config).
func (p Protocol) String() string {
	switch p {
	case ProtoOff:
		return "off"
	case ProtoHalfBlock:
		return "halfblock"
	case ProtoSixel:
		return "sixel"
	case ProtoKitty:
		return "kitty"
	default:
		return "unknown"
	}
}

// Render is one renderer's output for a single image at a single target size.
// Lines and Fallback are always exactly Cells.Y rows long and each row is
// Cells.X cells wide (per lipgloss.Width). The messages-pane render cache
// treats Lines like any other text content.
type Render struct {
	// Cells is the (cols, rows) footprint in terminal cells.
	Cells image.Point

	// Lines is the per-row text/escape content baked into the message cache.
	Lines []string

	// Fallback is the half-block equivalent used when partial visibility
	// prevents the primary protocol from emitting (sixel only). For
	// half-block and kitty renders, Fallback equals Lines.
	Fallback []string

	// OnFlush is an optional pre-frame side effect (kitty image upload).
	// Called at most once per frame across all rendered images. Idempotent.
	OnFlush func(io.Writer) error

	// ID is a protocol-specific image ID. Zero when the protocol has no
	// notion of a stable image identifier.
	ID uint32
}

// Renderer encodes an in-memory image into a Render at a target cell footprint.
type Renderer interface {
	Render(img image.Image, target image.Point) Render
}

// RenderImage encodes img at target cells using the given protocol's renderer.
// Returns a zero Render if proto == ProtoOff.
func RenderImage(proto Protocol, img image.Image, target image.Point) Render {
	switch proto {
	case ProtoOff:
		return Render{Cells: target}
	case ProtoHalfBlock:
		return HalfBlockRenderer{}.Render(img, target)
	case ProtoSixel:
		return sixelRenderer.Render(img, target)
	case ProtoKitty:
		return kittyRenderer.Render(img, target)
	}
	return Render{}
}

// Singleton renderers — concrete instances appear in kitty.go / sixel.go.
// Until those exist, fall back to half-block so this file builds in isolation.
var (
	sixelRenderer Renderer = HalfBlockRenderer{}
	kittyRenderer Renderer = HalfBlockRenderer{}
)
