# Image Pipeline Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate UI lag when slk fetches and renders images by moving all heavy work (decode, downscale, protocol encode) off the bubbletea Update goroutine and stopping full cache invalidation on every image arrival.

**Architecture:** The fetch goroutine in `Fetcher.Fetch` already decodes and downscales the image, then throws the result away. The render path (`Fetcher.Cached` called from `Model.View` → `buildCache`) re-opens the file and re-decodes synchronously on the UI thread. We will (1) have `Fetch` populate the `decoded` memo before sending `ImageReadyMsg`, so `Cached` becomes a pure map lookup; (2) precompute the per-protocol render string in a worker and stash it next to the decoded image, so `renderAttachmentBlock` doesn't run sixel/halfblock/kitty encoding on the UI thread; (3) replace the blunt `m.cache = nil` on `ImageReadyMsg` with a per-message cache invalidation so a burst of N arrivals doesn't trigger N full cache walks.

**Tech Stack:** Go 1.22+, bubbletea, golang.org/x/image/draw, golang.org/x/sync/singleflight, existing `internal/image` package (Fetcher, Cache, kitty/sixel/halfblock renderers).

**Verification baseline:** Before any code changes, all tests pass via `go test ./...`. After every task, that must remain true.

---

## File Structure

**Modified files:**
- `internal/image/fetcher.go` — populate `decoded` memo from the fetch goroutine; new `RenderCached` lookup; new `prerender` map keyed by `(key, target, protocol)`
- `internal/image/fetcher_test.go` — new tests for the prerender pipeline
- `internal/ui/messages/model.go` — `HandleImageReady` invalidates only the affected message row, not the whole cache; `renderAttachmentBlock` consumes prerendered output
- `internal/ui/messages/model_test.go` (or a new test file in the same package) — tests for scoped invalidation

**No new files** — we extend existing types rather than introduce parallel structures, since the fetcher already owns the decoded memo and is the natural home for prerendered output too.

---

## Task 1: Move decode + downscale off the UI thread

The fetch goroutine already produces a decoded, downscaled `image.Image` in `fetchInner` (`fetcher.go:175-188`) and returns it as `FetchResult.Img`, but this result is discarded by the goroutine in `renderAttachmentBlock`. The next render then calls `Fetcher.Cached`, which re-opens the file and re-decodes synchronously on the Update goroutine.

We populate the `decoded` sync.Map from `fetchInner` itself, so by the time `ImageReadyMsg` is delivered, `Cached` is a pure map hit.

**Files:**
- Modify: `internal/image/fetcher.go:136-192` (Fetch / fetchInner)
- Modify: `internal/image/fetcher_test.go` (add new test)

- [ ] **Step 1: Write the failing test**

Append this test to `internal/image/fetcher_test.go`:

```go
// After Fetch completes, Cached(key, target) must hit the in-memory
// memo without re-opening the file from disk. We assert this by
// deleting the on-disk file and confirming Cached still returns the
// image — only possible if the memo was populated by the fetch path.
func TestFetcher_FetchPopulatesDecodedMemo(t *testing.T) {
	pngBytes := tinyPNG(t, 100, 100, imgcolor.RGBA{0, 0, 200, 255})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cache, _ := NewCache(dir, 10)
	f := NewFetcher(cache, http.DefaultClient)

	target := image.Pt(20, 20)
	if _, err := f.Fetch(context.Background(), FetchRequest{
		Key: "k1", URL: srv.URL, Target: target,
	}); err != nil {
		t.Fatal(err)
	}

	// Delete the cache file. If Cached still returns true, we know the
	// memo was populated and Cached did NOT do disk I/O + decode.
	cache.Delete("k1")

	img, ok := f.Cached("k1", target)
	if !ok {
		t.Fatal("expected Cached to hit memo after Fetch, even with file deleted")
	}
	if img == nil {
		t.Fatal("expected non-nil image from memo")
	}
	if img.Bounds().Dx() != 20 || img.Bounds().Dy() != 20 {
		t.Errorf("expected 20x20 image from memo, got %v", img.Bounds())
	}
}

// Suppress unused-import lint if singleflight isn't used in this test.
var _ = sync.Mutex{}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestFetcher_FetchPopulatesDecodedMemo -v`
Expected: FAIL with "expected Cached to hit memo after Fetch, even with file deleted" (because today `fetchInner` doesn't populate `f.decoded`).

- [ ] **Step 3: Populate the decoded memo from fetchInner**

In `internal/image/fetcher.go`, modify `fetchInner` to store the decoded image in `f.decoded` before returning. Replace the existing function body (lines 155-192) with:

```go
func (f *Fetcher) fetchInner(ctx context.Context, req FetchRequest) (FetchResult, error) {
	path, hit := f.cache.Get(req.Key)
	if !hit {
		body, ct, err := f.download(ctx, req.URL)
		if err != nil {
			return FetchResult{}, err
		}
		ext := extFromMime(ct, req.URL)
		path, err = f.cache.Put(req.Key, ext, body)
		if err != nil {
			return FetchResult{}, err
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return FetchResult{}, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		// Cache poisoning recovery: a previously persisted file isn't
		// decodable as an image (e.g., an HTML auth-failure response from
		// before this auth path was wired up). Evict so the next Fetch
		// re-downloads with the now-correct credentials.
		file.Close()
		f.cache.Delete(req.Key)
		return FetchResult{}, fmt.Errorf("decode %s: %w (cache evicted)", path, err)
	}

	if req.Target.X > 0 && req.Target.Y > 0 {
		img = downscale(img, req.Target)
	}

	// Populate the render-time memo so the UI thread's Cached() call
	// becomes a pure map lookup instead of os.Open + image.Decode +
	// downscale. Critical for keeping the bubbletea Update goroutine
	// responsive when many images arrive in a burst (channel switch
	// or scroll-up into unseen history).
	f.decoded.Store(decodedMemoKey(req.Key, req.Target), img)

	mime := mimeFromExt(filepath.Ext(path))
	return FetchResult{Img: img, Source: path, Mime: mime}, nil
}
```

Also remove the now-unused `sync.Mutex{}` line at the bottom of the test file if golangci-lint complains; the test uses `sync` indirectly via t.Helper, so the import should stay.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/image/ -run TestFetcher_FetchPopulatesDecodedMemo -v`
Expected: PASS.

- [ ] **Step 5: Run the full image package tests**

Run: `go test ./internal/image/ -v`
Expected: all PASS, including the existing `TestFetcher_FreshFetchCachesAndDecodes` and any singleflight tests.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: all PASS, no regressions.

- [ ] **Step 7: Commit**

```bash
git add internal/image/fetcher.go internal/image/fetcher_test.go
git commit -m "perf(image): populate decoded memo from fetch goroutine

The fetch goroutine in fetchInner already produces a decoded,
downscaled image and returns it as FetchResult.Img. Until now the
result was thrown away by the calling goroutine, and the next View()
re-opened the cache file and re-decoded synchronously on the
bubbletea Update goroutine.

Storing the decoded image in f.decoded before returning means the
UI thread's Cached() call becomes a pure map lookup. This is the
single biggest contributor to UI lag when scrolling up to unseen
images: with N images arriving in a burst, the UI thread no longer
runs N synchronous PNG decodes + bilinear downscales."
```

---

## Task 2: Pre-render protocol output in the fetch goroutine

Even after Task 1, `renderAttachmentBlock` still runs the protocol-specific encoding (sixel `gosixel.Encode`, halfblock per-pixel ANSI build, kitty PNG encode + base64) synchronously on the Update goroutine. We move this work into the fetch goroutine and cache the resulting `Render` so the UI thread just looks it up.

**Strategy:** Add a parallel `prerendered` `sync.Map` to `Fetcher` keyed by `(key, target, protocol)`. After the fetch goroutine populates `f.decoded`, it also calls `RenderImage` (for sixel/halfblock) or the kitty path (for kitty) and stashes the result. A new `Fetcher.Prerendered(key, target, proto)` returns the cached `Render` or `(zero, false)`. `renderAttachmentBlock` checks `Prerendered` first; on miss it falls back to today's path (which after Task 1 is also fast — pure map lookup + RenderImage on the UI thread, but we want to eliminate even that for the hot post-fetch case).

Kitty is special: its `Render` carries an `OnFlush` closure that writes APC upload escapes to `KittyOutput` and depends on the `KittyRenderer` having a stable `SetSource` mapping. We pre-call `SetSource` and `RenderKey` in the worker so the heavy PNG encode + base64 happens off the UI thread.

**Files:**
- Modify: `internal/image/fetcher.go` — add prerender map, hook into fetch path, expose Prerendered/Configure methods
- Modify: `internal/image/fetcher_test.go` — new test
- Modify: `internal/ui/messages/model.go:1465-1542` — consume Prerendered when present
- Modify: `internal/ui/messages/model.go` (the `imgCtx` struct setup, search for where `KittyRender` is configured, around `SetImageContext` definition)

- [ ] **Step 1: Read the imgCtx wiring**

Run: `rg -n "imgCtx|ImageContext|SetImageContext|KittyRender" internal/ui/messages/model.go | head -40`
Read the surrounding context so the next steps name fields correctly. Make a note of: the `Context` (or `imgpkg.Context`) struct definition and which fields it carries (Protocol, Fetcher, KittyRender, CellPixels, MaxRows, MaxCols, SendMsg).

- [ ] **Step 2: Write the failing test for prerender storage**

Append this test to `internal/image/fetcher_test.go`:

```go
// After Fetch completes for a configured (proto), Prerendered must
// return a non-empty Render at the requested target so the UI thread
// doesn't have to call RenderImage synchronously.
func TestFetcher_FetchPopulatesPrerender(t *testing.T) {
	pngBytes := tinyPNG(t, 100, 100, imgcolor.RGBA{200, 0, 0, 255})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	// Configure prerender: when a fetch lands, also encode for halfblock
	// at a 10x5 cell target.
	cellTarget := image.Pt(10, 5)
	pixelTarget := image.Pt(20, 10) // 2x2 px per cell, mirrors caller convention
	f.ConfigurePrerender(ProtoHalfBlock, cellTarget, pixelTarget)

	if _, err := f.Fetch(context.Background(), FetchRequest{
		Key: "k1", URL: srv.URL, Target: pixelTarget,
	}); err != nil {
		t.Fatal(err)
	}

	r, ok := f.Prerendered("k1", cellTarget, ProtoHalfBlock)
	if !ok {
		t.Fatal("expected Prerendered to return a halfblock render after Fetch")
	}
	if len(r.Lines) != cellTarget.Y {
		t.Errorf("expected %d lines, got %d", cellTarget.Y, len(r.Lines))
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/image/ -run TestFetcher_FetchPopulatesPrerender -v`
Expected: FAIL — `ConfigurePrerender` and `Prerendered` don't exist yet.

- [ ] **Step 4: Add Prerendered storage to Fetcher**

In `internal/image/fetcher.go`, add new fields to the `Fetcher` struct (immediately after the `decoded` field, around line 69):

```go
	// prerendered caches the result of RenderImage(proto, decoded, cellTarget)
	// per (key, cellTarget, proto). The fetch goroutine populates this so
	// the bubbletea Update goroutine never runs sixel encoding, halfblock
	// per-pixel ANSI building, or kitty PNG-encode-and-base64 work.
	//
	// prerenderProto / prerenderCells / prerenderPixels capture the active
	// protocol and target sizes; consumers configure these via
	// ConfigurePrerender at startup. When unset, the fetch path skips
	// prerendering and the UI falls back to on-thread RenderImage.
	prerendered    sync.Map // string("<key>|<cw>x<ch>|<proto>") -> Render
	prerenderMu    sync.RWMutex
	prerenderProto Protocol
	prerenderCells image.Point
	prerenderPixel image.Point
	prerenderKitty *KittyRenderer // non-nil when prerenderProto == ProtoKitty
```

Add the configuration method anywhere after `SetAuths` (e.g. right before `Fetch`):

```go
// ConfigurePrerender enables eager protocol encoding in the fetch
// goroutine. After every successful Fetch the resulting decoded image
// is run through RenderImage(proto, ...) at cellTarget cells and stashed
// for retrieval via Prerendered. Pixel target is the dimensions used to
// downscale the image (matches what the caller passes as
// FetchRequest.Target). Pass ProtoOff to disable.
//
// Safe to call once at startup. Calling more than once resets the
// prerender cache (sizes/protocol may have changed, e.g. cell-pixel
// reprobe).
func (f *Fetcher) ConfigurePrerender(proto Protocol, cellTarget, pixelTarget image.Point) {
	f.prerenderMu.Lock()
	defer f.prerenderMu.Unlock()
	f.prerenderProto = proto
	f.prerenderCells = cellTarget
	f.prerenderPixel = pixelTarget
	f.prerendered = sync.Map{}
}

// ConfigurePrerenderKitty hooks a KittyRenderer into the prerender
// pipeline. Must be called when proto == ProtoKitty so the worker can
// SetSource + RenderKey on the same renderer the UI thread will look up
// from. Pass nil to clear.
func (f *Fetcher) ConfigurePrerenderKitty(kr *KittyRenderer) {
	f.prerenderMu.Lock()
	defer f.prerenderMu.Unlock()
	f.prerenderKitty = kr
}

// Prerendered returns a previously-prepared Render for (key, cellTarget,
// proto), or (zero, false) if none. Safe to call from the UI thread; pure
// map lookup, no decode, no encode.
func (f *Fetcher) Prerendered(key string, cellTarget image.Point, proto Protocol) (Render, bool) {
	mk := prerenderKey(key, cellTarget, proto)
	if v, ok := f.prerendered.Load(mk); ok {
		if r, ok := v.(Render); ok {
			return r, true
		}
	}
	return Render{}, false
}

func prerenderKey(key string, cellTarget image.Point, proto Protocol) string {
	return fmt.Sprintf("%s|%dx%d|%d", key, cellTarget.X, cellTarget.Y, proto)
}
```

Now hook it into `fetchInner` — at the very end, after `f.decoded.Store`, append:

```go
	// Eagerly run protocol encoding off the UI thread so the next
	// View() doesn't have to. Skipped when not configured.
	f.maybePrerender(req.Key, img)
```

And add the helper:

```go
// maybePrerender runs the active protocol's RenderImage on the
// just-decoded image and stashes the result. No-op when prerender is
// not configured. Called from the fetch goroutine.
func (f *Fetcher) maybePrerender(key string, img image.Image) {
	f.prerenderMu.RLock()
	proto := f.prerenderProto
	cellT := f.prerenderCells
	kr := f.prerenderKitty
	f.prerenderMu.RUnlock()

	if proto == ProtoOff || cellT.X <= 0 || cellT.Y <= 0 {
		return
	}

	var r Render
	switch proto {
	case ProtoKitty:
		if kr == nil {
			return
		}
		ckey := "F-" + key // mirrors renderAttachmentBlock's stable kitty source key
		kr.SetSource(ckey, img)
		out := kr.RenderKey(ckey, cellT)
		r = Render{
			Cells:    cellT,
			Lines:    out.Lines,
			Fallback: out.Lines, // kitty has no fallback
			OnFlush:  out.OnFlush,
			ID:       out.ID,
		}
	default:
		r = RenderImage(proto, img, cellT)
	}
	f.prerendered.Store(prerenderKey(key, cellT, proto), r)
}
```

Note: `KittyRenderer.RenderKey` returns a `Render` (verify by reading `internal/image/kitty.go` around line 91-117 if the exact field names differ; adapt the conversion above accordingly). If `RenderKey` already returns a `Render`, use it directly: `r = kr.RenderKey(ckey, cellT)`.

- [ ] **Step 5: Verify by reading kitty.go**

Run: `rg -n "func.*RenderKey|type.*Render struct" internal/image/kitty.go internal/image/renderer.go`
Confirm the exact return type of `RenderKey`. If it already returns `Render`, simplify the kitty branch in `maybePrerender` to `r = kr.RenderKey(ckey, cellT)`.

- [ ] **Step 6: Run the prerender test**

Run: `go test ./internal/image/ -run TestFetcher_FetchPopulatesPrerender -v`
Expected: PASS.

- [ ] **Step 7: Run all image-package tests**

Run: `go test ./internal/image/ -v`
Expected: all PASS.

- [ ] **Step 8: Wire ConfigurePrerender into the messages model**

The fetcher needs to know what protocol and cell target to encode for. Today the messages-pane `imgCtx` carries `Protocol`, `KittyRender`, `CellPixels`, `MaxRows`, etc. Find the function that constructs/updates `imgCtx` (look for `SetImageContext` or similar on `*Model`).

Run: `rg -n "func \(m \*Model\) SetImage|imgCtx =" internal/ui/messages/model.go`

Inside that setter, after the new context is stored, call `ConfigurePrerender` on the fetcher. **Important:** the current code computes a per-attachment `pixelTarget` based on each attachment's available width (`computeImageTarget`). For prerender, we need a sensible default. Use the most common case: the typical cell target is bounded by `ctx.MaxRows` (rows) and the channel's content width (cols). Since prerender happens at fetch time and the caller already passed `req.Target` (pixel target), we should key prerender by the **same `cellTarget` the renderer will look up at View time**.

Two implementation options:
- (A) Store cellTarget alongside the FetchRequest so the worker knows the cell extent.
- (B) Derive cellTarget from `req.Target` (pixels) ÷ `ctx.CellPixels` and persist `CellPixels` in the fetcher via ConfigurePrerender.

Use option A — cleaner and avoids the fetcher knowing about cell metrics. Modify `FetchRequest` (`fetcher.go:24-29`) to add an optional `CellTarget image.Point`:

```go
// FetchRequest describes one image fetch.
type FetchRequest struct {
	Key        string      // cache key (e.g. "F0123ABCD-720" or "avatar-U123")
	URL        string      // remote URL
	Target     image.Point // target downscale size in pixels (0 = no downscale)
	CellTarget image.Point // optional target in terminal cells; when nonzero,
	                       // the fetcher will pre-render the image into the
	                       // active prerender protocol for this cell footprint.
}
```

And update `maybePrerender` so it accepts a per-request cell target (call site passes `req.CellTarget`):

Replace the `maybePrerender` call inside `fetchInner` (Task 2 Step 4) with:

```go
	f.maybePrerender(req.Key, img, req.CellTarget)
```

And update the helper signature/body:

```go
func (f *Fetcher) maybePrerender(key string, img image.Image, cellT image.Point) {
	f.prerenderMu.RLock()
	proto := f.prerenderProto
	kr := f.prerenderKitty
	f.prerenderMu.RUnlock()

	if proto == ProtoOff || cellT.X <= 0 || cellT.Y <= 0 {
		return
	}

	var r Render
	switch proto {
	case ProtoKitty:
		if kr == nil {
			return
		}
		ckey := "F-" + key
		kr.SetSource(ckey, img)
		r = kr.RenderKey(ckey, cellT)
	default:
		r = RenderImage(proto, img, cellT)
	}
	f.prerendered.Store(prerenderKey(key, cellT, proto), r)
}
```

Drop the now-unused `prerenderCells` / `prerenderPixel` fields from the struct and from `ConfigurePrerender` (the method now only configures protocol + kitty renderer):

```go
// ConfigurePrerender enables eager protocol encoding in the fetch
// goroutine. After every successful Fetch whose request carries a
// non-zero CellTarget, the decoded image is run through
// RenderImage(proto, ..., cellTarget) and stashed for retrieval via
// Prerendered. Pass ProtoOff to disable.
//
// Safe to call at startup or whenever the active protocol changes
// (theme switch / terminal capability re-probe). Resets the prerender
// cache.
func (f *Fetcher) ConfigurePrerender(proto Protocol) {
	f.prerenderMu.Lock()
	defer f.prerenderMu.Unlock()
	f.prerenderProto = proto
	f.prerendered = sync.Map{}
}
```

Update the failing test in Step 2 to match: replace the `f.ConfigurePrerender(ProtoHalfBlock, cellTarget, pixelTarget)` line with `f.ConfigurePrerender(ProtoHalfBlock)`, and add `CellTarget: cellTarget` to the `FetchRequest`:

```go
	f.ConfigurePrerender(ProtoHalfBlock)

	if _, err := f.Fetch(context.Background(), FetchRequest{
		Key: "k1", URL: srv.URL, Target: pixelTarget, CellTarget: cellTarget,
	}); err != nil {
		t.Fatal(err)
	}
```

Re-run the test:

Run: `go test ./internal/image/ -run TestFetcher_FetchPopulatesPrerender -v`
Expected: PASS.

- [ ] **Step 9: Wire ConfigurePrerender at the messages-pane level**

In the `imgCtx` setter on `*Model` (the function you found in Step 1), after storing the new context, call:

```go
	if m.imgCtx.Fetcher != nil {
		m.imgCtx.Fetcher.ConfigurePrerender(m.imgCtx.Protocol)
		if m.imgCtx.Protocol == imgpkg.ProtoKitty {
			m.imgCtx.Fetcher.ConfigurePrerenderKitty(m.imgCtx.KittyRender)
		} else {
			m.imgCtx.Fetcher.ConfigurePrerenderKitty(nil)
		}
	}
```

The exact field names (`Fetcher`, `Protocol`, `KittyRender`) come from the `imgpkg.Context` struct you already use in `renderAttachmentBlock` (`model.go:1430-1431`).

- [ ] **Step 10: Pass CellTarget from the goroutine spawn site**

In `internal/ui/messages/model.go`, modify the goroutine in `renderAttachmentBlock` (around line 1488-1508) to include `CellTarget: target` (the cell-extent `image.Point` already computed at line 1441):

```go
		go func() {
			_, err := ctx.Fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
				Key:        key,
				URL:        url,
				Target:     pixelTarget,
				CellTarget: target,
			})
			// (remainder unchanged)
```

- [ ] **Step 11: Consume Prerendered on the cached path**

Still in `renderAttachmentBlock` (`model.go:1465-1542`), replace the cached branch (after `img, cached := ctx.Fetcher.Cached(key, pixelTarget)` returns true) with a Prerendered-first lookup:

```go
	// Fast path: prerendered output baked by the fetch goroutine off
	// the UI thread. This is the hot path post-Fetch — no protocol
	// encoding runs on the bubbletea Update goroutine.
	if pr, ok := ctx.Fetcher.Prerendered(key, target, ctx.Protocol); ok {
		var fl []func(io.Writer) error
		var sxlMap map[int]sixelEntry
		if ctx.Protocol == imgpkg.ProtoSixel && pr.OnFlush != nil {
			var bb bytes.Buffer
			if err := pr.OnFlush(&bb); err == nil {
				sxlMap = map[int]sixelEntry{
					baseRow: {bytes: bb.Bytes(), fallback: pr.Fallback, height: target.Y},
				}
			}
		} else if pr.OnFlush != nil {
			fl = []func(io.Writer) error{pr.OnFlush}
		}
		return pr.Lines, fl, sxlMap, target.Y, hit
	}

	// Slow path: prerender wasn't populated (e.g. protocol changed
	// since fetch, or this is a cache file from a previous session
	// that hasn't been refreshed through Fetch). Fall through to the
	// existing on-thread RenderImage path. Cached() already returned
	// the decoded image as a pure memo lookup (Task 1) so the
	// remaining cost is one protocol encode.
	if ctx.Protocol == imgpkg.ProtoKitty && ctx.KittyRender != nil {
		ckey := "F-" + att.FileID
		ctx.KittyRender.SetSource(ckey, img)
		out := ctx.KittyRender.RenderKey(ckey, target)
		var fl []func(io.Writer) error
		if out.OnFlush != nil {
			fl = []func(io.Writer) error{out.OnFlush}
		}
		return out.Lines, fl, nil, target.Y, hit
	}

	out := imgpkg.RenderImage(ctx.Protocol, img, target)
	var fl []func(io.Writer) error
	var sxlMap map[int]sixelEntry
	if ctx.Protocol == imgpkg.ProtoSixel {
		if out.OnFlush != nil {
			var bb bytes.Buffer
			if err := out.OnFlush(&bb); err == nil {
				sxlMap = map[int]sixelEntry{
					baseRow: {bytes: bb.Bytes(), fallback: out.Fallback, height: target.Y},
				}
			}
		}
	} else if out.OnFlush != nil {
		fl = []func(io.Writer) error{out.OnFlush}
	}
	return out.Lines, fl, sxlMap, target.Y, hit
```

- [ ] **Step 12: Build and run all tests**

Run: `go build ./...`
Expected: success.

Run: `go test ./...`
Expected: all PASS, no regressions.

- [ ] **Step 13: Commit**

```bash
git add internal/image/fetcher.go internal/image/fetcher_test.go internal/ui/messages/model.go
git commit -m "perf(image): pre-render protocol output off the UI thread

After Fetch decodes the image, the fetch goroutine now also runs the
active protocol's RenderImage (sixel encode / halfblock per-pixel
ANSI build / kitty PNG encode + base64) and stashes the result. The
bubbletea Update goroutine looks up the prerendered Render via
Fetcher.Prerendered with no encoding work.

Combined with the decode-memo population in the previous commit, a
fresh image arrival now causes zero decode + zero encode on the UI
thread. Scrolling up to N unseen images no longer freezes the UI
while N protocol encodes run serially on the View goroutine."
```

---

## Task 3: Per-message cache invalidation on ImageReadyMsg

`HandleImageReady` currently sets `m.cache = nil`, which forces `buildCache` to walk every message in the channel on the next `View()`. With N images arriving in a burst, that's N full walks. We replace this with a per-message invalidation: only the affected message's cached row is dropped.

**Strategy:** The render cache is `m.cache` — a slice (or struct containing one) keyed implicitly by message index. Read the cache structure first to confirm. The minimal change: add a `m.invalidEntries map[string]struct{}` keyed by message TS (timestamp); `buildCache` rebuilds only those entries (and any rows whose absolute Y position shifts as a result). If the cache layout makes per-row rebuild infeasible, the fallback is to coalesce arrivals: defer cache invalidation by debouncing `ImageReadyMsg` for ~50ms so a burst becomes one rebuild.

**Files:**
- Modify: `internal/ui/messages/model.go:382-393` (HandleImageReady)
- Modify: `internal/ui/messages/model.go` (buildCache, search around line 1024)
- Modify: `internal/ui/messages/model_test.go` (add test) — or wherever existing model tests live

- [ ] **Step 1: Read the render cache structure**

Run: `rg -n "m\.cache|type viewEntry|type renderCache|cache \[\]" internal/ui/messages/model.go | head -40`
Read enough context (lines around the cache type definition and `buildCache`) to know:
- Is `m.cache` a slice of per-message entries? A flat list of lines? A struct holding both?
- Does each entry know its source TS / message index?
- Where does `buildCache` walk messages and produce entries?

Note in your scratchpad whether per-entry rebuild is feasible (yes if `m.cache` is `[]viewEntry` keyed by message order) or whether we need the debounce fallback.

- [ ] **Step 2: Decide approach based on Step 1 findings**

If `m.cache` is a slice of per-message entries that can be rebuilt independently → implement per-entry invalidation (Steps 3–7 below).

If the cache is a flat structure where line offsets cascade → implement coalesced debounce (Steps 3D–7D below). Most likely the slice approach is feasible.

- [ ] **Step 3 (per-entry invalidation): Write a failing test**

Add to `internal/ui/messages/model_test.go` (or create a new `model_image_invalidation_test.go` in the same package). Sketch — adapt field names to the actual `Model` API:

```go
// HandleImageReady should invalidate only the affected message row,
// not the entire render cache. We assert by populating the cache,
// calling HandleImageReady for one TS, and confirming sibling entries
// are still present.
func TestModel_HandleImageReady_PerEntryInvalidation(t *testing.T) {
	m := New([]MessageItem{
		{TS: "111.111", Text: "msg one"},
		{TS: "222.222", Text: "msg two"},
		{TS: "333.333", Text: "msg three"},
	}, "test-channel")
	// Force a cache build by calling View once. Width/height plumbing
	// depends on the Model API; reuse whatever helper existing tests
	// use to drive a render.
	_ = m.View()
	if m.cache == nil {
		t.Fatal("expected cache populated after View()")
	}
	cacheBefore := m.cache

	m.HandleImageReady("test-channel", "222.222", "F222-key")

	// Cache must NOT be entirely nil: only the matching entry was
	// invalidated.
	if m.cache == nil {
		t.Fatal("HandleImageReady should not nil the entire cache")
	}
	// Sibling entries must point to the same memory as before (cheap
	// pointer equality if entries are pointers, or value equality if
	// entries are values).
	// (Adjust assertion to the cache type.)
	_ = cacheBefore
}
```

- [ ] **Step 4: Run the test to verify it fails**

Run: `go test ./internal/ui/messages/ -run TestModel_HandleImageReady_PerEntryInvalidation -v`
Expected: FAIL because today's HandleImageReady sets `m.cache = nil`.

- [ ] **Step 5: Implement per-entry invalidation**

Replace `HandleImageReady` (`model.go:382-393`) with:

```go
// HandleImageReady is invoked by the host (App.Update) when an
// ImageReadyMsg lands. It marks the affected message's render-cache
// entry as stale and clears the in-flight bit for the specific key,
// without invalidating sibling entries. This is critical for perf
// when a burst of images arrives (channel switch, scroll-up to
// unseen history): N arrivals used to trigger N full cache walks
// over every message in the channel; now they trigger N pointed
// rebuilds of one entry each.
//
// Messages for a non-active channel are ignored.
//
// key may be empty for legacy callers; in that case the entire cache
// is invalidated (old behavior) for safety.
func (m *Model) HandleImageReady(channel, ts, key string) {
	if channel != m.channelName {
		return
	}
	if key == "" {
		m.cache = nil
		m.fetchingImages = nil
		m.dirty()
		return
	}
	if m.fetchingImages != nil {
		delete(m.fetchingImages, key)
	}
	if ts != "" && m.cache != nil {
		m.invalidateEntry(ts)
	} else {
		m.cache = nil
	}
	m.dirty()
}

// invalidateEntry marks the cache entry for the message with the given
// TS as stale, so the next buildCache rebuilds only that entry. Sibling
// entries are kept intact. No-op if the cache is empty or no matching
// entry is found.
func (m *Model) invalidateEntry(ts string) {
	// Find the cache entry whose source message has this TS, and clear
	// it so buildCache repopulates this slot (and only this slot).
	// (Adapt the field/index lookup to the actual cache shape.)
	for i := range m.cache {
		if m.cache[i].ts == ts { // or whatever the field is called
			// Zero the entry so buildCache treats it as missing and
			// rebuilds in-place. Sibling entries are untouched.
			m.cache[i] = viewEntry{}
			return
		}
	}
}
```

The exact entry zeroing depends on the cache type. If `m.cache` is `[]viewEntry`, zeroing the element works as long as `buildCache` checks for a sentinel (e.g. zero TS) and rebuilds those slots. If `buildCache` unconditionally rebuilds every slot, we instead need a separate `m.staleEntries map[int]struct{}` and a guard at the top of `buildCache` that early-returns the existing cache when nothing is stale.

Open `buildCache` (around `model.go:1024`) and verify. The cleanest implementation when `buildCache` does a full walk is:

```go
// (in Model struct)
staleEntries map[string]struct{} // TS set; nil means "rebuild all"

// (replacing m.cache = nil call sites with):
if m.staleEntries == nil {
	m.staleEntries = map[string]struct{}{}
}
m.staleEntries[ts] = struct{}{}
```

Then `buildCache` (and the View-time guard `if m.cache == nil { buildCache(...) }`) becomes:

```go
if m.cache == nil || len(m.staleEntries) > 0 {
	m.buildCache(...)
}
```

And `buildCache` itself, instead of allocating a fresh slice, iterates `m.messages` and reuses existing `m.cache[i]` when the message's TS is NOT in `staleEntries`. After the walk, clear `m.staleEntries`.

Implement whichever variant matches the existing structure; the test in Step 3 just requires that sibling entries survive HandleImageReady.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/ui/messages/ -run TestModel_HandleImageReady_PerEntryInvalidation -v`
Expected: PASS.

- [ ] **Step 7: Run the full messages-package tests**

Run: `go test ./internal/ui/messages/...`
Expected: all PASS, including any cache / scroll / theme-switch tests that depend on cache invalidation behavior. If any of those tests rely on `m.cache == nil` after HandleImageReady, update them to assert the new per-entry behavior.

- [ ] **Step 8: Run the full test suite**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/messages/model_test.go
git commit -m "perf(messages): per-message cache invalidation on ImageReadyMsg

HandleImageReady used to set m.cache = nil, forcing buildCache to
walk every message in the channel on the next View(). With N images
arriving in a burst (channel switch into unseen history, or scroll-up
to a region with many attachments), this caused N full cache walks
back-to-back on the bubbletea Update goroutine.

Replace with per-message invalidation: a single TS is marked stale
and buildCache rebuilds only that entry, reusing sibling entries
verbatim. The legacy whole-cache nil path is kept for callers that
pass an empty key."
```

---

## Task 4: Manual smoke test

Automated tests cover the unit behavior. Confirm the user-visible improvement against a real Slack workspace.

- [ ] **Step 1: Build the binary**

Run: `make build`
Expected: `bin/slk` produced, no errors.

- [ ] **Step 2: Run against a workspace with image-heavy channels**

Manually start `./bin/slk`, switch to a channel known to have many image attachments, and scroll up rapidly through unseen history. Compare to the user-described baseline ("choppy and laggy when fetching images").

Expected:
- Initial channel switch: placeholders appear immediately, then images fill in one by one.
- Scroll input remains responsive while fetches land — no multi-second freezes.
- No new visual glitches (missing images, wrong sizes, leftover placeholders).

- [ ] **Step 3: If issues observed**

Capture: which image protocol is active (kitty / sixel / halfblock — visible via `Ctrl+Y` theme menu or in logs), how many images, terminal in use. File a follow-up issue rather than block this branch — Phase 2 of the parent investigation has additional fixes (Cache.mu, stdout serialization, avatar pool).

- [ ] **Step 4: Commit any test/docs follow-ups discovered, then offer the branch for review/merge**

```bash
git status
# If clean:
git log --oneline main..HEAD
```

Report the commit list. The branch is ready for the finishing-a-development-branch skill.

---

## Self-review notes

**Spec coverage:** Plan covers Phase 1 of the investigation: decode-once + encode-off-thread + scoped invalidation. Phase 2 items (Cache.mu cleanup, stdout serialization, avatar pool throttling, BlockImageReadyMsg handler) are deferred to a follow-up branch as agreed.

**Risk areas worth flagging during review:**
1. **`maybePrerender` runs while the fetch goroutine still holds the `singleflight` slot.** If sixel encoding takes 100+ms, that's added latency before `ImageReadyMsg` is sent. Acceptable: the encode would have run on the UI thread otherwise, and singleflight's purpose is dedup, not low latency. Worth noting in a code comment.
2. **`KittyRenderer.SetSource` + `RenderKey`** are called from a goroutine in `maybePrerender` and from the UI thread in the fallback path. The renderer's mutex (`KittyRenderer.mu`, `kitty.go:59`) already serializes them. Verify by reading `kitty.go` during Task 2 Step 5.
3. **Per-entry invalidation in Task 3** depends on `buildCache`'s structure. If the cache is not slice-of-entries-keyed-by-message, fall back to coalescing (debounce arrivals into one rebuild). Step 1 of Task 3 explicitly handles this by re-deciding the approach.
4. **`FetchRequest.CellTarget` is optional** — callers that don't set it (e.g. avatars, full-screen preview) still work; prerender just no-ops. Verify avatar fetcher and preview don't break by running their tests.
