package emoji

import (
	"bytes"
	goimage "image"
	"sync/atomic"

	imgpkg "github.com/gammons/slk/internal/image"
)

// Inline sixel emoji: the kitty path puts the image's position in the
// cells themselves (unicode placeholders), which sixel cannot do. The
// text instead carries a marker rune per image; the message renderer
// replaces each marker with spaces and records a sixel placement for
// the painter.
//
// A marker encodes the image's index, not just "an emoji", so a marker
// split by a wrap or a literal rune in the message cannot consume
// another image's placement. With two-cell images the marker is a lead
// rune followed by an index rune; with one-cell images it is the index
// rune alone. All are Private Use Area runes of display width one.
const (
	inlineMarkerLead  rune = 0xE000
	inlineMarkerBase  rune = 0xE100
	inlineMarkerLimit      = 0x1800 // 0xE100..0xF8FF: 6144 images per message
)

// InlineMarkerLead is the rune that begins a two-cell image marker.
func InlineMarkerLead() rune { return inlineMarkerLead }

// InlineMarkerIndex reports the image index a rune encodes, false for
// any rune outside the marker range. Known limit: a message that
// literally contains U+E000 followed by one of these runes still reads
// as a marker. That is private-use text nobody types; escaping it would
// cost more than the collision.
func InlineMarkerIndex(r rune) (int, bool) {
	i := int(r - inlineMarkerBase)
	if r < inlineMarkerBase || i >= inlineMarkerLimit {
		return 0, false
	}
	return i, true
}

// inlineMarker is the text a sink writes for image idx: cells wide.
func inlineMarker(idx, cells int) string {
	index := string(inlineMarkerBase + rune(idx))
	if cells == 1 {
		return index
	}
	return string(inlineMarkerLead) + index
}

var (
	sixelInlineOn    atomic.Bool
	sixelInlineCells atomic.Int32
)

func init() { sixelInlineCells.Store(2) }

// SetSixelInlineMode records whether inline emoji should be drawn as
// sixel images. Called once at startup, after the terminal protocol is
// known; cells is 1 or 2 and clamps to 2.
func SetSixelInlineMode(active bool, cells int) {
	if cells != 1 {
		cells = 2
	}
	sixelInlineOn.Store(active)
	sixelInlineCells.Store(int32(cells))
}

// SixelInlineActive reports whether inline emoji images are enabled.
func SixelInlineActive() bool { return sixelInlineOn.Load() }

// SixelInlineCells is the cell width of an inline emoji image (1 or 2).
func SixelInlineCells() int { return int(sixelInlineCells.Load()) }

// InlineImage is one emoji image an InlineSixel sink placed, in render
// order. Bytes are empty until the image has been fetched and encoded;
// the message re-renders on EmojiImageReadyMsg and fills them in.
type InlineImage struct {
	URL      string
	Key      string
	Bytes    []byte
	Fallback []string
	Width    int
}

// InlineSixel places emoji as sixel images inside rendered text. It
// belongs to one render pass: Place appends to the image list, and the
// caller resolves the markers in the same pass.
type InlineSixel struct {
	ctx    PlaceContext
	cells  int
	images []InlineImage
}

// NewInlineSixel returns a sink when sixel inline emoji is enabled and
// the terminal protocol is sixel. It returns nil, leaving emoji to the
// glyph/text path, for any other protocol or without a fetcher.
func NewInlineSixel(ctx PlaceContext, cells int, proto imgpkg.Protocol) *InlineSixel {
	if !SixelInlineActive() || proto != imgpkg.ProtoSixel || ctx.Fetcher == nil {
		return nil
	}
	if cells != 1 {
		cells = 2
	}
	return &InlineSixel{ctx: ctx, cells: cells}
}

// Marker is the text this sink writes for the image most recently
// placed, for tests and callers that need the exact marker text.
func (s *InlineSixel) Marker() string {
	if len(s.images) == 0 {
		return ""
	}
	return inlineMarker(len(s.images)-1, s.cells)
}

// Cells is the image's width in terminal cells.
func (s *InlineSixel) Cells() int { return s.cells }

// Place records the image at url and returns its marker. ok is false
// when the URL is empty, has failed to fetch before, or the render has
// more images than a marker can index; the caller then writes the
// emoji's plain text.
//
// A cold image still returns its marker: the cells are reserved and the
// fetch's EmojiImageReadyMsg re-renders the message with the payload.
func (s *InlineSixel) Place(url string) (string, bool) {
	if url == "" || isEmojiURLFailed(url) || len(s.images) >= inlineMarkerLimit {
		return "", false
	}
	key := EmojiCacheKey(url)
	target := goimage.Pt(s.cells, 1)
	img := InlineImage{URL: url, Key: key, Width: s.cells}
	if r, ok := s.ctx.Fetcher.Prerendered(key, target, imgpkg.ProtoSixel); ok {
		var bb bytes.Buffer
		if r.OnFlush != nil && r.OnFlush(&bb) == nil {
			img.Bytes = bb.Bytes()
		}
		img.Fallback = r.Fallback
	}
	if len(img.Bytes) == 0 {
		s.spawnFetch(key, url, target)
	}
	idx := len(s.images)
	s.images = append(s.images, img)
	return inlineMarker(idx, s.cells), true
}

// spawnFetch is spawnEmojiFetch with the sink's fetch context.
func (s *InlineSixel) spawnFetch(key, url string, target goimage.Point) {
	spawnEmojiFetch(s.ctx, key, url, target)
}

// Images returns the placed images in render order, one per marker the
// sink emitted.
func (s *InlineSixel) Images() []InlineImage { return s.images }
