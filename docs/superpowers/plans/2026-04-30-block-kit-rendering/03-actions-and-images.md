# Phase 3: Actions and Images

> See `00-overview.md` for goal, architecture, and conventions. Phases 1-2 must be complete.

This phase adds rendering for `actions` blocks (a row of muted controls), full-size `image` blocks (via the existing image pipeline), and section image accessories (small thumbnails next to body text).

The image pipeline reuse is the trickiest part. The existing `renderAttachmentBlock` keys images by Slack file ID and picks from a thumb ladder. Block images and accessory images have a single URL with no file ID and no thumb variants, so we use a SHA-1 of the URL as the cache key and skip thumb selection entirely (one fetch at the URL, downscale to target dims).

---

## Task 9: Render `actions` block as a row of muted controls

**Files:**
- Modify: `internal/ui/messages/blockkit/render.go`
- Modify: `internal/ui/messages/blockkit/render_test.go`

- [ ] **Step 1: Failing tests**

Append to `render_test.go`:

```go
func TestRenderActionsBlockSetsInteractive(t *testing.T) {
	r := Render([]Block{ActionsBlock{
		Elements: []ActionElement{
			{Kind: "button", Label: "Approve"},
			{Kind: "button", Label: "Deny"},
		},
	}}, Context{}, 80)
	if !r.Interactive {
		t.Error("Interactive should be true after rendering actions")
	}
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "[ Approve ]") || !strings.Contains(plain, "[ Deny ]") {
		t.Errorf("got %q", plain)
	}
}

func TestRenderActionsBlockWrapsAtWidth(t *testing.T) {
	// Three buttons of "[ Long Button Name ]" (~20 cols each) at
	// width 30 must wrap.
	r := Render([]Block{ActionsBlock{
		Elements: []ActionElement{
			{Kind: "button", Label: "Long Button Name"},
			{Kind: "button", Label: "Long Button Name"},
			{Kind: "button", Label: "Long Button Name"},
		},
	}}, Context{}, 30)
	if r.Height < 2 {
		t.Errorf("Height = %d, want >= 2 (wrapped)", r.Height)
	}
}

func TestRenderActionsBlockMixedKinds(t *testing.T) {
	r := Render([]Block{ActionsBlock{
		Elements: []ActionElement{
			{Kind: "button", Label: "Go"},
			{Kind: "static_select", Label: "env"},
			{Kind: "datepicker", Label: "2026-01-01"},
		},
	}}, Context{}, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "[ Go ]") {
		t.Errorf("missing button: %q", plain)
	}
	if !strings.Contains(plain, "env ▾") {
		t.Errorf("missing select: %q", plain)
	}
	if !strings.Contains(plain, "📅") {
		t.Errorf("missing datepicker: %q", plain)
	}
}
```

- [ ] **Step 2: Run, verify failure, then implement.**

In `render.go`, add to the `appendBlock` switch:

```go
case ActionsBlock:
    appendActions(out, v, width)
```

Then add:

```go
func appendActions(out *RenderResult, a ActionsBlock, width int) {
	if len(a.Elements) == 0 {
		return
	}
	out.Interactive = true

	gap := lipgloss.NewStyle().Background(lipgloss.NoColor{}).Render("  ")
	gapW := 2

	var rows []string
	current := ""
	currentW := 0
	for _, el := range a.Elements {
		label := renderControlLabel(el.Kind, el.Label)
		labelW := lipgloss.Width(label)
		var candidateW int
		var candidate string
		if current == "" {
			candidate = label
			candidateW = labelW
		} else {
			candidate = current + gap + label
			candidateW = currentW + gapW + labelW
		}
		if candidateW > width && current != "" {
			rows = append(rows, current)
			current = label
			currentW = labelW
		} else {
			current = candidate
			currentW = candidateW
		}
	}
	if current != "" {
		rows = append(rows, current)
	}
	out.Lines = append(out.Lines, rows...)
}
```

- [ ] **Step 3: Run tests, build, commit**

```bash
go test ./internal/ui/messages/blockkit/ -v
make build
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): render actions block as wrapping row of muted controls"
```

---

## Task 10: Render full-size `image` blocks via the image pipeline

**Files:**
- Create: `internal/ui/messages/blockkit/image.go`
- Modify: `internal/ui/messages/blockkit/render.go`
- Modify: `internal/ui/messages/blockkit/render_test.go`

This task introduces the URL-keyed image flow. Three rendering paths:

1. `Context.Fetcher == nil` or `Context.Protocol == ProtoOff` → render as a single fallback line `[image] <URL>` (OSC-8 hyperlinked).
2. URL not yet cached → reserved-height placeholder + async prefetch (mirrors `renderAttachmentBlock`).
3. Cached → render via the active protocol.

For the test, we focus on the first path because the others require live image bytes. Tests for paths 2/3 happen as part of the integration test in Phase 5.

- [ ] **Step 1: Add the failing test**

Append to `render_test.go`:

```go
func TestRenderImageBlockNoFetcherFallsBackToTextLink(t *testing.T) {
	r := Render([]Block{ImageBlock{
		URL:   "https://example.com/chart.png",
		Title: "Q3 chart",
		Alt:   "chart",
	}}, Context{}, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "https://example.com/chart.png") {
		t.Errorf("expected URL in fallback, got %q", plain)
	}
	if !strings.Contains(plain, "[image]") {
		t.Errorf("expected '[image]' marker in fallback, got %q", plain)
	}
}

func TestRenderImageBlockShowsTitleAboveFallback(t *testing.T) {
	r := Render([]Block{ImageBlock{
		URL:   "https://example.com/x.png",
		Title: "Q3 Metrics",
	}}, Context{}, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "Q3 Metrics") {
		t.Errorf("expected title %q in %q", "Q3 Metrics", plain)
	}
}
```

- [ ] **Step 2: Create image.go with the helper**

```go
// internal/ui/messages/blockkit/image.go
package blockkit

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	imgpkg "github.com/gammons/slk/internal/image"
)

// urlCacheKey hashes a URL into a short, stable cache key suitable
// for use with the image package's fetcher and renderer.
func urlCacheKey(url string) string {
	sum := sha1.Sum([]byte(url))
	return "BK-" + hex.EncodeToString(sum[:8])
}

// renderImageFallback returns a single OSC-8 hyperlinked line for an
// image we cannot render inline (no fetcher, ProtoOff, or empty URL).
// Format: "[image] <url>"
func renderImageFallback(url string) string {
	if url == "" {
		return mutedStyle().Render("[image] (no url)")
	}
	label := "[image] " + url
	// OSC-8 hyperlink: \x1b]8;;URL\x1b\\LABEL\x1b]8;;\x1b\\
	hyper := "\x1b]8;;" + url + "\x1b\\" + label + "\x1b]8;;\x1b\\"
	return mutedStyle().Render(hyper)
}

// suppressUnused keeps the imgpkg/lipgloss/ansi imports in this file
// for use by future tasks. Remove once additional helpers are added.
var _ = imgpkg.ProtoOff
var _ = lipgloss.NoColor{}
var _ = ansi.Strip
var _ = fmt.Sprintf
```

(The `suppressUnused` block will go away when Phase 5 wires in the actual fetch path; for now we just want the file to compile.)

- [ ] **Step 3: Add image-block rendering to render.go**

In `appendBlock`, add:

```go
case ImageBlock:
    appendImageBlock(out, v, ctx, width)
```

Then:

```go
func appendImageBlock(out *RenderResult, b ImageBlock, ctx Context, width int) {
	// Title (if any) renders as a small bold line above the image.
	if b.Title != "" {
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(getMessagePrimary()).Background(getMessageBg())
		title := b.Title
		if lipgloss.Width(title) > width {
			title = truncateToWidth(title, width)
		}
		out.Lines = append(out.Lines, titleStyle.Render(title))
	}

	// If we can't render images at all, emit the fallback link.
	if ctx.Fetcher == nil || ctx.Protocol == imgpkg.ProtoOff {
		out.Lines = append(out.Lines, renderImageFallback(b.URL))
		return
	}

	// Compute target cell dims. Without a thumb ladder we use the
	// declared image_width/image_height when present, falling back
	// to a reasonable default 16:9 aspect.
	target := computeBlockImageTarget(b, ctx, width)
	if target.X <= 0 || target.Y <= 0 {
		out.Lines = append(out.Lines, renderImageFallback(b.URL))
		return
	}

	rowStart := len(out.Lines)
	lines, flushes, sxlMap, hit, ok := fetchOrPlaceholder(b.URL, target, ctx, rowStart)
	if !ok {
		out.Lines = append(out.Lines, renderImageFallback(b.URL))
		return
	}
	out.Lines = append(out.Lines, lines...)
	out.Flushes = append(out.Flushes, flushes...)
	if sxlMap != nil {
		if out.SixelRows == nil {
			out.SixelRows = map[int]SixelEntry{}
		}
		for k, v := range sxlMap {
			out.SixelRows[k] = v
		}
	}
	if hit.RowEnd > hit.RowStart {
		out.Hits = append(out.Hits, hit)
	}
}
```

`computeBlockImageTarget`, `fetchOrPlaceholder`, `getMessagePrimary`, and `getMessageBg` need to exist. Add to `image.go`:

```go
import (
	"context"
	"image"
	"io"
	"log"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)

// inflightURL tracks per-URL fetches across all blockkit Render
// calls. The image package's Fetcher uses singleflight internally,
// but each goroutine still independently fires Context.SendMsg, and
// we want at most one in flight per URL so post-fetch invalidation
// only triggers once.
var (
	inflightURL   = map[string]struct{}{}
	inflightURLMu sync.Mutex
)

// computeBlockImageTarget chooses (cols, rows) for an image block
// render. Width-bounded; height capped at ctx.MaxRows (default 20).
// Aspect ratio derived from b.Width/b.Height when present, else 16:9.
func computeBlockImageTarget(b ImageBlock, ctx Context, width int) image.Point {
	if ctx.CellPixels.X <= 0 || ctx.CellPixels.Y <= 0 {
		return image.Point{}
	}
	aspect := 16.0 / 9.0
	if b.Width > 0 && b.Height > 0 {
		aspect = float64(b.Width) / float64(b.Height)
	}
	cellRatio := float64(ctx.CellPixels.X) / float64(ctx.CellPixels.Y)
	maxRows := ctx.MaxRows
	if maxRows <= 0 {
		maxRows = 20
	}
	maxCols := width
	if ctx.MaxCols > 0 && ctx.MaxCols < maxCols {
		maxCols = ctx.MaxCols
	}
	rows := maxRows
	cols := int(float64(rows) * aspect / cellRatio)
	if cols > maxCols {
		cols = maxCols
		rows = int(float64(cols) * cellRatio / aspect)
	}
	if rows < 1 || cols < 1 {
		return image.Point{}
	}
	return image.Pt(cols, rows)
}

// fetchOrPlaceholder is the core fetch+render flow shared by image
// blocks and image accessories. Returns rendered lines + flushes +
// sixelRows + hit rect; the bool is true if work was done (lines
// returned), false if the caller should fall back (e.g., bad URL).
func fetchOrPlaceholder(url string, target image.Point, ctx Context, rowStart int) (
	[]string, []func(io.Writer) error, map[int]SixelEntry, HitRect, bool,
) {
	if url == "" {
		return nil, nil, nil, HitRect{}, false
	}
	key := urlCacheKey(url)
	pixelTarget := image.Pt(target.X*ctx.CellPixels.X, target.Y*ctx.CellPixels.Y)

	hit := HitRect{
		RowStart: rowStart,
		RowEnd:   rowStart + target.Y,
		ColStart: 0,
		ColEnd:   target.X,
		URL:      url,
	}

	img, cached := ctx.Fetcher.Cached(key, pixelTarget)
	if !cached {
		// Spawn one fetcher per URL.
		inflightURLMu.Lock()
		_, busy := inflightURL[key]
		if !busy {
			inflightURL[key] = struct{}{}
		}
		inflightURLMu.Unlock()
		if !busy {
			channel := ctx.Channel
			ts := ctx.MessageTS
			send := ctx.SendMsg
			go func() {
				_, err := ctx.Fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
					Key: key, URL: url, Target: pixelTarget,
				})
				inflightURLMu.Lock()
				delete(inflightURL, key)
				inflightURLMu.Unlock()
				if err != nil {
					log.Printf("blockkit image fetch failed: key=%s url=%s err=%v", key, url, err)
					return
				}
				if send != nil {
					send(BlockImageReadyMsg{Channel: channel, TS: ts, URL: url})
				}
			}()
		}
		return blockPlaceholder(target), nil, nil, hit, true
	}

	// Cached: render via active protocol.
	if ctx.Protocol == imgpkg.ProtoKitty && ctx.KittyRender != nil {
		ckey := "BK-" + key
		ctx.KittyRender.SetSource(ckey, img)
		out := ctx.KittyRender.RenderKey(ckey, target)
		var fl []func(io.Writer) error
		if out.OnFlush != nil {
			fl = []func(io.Writer) error{out.OnFlush}
		}
		return out.Lines, fl, nil, hit, true
	}
	out := imgpkg.RenderImage(ctx.Protocol, img, target)
	var fl []func(io.Writer) error
	var sxlMap map[int]SixelEntry
	if ctx.Protocol == imgpkg.ProtoSixel && out.OnFlush != nil {
		// Capture sixel bytes once.
		var bb [1024]byte
		_ = bb // placeholder; actual capture in Phase 5 integration if needed
		// For now, surface OnFlush as a regular flush. This path is
		// less optimal than the file-attachment path's sentinel
		// mechanism but works correctly; Phase 5 may upgrade.
		fl = []func(io.Writer) error{out.OnFlush}
	} else if out.OnFlush != nil {
		fl = []func(io.Writer) error{out.OnFlush}
	}
	return out.Lines, fl, sxlMap, hit, true
}

// blockPlaceholder produces a target.Y-row block of theme-surface-
// colored spaces, with a single "⏳ loading…" cell on the middle row.
func blockPlaceholder(target image.Point) []string {
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark)
	pad := strings.Repeat(" ", target.X)
	row := bg.Render(pad)
	out := make([]string, target.Y)
	for i := range out {
		out[i] = row
	}
	if target.Y > 0 {
		mid := target.Y / 2
		label := "⏳ loading…"
		w := lipgloss.Width(label)
		if w <= target.X {
			leftPad := (target.X - w) / 2
			out[mid] = bg.Render(strings.Repeat(" ", leftPad)) + bg.Render(label) +
				bg.Render(strings.Repeat(" ", target.X-leftPad-w))
		}
	}
	return out
}

func getMessagePrimary() lipgloss.TerminalColor { return styles.Primary }
func getMessageBg() lipgloss.TerminalColor      { return styles.Background }

// BlockImageReadyMsg is dispatched by the prefetcher when a block
// image has finished downloading. The host's Update handler wires
// this to a render-cache invalidation for the matching message.
type BlockImageReadyMsg struct {
	Channel string
	TS      string
	URL     string
}
```

**Important:** The `_ = imgpkg.ProtoOff` etc. lines from Step 2 should be REMOVED — `image.go` now uses these imports for real.

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/messages/blockkit/ -v
```

Expected: the two new tests PASS (they hit the no-fetcher fallback). Prior tests still PASS.

- [ ] **Step 5: Run `make build`**

If you see `lipgloss.TerminalColor: undefined` or similar, check the lipgloss v2 API for the correct return type. The styles package's color vars are typed as the lipgloss color interface; the simplest fix is to declare `getMessagePrimary` etc. with the same return type the styles package uses. Check `internal/ui/styles/styles.go` line 238 for the type — it's `lipgloss.Color`. Update the helper signatures accordingly.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): render image blocks via image pipeline with URL-keyed cache"
```

---

## Task 11: Render section image accessory (small thumbnail)

**Files:**
- Modify: `internal/ui/messages/blockkit/render.go`
- Modify: `internal/ui/messages/blockkit/render_test.go`

The image accessory is a small inline image (4 rows × 8 cols hard cap) placed to the right of the section body text. At narrow widths it stacks below the body. When no fetcher is configured we fall back to `[image: alt]`.

- [ ] **Step 1: Failing tests**

Append:

```go
func TestRenderSectionImageAccessoryNoFetcherFallback(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Service status",
		Accessory: ImageAccessory{URL: "https://example.com/logo.png", AltText: "logo"},
	}}, ctx, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "Service status") {
		t.Errorf("missing body: %q", plain)
	}
	if !strings.Contains(plain, "[image: logo]") {
		t.Errorf("expected '[image: logo]' fallback, got %q", plain)
	}
}
```

- [ ] **Step 2: Run, see failure, then implement**

Modify `appendSection` to handle image accessories. The cleanest split is:

```go
func appendSection(out *RenderResult, s SectionBlock, ctx Context, width int) {
	bodyW := width
	var rightLines []string
	rightW := 0

	switch acc := s.Accessory.(type) {
	case LabelAccessory:
		label := renderControlLabel(acc.Kind, acc.Label)
		rightW = lipgloss.Width(label)
		rightLines = []string{label}
		out.Interactive = true
	case ImageAccessory:
		// 4 rows × 8 cols hard cap, regardless of ctx.MaxRows.
		const accRows, accCols = 4, 8
		if ctx.Fetcher == nil || ctx.Protocol == imgpkg.ProtoOff {
			label := mutedStyle().Render("[image: " + fallbackAlt(acc.AltText) + "]")
			rightLines = []string{label}
			rightW = lipgloss.Width(label)
		} else {
			target := image.Pt(accCols, accRows)
			rowStart := 0 // refined below after we know body height
			lines, flushes, sxl, hit, ok := fetchOrPlaceholder(acc.URL, target, ctx, rowStart)
			if !ok {
				label := mutedStyle().Render("[image: " + fallbackAlt(acc.AltText) + "]")
				rightLines = []string{label}
				rightW = lipgloss.Width(label)
			} else {
				rightLines = lines
				rightW = accCols
				out.Flushes = append(out.Flushes, flushes...)
				if sxl != nil {
					if out.SixelRows == nil {
						out.SixelRows = map[int]SixelEntry{}
					}
					for k, v := range sxl {
						out.SixelRows[k] = v
					}
				}
				// Hit rect rowStart adjusted by caller after body is laid out.
				out.Hits = append(out.Hits, hit) // adjusted below
			}
		}
	}

	if rightW > 0 && width >= narrowBreakpoint {
		bodyW = width - rightW - 2
		if bodyW < 10 {
			bodyW = width
			// Fall through to stacked.
			out.Lines = append(out.Lines, renderTextLines(s.Text, ctx, width)...)
			out.Lines = append(out.Lines, rightLines...)
			rightLines = nil
		}
	}

	if rightLines != nil {
		var bodyLines []string
		if s.Text != "" {
			bodyLines = renderTextLines(s.Text, ctx, bodyW)
		}
		startBody := len(out.Lines)
		if width >= narrowBreakpoint && rightW > 0 {
			out.Lines = append(out.Lines, joinSideBySide(bodyLines, rightLines, bodyW, 2)...)
		} else {
			out.Lines = append(out.Lines, bodyLines...)
			out.Lines = append(out.Lines, rightLines...)
		}
		// Adjust the last hit's rowStart if we just added one for an
		// image accessory; its rows live at startBody offset.
		if len(out.Hits) > 0 {
			h := &out.Hits[len(out.Hits)-1]
			if h.URL != "" {
				h.RowStart += startBody
				h.RowEnd += startBody
				// Shift cols by bodyW + gutter so the hit lands on
				// the right column.
				h.ColStart += bodyW + 2
				h.ColEnd += bodyW + 2
			}
		}
	} else if s.Text != "" && s.Accessory == nil {
		// No accessory at all, plain body.
		out.Lines = append(out.Lines, renderTextLines(s.Text, ctx, width)...)
	} else if s.Text != "" {
		// Accessory was rendered above by the stacked fallback path.
		// Body was already appended.
	}

	if len(s.Fields) > 0 {
		out.Lines = append(out.Lines, renderFieldsGrid(s.Fields, ctx, width)...)
	}
}

func fallbackAlt(alt string) string {
	if alt == "" {
		return "image"
	}
	return alt
}
```

Note the imports: `appendSection` now uses `image.Pt` and `imgpkg.ProtoOff` so make sure render.go imports both:

```go
import (
	"image"
	"strings"

	"charm.land/lipgloss/v2"
	imgpkg "github.com/gammons/slk/internal/image"
)
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/ui/messages/blockkit/ -v
```

Expected: all tests pass, including the new image-accessory fallback test. Side-by-side label-accessory tests from Task 7 should still pass — verify carefully because we restructured `appendSection`.

- [ ] **Step 4: Build**

```bash
make build
```

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): render section image accessory at fixed small cap"
```

---

## Phase 3 self-check

- [ ] Tasks 9, 10, 11 committed
- [ ] All package tests pass
- [ ] `make build` clean
- [ ] `go test ./... -race` passes (no regressions outside blockkit)
- [ ] No unused imports left over from `image.go`'s scaffold lines
