// Package blocks holds the parsed Slack Block Kit and legacy attachment
// data carried on a message. The engine builds these from Slack payloads
// and the TUI renders them (internal/ui/messages/blockkit).
package blocks

import "github.com/slack-go/slack"

// Block is a Slack Block Kit layout block. The unexported blockType()
// method seals the interface to this package so external packages
// cannot accidentally implement it.
type Block interface {
	blockType() string
}

// TypeName is the Slack block-type string for b ("section", "header",
// ..., or the original type of an UnknownBlock).
func TypeName(b Block) string { return b.blockType() }

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
// blockkit.RichTextToMrkdwn) — there's no need for a parallel typed tree.
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
