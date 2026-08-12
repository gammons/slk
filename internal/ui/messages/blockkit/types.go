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
	"time"

	"github.com/slack-go/slack"

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
// (e.g. "video", "markdown", "file"). It renders as a single muted
// placeholder line.
type UnknownBlock struct {
	Type string
}

func (b UnknownBlock) blockType() string { return b.Type }

// RichTextBlock is the Slack `rich_text` block: an ordered list of
// rich_text_section / rich_text_list / rich_text_preformatted /
// rich_text_quote elements that together describe a fully-styled
// message body. We keep the slack-go inline element types as-is
// rather than mirror the whole type hierarchy, because the renderer
// path for rich_text routes through the host's mrkdwn pipeline (see
// RichTextToMrkdwn) — there's no need for a parallel typed tree.
//
// Slack also exposes a flattened mrkdwn `text` field on every
// message that contains a rich_text block, BUT that fallback is
// lossy: standalone "\n" elements collapse into spaces, so any
// multi-line bot message (GitHub Pending Reviews, build digests,
// etc.) renders horizontally if we trust the `text` field. The
// host detects a RichTextBlock and substitutes RichTextToMrkdwn's
// output before passing through to RenderSlackMarkdown.
type RichTextBlock struct {
	Elements []slack.RichTextElement
}

func (RichTextBlock) blockType() string { return "rich_text" }

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
	// Blocks holds Block Kit blocks nested inside the attachment.
	// Slack's newer link-unfurl shape (Linear/Jira/GitHub issue
	// cards, etc.) carries all visible content here while
	// Title/Text/Fields are empty. Rendered inside the colored
	// stripe after the classic fields. nil when absent.
	Blocks []Block
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
	Width    int
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
