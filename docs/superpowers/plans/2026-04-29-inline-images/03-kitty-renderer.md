# Phase 3: Kitty Graphics Renderer

> Index: `00-overview.md`. Previous: `02-avatar-refactor.md`. Next: `04-sixel-renderer.md`.

**Goal:** Implement the kitty graphics renderer using the unicode-placeholder placement mode. Add an image registry to track uploaded images per session. Add a startup probe that downgrades to half-block on terminals that fail it.

**Spec sections covered:** Renderer (Kitty), Capability Detection (kitty version probe).

**Background:** The kitty graphics protocol with unicode placeholders works by transmitting an image once with `a=t,U=1,i=<ID>`, then placing it via cells of `U+10EEEE` decorated with combining-diacritic marks that encode (image_row, image_col). The terminal overlays the image on those cells. Reference: https://sw.kovidgoyal.net/kitty/graphics-protocol/#unicode-placeholders

---

## Task 3.1: Image registry

**Files:**
- Create: `internal/image/registry.go`
- Create: `internal/image/registry_test.go`

The registry mints unique kitty image IDs and tracks which images have been uploaded this session.

- [ ] **Step 1: Write the failing test**

Create `internal/image/registry_test.go`:

```go
package image

import (
	"image"
	"testing"
)

func TestRegistry_AssignsStableIDs(t *testing.T) {
	r := NewRegistry()
	id1, fresh1 := r.Lookup("file-A", image.Pt(40, 20))
	if !fresh1 {
		t.Error("expected fresh on first lookup")
	}
	if id1 == 0 {
		t.Error("expected non-zero ID")
	}

	id2, fresh2 := r.Lookup("file-A", image.Pt(40, 20))
	if fresh2 {
		t.Error("expected not fresh on repeat")
	}
	if id2 != id1 {
		t.Errorf("expected stable ID %d, got %d", id1, id2)
	}
}

func TestRegistry_DifferentSizesDifferentIDs(t *testing.T) {
	r := NewRegistry()
	a, _ := r.Lookup("file", image.Pt(40, 20))
	b, _ := r.Lookup("file", image.Pt(20, 10))
	if a == b {
		t.Error("different cell footprints should yield different IDs")
	}
}

func TestRegistry_IDsNonZero(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 10; i++ {
		id, _ := r.Lookup("k"+string(rune('A'+i)), image.Pt(1, 1))
		if id == 0 {
			t.Errorf("got zero ID at i=%d", i)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestRegistry`
Expected: build error.

- [ ] **Step 3: Implement**

Create `internal/image/registry.go`:

```go
package image

import (
	"fmt"
	"image"
	"sync"
	"sync/atomic"
)

// Registry mints stable kitty image IDs per (cache key, target cells) pair.
// Lookup returns (id, fresh): fresh is true the first time the ID is
// requested, indicating the caller must transmit the image bytes.
type Registry struct {
	next atomic.Uint32
	mu   sync.Mutex
	ids  map[string]uint32
}

// NewRegistry constructs a registry. IDs start at 1 (kitty rejects 0).
func NewRegistry() *Registry {
	r := &Registry{ids: map[string]uint32{}}
	r.next.Store(1)
	return r
}

// Lookup returns a stable ID for the given (key, target) pair.
// fresh is true on the first call for a given pair.
func (r *Registry) Lookup(key string, target image.Point) (id uint32, fresh bool) {
	k := registryKey(key, target)
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.ids[k]; ok {
		return id, false
	}
	id = r.next.Add(1)
	r.ids[k] = id
	return id, true
}

func registryKey(key string, target image.Point) string {
	return fmt.Sprintf("%s|%dx%d", key, target.X, target.Y)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/image/ -run TestRegistry -v`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/image/registry.go internal/image/registry_test.go
git commit -m "feat(image): add kitty image registry"
```

---

## Task 3.2: Kitty renderer (upload + placeholder rows)

**Files:**
- Create: `internal/image/kitty.go`
- Create: `internal/image/kitty_test.go`
- Modify: `internal/image/renderer.go` (replace `kittyRenderer` package var)

The renderer needs:
- A registry to mint IDs and track upload state.
- An upload escape encoder.
- A placeholder-row encoder.

The kitty unicode-placeholder format encodes (row, col) within the image using diacritic combining marks. Each placeholder cell is `U+10EEEE` followed by:
- A combining mark for the row index (from kitty's diacritic table).
- A combining mark for the column index.
- A combining mark for the upper byte of the image ID (only on the first cell; optional for the rest if the upper byte is 0).

The full byte sequence per row is wrapped in a 24-bit-color SGR encoding the lower 24 bits of the image ID: `\x1b[38;2;<r>;<g>;<b>m` where `r`, `g`, `b` are the three bytes of the image ID (lowest 24 bits).

The diacritic table is fixed by the kitty protocol; reproduced from kitty's `rowcolumn-diacritics.txt`.

- [ ] **Step 1: Write the failing test**

Create `internal/image/kitty_test.go`:

```go
package image

import (
	"bytes"
	"image"
	imgcolor "image/color"
	"strings"
	"testing"
)

func TestKitty_UploadEscapeFormat(t *testing.T) {
	src := makeSolid(64, 64, imgcolor.RGBA{1, 2, 3, 255})
	r := NewKittyRenderer(NewRegistry())
	out := r.Render(src, image.Pt(10, 5))

	if out.OnFlush == nil {
		t.Fatal("expected OnFlush set on first render")
	}
	if out.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	var buf bytes.Buffer
	if err := out.OnFlush(&buf); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.HasPrefix(s, "\x1b_G") {
		t.Errorf("expected \\e_G prefix, got %q", s[:min(20, len(s))])
	}
	if !strings.HasSuffix(s, "\x1b\\") {
		t.Errorf("expected \\e\\ suffix")
	}
	if !strings.Contains(s, "a=t") {
		t.Error("missing a=t (transmit)")
	}
	if !strings.Contains(s, "f=100") {
		t.Error("missing f=100 (PNG)")
	}
	if !strings.Contains(s, "U=1") {
		t.Error("missing U=1 (unicode placeholder)")
	}
}

func TestKitty_SecondRenderSameImageNoFlush(t *testing.T) {
	reg := NewRegistry()
	r := NewKittyRenderer(reg)
	src := makeSolid(8, 8, imgcolor.RGBA{1, 2, 3, 255})

	// Bind the source via the registry directly to simulate "same image".
	r.SetSource("test-key", src)
	out1 := r.RenderKey("test-key", image.Pt(4, 2))
	if out1.OnFlush == nil {
		t.Fatal("first render should flush")
	}

	out2 := r.RenderKey("test-key", image.Pt(4, 2))
	if out2.OnFlush != nil {
		t.Error("second render of same (key, size) should not flush")
	}
	if out2.ID != out1.ID {
		t.Error("ID should be stable across renders of same (key, size)")
	}
}

func TestKitty_PlaceholderRows(t *testing.T) {
	src := makeSolid(20, 20, imgcolor.RGBA{255, 255, 255, 255})
	r := NewKittyRenderer(NewRegistry())
	out := r.Render(src, image.Pt(10, 5))

	if len(out.Lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(out.Lines))
	}
	for i, line := range out.Lines {
		// Each line should contain the placeholder rune.
		if !strings.Contains(line, "\U0010EEEE") {
			t.Errorf("line %d missing placeholder rune: %q", i, line[:min(30, len(line))])
		}
		// Each line should be wrapped in an SGR foreground escape.
		if !strings.Contains(line, "\x1b[38;2;") {
			t.Errorf("line %d missing 24-bit SGR: %q", i, line[:min(30, len(line))])
		}
	}
}

func min(a, b int) int { if a < b { return a }; return b }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestKitty`
Expected: build error.

- [ ] **Step 3: Implement**

Create `internal/image/kitty.go`:

```go
package image

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	imgpng "image/png"
	"io"
	"strings"
	"sync"

	"golang.org/x/image/draw"
)

// kittyDiacritics is the 297-entry table from kitty's rowcolumn-diacritics.txt.
// Index i -> rune used to encode row-or-column index i in unicode-placeholder mode.
var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A,
	0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357, 0x035B, 0x0363, 0x0364, 0x0365,
	0x0366, 0x0367, 0x0368, 0x0369, 0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F,
	0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1, 0x05A8, 0x05A9,
	0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615,
	0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6,
	0x06D7, 0x06D8, 0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736,
	0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743, 0x0745, 0x0747, 0x0749, 0x074A,
	0x07EB, 0x07EC, 0x07ED, 0x07EE, 0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817,
	0x0818, 0x0819, 0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C, 0x082D, 0x0951,
	0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87, 0x135D, 0x135E, 0x135F, 0x17DD,
	0x193A, 0x1A17, 0x1A75, 0x1A76, 0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C,
	0x1B6B, 0x1B6D, 0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4, 0x1DC5, 0x1DC6,
	0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1, 0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5,
	0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9, 0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF,
	0x1DE0, 0x1DE1, 0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7, 0x20E9, 0x20F0,
	0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2, 0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6,
	0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA, 0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0,
	0x2DF1, 0x2DF2, 0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D, 0xA6F0, 0xA6F1,
	0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5, 0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9,
	0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED, 0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2,
	0xAAB3, 0xAAB7, 0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFB1E, 0xFE20, 0xFE21, 0xFE22,
	0xFE23, 0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187, 0x1D188,
	0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243, 0x1D244,
}

const placeholderRune = '\U0010EEEE'

// KittyRenderer encodes images via the kitty graphics protocol with
// unicode-placeholder placement.
type KittyRenderer struct {
	registry *Registry

	mu      sync.Mutex
	sources map[string]image.Image // key -> source image, used for upload bytes
}

// NewKittyRenderer constructs a kitty renderer backed by the given registry.
func NewKittyRenderer(reg *Registry) *KittyRenderer {
	return &KittyRenderer{
		registry: reg,
		sources:  map[string]image.Image{},
	}
}

// Render dispatches by content hash; for repeated calls with the same image,
// callers should use RenderKey + SetSource for stable IDs.
//
// This convenience path uses the image's bounds string as the key.
func (k *KittyRenderer) Render(img image.Image, target image.Point) Render {
	key := fmt.Sprintf("anon-%v-%dx%d", img.Bounds(), target.X, target.Y)
	k.SetSource(key, img)
	return k.RenderKey(key, target)
}

// SetSource binds a stable cache key to an image. Subsequent RenderKey calls
// with the same key reuse the registered image for upload bytes.
func (k *KittyRenderer) SetSource(key string, img image.Image) {
	k.mu.Lock()
	k.sources[key] = img
	k.mu.Unlock()
}

// RenderKey produces a Render for the given (key, target).
// On the first call for a (key, target) pair, OnFlush is set to upload the
// image bytes; subsequent calls return OnFlush=nil.
func (k *KittyRenderer) RenderKey(key string, target image.Point) Render {
	k.mu.Lock()
	src, ok := k.sources[key]
	k.mu.Unlock()
	if !ok || target.X <= 0 || target.Y <= 0 {
		return Render{Cells: target}
	}

	id, fresh := k.registry.Lookup(key, target)

	// Resize the source to the exact pixel dims we'll claim (cells × cellPx).
	pxW := target.X * 8 // estimate; over-resolution at small cells is harmless
	pxH := target.Y * 16
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), src, src.Bounds(), draw.Over, nil)

	lines := buildPlaceholderLines(id, target)

	r := Render{
		Cells:    target,
		Lines:    lines,
		Fallback: lines, // kitty handles partial visibility natively
		ID:       id,
	}
	if fresh {
		// Encode PNG once and capture for OnFlush.
		var pngBuf bytes.Buffer
		if err := imgpng.Encode(&pngBuf, resized); err == nil {
			payload := base64.StdEncoding.EncodeToString(pngBuf.Bytes())
			id := id // capture
			r.OnFlush = func(w io.Writer) error {
				return emitKittyUpload(w, id, payload)
			}
		}
	}
	return r
}

// emitKittyUpload writes the kitty transmit-and-display escape, chunking the
// base64 payload into 4096-byte segments per protocol requirement.
func emitKittyUpload(w io.Writer, id uint32, payload string) error {
	const chunk = 4096
	for i := 0; i < len(payload); i += chunk {
		end := i + chunk
		more := 1
		if end >= len(payload) {
			end = len(payload)
			more = 0
		}
		var hdr string
		if i == 0 {
			hdr = fmt.Sprintf("a=t,f=100,t=d,i=%d,U=1,q=2,m=%d", id, more)
		} else {
			hdr = fmt.Sprintf("m=%d", more)
		}
		if _, err := fmt.Fprintf(w, "\x1b_G%s;%s\x1b\\", hdr, payload[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func buildPlaceholderLines(id uint32, cells image.Point) []string {
	// SGR encodes the lower 24 bits of the image ID in fg color: r=byte0, g=byte1, b=byte2.
	r := byte(id & 0xFF)
	g := byte((id >> 8) & 0xFF)
	b := byte((id >> 16) & 0xFF)
	sgr := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
	reset := "\x1b[39m"

	lines := make([]string, cells.Y)
	for row := 0; row < cells.Y; row++ {
		var b strings.Builder
		b.WriteString(sgr)
		rowDia := diacritic(row)
		for col := 0; col < cells.X; col++ {
			b.WriteRune(placeholderRune)
			b.WriteRune(rowDia)
			b.WriteRune(diacritic(col))
		}
		b.WriteString(reset)
		lines[row] = b.String()
	}
	return lines
}

func diacritic(i int) rune {
	if i < 0 || i >= len(kittyDiacritics) {
		return kittyDiacritics[0]
	}
	return kittyDiacritics[i]
}
```

- [ ] **Step 4: Wire into the dispatcher**

Modify `internal/image/renderer.go`:

```go
// Replace the placeholder vars with these:
var (
	registry      = NewRegistry()
	kittyRenderer Renderer = NewKittyRenderer(registry)
	sixelRenderer Renderer = HalfBlockRenderer{} // overwritten in Phase 4
)

// Registry exposes the shared registry so callers can SetSource.
// (Add this getter to support callers that need to pre-bind sources.)
func KittyRendererInstance() *KittyRenderer {
	return kittyRenderer.(*KittyRenderer)
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/image/ -run TestKitty -v`
Expected: pass. The `TestKitty_PlaceholderRows` test verifies the SGR + placeholder rune appear; full diacritic-encoding correctness is hard to assert without parsing the lines, and is best left to manual verification on a real kitty terminal.

- [ ] **Step 6: Commit**

```bash
git add internal/image/kitty.go internal/image/kitty_test.go internal/image/renderer.go
git commit -m "feat(image): add kitty graphics renderer with unicode-placeholder placement"
```

---

## Task 3.3: Kitty version probe

**Files:**
- Create: `internal/image/probe.go`
- Create: `internal/image/probe_test.go`

A startup probe that confirms kitty graphics is actually working. On timeout or failure, the caller downgrades the protocol. The probe is best-effort; we don't trust it in tests so we just verify the timeout path.

The probe sends a 1-pixel image upload with `q=2` (quiet) and a status query, then waits for a reply with a short timeout. On any read error or timeout, returns `false`.

- [ ] **Step 1: Write the failing test**

Create `internal/image/probe_test.go`:

```go
package image

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProbeKittyGraphics_TimeoutFails(t *testing.T) {
	// A reader that never produces data → timeout → false.
	r := blockingReader{}
	var w bytes.Buffer
	ok := ProbeKittyGraphics(&w, r, 50*time.Millisecond)
	if ok {
		t.Error("expected probe to fail on timeout")
	}
	// Verify the probe wrote something resembling an upload escape.
	if !strings.Contains(w.String(), "\x1b_G") {
		t.Errorf("expected \\e_G in probe output, got %q", w.String())
	}
}

type blockingReader struct{}

func (blockingReader) Read(p []byte) (int, error) {
	time.Sleep(time.Hour) // effectively forever, killed by t timeout
	return 0, nil
}
```

- [ ] **Step 2: Run test to fail**

Run: `go test ./internal/image/ -run TestProbeKittyGraphics`
Expected: build error.

- [ ] **Step 3: Implement**

Create `internal/image/probe.go`:

```go
package image

import (
	"bufio"
	"fmt"
	"io"
	"time"
)

// ProbeKittyGraphics sends a tiny image upload with response requested and
// waits up to timeout for the OK reply. Returns true if the terminal
// acknowledges. Used at startup to downgrade ProtoKitty when the terminal
// claims kitty support but doesn't actually deliver (e.g., iTerm2's
// limited kitty implementation).
//
// Inputs:
//   w: terminal writer (typically os.Stdout)
//   r: terminal reader (typically os.Stdin in raw mode)
//   timeout: how long to wait for the reply
func ProbeKittyGraphics(w io.Writer, r io.Reader, timeout time.Duration) bool {
	// Minimal 1x1 PNG (fixed bytes — the literal smallest valid PNG).
	// f=100 (PNG), t=d (direct), s=1 (size), q=0 (don't suppress reply).
	// The actual base64-encoded 1x1 PNG bytes:
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+P+/HgAFhAJ/wlseKgAAAABJRU5ErkJggg=="
	const probeID = 9999
	header := fmt.Sprintf("a=T,f=100,t=d,i=%d,q=0", probeID)
	if _, err := fmt.Fprintf(w, "\x1b_G%s;%s\x1b\\", header, tinyPNG); err != nil {
		return false
	}

	type result struct {
		ok bool
	}
	ch := make(chan result, 1)
	go func() {
		br := bufio.NewReader(r)
		// Look for "\x1b_G...;OK\x1b\\".
		for {
			b, err := br.ReadByte()
			if err != nil {
				ch <- result{false}
				return
			}
			if b != 0x1b {
				continue
			}
			next, err := br.ReadByte()
			if err != nil || next != '_' {
				continue
			}
			next, err = br.ReadByte()
			if err != nil || next != 'G' {
				continue
			}
			// Read until ESC \.
			payload, err := br.ReadString(0x1b)
			if err != nil {
				ch <- result{false}
				return
			}
			ch <- result{contains(payload, ";OK")}
			return
		}
	}()

	select {
	case res := <-ch:
		return res.ok
	case <-time.After(timeout):
		return false
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

⚠️ The probe reads from the terminal. Calling it from a TUI that has already taken over the terminal in alt-screen / raw mode is delicate. The intended call site is in `cmd/slk/main.go` **before** `tea.NewProgram(...).Run()`, with the terminal in raw mode briefly. Detail: see Phase 5 wiring.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/image/ -run TestProbeKittyGraphics -v -timeout 30s`
Expected: pass within ~60 ms.

- [ ] **Step 5: Commit**

```bash
git add internal/image/probe.go internal/image/probe_test.go
git commit -m "feat(image): add kitty graphics startup probe"
```

---

## Phase 3 done

The kitty renderer is implemented and wired into `RenderImage(...)`. The startup probe is available for callers to downgrade on iTerm2 and older kitty versions.

**Verify:**
```bash
go test ./internal/image/... -v
```

Continue to `04-sixel-renderer.md`.
