package messages

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	stdimage "image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/core/blocks"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
)

// blockImageCacheKey mirrors blockkit's unexported urlCacheKey ("BK-"
// + first 8 hex chars of the URL's SHA-1) so the test can prime the
// disk cache under the key the block path will look up.
func blockImageCacheKey(url string) string {
	sum := sha1.Sum([]byte(url))
	return "BK-" + hex.EncodeToString(sum[:8])
}

// setupBlockImageMessageModel primes the image cache for one Block Kit
// image URL and returns a Model showing a single message whose Blocks
// carry that image. Mirrors setupImageMessageModel, which covers the
// file-attachment path; this one covers the block path, whose sixel
// bytes used to be emitted as an unpositioned side-channel flush
// instead of a placement.
func setupBlockImageMessageModel(t *testing.T, protocol imgpkg.Protocol) *Model {
	t.Helper()
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	const url = "https://example.com/block-image.png"
	key := blockImageCacheKey(url)
	pngBytes := makeTestPNG(720, 720)
	if _, err := cache.Put(key, "png", pngBytes); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:    key,
		URL:    "unused://disk-cache-hits-skip-network",
		Target: stdimage.Pt(320, 320),
	}); err != nil {
		t.Fatalf("Fetch (memo prime): %v", err)
	}
	msg := MessageItem{
		TS:        "1700000000.000100",
		UserID:    "U1",
		UserName:  "alice",
		Text:      "look at this",
		Timestamp: "10:30 AM",
		Blocks: []blocks.Block{
			// Declared square: with CellPixels (8,16) and MaxRows 20
			// the block target comes out (40,20) cells = the (320,320)
			// pixel target the cache is primed with below.
			blocks.ImageBlock{URL: url, Alt: "a square", Width: 320, Height: 320},
		},
	}
	m := New([]MessageItem{msg}, "C123")
	m.SetImageContext(imgrender.ImageContext{
		Protocol:   protocol,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	})
	return m
}

// A fully visible Block Kit image under sixel must publish a placement
// through the same pipeline as file attachments. Before the fix, the
// block path returned its OnFlush as a kitty-style flush; for sixel
// that wrote the raw DCS pixel stream to the terminal at the cursor's
// current position on every View, smearing images across the screen
// while scrolling, and published no placement at all.
func TestView_SixelBlockImage_PublishesPlacement(t *testing.T) {
	m := setupBlockImageMessageModel(t, imgpkg.ProtoSixel)
	out := m.View(60, 80)
	if strings.Contains(out, "\x1bP") {
		t.Error("sixel bytes leaked into the frame string; they must be painted out-of-band")
	}
	placements := m.SixelPlacements()
	if len(placements) != 1 {
		t.Fatalf("SixelPlacements() = %d, want 1 for a fully visible block image", len(placements))
	}
	p := placements[0]
	if len(p.Bytes) == 0 || !strings.HasPrefix(string(p.Bytes), "\x1bP") {
		t.Error("placement carries no sixel DCS payload")
	}
	if p.Rows <= 0 || p.Cols <= 0 {
		t.Errorf("placement footprint = %dx%d, want a positive erase region", p.Cols, p.Rows)
	}
	// The placement's identity must be the fetcher's BK- cache key, not
	// a hash of the payload: the identity is recomputed every frame,
	// and hashing hundreds of KB per visible image per frame is real
	// CPU spent for nothing.
	if want := blockImageCacheKey("https://example.com/block-image.png"); !strings.HasPrefix(p.Key, want+"/") {
		t.Errorf("placement Key = %q, want prefix %q (the fetcher cache key, not a payload hash)", p.Key, want+"/")
	}
}

// The halfblock fallback must cover the image's rows when the image is
// only partially visible: the block path previously never populated
// SixelRows, so there was no fallback either and a partially visible
// image rendered as blank placeholder rows.
func TestView_SixelBlockImage_PartialVisibilityUsesFallback(t *testing.T) {
	m := setupBlockImageMessageModel(t, imgpkg.ProtoSixel)
	// A viewport too short to show the whole image (~20 rows) forces
	// the partial-visibility path.
	out := m.View(14, 80)
	if strings.Contains(out, "\x1bP") {
		t.Error("sixel bytes leaked into the frame string; they must be painted out-of-band")
	}
	if len(m.SixelPlacements()) != 0 {
		t.Fatalf("SixelPlacements() = %d, want 0 for a partially visible image", len(m.SixelPlacements()))
	}
	// The fallback is halfblock text: colored half-block glyphs (▀▄)
	// replacing the plain placeholder spaces.
	if !strings.Contains(out, "▀") && !strings.Contains(out, "▄") {
		t.Error("partially visible block image did not render its halfblock fallback; the rows are blank")
	}
}

// The thread pane renders replies without the sixel placement
// pipeline (plan decision P3: RenderOptions carries no SixelRows). It
// must still show the image: the halfblock fallback is spliced into
// the rendered lines in place of the blank reserved rows. Without the
// splice a cached block image left its rows blank in threads.
func TestRenderMessage_WithoutSixelRows_SplicesBlockImageFallback(t *testing.T) {
	m := setupBlockImageMessageModel(t, imgpkg.ProtoSixel)
	rc := m.renderContext()
	r := RenderMessage(m.messages[0], 80, rc, RenderOptions{ImageHits: true})
	if strings.Contains(r.Content, "\x1bP") {
		t.Error("sixel bytes leaked into the rendered content")
	}
	if !strings.Contains(r.Content, "▀") && !strings.Contains(r.Content, "▄") {
		t.Error("thread-pane render left the block image's rows blank; the halfblock fallback was not spliced")
	}
	if len(r.SixelRows) != 0 {
		t.Fatalf("RenderedMessage.SixelRows = %d entries, want 0 without the sixel pipeline", len(r.SixelRows))
	}
}

// setupAttachmentPrerenderMessageModel primes a FILE-attachment image
// through the fetcher's prerender path: the fetch runs after
// SetImageContext (so ConfigurePrerender is active) and carries the
// cell target, so RenderBlock serves the cached Render - whose Lines
// share the fetcher cache's backing array.
func setupAttachmentPrerenderMessageModel(t *testing.T) *Model {
	t.Helper()
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	const fileID = "F0123ABCD"
	const key = fileID + "-720"
	pngBytes := makeTestPNG(720, 720)
	if _, err := cache.Put(key, "png", pngBytes); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	msg := MessageItem{
		TS:        "1700000000.000100",
		UserID:    "U1",
		UserName:  "alice",
		Text:      "look at this",
		Timestamp: "10:30 AM",
		Attachments: []Attachment{{
			Kind:   "image",
			Name:   "screenshot.png",
			URL:    "https://example.com/perma",
			FileID: fileID,
			Mime:   "image/png",
			Thumbs: []ThumbSpec{{URL: "https://example.com/720.png", W: 720, H: 720}},
		}},
	}
	m := New([]MessageItem{msg}, "C123")
	m.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoSixel,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	})
	// One fetch memoizes the decoded image under the pixel target AND
	// bakes the prerender for the cell target RenderBlock looks up.
	if _, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:        key,
		URL:        "unused://disk-cache-hits-skip-network",
		Target:     stdimage.Pt(320, 320),
		CellTarget: stdimage.Pt(40, 20),
	}); err != nil {
		t.Fatalf("Fetch (memo + prerender prime): %v", err)
	}
	return m
}

// The thread pane's fallback splice must not write through to the
// fetcher's prerendered Render. RenderBlock's fast path returns the
// cached Render's own Lines slice; splicing the halfblock fallback in
// place would replace the cache's blank reserved rows, and every later
// render of the same image - the channel pane's included - would draw
// halfblock text under the sixel paint.
func TestRenderMessage_ThreadSpliceDoesNotCorruptPrerenderedLines(t *testing.T) {
	m := setupAttachmentPrerenderMessageModel(t)
	rc := m.renderContext()

	thread := RenderMessage(m.messages[0], 80, rc, RenderOptions{ImageHits: true})
	if !strings.Contains(thread.Content, "▀") && !strings.Contains(thread.Content, "▄") {
		t.Fatal("thread-pane render did not splice the attachment fallback; the setup does not exercise the splice")
	}

	channel := RenderMessage(m.messages[0], 80, rc, RenderOptions{ImageHits: true, SixelRows: true})
	if strings.Contains(channel.Content, "▀") || strings.Contains(channel.Content, "▄") {
		t.Error("channel-pane render drew halfblock rows for a prerendered image; the thread splice corrupted the cached lines")
	}
	if len(channel.SixelRows) != 1 {
		t.Fatalf("RenderedMessage.SixelRows = %d entries, want 1 for the channel render", len(channel.SixelRows))
	}
}

// setupLegacyImageMessageModel primes the cache for one legacy
// attachment's image_url, for thread-splice tests over the striped
// attachment body.
func setupLegacyImageMessageModel(t *testing.T) *Model {
	t.Helper()
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	const url = "https://example.com/legacy-image.png"
	key := blockImageCacheKey(url)
	if _, err := cache.Put(key, "png", makeTestPNG(720, 720)); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key: key,
		URL: "unused://disk-cache-hits-skip-network",
		// The attachment target is (71,20) cells at CellPixels (8,16):
		// a 16:9 image at MaxRows 20 in a 78-column body.
		Target: stdimage.Pt(568, 320),
	}); err != nil {
		t.Fatalf("Fetch (memo prime): %v", err)
	}
	msg := MessageItem{
		TS:        "1700000000.000100",
		UserID:    "U1",
		UserName:  "alice",
		Text:      "look at this",
		Timestamp: "10:30 AM",
		LegacyAttachments: []blocks.LegacyAttachment{{
			Pretext:  "above the bar",
			ImageURL: url,
		}},
	}
	m := New([]MessageItem{msg}, "C123")
	m.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoSixel,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	})
	return m
}

// A thread-pane render of a legacy attachment's image grafts the bare
// halfblock fallback at the entry's column, over the row's own stripe:
// the stripe appears once, with the image right of it - not twice
// (fallback baking it in) and not gone (bare row replacing the line).
func TestRenderMessage_WithoutSixelRows_LegacyImageGraftsOverStripe(t *testing.T) {
	m := setupLegacyImageMessageModel(t)
	rc := m.renderContext()
	r := RenderMessage(m.messages[0], 80, rc, RenderOptions{ImageHits: true})

	var fallbackRows int
	for _, line := range strings.Split(r.Content, "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "▀") && !strings.Contains(plain, "▄") {
			continue
		}
		fallbackRows++
		if n := strings.Count(plain, "█"); n != 1 {
			t.Errorf("fallback row shows the stripe %d times, want exactly 1: %q", n, plain[:min(20, len(plain))])
		}
		if !strings.HasPrefix(plain, "█ ") {
			t.Errorf("fallback row does not start with the stripe: %q", plain[:min(20, len(plain))])
		}
	}
	if fallbackRows == 0 {
		t.Fatal("no fallback rows in the thread render; the setup does not exercise the splice")
	}
}

// A thread-pane render of a section with an image accessory grafts
// the bare fallback at the accessory's column, over the composed row:
// the body text appears once with the image beside it, not twice and
// not blanked.
func TestRenderMessage_WithoutSixelRows_SectionAccessoryGraftsAtColumn(t *testing.T) {
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	const url = "https://example.com/accessory.png"
	key := blockImageCacheKey(url)
	if _, err := cache.Put(key, "png", makeTestPNG(720, 720)); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key: key,
		URL: "unused://disk-cache-hits-skip-network",
		// The accessory target is (8,4) cells at CellPixels (8,16).
		Target: stdimage.Pt(64, 64),
	}); err != nil {
		t.Fatalf("Fetch (memo prime): %v", err)
	}
	msg := MessageItem{
		TS:        "1700000000.000100",
		UserID:    "U1",
		UserName:  "alice",
		Text:      "look at this",
		Timestamp: "10:30 AM",
		Blocks: []blocks.Block{
			blocks.SectionBlock{
				Text:      "section body text",
				Accessory: blocks.ImageAccessory{URL: url},
			},
		},
	}
	m := New([]MessageItem{msg}, "C123")
	m.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoSixel,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	})

	r := RenderMessage(m.messages[0], 80, m.renderContext(), RenderOptions{ImageHits: true})

	// The body text must survive exactly once - not blanked by the
	// fallback, not doubled by baking it into the fallback rows - and
	// it must share its row with the image: the accessory sits beside
	// the text, not below a replaced row.
	total := strings.Count(r.Content, "section body text")
	if total != 1 {
		t.Fatalf("section body text appears %d times in the render, want exactly 1", total)
	}
	var beside bool
	for _, line := range strings.Split(r.Content, "\n") {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "section body text") && strings.ContainsAny(plain, "▀▄") {
			beside = true
		}
	}
	if !beside {
		t.Error("the section body text does not share its row with the image; the fallback replaced the row instead of grafting beside it")
	}
	if !strings.ContainsAny(r.Content, "▀▄") {
		t.Fatal("no fallback rows in the thread render; the setup does not exercise the splice")
	}
}
