# Phase 1: Data Model & Parsing

> See `00-overview.md` for goal, architecture, and conventions.

This phase introduces the `blockkit` package with its types and parsing logic, then threads two new fields through `MessageItem` and the four message-ingestion sites in `cmd/slk/main.go`. The package is fully self-contained; nothing calls `Render` yet (it's a no-op stub returning an empty `RenderResult`). At the end of this phase the app builds, all existing tests pass, and bot messages render exactly as before.

---

## Task 1: Scaffold `blockkit` package — types only

**Files:**
- Create: `internal/ui/messages/blockkit/types.go`
- Create: `internal/ui/messages/blockkit/render.go` (stub)
- Create: `internal/ui/messages/blockkit/types_test.go`

The `Block` interface is a "sealed-by-convention" pattern: any value that implements `blockType() string` is a `Block`. We use an unexported method so external packages cannot accidentally implement `Block`. Concrete blocks live in this same package.

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/messages/blockkit/types_test.go
package blockkit

import "testing"

func TestBlockTypesImplementInterface(t *testing.T) {
	var blocks []Block = []Block{
		SectionBlock{},
		HeaderBlock{},
		ContextBlock{},
		DividerBlock{},
		ImageBlock{},
		ActionsBlock{},
		UnknownBlock{Type: "video"},
	}
	if got := blocks[6].blockType(); got != "video" {
		t.Errorf("UnknownBlock.blockType() = %q, want %q", got, "video")
	}
}

func TestRenderResultZeroValueIsSafe(t *testing.T) {
	var r RenderResult
	if r.Height != 0 {
		t.Error("zero RenderResult should have Height 0")
	}
	if r.Interactive {
		t.Error("zero RenderResult should not be Interactive")
	}
	if len(r.Lines) != 0 {
		t.Error("zero RenderResult should have empty Lines")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/ui/messages/blockkit/... -run "TestBlockTypesImplementInterface|TestRenderResultZeroValueIsSafe" -v
```

Expected: FAIL with `package internal/ui/messages/blockkit/...: no Go files`.

- [ ] **Step 3: Create types.go**

```go
// internal/ui/messages/blockkit/types.go
//
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
	Text      string         // resolved mrkdwn (or plain) text; empty if absent
	Fields    []string       // each field is mrkdwn; rendered in a 2-col grid
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
	Lines       []string                 // ANSI-styled, ready to join with "\n"
	Flushes     []func(io.Writer) error  // kitty image upload callbacks
	SixelRows   map[int]SixelEntry       // sixel sentinel rows keyed by row-within-Lines
	Height      int                      // == len(Lines); cached for caller's row math
	Hits        []HitRect                // clickable image footprints
	Interactive bool                     // any interactive element rendered
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
	// SendMsg is used by image-block prefetchers to signal completion.
	// May be nil; when nil, image blocks render as a placeholder
	// indefinitely until the next render-cache invalidation.
	SendMsg func(any)
	// MessageTS / Channel are echoed back on async image-ready
	// messages so the host can target the right entry for cache
	// invalidation.
	MessageTS string
	Channel   string
}
```

- [ ] **Step 4: Create render.go stub**

```go
// internal/ui/messages/blockkit/render.go
package blockkit

// Render produces a RenderResult for a slice of blocks at the given
// content width. Width is the available content width AFTER the
// caller has subtracted avatar gutter and border columns.
//
// This is currently a stub returning an empty RenderResult. Phase 2
// fills it in.
func Render(blocks []Block, ctx Context, width int) RenderResult {
	return RenderResult{}
}

// RenderLegacy produces a RenderResult for a slice of legacy
// attachments. Same width contract as Render. Phase 4 fills it in.
func RenderLegacy(atts []LegacyAttachment, ctx Context, width int) RenderResult {
	return RenderResult{}
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/ui/messages/blockkit/... -v
```

Expected: PASS, both tests.

- [ ] **Step 6: Run `make build` to verify the package compiles in the wider tree**

Expected: build succeeds, binary produced at `bin/slk`.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): scaffold package with sealed Block interface and RenderResult"
```

---

## Task 2: Parse slack-go blocks into typed structs

**Files:**
- Create: `internal/ui/messages/blockkit/parse.go`
- Create: `internal/ui/messages/blockkit/parse_test.go`

This task ONLY parses blocks. Legacy attachments come in Task 3. The parser is total — every input block becomes some output `Block` value, with `UnknownBlock` as the catch-all.

- [ ] **Step 1: Write the failing test**

```go
// internal/ui/messages/blockkit/parse_test.go
package blockkit

import (
	"testing"

	"github.com/slack-go/slack"
)

func TestParseEmptyBlocksReturnsNil(t *testing.T) {
	got := Parse(slack.Blocks{})
	if got != nil {
		t.Errorf("Parse(empty) = %v, want nil", got)
	}
}

func TestParseHeaderBlock(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject("plain_text", "Deploy successful", false, false)),
	}}
	got := Parse(in)
	if len(got) != 1 {
		t.Fatalf("got %d blocks, want 1", len(got))
	}
	hb, ok := got[0].(HeaderBlock)
	if !ok {
		t.Fatalf("got %T, want HeaderBlock", got[0])
	}
	if hb.Text != "Deploy successful" {
		t.Errorf("Text = %q, want %q", hb.Text, "Deploy successful")
	}
}

func TestParseDividerBlock(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{slack.NewDividerBlock()}}
	got := Parse(in)
	if len(got) != 1 {
		t.Fatalf("got %d blocks, want 1", len(got))
	}
	if _, ok := got[0].(DividerBlock); !ok {
		t.Errorf("got %T, want DividerBlock", got[0])
	}
}

func TestParseSectionBlockWithText(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "*hello* world", false, false),
			nil, nil,
		),
	}}
	got := Parse(in)
	sb, ok := got[0].(SectionBlock)
	if !ok {
		t.Fatalf("got %T, want SectionBlock", got[0])
	}
	if sb.Text != "*hello* world" {
		t.Errorf("Text = %q, want %q", sb.Text, "*hello* world")
	}
	if len(sb.Fields) != 0 {
		t.Errorf("Fields len = %d, want 0", len(sb.Fields))
	}
	if sb.Accessory != nil {
		t.Errorf("Accessory = %v, want nil", sb.Accessory)
	}
}

func TestParseSectionBlockWithFields(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(nil, []*slack.TextBlockObject{
			slack.NewTextBlockObject("mrkdwn", "*Service*\nweb", false, false),
			slack.NewTextBlockObject("mrkdwn", "*Region*\nus-east-1", false, false),
		}, nil),
	}}
	sb := Parse(in)[0].(SectionBlock)
	if len(sb.Fields) != 2 {
		t.Fatalf("Fields len = %d, want 2", len(sb.Fields))
	}
	if sb.Fields[0] != "*Service*\nweb" {
		t.Errorf("Fields[0] = %q", sb.Fields[0])
	}
}

func TestParseSectionBlockWithButtonAccessory(t *testing.T) {
	btn := slack.NewButtonBlockElement("approve", "approve_value",
		slack.NewTextBlockObject("plain_text", "Approve", false, false))
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "Ready?", false, false),
			nil,
			slack.NewAccessory(btn),
		),
	}}
	sb := Parse(in)[0].(SectionBlock)
	if sb.Accessory == nil {
		t.Fatal("Accessory is nil")
	}
	la, ok := sb.Accessory.(LabelAccessory)
	if !ok {
		t.Fatalf("Accessory = %T, want LabelAccessory", sb.Accessory)
	}
	if la.Kind != "button" || la.Label != "Approve" {
		t.Errorf("got %+v, want {button Approve}", la)
	}
}

func TestParseSectionBlockWithImageAccessory(t *testing.T) {
	img := slack.NewImageBlockElement("https://example.com/logo.png", "company logo")
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "Hello", false, false),
			nil,
			slack.NewAccessory(img),
		),
	}}
	sb := Parse(in)[0].(SectionBlock)
	ia, ok := sb.Accessory.(ImageAccessory)
	if !ok {
		t.Fatalf("Accessory = %T, want ImageAccessory", sb.Accessory)
	}
	if ia.URL != "https://example.com/logo.png" {
		t.Errorf("URL = %q", ia.URL)
	}
	if ia.AltText != "company logo" {
		t.Errorf("AltText = %q", ia.AltText)
	}
}

func TestParseImageBlock(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewImageBlock("https://example.com/chart.png", "chart", "block1",
			slack.NewTextBlockObject("plain_text", "Q3 metrics", false, false)),
	}}
	ib := Parse(in)[0].(ImageBlock)
	if ib.URL != "https://example.com/chart.png" {
		t.Errorf("URL = %q", ib.URL)
	}
	if ib.Title != "Q3 metrics" {
		t.Errorf("Title = %q", ib.Title)
	}
	if ib.Alt != "chart" {
		t.Errorf("Alt = %q", ib.Alt)
	}
}

func TestParseContextBlockMixedElements(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewContextBlock("ctx",
			slack.NewImageBlockElement("https://example.com/icon.png", "icon"),
			slack.NewTextBlockObject("mrkdwn", "*by* gammons", false, false),
		),
	}}
	cb := Parse(in)[0].(ContextBlock)
	if len(cb.Elements) != 2 {
		t.Fatalf("Elements len = %d, want 2", len(cb.Elements))
	}
	if cb.Elements[0].ImageURL != "https://example.com/icon.png" {
		t.Errorf("Elements[0].ImageURL = %q", cb.Elements[0].ImageURL)
	}
	if cb.Elements[1].Text != "*by* gammons" {
		t.Errorf("Elements[1].Text = %q", cb.Elements[1].Text)
	}
}

func TestParseActionsBlock(t *testing.T) {
	btn := slack.NewButtonBlockElement("a", "v",
		slack.NewTextBlockObject("plain_text", "Click", false, false))
	in := slack.Blocks{BlockSet: []slack.Block{slack.NewActionBlock("act", btn)}}
	ab := Parse(in)[0].(ActionsBlock)
	if len(ab.Elements) != 1 {
		t.Fatalf("Elements len = %d, want 1", len(ab.Elements))
	}
	if ab.Elements[0].Kind != "button" || ab.Elements[0].Label != "Click" {
		t.Errorf("got %+v", ab.Elements[0])
	}
}

func TestParseUnknownBlockTypePreservesType(t *testing.T) {
	in := slack.Blocks{BlockSet: []slack.Block{
		&slack.UnknownBlock{Type: slack.MessageBlockType("video")},
	}}
	got := Parse(in)
	ub, ok := got[0].(UnknownBlock)
	if !ok {
		t.Fatalf("got %T, want UnknownBlock", got[0])
	}
	if ub.Type != "video" {
		t.Errorf("Type = %q, want %q", ub.Type, "video")
	}
}

func TestParseRichTextBecomesUnknownToFallThroughToText(t *testing.T) {
	// rich_text is intentionally not walked; we want it to land in
	// UnknownBlock so the renderer can produce a placeholder. The
	// host's Message.Text fallback handles the actual content.
	in := slack.Blocks{BlockSet: []slack.Block{
		slack.NewRichTextBlock("rt"),
	}}
	got := Parse(in)
	if _, ok := got[0].(UnknownBlock); !ok {
		t.Errorf("got %T, want UnknownBlock for rich_text", got[0])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestParse" -v
```

Expected: FAIL with `undefined: Parse`.

- [ ] **Step 3: Implement parse.go**

```go
// internal/ui/messages/blockkit/parse.go
package blockkit

import (
	"github.com/slack-go/slack"
)

// Parse converts a slack-go Blocks value into our typed Block slice.
// Every input block produces exactly one output entry; unhandled
// types become UnknownBlock so the renderer can show a placeholder.
// Returns nil for an empty input.
func Parse(in slack.Blocks) []Block {
	if len(in.BlockSet) == 0 {
		return nil
	}
	out := make([]Block, 0, len(in.BlockSet))
	for _, b := range in.BlockSet {
		out = append(out, parseOne(b))
	}
	return out
}

func parseOne(b slack.Block) Block {
	switch v := b.(type) {
	case *slack.HeaderBlock:
		return HeaderBlock{Text: textOf(v.Text)}
	case *slack.DividerBlock:
		return DividerBlock{}
	case *slack.SectionBlock:
		return parseSection(v)
	case *slack.ContextBlock:
		return parseContext(v)
	case *slack.ImageBlock:
		title := ""
		if v.Title != nil {
			title = v.Title.Text
		}
		return ImageBlock{
			URL:    v.ImageURL,
			Title:  title,
			Alt:    v.AltText,
			Width:  v.ImageWidth,
			Height: v.ImageHeight,
		}
	case *slack.ActionBlock:
		return parseActions(v)
	default:
		return UnknownBlock{Type: string(b.BlockType())}
	}
}

func parseSection(s *slack.SectionBlock) SectionBlock {
	out := SectionBlock{Text: textOf(s.Text)}
	for _, f := range s.Fields {
		out.Fields = append(out.Fields, textOf(f))
	}
	if s.Accessory != nil {
		out.Accessory = parseAccessory(s.Accessory)
	}
	return out
}

func parseContext(c *slack.ContextBlock) ContextBlock {
	out := ContextBlock{}
	if c.ContextElements.Elements == nil {
		return out
	}
	for _, e := range c.ContextElements.Elements {
		switch v := e.(type) {
		case *slack.TextBlockObject:
			out.Elements = append(out.Elements, ContextElement{Text: v.Text})
		case *slack.ImageBlockElement:
			url := ""
			if v.ImageURL != nil {
				url = *v.ImageURL
			}
			out.Elements = append(out.Elements, ContextElement{ImageURL: url, AltText: v.AltText})
		}
	}
	return out
}

func parseActions(a *slack.ActionBlock) ActionsBlock {
	out := ActionsBlock{}
	if a.Elements == nil {
		return out
	}
	for _, e := range a.Elements.ElementSet {
		out.Elements = append(out.Elements, actionElementOf(e))
	}
	return out
}

func parseAccessory(a *slack.Accessory) AccessoryElement {
	switch {
	case a.ImageElement != nil:
		url := ""
		if a.ImageElement.ImageURL != nil {
			url = *a.ImageElement.ImageURL
		}
		return ImageAccessory{URL: url, AltText: a.ImageElement.AltText}
	case a.ButtonElement != nil:
		return LabelAccessory{Kind: "button", Label: textOf(a.ButtonElement.Text)}
	case a.OverflowElement != nil:
		return LabelAccessory{Kind: "overflow", Label: ""}
	case a.SelectElement != nil:
		return LabelAccessory{Kind: "static_select", Label: textOf(a.SelectElement.Placeholder)}
	case a.MultiSelectElement != nil:
		return LabelAccessory{Kind: "multi_select", Label: textOf(a.MultiSelectElement.Placeholder)}
	case a.DatePickerElement != nil:
		return LabelAccessory{Kind: "datepicker", Label: a.DatePickerElement.InitialDate}
	case a.TimePickerElement != nil:
		return LabelAccessory{Kind: "timepicker", Label: a.TimePickerElement.InitialTime}
	case a.RadioButtonsElement != nil:
		return LabelAccessory{Kind: "radio_buttons", Label: ""}
	case a.CheckboxGroupsBlockElement != nil:
		return LabelAccessory{Kind: "checkboxes", Label: ""}
	case a.WorkflowButtonElement != nil:
		return LabelAccessory{Kind: "workflow_button", Label: textOf(a.WorkflowButtonElement.Text)}
	default:
		return LabelAccessory{Kind: "unknown", Label: ""}
	}
}

func actionElementOf(e slack.BlockElement) ActionElement {
	switch v := e.(type) {
	case *slack.ButtonBlockElement:
		return ActionElement{Kind: "button", Label: textOf(v.Text)}
	case *slack.OverflowBlockElement:
		return ActionElement{Kind: "overflow"}
	case *slack.SelectBlockElement:
		return ActionElement{Kind: "static_select", Label: textOf(v.Placeholder)}
	case *slack.MultiSelectBlockElement:
		return ActionElement{Kind: "multi_select", Label: textOf(v.Placeholder)}
	case *slack.DatePickerBlockElement:
		return ActionElement{Kind: "datepicker", Label: v.InitialDate}
	case *slack.TimePickerBlockElement:
		return ActionElement{Kind: "timepicker", Label: v.InitialTime}
	case *slack.RadioButtonsBlockElement:
		return ActionElement{Kind: "radio_buttons"}
	case *slack.CheckboxGroupsBlockElement:
		return ActionElement{Kind: "checkboxes"}
	case *slack.WorkflowButtonBlockElement:
		return ActionElement{Kind: "workflow_button", Label: textOf(v.Text)}
	default:
		return ActionElement{Kind: "unknown"}
	}
}

// textOf returns the .Text of a TextBlockObject, or "" if nil.
func textOf(t *slack.TextBlockObject) string {
	if t == nil {
		return ""
	}
	return t.Text
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestParse" -v
```

Expected: PASS for all eleven tests.

- [ ] **Step 5: Run `make build`**

Expected: binary produced.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): parse slack-go Blocks into typed sealed-interface tree"
```

---

## Task 3: Parse legacy `attachments` into `LegacyAttachment`

**Files:**
- Modify: `internal/ui/messages/blockkit/parse.go`
- Modify: `internal/ui/messages/blockkit/parse_test.go`

- [ ] **Step 1: Add the failing tests to parse_test.go**

Append:

```go
func TestParseAttachmentsEmptyReturnsNil(t *testing.T) {
	got := ParseAttachments(nil)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestParseAttachmentBasicFields(t *testing.T) {
	in := []slack.Attachment{{
		Color:     "danger",
		Pretext:   "Heads up:",
		Title:     "Service down",
		TitleLink: "https://status.example.com",
		Text:      "checkout-svc returning 5xx",
		Footer:    "Datadog",
	}}
	got := ParseAttachments(in)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	a := got[0]
	if a.Color != "danger" {
		t.Errorf("Color = %q", a.Color)
	}
	if a.Pretext != "Heads up:" {
		t.Errorf("Pretext = %q", a.Pretext)
	}
	if a.Title != "Service down" {
		t.Errorf("Title = %q", a.Title)
	}
	if a.TitleLink != "https://status.example.com" {
		t.Errorf("TitleLink = %q", a.TitleLink)
	}
	if a.Text != "checkout-svc returning 5xx" {
		t.Errorf("Text = %q", a.Text)
	}
	if a.Footer != "Datadog" {
		t.Errorf("Footer = %q", a.Footer)
	}
}

func TestParseAttachmentFields(t *testing.T) {
	in := []slack.Attachment{{
		Fields: []slack.AttachmentField{
			{Title: "Service", Value: "checkout-svc", Short: true},
			{Title: "Region", Value: "us-east-1", Short: true},
			{Title: "Notes", Value: "long form note", Short: false},
		},
	}}
	a := ParseAttachments(in)[0]
	if len(a.Fields) != 3 {
		t.Fatalf("Fields len = %d", len(a.Fields))
	}
	if a.Fields[0].Title != "Service" || a.Fields[0].Value != "checkout-svc" || !a.Fields[0].Short {
		t.Errorf("Fields[0] = %+v", a.Fields[0])
	}
	if a.Fields[2].Short {
		t.Errorf("Fields[2] should not be Short")
	}
}

func TestParseAttachmentTimestampParsesUnixSeconds(t *testing.T) {
	in := []slack.Attachment{{Ts: "1700000000"}}
	a := ParseAttachments(in)[0]
	if a.TS != 1700000000 {
		t.Errorf("TS = %d, want 1700000000", a.TS)
	}
}

func TestParseAttachmentImageAndThumb(t *testing.T) {
	in := []slack.Attachment{{
		ImageURL: "https://example.com/img.png",
		ThumbURL: "https://example.com/thumb.png",
	}}
	a := ParseAttachments(in)[0]
	if a.ImageURL != "https://example.com/img.png" {
		t.Errorf("ImageURL = %q", a.ImageURL)
	}
	if a.ThumbURL != "https://example.com/thumb.png" {
		t.Errorf("ThumbURL = %q", a.ThumbURL)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestParseAttachment" -v
```

Expected: FAIL with `undefined: ParseAttachments`.

- [ ] **Step 3: Add ParseAttachments to parse.go**

Append to `parse.go`:

```go
// ParseAttachments converts slack-go Attachment slice to our
// LegacyAttachment slice. Returns nil for empty input.
func ParseAttachments(in []slack.Attachment) []LegacyAttachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]LegacyAttachment, 0, len(in))
	for _, a := range in {
		out = append(out, parseAttachment(a))
	}
	return out
}

func parseAttachment(a slack.Attachment) LegacyAttachment {
	la := LegacyAttachment{
		Color:      a.Color,
		Pretext:    a.Pretext,
		Title:      a.Title,
		TitleLink:  a.TitleLink,
		Text:       a.Text,
		ImageURL:   a.ImageURL,
		ThumbURL:   a.ThumbURL,
		Footer:     a.Footer,
		FooterIcon: a.FooterIcon,
	}
	for _, f := range a.Fields {
		la.Fields = append(la.Fields, LegacyField{
			Title: f.Title, Value: f.Value, Short: f.Short,
		})
	}
	if a.Ts != "" {
		// json.Number; safe to parse as int64.
		if n, err := a.Ts.Int64(); err == nil {
			la.TS = n
		}
	}
	return la
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/messages/blockkit/ -v
```

Expected: PASS for all tests including the new attachment tests.

- [ ] **Step 5: Run `make build`**

- [ ] **Step 6: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): parse legacy attachments into LegacyAttachment"
```

---

## Task 4: Thread `Blocks` and `LegacyAttachments` through `MessageItem` and ingestion

**Files:**
- Modify: `internal/ui/messages/model.go` (lines 25-41 — `MessageItem` struct)
- Modify: `cmd/slk/main.go` (around `extractAttachments` at line 1098, and the four call sites at lines 1259, 1322, 1386, ~1587)

The renderer is still a no-op, so this task is pure plumbing. Verification is by `make build` and `make test` — no new behavior to assert beyond "existing tests still pass and the binary still runs."

- [ ] **Step 1: Add `Blocks` and `LegacyAttachments` to `MessageItem`**

Edit `internal/ui/messages/model.go`. The struct definition is at line 25. Add the import line for `blockkit` and the two new fields. Result:

```go
import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/scrollbar"
	"github.com/gammons/slk/internal/ui/selection"
	"github.com/gammons/slk/internal/ui/styles"
	emoji "github.com/kyokomi/emoji/v2"
)

type MessageItem struct {
	TS          string
	UserName    string
	UserID      string
	Text        string
	Timestamp   string
	DateStr     string
	ThreadTS    string
	ReplyCount  int
	Reactions   []ReactionItem
	Attachments []Attachment
	IsEdited    bool
	Subtype     string

	// Blocks holds parsed Slack Block Kit blocks. Rendered between
	// the body Text and the file Attachments by Phase 5.
	Blocks []blockkit.Block

	// LegacyAttachments holds parsed entries from the legacy
	// `attachments` field (color stripe + title + fields style bot
	// cards). Rendered after Blocks.
	LegacyAttachments []blockkit.LegacyAttachment
}
```

- [ ] **Step 2: Add `extractBlocks` and `extractLegacyAttachments` to `cmd/slk/main.go`**

Find `extractAttachments` (line 1098). Immediately after it, add:

```go
// extractBlocks converts a slack.Blocks value to our typed block
// slice for storage on a MessageItem. Empty input returns nil.
func extractBlocks(b slack.Blocks) []blockkit.Block {
	return blockkit.Parse(b)
}

// extractLegacyAttachments converts slack-go Attachment slice into
// our LegacyAttachment type. Empty input returns nil.
func extractLegacyAttachments(a []slack.Attachment) []blockkit.LegacyAttachment {
	return blockkit.ParseAttachments(a)
}
```

Then add the import at the top of `cmd/slk/main.go`:

```go
"github.com/gammons/slk/internal/ui/messages/blockkit"
```

- [ ] **Step 3: Populate the two new fields at all four ingestion sites**

There are four `MessageItem{...}` literals in `cmd/slk/main.go` that currently set `Attachments: extractAttachments(m.Files)`. At each one, also set `Blocks` and `LegacyAttachments`:

1. `fetchOlderMessages` (around line 1249-1260): inside the `MessageItem{...}` literal, add lines:
   ```go
   Blocks:            extractBlocks(m.Blocks),
   LegacyAttachments: extractLegacyAttachments(m.Attachments),
   ```
2. `fetchChannelMessages` (around line 1312-1323): same two lines.
3. `fetchThreadReplies` (around line 1376-1387): same two lines.
4. The upload-completion path around line 1587: same two lines IF that site receives a `slack.Message` (it constructs a synthetic `MessageItem` for the just-uploaded message — if no blocks/attachments are available there, leave them nil; just verify the build still passes).

Use `rg -n "Attachments: extractAttachments" cmd/slk/main.go` to enumerate the exact sites. Verify each site: the variable holding the `slack.Message` exposes `.Blocks` (a `slack.Blocks`) and `.Attachments` (a `[]slack.Attachment`). slack-go field names verified.

- [ ] **Step 4: Run `make build`**

Expected: build succeeds. If you see "imported and not used" errors, the `blockkit` import was added in a file that doesn't reference the package — fix by ensuring the `extract*` functions are colocated with the import.

- [ ] **Step 5: Run `make test`**

```bash
go test ./... -race
```

Expected: all existing tests pass. No new tests added in this task (the renderer is a no-op so there's no behavior to assert).

- [ ] **Step 6: Manual sanity smoke**

```bash
./bin/slk
```

Open a channel that has bot messages. Confirm the app starts, renders messages exactly as before this phase (because the renderer is still a stub), and quits cleanly. Then exit.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/messages/model.go cmd/slk/main.go
git commit -m "feat(messages): thread blockkit.Block and LegacyAttachment through MessageItem and ingestion"
```

---

## Phase 1 self-check before proceeding

- [ ] All seven tasks committed
- [ ] `make build` clean
- [ ] `make test` clean (no new failures)
- [ ] Manual smoke shows no behavioral change
- [ ] `internal/ui/messages/blockkit/` is self-contained — depends only on `slack-go`, `lipgloss`, project's `internal/image`, and stdlib

If any of the above fails, stop and fix before starting Phase 2.
