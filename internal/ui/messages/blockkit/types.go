// Package blockkit parses and renders Slack Block Kit blocks and the
// legacy `attachments` field. The package is intentionally
// self-contained: it depends only on slack-go (for input types),
// lipgloss (for styling), charmbracelet/x/ansi (for width-aware line
// surgery in SpliceSixelFallbacks), the project's styles package (for
// theme colors), and the project's image package (for block image
// rendering). It does NOT depend on the rest of internal/ui/messages
// to keep import cycles impossible.
//
// The package's two entry points are Render (for blocks) and
// RenderLegacy (for legacy attachments). Both return RenderResult,
// which has the same tuple shape as the existing renderAttachmentBlock
// in internal/ui/messages/model.go so callers can aggregate results
// across multiple render passes uniformly.
package blockkit

import (
	"image"
	"io"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	imgpkg "github.com/gammons/slk/internal/image"
)

// The Block Kit data types live in internal/core/blocks so the engine
// can build them without importing the TUI.
type (
	Block            = blocks.Block
	SectionBlock     = blocks.SectionBlock
	HeaderBlock      = blocks.HeaderBlock
	ContextBlock     = blocks.ContextBlock
	ContextElement   = blocks.ContextElement
	DividerBlock     = blocks.DividerBlock
	ImageBlock       = blocks.ImageBlock
	ActionsBlock     = blocks.ActionsBlock
	UnknownBlock     = blocks.UnknownBlock
	RichTextBlock    = blocks.RichTextBlock
	AccessoryElement = blocks.AccessoryElement
	ImageAccessory   = blocks.ImageAccessory
	LabelAccessory   = blocks.LabelAccessory
	ActionElement    = blocks.ActionElement
	LegacyAttachment = blocks.LegacyAttachment
	LegacyField      = blocks.LegacyField
	LegacyAction     = blocks.LegacyAction
	ActionConfirm    = blocks.ActionConfirm
)

func blockType(b Block) string { return blocks.TypeName(b) }

// RenderResult is the output of Render and RenderLegacy. The tuple
// shape mirrors the existing renderAttachmentBlock in
// internal/ui/messages/model.go so callers can aggregate results
// across passes uniformly.
type RenderResult struct {
	Lines       []string                // ANSI-styled, ready to join with "\n"
	Flushes     []func(io.Writer) error // kitty image upload callbacks
	SixelRows   map[int]SixelEntry      // sixel sentinel rows keyed by row index into Lines (same coord system as HitRect.RowStart)
	Height      int                     // == len(Lines); cached for caller's row math
	Hits        []HitRect               // clickable image footprints
	Interactive bool                    // any interactive element rendered
}

// SixelEntry is one sixel image's pre-encoded bytes plus its
// halfblock-equivalent fallback for partial-visibility frames.
// Mirrors internal/ui/messages.sixelEntry exactly so the integration
// site can copy the contents without conversion.
type SixelEntry struct {
	// Key is the fetcher's cache key for the image (the "BK-" URL
	// hash). It becomes the placement's content identity, so the
	// painter detects a slot swap without hashing the payload - which
	// it would otherwise do every frame, and block payloads are often
	// hundreds of KB.
	Key      string
	Bytes    []byte
	Fallback []string
	Height   int
	Width    int
	// Col is the display column where the image starts, relative to
	// the content column of the rendered lines (0 for full-width
	// images). Section accessories sit right of the body text and
	// legacy attachments sit right of the color stripe; the painter
	// adds this to the entry's content column, exactly as the hit
	// rects already carry their own column offsets.
	Col int
}

// SpliceSixelFallbacks grafts, in place, each SixelRows entry's
// halfblock fallback rows over the blank rows the render reserved for
// its images. For hosts that cannot paint sixel placements (the
// thread pane): without this, a cached image's reserved rows render
// as blank space. Keys are row indexes into lines, the same
// coordinate system as SixelRows itself.
//
// The fallback rows are bare halfblock; the entry's Col says where
// the image sits. Each row is grafted at Col over the line's own
// content - keeping what stands left of the image (a legacy
// attachment's stripe, a section accessory's body text) and the line's
// tail beyond it - so the spliced row keeps the surroundings the
// reserved row carried, instead of replacing them.
func SpliceSixelFallbacks(lines []string, rows map[int]SixelEntry) {
	for k, v := range rows {
		for i := 0; i < v.Height && i < len(v.Fallback); i++ {
			if k+i < 0 || k+i >= len(lines) {
				continue
			}
			end := v.Col + ansi.StringWidth(v.Fallback[i])
			lines[k+i] = ansi.Cut(lines[k+i], 0, v.Col) + v.Fallback[i] +
				ansi.Cut(lines[k+i], end, ansi.StringWidth(lines[k+i]))
		}
	}
}

// HitRect is one clickable image footprint expressed in (row, col)
// coordinates RELATIVE TO RenderResult.Lines. The integration site
// translates these to absolute entry coordinates by adding the
// row offset and column-base before storing them as the entry's
// entrylist.Hit rects.
type HitRect struct {
	RowStart int    // inclusive
	RowEnd   int    // exclusive
	ColStart int    // inclusive
	ColEnd   int    // exclusive
	URL      string // for use as a stable cache key + click action
}

// Context bundles the dependencies the renderer needs from the host
// application. It is passed by value into Render and RenderLegacy.
// All fields are optional; Render must degrade gracefully when any
// are zero (e.g. no image rendering when Fetcher is nil).
type Context struct {
	Protocol    imgpkg.Protocol
	Fetcher     core.ImageFetcher
	KittyRender *imgpkg.KittyRenderer
	CellPixels  image.Point
	MaxRows     int // for full-size image blocks
	MaxCols     int
	UserNames   map[string]string // for resolving <@U…> mentions in mrkdwn
	// SendMsg is a tea.Cmd-style callback typed as func(any) so this
	// package does not need to import bubbletea. Used by image-block
	// prefetchers to signal completion. May be nil; when nil, image
	// blocks render as a placeholder indefinitely until the next
	// render-cache invalidation.
	SendMsg func(any)
	// MessageTS / Channel are echoed back on async image-ready
	// messages so the host can target the right entry for cache
	// invalidation.
	MessageTS string
	Channel   string
	// RenderText converts Slack-flavored mrkdwn (with mentions/links/
	// emoji shortcodes) to ANSI-styled text. The host wires this to
	// internal/ui/messages.RenderSlackMarkdown. May be nil; when nil,
	// raw text passes through unchanged.
	RenderText func(s string, userNames map[string]string) string

	// WrapText word-wraps an ANSI-styled string to the given display
	// width. The host wires this to internal/ui/messages.WordWrap.
	// When nil, text passes through unchanged.
	WrapText func(s string, width int) string

	// Perf, when non-nil, accumulates per-sub-lane timing inside
	// RenderLegacy (and only there for now). Allocated by the host
	// under SLK_DEBUG and read back after the call so the parent
	// buildCache breakdown can attribute the legacy lane's cost
	// across text rendering, image fetch/render, and the rest
	// (stripe prefix, width measurement, truncation). Nil disables
	// every call site's timing.
	Perf *LegacyPerf
}

// LegacyPerf accumulates per-sub-lane wall-clock for the legacy
// attachment renderer. All fields are zero unless Context.Perf was
// non-nil at the time of RenderLegacy. attachmentCount is the
// number of LegacyAttachment values processed across the call.
//
// Fields are unexported so external packages cannot mutate them
// mid-render; accessor methods expose read-only values for the
// host's [perf] log aggregation.
//
// The image sub-fields (cachedCheck / kittyRender / renderImage /
// placeholder) further attribute the imageTotal across the four
// branches inside fetchOrPlaceholder. ctx.Perf is consulted from
// fetchOrPlaceholder unconditionally, but RenderLegacy is the only
// caller that sets it -- so modern blockkit Render() image work is
// NOT counted here (its bkCtx.Perf stays nil at the call site in
// messages/model.go).
type LegacyPerf struct {
	attachmentCount int
	textTotal       time.Duration // renderTextLines for pretext / text / per-field value / footer
	imageTotal      time.Duration // computeBlockImageTarget + fetchOrPlaceholder + renderImageFallback
	otherTotal      time.Duration // title + footer formatting, stripeStyle.Render, per-line stripe concat

	// Sub-breakdown within imageTotal, populated by fetchOrPlaceholder.
	// Each (total, count) pair lets the host log avg-per-call.
	imgCachedCheckTotal time.Duration // ctx.Fetcher.Cached(key, pixelTarget)
	imgCachedCheckCount int
	imgKittyTotal       time.Duration // KittyRender.SetSource + RenderKey (cached + ProtoKitty)
	imgKittyCount       int
	imgRenderImageTotal time.Duration // imgpkg.RenderImage (cached + other protocols)
	imgRenderImageCount int
	imgPlaceholderTotal time.Duration // blockPlaceholder (not cached, fetch spawned in background)
	imgPlaceholderCount int
}

// TextTotal returns cumulative wall-clock spent in renderTextLines
// inside RenderLegacy across this call. Safe to call on a nil receiver.
func (p *LegacyPerf) TextTotal() time.Duration {
	if p == nil {
		return 0
	}
	return p.textTotal
}

// ImageTotal returns cumulative wall-clock spent in image fetch /
// render / fallback inside RenderLegacy across this call. Safe to
// call on a nil receiver.
func (p *LegacyPerf) ImageTotal() time.Duration {
	if p == nil {
		return 0
	}
	return p.imageTotal
}

// OtherTotal returns cumulative wall-clock spent in everything else
// inside RenderLegacy (stripe, title, footer formatting, width
// measurement, truncation). Safe to call on a nil receiver.
func (p *LegacyPerf) OtherTotal() time.Duration {
	if p == nil {
		return 0
	}
	return p.otherTotal
}

// AttachmentCount returns the number of LegacyAttachment values
// processed across this RenderLegacy call. Safe to call on a nil
// receiver.
func (p *LegacyPerf) AttachmentCount() int {
	if p == nil {
		return 0
	}
	return p.attachmentCount
}

// ImgCachedCheck returns (total, count) for ctx.Fetcher.Cached calls
// inside fetchOrPlaceholder. Safe to call on a nil receiver.
func (p *LegacyPerf) ImgCachedCheck() (time.Duration, int) {
	if p == nil {
		return 0, 0
	}
	return p.imgCachedCheckTotal, p.imgCachedCheckCount
}

// ImgKitty returns (total, count) for KittyRender.SetSource +
// RenderKey calls (cached image + ProtoKitty path). Safe on nil.
func (p *LegacyPerf) ImgKitty() (time.Duration, int) {
	if p == nil {
		return 0, 0
	}
	return p.imgKittyTotal, p.imgKittyCount
}

// ImgRenderImage returns (total, count) for imgpkg.RenderImage calls
// (cached image + non-kitty protocol path). Safe on nil.
func (p *LegacyPerf) ImgRenderImage() (time.Duration, int) {
	if p == nil {
		return 0, 0
	}
	return p.imgRenderImageTotal, p.imgRenderImageCount
}

// ImgPlaceholder returns (total, count) for blockPlaceholder calls
// (uncached -- fetch was spawned in background, this rendered the
// reserved-height stand-in). Safe on nil.
func (p *LegacyPerf) ImgPlaceholder() (time.Duration, int) {
	if p == nil {
		return 0, 0
	}
	return p.imgPlaceholderTotal, p.imgPlaceholderCount
}
