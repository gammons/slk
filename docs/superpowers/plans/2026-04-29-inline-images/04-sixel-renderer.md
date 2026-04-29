# Phase 4: Sixel Renderer

> Index: `00-overview.md`. Previous: `03-kitty-renderer.md`. Next: `05-inline-images-messages-pane.md`.

**Goal:** Implement the sixel renderer using `github.com/mattn/go-sixel`. The sixel `Lines` slice contains a zero-width sentinel marker on row 0 (signaling "emit sixel here") plus pure spaces; the messages pane in Phase 6 recognizes the sentinel and emits the bytes. A pre-computed half-block `Fallback` is generated alongside for partial-visibility frames.

**Spec sections covered:** Renderer (Sixel).

---

## Task 4.1: Add `mattn/go-sixel` dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the dep**

Run: `go get github.com/mattn/go-sixel && go mod tidy`

- [ ] **Step 2: Verify it builds**

Run: `go build ./...` → success.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add github.com/mattn/go-sixel dependency"
```

---

## Task 4.2: Sentinel rune constant

**Files:**
- Modify: `internal/image/renderer.go` (add constants)

- [ ] **Step 1: Append to `internal/image/renderer.go`**

```go
// SixelSentinel is a private-use codepoint reserved for slk to mark a row
// in viewEntry.Lines that should trigger a sixel byte stream emission
// during messages-pane rendering. The character is U+E0001 (LANGUAGE TAG),
// chosen because no terminal renders it with a glyph; it is effectively
// zero-width and ignored by selection/copy.
const SixelSentinel = '\U000E0001'
```

- [ ] **Step 2: Commit**

```bash
git add internal/image/renderer.go
git commit -m "feat(image): reserve SixelSentinel codepoint"
```

---

## Task 4.3: Sixel renderer

**Files:**
- Create: `internal/image/sixel.go`
- Create: `internal/image/sixel_test.go`
- Modify: `internal/image/renderer.go` (replace `sixelRenderer` package var)

- [ ] **Step 1: Write the failing test**

Create `internal/image/sixel_test.go`:

```go
package image

import (
	"bytes"
	"image"
	imgcolor "image/color"
	"strings"
	"testing"
)

func TestSixel_RenderShape(t *testing.T) {
	src := makeSolid(40, 20, imgcolor.RGBA{200, 100, 50, 255})
	r := NewSixelRenderer()
	out := r.Render(src, image.Pt(20, 5))

	if out.Cells != image.Pt(20, 5) {
		t.Errorf("Cells: got %v, want (20,5)", out.Cells)
	}
	if len(out.Lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(out.Lines))
	}
	// Row 0 must contain the sentinel.
	if !strings.ContainsRune(out.Lines[0], SixelSentinel) {
		t.Errorf("row 0 missing sentinel: %q", out.Lines[0])
	}
	// Subsequent rows are pure spaces of width 20.
	for i := 1; i < len(out.Lines); i++ {
		if strings.ContainsRune(out.Lines[i], SixelSentinel) {
			t.Errorf("row %d has unexpected sentinel", i)
		}
	}
	// Fallback is a half-block render with the same height.
	if len(out.Fallback) != 5 {
		t.Errorf("Fallback len got %d, want 5", len(out.Fallback))
	}
}

func TestSixel_OnFlushWritesSixelBytes(t *testing.T) {
	src := makeSolid(20, 20, imgcolor.RGBA{0, 255, 0, 255})
	r := NewSixelRenderer()
	out := r.Render(src, image.Pt(10, 5))

	if out.OnFlush == nil {
		t.Fatal("expected OnFlush set")
	}
	var buf bytes.Buffer
	if err := out.OnFlush(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	// Sixel byte stream starts with DCS \x1bP and ends with ST \x1b\\.
	if !strings.HasPrefix(s, "\x1bP") {
		t.Errorf("expected sixel DCS prefix \\eP, got %q", s[:min(20, len(s))])
	}
	if !strings.HasSuffix(s, "\x1b\\") {
		t.Errorf("expected ST suffix")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestSixel`
Expected: build error — `NewSixelRenderer` undefined.

- [ ] **Step 3: Implement**

Create `internal/image/sixel.go`:

```go
package image

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"strings"

	gosixel "github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
)

// SixelRenderer encodes images as DEC sixel byte streams.
type SixelRenderer struct{}

// NewSixelRenderer returns a stateless sixel renderer.
func NewSixelRenderer() *SixelRenderer {
	return &SixelRenderer{}
}

// Render emits a Render whose Lines contain a sentinel marker on row 0;
// the messages-pane line writer recognizes the sentinel and emits the
// sixel byte stream via OnFlush only when the image fits fully on screen.
// Otherwise, the Fallback (half-block) Lines are emitted instead.
func (s *SixelRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		return Render{Cells: target}
	}

	// Resize to a pixel size that produces clean cell-aligned sixel.
	pxW := target.X * 8
	pxH := target.Y * 16
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	var sx bytes.Buffer
	enc := gosixel.NewEncoder(&sx)
	if err := enc.Encode(resized); err != nil {
		// Encoding failed → fall back entirely to half-block.
		return HalfBlockRenderer{}.Render(img, target)
	}
	sixelBytes := sx.Bytes()

	// Half-block fallback (same target size).
	hb := HalfBlockRenderer{}.Render(img, target)

	// Build Lines: row 0 = sentinel + spaces, rows 1..N-1 = pure spaces.
	lines := make([]string, target.Y)
	rowSpaces := strings.Repeat(" ", target.X)
	lines[0] = string(SixelSentinel) + strings.Repeat(" ", target.X-1)
	if target.X < 1 {
		lines[0] = string(SixelSentinel)
	}
	for i := 1; i < target.Y; i++ {
		lines[i] = rowSpaces
	}

	bs := sixelBytes
	return Render{
		Cells:    target,
		Lines:    lines,
		Fallback: hb.Lines,
		OnFlush: func(w io.Writer) error {
			_, err := w.Write(bs)
			return err
		},
	}
}

// _ used to silence unused-import warnings during partial development.
var _ = fmt.Sprintf
```

(Remove the `_ = fmt.Sprintf` line if `fmt` is unused — `go vet` will catch.)

- [ ] **Step 4: Wire into the dispatcher**

Modify `internal/image/renderer.go`:

```go
var (
	registry      = NewRegistry()
	kittyRenderer Renderer = NewKittyRenderer(registry)
	sixelRenderer Renderer = NewSixelRenderer()
)
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/image/... -v`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/image/sixel.go internal/image/sixel_test.go internal/image/renderer.go
git commit -m "feat(image): add sixel renderer with halfblock fallback"
```

---

## Phase 4 done

The sixel renderer is wired in. `RenderImage(ProtoSixel, …)` produces a `Render` with sentinel-marked rows + a halfblock `Fallback`. The messages pane in Phase 6 will learn to honor both.

**Verify:**
```bash
go test ./internal/image/... -v
```

Continue to `05-inline-images-messages-pane.md`.
