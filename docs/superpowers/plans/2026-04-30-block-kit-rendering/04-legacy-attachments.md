# Phase 4: Legacy Attachment Rendering

> See `00-overview.md` for goal, architecture, and conventions. Phases 1-3 must be complete.

This phase fills in `RenderLegacy`. The signature visual is the colored vertical stripe on the left, with `pretext` rendered ABOVE the stripe and `title`/`text`/`fields`/`image_url`/`thumb_url`/`footer` rendered INSIDE (i.e., to the right of) the stripe.

---

## Task 12: Render legacy attachment skeleton (color stripe + pretext + title + text + footer)

**Files:**
- Create: `internal/ui/messages/blockkit/color.go`
- Create: `internal/ui/messages/blockkit/color_test.go`
- Modify: `internal/ui/messages/blockkit/attachments.go` (currently empty stub from Phase 1)
- Create: `internal/ui/messages/blockkit/attachments_test.go`

- [ ] **Step 1: Failing color resolution tests**

```go
// internal/ui/messages/blockkit/color_test.go
package blockkit

import "testing"

func TestResolveAttachmentColorNamedTokens(t *testing.T) {
	cases := map[string]string{
		"good":    "#2EB67D", // theme accent green-equivalent
		"warning": "",        // we accept theme-warning passthrough; just must be non-border
		"danger":  "",
	}
	for in, want := range cases {
		got := ResolveAttachmentColor(in)
		if want != "" && got != want {
			t.Errorf("ResolveAttachmentColor(%q) = %q, want %q", in, got, want)
		}
		if got == "" {
			t.Errorf("ResolveAttachmentColor(%q) returned empty", in)
		}
	}
}

func TestResolveAttachmentColorPassthroughHex(t *testing.T) {
	cases := []string{"#FF0000", "#00ff00", "#1a1a1a"}
	for _, in := range cases {
		got := ResolveAttachmentColor(in)
		if got != in {
			t.Errorf("ResolveAttachmentColor(%q) = %q, want passthrough", in, got)
		}
	}
}

func TestResolveAttachmentColorEmptyFallsBackToBorder(t *testing.T) {
	got := ResolveAttachmentColor("")
	if got == "" {
		t.Error("expected non-empty fallback color for empty input")
	}
}

func TestResolveAttachmentColorInvalidHexFallsBackToBorder(t *testing.T) {
	got := ResolveAttachmentColor("not-a-color")
	if got == "" {
		t.Error("expected fallback for invalid input")
	}
	// Must NOT just echo the bad input back.
	if got == "not-a-color" {
		t.Error("invalid input should not be returned verbatim")
	}
}
```

- [ ] **Step 2: Implement color.go**

```go
// internal/ui/messages/blockkit/color.go
package blockkit

import (
	"regexp"

	"github.com/gammons/slk/internal/ui/styles"
)

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ResolveAttachmentColor maps a Slack attachment-color value to a
// terminal-renderable color string suitable for use as a lipgloss
// foreground/background. It accepts:
//   - "good"     → theme accent
//   - "warning"  → theme warning
//   - "danger"   → theme error
//   - "#RRGGBB"  → passthrough
//   - anything else (including "") → theme border (subdued)
func ResolveAttachmentColor(c string) string {
	switch c {
	case "good":
		return string(styles.Accent)
	case "warning":
		return string(styles.Warning)
	case "danger":
		return string(styles.Error)
	}
	if hexColorRe.MatchString(c) {
		return c
	}
	return string(styles.Border)
}
```

`styles.Accent`/`Warning`/`Error`/`Border` are `lipgloss.Color` values, which is a string type. Verify by reading `internal/ui/styles/styles.go` lines 240-248.

- [ ] **Step 3: Run color tests**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestResolveAttachmentColor" -v
```

Expected: 4 tests PASS. (The "good" specific value test only asserts non-empty for "warning" and "danger", and asserts the actual color string for "good" — relax that assertion if your theme's Accent isn't exactly `#2EB67D`. Read your `dracula` theme in styles/themes.go to find the actual value, OR just relax the test to assert non-empty AND not-equal-to-Border.)

If the "good" assertion is flaky across themes, adjust:

```go
func TestResolveAttachmentColorNamedTokens(t *testing.T) {
	border := ResolveAttachmentColor("")
	for _, name := range []string{"good", "warning", "danger"} {
		got := ResolveAttachmentColor(name)
		if got == "" {
			t.Errorf("%q returned empty", name)
		}
		if got == border {
			t.Errorf("%q resolved to border fallback %q; expected a distinct theme color", name, got)
		}
	}
}
```

- [ ] **Step 4: Failing attachment-render tests**

```go
// internal/ui/messages/blockkit/attachments_test.go
package blockkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderLegacyEmptyReturnsZero(t *testing.T) {
	r := RenderLegacy(nil, Context{}, 80)
	if r.Height != 0 {
		t.Errorf("Height = %d, want 0", r.Height)
	}
}

func TestRenderLegacyTitleAndText(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title: "Service down",
		Text:  "checkout-svc returning 5xx",
	}}, ctx, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "Service down") {
		t.Errorf("missing title: %q", plain)
	}
	if !strings.Contains(plain, "checkout-svc returning 5xx") {
		t.Errorf("missing text: %q", plain)
	}
}

func TestRenderLegacyHasColorStripeOnEveryRow(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Color: "danger",
		Title: "T",
		Text:  "line1\nline2\nline3",
	}}, ctx, 40)
	// Every line of the rendered attachment must start with the
	// stripe glyph "█".
	for i, line := range r.Lines {
		plain := ansi.Strip(line)
		if !strings.HasPrefix(plain, "█") {
			t.Errorf("line %d does not start with stripe glyph: %q", i, plain)
		}
	}
}

func TestRenderLegacyPretextRendersAboveStripe(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Pretext: "Heads up:",
		Title:   "Inside",
	}}, ctx, 40)
	if r.Height < 2 {
		t.Fatalf("Height = %d, want >= 2", r.Height)
	}
	first := ansi.Strip(r.Lines[0])
	if !strings.Contains(first, "Heads up:") {
		t.Errorf("Lines[0] = %q, want pretext", first)
	}
	if strings.HasPrefix(first, "█") {
		t.Errorf("Lines[0] = %q, pretext must NOT have stripe", first)
	}
}

func TestRenderLegacyFooterAndTimestamp(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title:  "T",
		Footer: "Datadog",
		TS:     1700000000,
	}}, ctx, 60)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "Datadog") {
		t.Errorf("missing footer: %q", plain)
	}
	// Timestamp formatted as YYYY-MM-DD or h:mm AM/PM is acceptable;
	// loose check: a 4-digit year somewhere in the output.
	if !strings.Contains(plain, "2023") {
		t.Errorf("expected formatted timestamp '2023…' in %q", plain)
	}
}
```

- [ ] **Step 5: Implement attachments.go**

Replace the stub:

```go
// internal/ui/messages/blockkit/attachments.go
package blockkit

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// stripeGlyph is the leading character on every line inside the
// attachment's colored region.
const stripeGlyph = "█"

// stripeCol is the column count consumed by the stripe (1 visible
// glyph + 1 space gutter = 2).
const stripeCol = 2

// RenderLegacy renders a slice of legacy attachments, each as its
// own colored card. Attachments are joined with a single blank line
// between them.
func RenderLegacy(atts []LegacyAttachment, ctx Context, width int) RenderResult {
	if len(atts) == 0 || width <= 0 {
		return RenderResult{}
	}
	var out RenderResult
	for i, a := range atts {
		if i > 0 {
			out.Lines = append(out.Lines, "")
		}
		appendLegacyAttachment(&out, a, ctx, width)
	}
	out.Height = len(out.Lines)
	return out
}

func appendLegacyAttachment(out *RenderResult, a LegacyAttachment, ctx Context, width int) {
	// Pretext renders ABOVE the stripe, full width, no indent.
	if a.Pretext != "" {
		for _, line := range renderTextLines(a.Pretext, ctx, width) {
			out.Lines = append(out.Lines, line)
		}
	}

	// Determine stripe color and a styled stripe-cell renderer.
	stripeColor := ResolveAttachmentColor(a.Color)
	stripeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(stripeColor))
	contentW := width - stripeCol
	if contentW < 1 {
		contentW = 1
	}

	// Body lines (title, text, footer). Image / thumb / fields are
	// added by Task 13.
	var body []string
	if a.Title != "" {
		title := a.Title
		if a.TitleLink != "" {
			// OSC-8 hyperlink so the title is clickable.
			title = "\x1b]8;;" + a.TitleLink + "\x1b\\" + title + "\x1b]8;;\x1b\\"
		}
		titleStyle := lipgloss.NewStyle().Bold(true)
		// Wrap to contentW. If the title contains an OSC-8 escape,
		// wrapping needs to preserve it; for simplicity we truncate
		// rather than wrap — Slack titles are short.
		if lipgloss.Width(title) > contentW {
			title = truncateToWidth(title, contentW)
		}
		body = append(body, titleStyle.Render(title))
	}
	if a.Text != "" {
		body = append(body, renderTextLines(a.Text, ctx, contentW)...)
	}
	if a.Footer != "" || a.TS != 0 {
		footer := a.Footer
		if a.TS != 0 {
			ts := time.Unix(a.TS, 0).Format("2006-01-02 3:04 PM")
			if footer != "" {
				footer += " · " + ts
			} else {
				footer = ts
			}
		}
		if lipgloss.Width(footer) > contentW {
			footer = truncateToWidth(footer, contentW)
		}
		body = append(body, mutedStyle().Render(footer))
	}

	// Prefix every body line with the colored stripe + 1 col space.
	stripe := stripeStyle.Render(stripeGlyph) + " "
	for _, line := range body {
		out.Lines = append(out.Lines, stripe+line)
	}
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/ui/messages/blockkit/ -v
```

Expected: all PASS, including the four new attachment tests.

- [ ] **Step 7: Build**

```bash
make build
```

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): render legacy attachment with color stripe, title, text, footer"
```

---

## Task 13: Legacy attachment fields grid + image_url + thumb_url

**Files:**
- Modify: `internal/ui/messages/blockkit/attachments.go`
- Modify: `internal/ui/messages/blockkit/attachments_test.go`

The fields grid sits between `Text` and `Footer`. Two consecutive `Short==true` fields share a row. `Short==false` fields take a full row each.

`image_url` renders inline at the full content width using the same `fetchOrPlaceholder` flow as image blocks. `thumb_url` is currently NOT rendered (it would need a `joinSideBySide` against `Text` similar to section image accessories — out of scope for v1; mention it in a `// TODO(blockkit): render thumb_url alongside text` comment so it's discoverable for follow-up).

- [ ] **Step 1: Failing tests**

Append to `attachments_test.go`:

```go
func TestRenderLegacyFieldsGridShortPairsShareRow(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title: "T",
		Fields: []LegacyField{
			{Title: "Service", Value: "web", Short: true},
			{Title: "Region", Value: "us-east-1", Short: true},
		},
	}}, ctx, 80)
	// At width 80, two Short fields share a row → at least one
	// rendered line contains BOTH "Service" and "Region".
	foundShared := false
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Service") && strings.Contains(plain, "Region") {
			foundShared = true
			break
		}
	}
	if !foundShared {
		t.Errorf("expected Service and Region on a shared row; lines = %q",
			ansi.Strip(strings.Join(r.Lines, "\n")))
	}
}

func TestRenderLegacyFieldsGridLongFieldFullWidth(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title: "T",
		Fields: []LegacyField{
			{Title: "Notes", Value: "long form", Short: false},
			{Title: "After", Value: "more", Short: false},
		},
	}}, ctx, 80)
	// Long fields stack — no shared row.
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Notes") && strings.Contains(plain, "After") {
			t.Errorf("Notes and After should not share a row: %q", plain)
		}
	}
}

func TestRenderLegacyImageURLFallbackWhenNoFetcher(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title:    "T",
		ImageURL: "https://example.com/chart.png",
	}}, ctx, 60)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "https://example.com/chart.png") {
		t.Errorf("expected ImageURL fallback link, got %q", plain)
	}
}
```

- [ ] **Step 2: Run tests, see failure, then add the fields grid + image plumbing**

In `attachments.go`, expand `appendLegacyAttachment`. After the `Text` lines and before the footer:

```go
	// Fields grid.
	if len(a.Fields) > 0 {
		body = append(body, renderLegacyFields(a.Fields, ctx, contentW)...)
	}
	// Inline image. Uses the same fetcher path as image blocks
	// (Phase 3); falls back to a single OSC-8 link line when no
	// fetcher is configured.
	// TODO(blockkit): render thumb_url alongside text via joinSideBySide.
	if a.ImageURL != "" {
		if ctx.Fetcher == nil || ctx.Protocol == imgpkg.ProtoOff {
			body = append(body, renderImageFallback(a.ImageURL))
		} else {
			target := computeBlockImageTarget(ImageBlock{URL: a.ImageURL}, ctx, contentW)
			rowStartInBody := len(body) // relative to body slice
			lines, flushes, sxl, hit, ok := fetchOrPlaceholder(a.ImageURL, target, ctx, 0)
			if ok {
				body = append(body, lines...)
				out.Flushes = append(out.Flushes, flushes...)
				if sxl != nil {
					if out.SixelRows == nil {
						out.SixelRows = map[int]SixelEntry{}
					}
					for k, v := range sxl {
						out.SixelRows[k] = v
					}
				}
				// We'll fix the hit row offset after stripe-prefix loop.
				_ = rowStartInBody
				_ = hit
				out.Hits = append(out.Hits, hit)
			} else {
				body = append(body, renderImageFallback(a.ImageURL))
			}
		}
	}
```

`imgpkg` needs to be imported in `attachments.go`. Add to its imports:

```go
import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	imgpkg "github.com/gammons/slk/internal/image"
)
```

After the stripe-prefix loop, adjust the most recently appended hit's RowStart/RowEnd to account for the pretext rows + stripe prefix offset:

```go
	// (old) Prefix every body line with the colored stripe + 1 col space.
	stripe := stripeStyle.Render(stripeGlyph) + " "
	startRow := len(out.Lines)
	for _, line := range body {
		out.Lines = append(out.Lines, stripe+line)
	}
	// Adjust any image hits added by this attachment so their rows
	// are absolute within out.Lines, and their cols account for the
	// stripeCol prefix.
	for i := len(out.Hits) - 1; i >= 0; i-- {
		h := &out.Hits[i]
		if h.URL == "" || h.URL != a.ImageURL {
			break
		}
		h.RowStart += startRow
		h.RowEnd += startRow
		h.ColStart += stripeCol
		h.ColEnd += stripeCol
	}
```

Add the fields helper:

```go
// renderLegacyFields lays out attachment fields. Two consecutive
// Short==true fields share a row; non-short fields take their own.
func renderLegacyFields(fields []LegacyField, ctx Context, width int) []string {
	var out []string
	i := 0
	for i < len(fields) {
		f := fields[i]
		if f.Short && i+1 < len(fields) && fields[i+1].Short {
			// Two-up.
			gutter := 2
			colW := (width - gutter) / 2
			if colW < 1 {
				colW = 1
			}
			left := renderLegacyField(f, ctx, colW)
			right := renderLegacyField(fields[i+1], ctx, colW)
			out = append(out, joinSideBySide(left, right, colW, gutter)...)
			i += 2
			continue
		}
		out = append(out, renderLegacyField(f, ctx, width)...)
		i++
	}
	return out
}

func renderLegacyField(f LegacyField, ctx Context, width int) []string {
	var out []string
	if f.Title != "" {
		title := fieldLabelStyle().Render(f.Title)
		if lipgloss.Width(title) > width {
			title = truncateToWidth(title, width)
		}
		out = append(out, title)
	}
	if f.Value != "" {
		out = append(out, renderTextLines(f.Value, ctx, width)...)
	}
	return out
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/ui/messages/blockkit/ -v
```

Expected: all PASS. Pay attention to the "color stripe on every row" test — fields rows must also start with `█`.

- [ ] **Step 4: Build**

```bash
make build
```

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/blockkit/
git commit -m "feat(blockkit): legacy attachment fields grid and image_url rendering"
```

---

## Phase 4 self-check

- [ ] Tasks 12 and 13 committed
- [ ] All blockkit tests PASS
- [ ] `make build` clean
- [ ] No regressions: `go test ./... -race`
- [ ] All attachment lines start with the stripe glyph except for pretext rows (which precede the stripe)
