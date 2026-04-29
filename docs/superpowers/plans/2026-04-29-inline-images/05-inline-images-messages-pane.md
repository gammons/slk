# Phase 5: Inline Image Rendering in the Messages Pane

> Index: `00-overview.md`. Previous: `04-sixel-renderer.md`. Next: `06-wire-kitty-sixel.md`.

**Goal:** Wire the `internal/image` package into the messages pane. Extend `Attachment` with image metadata, populate it from `slack.File`, render image attachments inline (using the half-block path only at this step), reserve their height during loading, and prefetch them lazily as messages enter the viewport. After this phase, every terminal sees inline halfblock images for all visible message attachments.

**Spec sections covered:** Attachment struct, renderMessagePlain integration, viewEntry cache, lazy-load coordination, config additions.

---

## Task 5.1: Config keys

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Find the existing `Appearance` and `Cache` struct definitions**

Run: `grep -n "type Appearance\|type Cache" internal/config/config.go`

- [ ] **Step 2: Add the new keys**

Modify the structs (exact field placement: keep alphabetical or grouped by topic, matching the file's existing style):

```go
type Appearance struct {
	// ... existing fields ...
	ImageProtocol string `toml:"image_protocol"` // "auto" | "kitty" | "sixel" | "halfblock" | "off"
	MaxImageRows  int    `toml:"max_image_rows"` // cap inline image height in rows
}

type Cache struct {
	// ... existing fields ...
	MaxImageCacheMB int64 `toml:"max_image_cache_mb"`
}
```

In the defaults function (probably `defaultConfig()` or similar), add:

```go
Appearance: Appearance{
    // ... existing defaults ...
    ImageProtocol: "auto",
    MaxImageRows:  20,
},
Cache: Cache{
    // ... existing defaults ...
    MaxImageCacheMB: 200,
},
```

- [ ] **Step 3: Add tests**

In `internal/config/config_test.go`, append:

```go
func TestConfig_ImageDefaults(t *testing.T) {
	c := defaultConfig() // or whatever the constructor is
	if c.Appearance.ImageProtocol != "auto" {
		t.Errorf("default image_protocol: got %q want %q", c.Appearance.ImageProtocol, "auto")
	}
	if c.Appearance.MaxImageRows != 20 {
		t.Errorf("default max_image_rows: got %d want 20", c.Appearance.MaxImageRows)
	}
	if c.Cache.MaxImageCacheMB != 200 {
		t.Errorf("default max_image_cache_mb: got %d want 200", c.Cache.MaxImageCacheMB)
	}
}

func TestConfig_ImageOverrides(t *testing.T) {
	toml := `
[appearance]
image_protocol = "halfblock"
max_image_rows = 10

[cache]
max_image_cache_mb = 50
`
	c, err := loadConfigFromString(toml) // or however the codebase loads from string
	if err != nil { t.Fatal(err) }
	if c.Appearance.ImageProtocol != "halfblock" {
		t.Errorf("image_protocol: got %q", c.Appearance.ImageProtocol)
	}
	if c.Appearance.MaxImageRows != 10 {
		t.Errorf("max_image_rows: got %d", c.Appearance.MaxImageRows)
	}
	if c.Cache.MaxImageCacheMB != 50 {
		t.Errorf("max_image_cache_mb: got %d", c.Cache.MaxImageCacheMB)
	}
}
```

If `loadConfigFromString` doesn't exist in the codebase, find the existing test pattern (likely `loadConfigFromBytes` or similar) and use it.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config/... -v`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add image_protocol, max_image_rows, max_image_cache_mb"
```

---

## Task 5.2: Extend the `Attachment` struct

**Files:**
- Modify: `internal/ui/messages/model.go` (around line 36–43)

- [ ] **Step 1: Locate the struct**

Run: `grep -n "^type Attachment" internal/ui/messages/model.go`

- [ ] **Step 2: Extend the struct**

Replace the existing definition (model.go:36-43) with:

```go
// Attachment is a Slack file attached to a message.
type Attachment struct {
	Kind string // "image" or "file"
	Name string // display filename / title
	URL  string // permalink (preferred) or url_private

	// Populated only for Kind == "image":
	FileID string      // Slack file ID for cache key
	Mime   string      // e.g. "image/png"
	Thumbs []ThumbSpec // sorted ascending; empty for non-image
}

// ThumbSpec is one Slack thumbnail variant.
type ThumbSpec struct {
	URL string
	W   int
	H   int
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: success (no callers reference the new fields yet).

- [ ] **Step 4: Commit**

```bash
git add internal/ui/messages/model.go
git commit -m "feat(messages): extend Attachment with image metadata"
```

---

## Task 5.3: Populate thumbs in `extractAttachments`

**Files:**
- Modify: `cmd/slk/main.go` (around line 958-975)

- [ ] **Step 1: Locate `extractAttachments`**

Run: `grep -n "func extractAttachments" cmd/slk/main.go`

- [ ] **Step 2: Update to populate the new fields**

Find the existing `extractAttachments` and modify the body to also populate `FileID`, `Mime`, and `Thumbs`. Sketch:

```go
func extractAttachments(files []slack.File) []messages.Attachment {
    out := make([]messages.Attachment, 0, len(files))
    for _, f := range files {
        kind := "file"
        if strings.HasPrefix(f.Mimetype, "image/") {
            kind = "image"
        }
        att := messages.Attachment{
            Kind: kind,
            Name: f.Name,
            URL:  pickAttachmentURL(f, kind),
        }
        if kind == "image" {
            att.FileID = f.ID
            att.Mime = f.Mimetype
            att.Thumbs = collectThumbs(f)
        }
        out = append(out, att)
    }
    return out
}

func collectThumbs(f slack.File) []messages.ThumbSpec {
    var out []messages.ThumbSpec
    add := func(url string, w, h int) {
        if url != "" && w > 0 && h > 0 {
            out = append(out, messages.ThumbSpec{URL: url, W: w, H: h})
        }
    }
    add(f.Thumb360, f.Thumb360W, f.Thumb360H)
    add(f.Thumb480, f.Thumb480W, f.Thumb480H)
    add(f.Thumb720, f.Thumb720W, f.Thumb720H)
    add(f.Thumb960, f.Thumb960W, f.Thumb960H)
    add(f.Thumb1024, f.Thumb1024W, f.Thumb1024H)
    return out
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./...`
Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(slk): populate Attachment image metadata in extractAttachments"
```

---

## Task 5.4: Construct image package wiring at startup

**Files:**
- Modify: `cmd/slk/main.go`

The Phase 2 wiring left a hardcoded `imageCache` cap of 200; replace with the config value. Also instantiate the `Renderer`-related state.

- [ ] **Step 1: Update `main.go`**

Near the existing image cache + fetcher construction (Phase 2 wiring), update:

```go
imageCache, err := imgpkg.NewCache(imagesDir, cfg.Cache.MaxImageCacheMB)
// ... rest as Phase 2 ...

// New: protocol + cell metrics + renderer-bound state.
proto := imgpkg.Detect(imgpkg.CaptureEnv(), cfg.Appearance.ImageProtocol)

// Optional: kitty version probe. Run only if proto == ProtoKitty AND stdin is a TTY.
// If the probe fails, downgrade to halfblock.
if proto == imgpkg.ProtoKitty && term.IsTerminal(int(os.Stdin.Fd())) {
    // Briefly enter raw mode to read the reply.
    state, err := term.MakeRaw(int(os.Stdin.Fd()))
    if err == nil {
        ok := imgpkg.ProbeKittyGraphics(os.Stdout, os.Stdin, 200*time.Millisecond)
        term.Restore(int(os.Stdin.Fd()), state)
        if !ok {
            log.Println("kitty probe failed, downgrading to halfblock")
            proto = imgpkg.ProtoHalfBlock
        }
    }
}
log.Printf("image protocol: %s", proto)

// Cell metrics for sizing decisions.
pxW, pxH := imgpkg.CellPixels(int(os.Stdout.Fd()))
log.Printf("cell pixels: %dx%d", pxW, pxH)
```

You'll need imports:

```go
"os"
"time"
"golang.org/x/term"
```

If `golang.org/x/term` isn't in `go.mod`: `go get golang.org/x/term && go mod tidy`.

- [ ] **Step 2: Pass into messages model constructor**

Find the existing `messages.New(...)` call and extend its options. The exact signature change is detailed in Task 5.6. For now, store these locals.

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add cmd/slk/main.go go.mod go.sum
git commit -m "feat(slk): detect image protocol at startup with kitty probe"
```

---

## Task 5.5: Extend `viewEntry` and add `renderAttachmentBlock`

**Files:**
- Modify: `internal/ui/messages/model.go`

- [ ] **Step 1: Extend `viewEntry`**

Find `type viewEntry struct` (around line 67-84) and add:

```go
type viewEntry struct {
    linesNormal   []string
    linesSelected []string
    linesPlain    []string
    height        int
    msgIdx        int

    // New: per-frame side effects (kitty image uploads).
    flushes []func(io.Writer) error
    // New: rows that contain a sixel sentinel; key = absolute row index
    // within linesNormal. The bytes here are emitted instead of the line
    // text when fully visible; otherwise the fallback row replaces it.
    sixelRows map[int]sixelEntry
}

type sixelEntry struct {
    bytes    []byte
    fallback string // halfblock-equivalent text for partial-visibility frames
    height   int    // image height in rows (so visibility can be checked)
}
```

Add `"io"` to imports if needed.

- [ ] **Step 2: Add `renderAttachmentBlock`**

Below `renderMessagePlain`, add:

```go
// imageContext is everything renderAttachmentBlock needs from the model.
type imageContext struct {
    Protocol     imgpkg.Protocol
    Renderer     imgpkg.Renderer // typically nil; we use RenderImage directly
    Fetcher      *imgpkg.Fetcher
    KittyRender  *imgpkg.KittyRenderer
    CellPixels   image.Point
    MaxRows      int
    AvailWidth   int // width remaining for image content
    Theme        ThemeColors // adapt to the existing type
}

// renderAttachmentBlock returns the rows to emit for one attachment.
// Returns (lines, flushes, sixelRows, height).
func (m *Model) renderAttachmentBlock(att Attachment, ctx imageContext, baseRow int) (
    lines []string, flushes []func(io.Writer) error, sixelRows map[int]sixelEntry, height int,
) {
    // Non-image: keep the existing single-line OSC 8 hyperlink.
    if att.Kind != "image" || ctx.Protocol == imgpkg.ProtoOff {
        l := renderTextAttachment(att) // wraps the existing RenderAttachments logic for one item
        return []string{l}, nil, nil, 1
    }

    // Compute target cells.
    target := computeImageTarget(att, ctx)
    if target.X <= 0 || target.Y <= 0 {
        l := renderTextAttachment(att)
        return []string{l}, nil, nil, 1
    }

    // Lookup decoded image in the fetcher's cache.
    fr, cached := tryFetchedImage(ctx.Fetcher, att, target, ctx.CellPixels)
    if !cached {
        // Reserved-height placeholder.
        ph := buildPlaceholder(att.Name, target, ctx.Theme)
        // Kick off async fetch (fire-and-forget; the prefetcher coordinates this too).
        go prefetchImage(ctx.Fetcher, att, target, ctx.CellPixels)
        return ph, nil, nil, target.Y
    }

    // Bind source for kitty's stable-key path.
    if ctx.Protocol == imgpkg.ProtoKitty && ctx.KittyRender != nil {
        ctx.KittyRender.SetSource("F-"+att.FileID, fr.Img)
        out := ctx.KittyRender.RenderKey("F-"+att.FileID, target)
        return out.Lines, flushFromRender(out), nil, target.Y
    }

    out := imgpkg.RenderImage(ctx.Protocol, fr.Img, target)
    var sixelRowsMap map[int]sixelEntry
    if ctx.Protocol == imgpkg.ProtoSixel && out.OnFlush != nil {
        var bb bytes.Buffer
        if err := out.OnFlush(&bb); err == nil {
            sixelRowsMap = map[int]sixelEntry{
                baseRow: {bytes: bb.Bytes(), fallback: strings.Join(out.Fallback, "\n"), height: target.Y},
            }
        }
    }
    return out.Lines, flushFromRender(out), sixelRowsMap, target.Y
}

// flushFromRender wraps OnFlush in a slice; nil-safe.
func flushFromRender(r imgpkg.Render) []func(io.Writer) error {
    if r.OnFlush == nil {
        return nil
    }
    return []func(io.Writer) error{r.OnFlush}
}

// computeImageTarget chooses (cols, rows) for the inline render.
// rows is capped at ctx.MaxRows; cols is min(message-pane available width,
// largest dimension that preserves aspect ratio).
func computeImageTarget(att Attachment, ctx imageContext) image.Point {
    if len(att.Thumbs) == 0 {
        return image.Pt(0, 0)
    }
    // Use the largest available thumb's aspect ratio.
    largest := att.Thumbs[len(att.Thumbs)-1]
    aspect := float64(largest.W) / float64(largest.H)

    rows := ctx.MaxRows
    cols := int(float64(rows) * aspect * float64(ctx.CellPixels.Y) / float64(ctx.CellPixels.X))
    if cols > ctx.AvailWidth {
        cols = ctx.AvailWidth
        rows = int(float64(cols) * float64(ctx.CellPixels.X) / (aspect * float64(ctx.CellPixels.Y)))
    }
    if rows < 1 || cols < 1 {
        return image.Pt(0, 0)
    }
    return image.Pt(cols, rows)
}

func tryFetchedImage(f *imgpkg.Fetcher, att Attachment, cellTarget, cellPx image.Point) (imgpkg.FetchResult, bool) {
    pixelTarget := image.Pt(cellTarget.X*cellPx.X, cellTarget.Y*cellPx.Y)
    url, suffix := imgpkg.PickThumb(toImgThumbs(att.Thumbs), pixelTarget)
    if url == "" {
        return imgpkg.FetchResult{}, false
    }
    key := att.FileID + "-" + suffix
    // Cache-only fetch path: if not cached, return false.
    res, err := f.Fetch(context.Background(), imgpkg.FetchRequest{Key: key, URL: url, Target: pixelTarget})
    return res, err == nil
}

func toImgThumbs(t []ThumbSpec) []imgpkg.ThumbSpec {
    out := make([]imgpkg.ThumbSpec, len(t))
    for i := range t {
        out[i] = imgpkg.ThumbSpec{URL: t[i].URL, W: t[i].W, H: t[i].H}
    }
    return out
}

// prefetchImage triggers an async fetch that fills the cache. The next
// re-render of the message will see the result via tryFetchedImage.
func prefetchImage(f *imgpkg.Fetcher, att Attachment, cellTarget, cellPx image.Point) {
    pixelTarget := image.Pt(cellTarget.X*cellPx.X, cellTarget.Y*cellPx.Y)
    url, suffix := imgpkg.PickThumb(toImgThumbs(att.Thumbs), pixelTarget)
    if url == "" {
        return
    }
    key := att.FileID + "-" + suffix
    _, _ = f.Fetch(context.Background(), imgpkg.FetchRequest{Key: key, URL: url, Target: pixelTarget})
}

// buildPlaceholder produces a target.Y-row block with theme-surface bg
// and a centered loading line.
func buildPlaceholder(name string, target image.Point, theme ThemeColors) []string {
    midRow := target.Y / 2
    row := strings.Repeat(" ", target.X)
    lines := make([]string, target.Y)
    for i := range lines {
        lines[i] = withBg(row, theme.SurfaceDark)
    }
    // Center "⏳ Loading <name>..." on midRow.
    label := "⏳ Loading " + name + "..."
    if w := lipgloss.Width(label); w > target.X {
        label = label[:target.X-1] + "…"
    }
    pad := (target.X - lipgloss.Width(label)) / 2
    lines[midRow] = withBg(strings.Repeat(" ", pad)+label+strings.Repeat(" ", target.X-pad-lipgloss.Width(label)), theme.SurfaceDark)
    return lines
}

func withBg(s string, hex string) string {
    return lipgloss.NewStyle().Background(lipgloss.Color(hex)).Render(s)
}
```

⚠️ The exact `ThemeColors` type and field names depend on the codebase. Locate the existing theme accessor (search `theme.SurfaceDark` or similar) and use the exact path used elsewhere in this file. Likewise `renderTextAttachment` is a small wrapper around the existing `RenderAttachments` for a single item — reuse the existing OSC 8 link formatting.

- [ ] **Step 3: Replace the call site in `renderMessagePlain`**

Find the existing call at `internal/ui/messages/model.go:795-798` (the `RenderAttachments(...)` invocation) and replace it with iteration:

```go
var attachLines []string
var attachFlushes []func(io.Writer) error
attachSixel := map[int]sixelEntry{}
for _, att := range msg.Attachments {
    rowOffset := len(bodyLines) + len(attachLines)
    lines, flushes, sxlRows, _ := m.renderAttachmentBlock(att, m.imgCtx, rowOffset)
    attachLines = append(attachLines, lines...)
    attachFlushes = append(attachFlushes, flushes...)
    for k, v := range sxlRows {
        attachSixel[k] = v
    }
}
```

Where `bodyLines` is the message-body slice already in scope; adapt to the actual variable names. The `m.imgCtx` field is added in the next step.

- [ ] **Step 4: Pipe results into `viewEntry` in `buildCache`**

In `buildCache` (`model.go:603-715`), where the per-message `viewEntry` is constructed, attach `flushes` and `sixelRows`:

```go
ve := viewEntry{
    linesNormal:   linesNormal,
    linesSelected: linesSelected,
    linesPlain:    linesPlain,
    height:        len(linesNormal),
    msgIdx:        i,
    flushes:       attachFlushes,
    sixelRows:     attachSixel,
}
```

(Adjust to match the actual variable names in the file.)

- [ ] **Step 5: Add `imgCtx` field on Model**

Add to the `Model` struct definition:

```go
imgCtx imageContext
```

And a setter:

```go
// SetImageContext configures the image-rendering pipeline. Call from main.go
// after constructing the messages model.
func (m *Model) SetImageContext(ctx imageContext) {
    m.imgCtx = ctx
    m.cache = nil
    m.dirty()
}
```

(Find the existing pattern for "options" — there may already be an `Options` struct used at construction. Prefer extending the existing pattern.)

- [ ] **Step 6: Wire from `cmd/slk/main.go`**

After the messages model is constructed, configure it:

```go
msgModel.SetImageContext(messages.ImageContext{
    Protocol:    proto,
    Fetcher:     imageFetcher,
    KittyRender: imgpkg.KittyRendererInstance(),
    CellPixels:  image.Pt(pxW, pxH),
    MaxRows:     cfg.Appearance.MaxImageRows,
    AvailWidth:  0, // computed dynamically per render in the model
    Theme:       theme.Colors(), // adapt
})
```

`AvailWidth` should be filled in dynamically by the model based on the current pane width — adjust `computeImageTarget` to take the available width as an argument computed during `buildCache`, not stored on the context.

- [ ] **Step 7: Verify build**

Run: `go build ./...`
Expected: success. Likely several plumbing fixes needed (unexported types, missing imports). Address them.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/model.go cmd/slk/main.go
git commit -m "feat(messages): render image attachments inline with reserved-height placeholders"
```

---

## Task 5.6: `ImageReadyMsg` + `invalidateMessage`

**Files:**
- Modify: `internal/ui/messages/model.go`

When a prefetched image lands, the model needs to invalidate just that one message's cache entry and re-render.

- [ ] **Step 1: Define the message and method**

Add near the top of `internal/ui/messages/model.go` (next to other tea.Msg types):

```go
// ImageReadyMsg is dispatched by the prefetcher when an image attachment
// has finished downloading and decoding into the cache.
type ImageReadyMsg struct {
    Channel string
    TS      string
}

// invalidateMessage drops the per-message cache for the given (channel, ts)
// and bumps the version counter so the App-level panel cache also rebuilds.
func (m *Model) invalidateMessage(channel, ts string) {
    if m.channel != channel {
        return
    }
    for i, msg := range m.messages {
        if msg.TS == ts {
            // Find the matching viewEntry and zero its cache slot.
            for j, ve := range m.cache {
                if ve.msgIdx == i {
                    m.cache[j] = viewEntry{} // force rebuild on next View()
                    break
                }
            }
            break
        }
    }
    // Simpler alternative: nil the entire cache. The price is one full
    // rebuild per image arrival, which is acceptable given the typical
    // arrival rate (a few per channel-open).
    m.cache = nil
    m.dirty()
}
```

The "simpler alternative" comment is the recommended path: drop the whole cache. It's O(N) but N is bounded by the visible window; the cache is rebuilt lazily anyway. **Use the simpler path.**

- [ ] **Step 2: Handle the message in `Update`**

In the model's `Update(msg tea.Msg)`:

```go
case ImageReadyMsg:
    m.invalidateMessage(msg.Channel, msg.TS)
    return m, nil
```

- [ ] **Step 3: Modify `prefetchImage` to dispatch the message**

Update the helper to take a tea program reference (or a callback). The cleanest path: use a small `Program` field on the model:

```go
type Model struct {
    // ...
    sendMsg func(tea.Msg) // injected by app.go
    // ...
}

func (m *Model) SetSendMsg(f func(tea.Msg)) {
    m.sendMsg = f
}
```

In app.go, after `tea.NewProgram(...)`, call `msgModel.SetSendMsg(p.Send)` (or whatever the equivalent of `Program.Send` is in this codebase).

Update `prefetchImage` to take `(channel, ts string, send func(tea.Msg))`:

```go
func prefetchImage(f *imgpkg.Fetcher, att Attachment, cellTarget, cellPx image.Point, channel, ts string, send func(tea.Msg)) {
    pixelTarget := image.Pt(cellTarget.X*cellPx.X, cellTarget.Y*cellPx.Y)
    url, suffix := imgpkg.PickThumb(toImgThumbs(att.Thumbs), pixelTarget)
    if url == "" {
        return
    }
    key := att.FileID + "-" + suffix
    if _, err := f.Fetch(context.Background(), imgpkg.FetchRequest{Key: key, URL: url, Target: pixelTarget}); err == nil && send != nil {
        send(ImageReadyMsg{Channel: channel, TS: ts})
    }
}
```

And the call site in `renderAttachmentBlock` passes `m.channel`, `msg.TS`, `m.sendMsg`. Thread them through.

- [ ] **Step 4: Verify**

Run: `go test ./...`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/app.go
git commit -m "feat(messages): wire ImageReadyMsg for lazy-load rerender"
```

---

## Task 5.7: Stable-height test

**Files:**
- Modify: `internal/ui/messages/model_test.go`

- [ ] **Step 1: Add a test**

```go
func TestImageReadyMsg_DoesNotChangeMessageHeight(t *testing.T) {
    m := newTestModel(t)
    // Inject a message with one image attachment whose thumb is 720x720.
    m.SetMessages([]MessageItem{{
        TS: "1700000000.001",
        User: "U1",
        Text: "look",
        Attachments: []Attachment{{
            Kind: "image", Name: "x.png", FileID: "F1",
            Thumbs: []ThumbSpec{{URL: "http://example/720", W: 720, H: 720}},
        }},
    }})
    // Trigger build with a fixed width.
    _ = m.View(80, 30)

    heightBefore := m.cache[0].height
    // Simulate the image arriving in cache: pre-populate the fetcher cache
    // with a known PNG at the expected key.
    prepopulateCache(t, m.imgCtx.Fetcher, "F1-720", makeTinyPNGBytes(t, 720, 720))

    // Send the ImageReadyMsg and re-render.
    m.invalidateMessage(m.channel, "1700000000.001")
    _ = m.View(80, 30)
    heightAfter := m.cache[0].height
    if heightBefore != heightAfter {
        t.Errorf("height changed across image load: before=%d after=%d", heightBefore, heightAfter)
    }
}
```

The helpers `newTestModel`, `prepopulateCache`, `makeTinyPNGBytes` need to be implemented in the test file. `prepopulateCache` calls `Cache.Put` directly with PNG bytes.

- [ ] **Step 2: Run**

Run: `go test ./internal/ui/messages/ -run TestImageReadyMsg_DoesNotChangeMessageHeight -v`
Expected: pass.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/messages/model_test.go
git commit -m "test(messages): assert stable height across image load"
```

---

## Task 5.8: Lazy-load prefetcher (visible-range coordinator)

**Files:**
- Modify: `internal/ui/messages/model.go`

Today the rendering of an image attachment kicks off a prefetch via `go prefetchImage(...)` only when that message is rendered. That means images outside the visible window aren't fetched until the user scrolls them in (which is fine), and the user's scroll appears responsive (image rows are placeholders during fetch).

This is the lazy-loading model. **No additional coordinator needed.** The original spec mentions a debounced viewport watcher; in practice, the per-render fire-and-forget pattern is simpler and sufficient because:
- Re-render only happens for visible messages.
- `singleflight` dedupes any duplicate concurrent fetches.
- The Cache is shared, so when the user scrolls back, hits are instant.

If profiling shows excessive fetch starts (e.g., due to repeated re-renders during scroll animations), add the debounced-viewport coordinator later as an optimization.

- [ ] **Step 1: No code changes for Task 5.8.**

Document this decision in a comment near `prefetchImage`:

```go
// Prefetching is per-render fire-and-forget: each visible message's
// image attachments are fetched on first render. singleflight + Cache
// dedupe; off-screen messages don't re-render so they don't fetch.
// A debounced-viewport coordinator was specced but not built — add later
// if profiling shows redundant fetch starts during scroll animations.
```

- [ ] **Step 2: Commit**

```bash
git add internal/ui/messages/model.go
git commit -m "docs(messages): document per-render prefetch model"
```

---

## Phase 5 done

Inline images render in halfblock on every terminal. Messages reserve the image's final height during loading, so scroll position is stable. The prefetcher is per-render fire-and-forget. iTerm2 / older kitty / xterm / alacritty all see halfblock pixels for inline images.

**Verify:**
```bash
go test ./... -v
go build ./...
```

Manual: open slk, switch to a channel with image attachments. Confirm halfblock images render.

Continue to `06-wire-kitty-sixel.md`.
