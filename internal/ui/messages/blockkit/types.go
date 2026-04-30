// Package blockkit parses and renders Slack Block Kit blocks and the
// legacy `attachments` field. The package is intentionally
// self-contained: it depends only on slack-go (for input types),
// lipgloss (for styling), the project's styles package (for theme
// colors), and the project's image package (for block image
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

	imgpkg "github.com/gammons/slk/internal/image"
)

// Block is a Slack Block Kit layout block. The unexported blockType()
// method seals the interface to this package so external packages
// cannot accidentally implement it.
type Block interface {
	blockType() string
}

// SectionBlock is the Slack `section` block: a body of mrkdwn text,
// optionally with a 2-column field grid and/or a single accessory
// rendered to the right of the body.
type SectionBlock struct {
	Text      string           // resolved mrkdwn (or plain) text; empty if absent
	Fields    []string         // each field is mrkdwn; rendered in a 2-col grid
	Accessory AccessoryElement // nil if absent
}

func (SectionBlock) blockType() string { return "section" }

// HeaderBlock is the Slack `header` block: bold, primary-colored,
// single-line plain text.
type HeaderBlock struct {
	Text string
}

func (HeaderBlock) blockType() string { return "header" }

// ContextBlock is the Slack `context` block: a single line mixing
// inline text (mrkdwn or plain) and small inline images. Order is
// preserved.
type ContextBlock struct {
	Elements []ContextElement
}

func (ContextBlock) blockType() string { return "context" }

// ContextElement is one item inside a ContextBlock. Exactly one of
// Text or ImageURL is set.
type ContextElement struct {
	Text     string // mrkdwn or plain
	ImageURL string // raw URL; rendered as a 1-row inline image
	AltText  string // alt for image elements (used as fallback)
}

// DividerBlock is the Slack `divider` block: a full-width horizontal
// rule.
type DividerBlock struct{}

func (DividerBlock) blockType() string { return "divider" }

// ImageBlock is the Slack `image` block: a full-width image with an
// optional title.
type ImageBlock struct {
	URL    string
	Title  string // optional title shown above the image
	Alt    string // fallback when the image cannot be rendered
	Width  int    // pixel width if known (0 if not)
	Height int    // pixel height if known (0 if not)
}

func (ImageBlock) blockType() string { return "image" }

// ActionsBlock is the Slack `actions` block: a row of interactive
// elements. We render them as muted, non-interactive labels.
type ActionsBlock struct {
	Elements []ActionElement
}

func (ActionsBlock) blockType() string { return "actions" }

// UnknownBlock is the catch-all for any block type the package does
// not handle. Its Type field is the original Slack block-type string
// (e.g. "video", "rich_text", "markdown", "file"). It renders as a
// single muted placeholder line.
type UnknownBlock struct {
	Type string
}

func (b UnknownBlock) blockType() string { return b.Type }

// AccessoryElement is one of the supported section-accessory element
// kinds. The set is intentionally narrow: image accessories render via
// the image pipeline, all other element kinds render as muted labels.
type AccessoryElement interface {
	accessoryKind() string
}

// ImageAccessory is an `image` element used as a section accessory.
// Rendered via the image pipeline at a small fixed cap (4 rows × 8
// cols) regardless of the user's max_image_rows setting.
type ImageAccessory struct {
	URL     string
	AltText string
}

func (ImageAccessory) accessoryKind() string { return "image" }

// LabelAccessory is any non-image accessory element (button,
// overflow, *_select, datepicker, etc.) rendered as a muted label.
// Kind is the slack-go element-type string ("button", "overflow",
// "static_select", etc.) so the renderer can pick the right glyph
// (e.g. ▾ for selects, ⋯ for overflow).
type LabelAccessory struct {
	Kind  string
	Label string // best-effort human label (button text, placeholder, current value)
}

func (LabelAccessory) accessoryKind() string { return "label" }

// ActionElement is one element inside an ActionsBlock. We use the
// same shape as LabelAccessory: kind + label.
type ActionElement struct {
	Kind  string
	Label string
}

// LegacyAttachment is one entry in Slack's legacy `attachments` array.
// All fields are optional; render code must guard for empty values.
type LegacyAttachment struct {
	Color      string // "good"/"warning"/"danger" or 6-digit hex; "" → theme border
	Pretext    string // mrkdwn rendered above the colored bar
	Title      string
	TitleLink  string // if set, Title is rendered as an OSC-8 hyperlink
	Text       string // mrkdwn rendered inside the bar
	Fields     []LegacyField
	ImageURL   string // optional image rendered inside the bar at full inline width
	ThumbURL   string // optional small thumbnail rendered to the right of Text
	Footer     string
	FooterIcon string // tiny inline image rendered before Footer
	TS         int64  // unix seconds; 0 means absent
}

// LegacyField is one entry in a LegacyAttachment's Fields slice.
// Short controls grid placement: two consecutive Short==true fields
// share a row.
type LegacyField struct {
	Title string
	Value string
	Short bool
}

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
	Bytes    []byte
	Fallback []string
	Height   int
}

// HitRect is one clickable image footprint expressed in (row, col)
// coordinates RELATIVE TO RenderResult.Lines. The integration site
// translates these to absolute viewEntry coordinates by adding the
// row offset and column-base before storing them on the viewEntry.
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
	Fetcher     *imgpkg.Fetcher
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
}
