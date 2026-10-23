package image

import (
	"bytes"
	"context"
	_ "embed"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gammons/slk/internal/slackhttp"
)

// TestTryDownload_NoCustomUserAgent verifies two related facts about
// the image fetcher after the BrowserTransport switch:
//  1. The fetcher no longer announces itself with the legacy
//     slk/inline-image-fetcher User-Agent (which was just as flagable
//     as Go-http-client/1.1).
//  2. BrowserTransport correctly *skips* header injection for
//     non-Slack hosts (the test uses a 127.0.0.1 httptest server),
//     so the browser UA does not leak to arbitrary destinations.
func TestTryDownload_NoCustomUserAgent(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(200)
		w.Write([]byte("not-a-real-png"))
	}))
	defer srv.Close()

	client := slackhttp.NewBrowserHTTPClient(nil)
	f := &Fetcher{http: client}

	body, _, status, err := f.tryDownload(context.Background(), srv.URL, TeamAuth{})
	if err != nil {
		t.Fatalf("tryDownload: %v", err)
	}
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if string(body) != "not-a-real-png" {
		t.Fatalf("body = %q", body)
	}

	// 127.0.0.1 is NOT a Slack host, so BrowserTransport correctly skips
	// header injection. This confirms host-scoping is working.
	if ua := gotHeaders.Get("User-Agent"); strings.HasPrefix(ua, "Mozilla/5.0") {
		t.Errorf("browser UA leaked to non-Slack host: %q", ua)
	}
	// The old explicit slk-specific UA must be gone — the fetcher no
	// longer sets one. Go's default ("Go-http-client/1.1") is the
	// expected fallback when no transport injects one.
	if ua := gotHeaders.Get("User-Agent"); strings.Contains(ua, "slk/inline-image-fetcher") {
		t.Errorf("legacy slk-specific UA still present: %q", ua)
	}
}

// tinyWebP is a tiny valid WebP image (VP8, 8x8 white) used to verify
// that the WebP decoder is registered with the stdlib image registry.
// Regenerate with:
//
//	ffmpeg -y -f lavfi -i color=c=white:s=8x8 -frames:v 1 \
//	    internal/image/testdata/tiny.webp
//
//go:embed testdata/tiny.webp
var tinyWebP []byte

func tinyPNG(t testing.TB, w, h int, c imgcolor.RGBA) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := imgpng.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFetcher_FreshFetchCachesAndDecodes(t *testing.T) {
	pngBytes := tinyPNG(t, 100, 100, imgcolor.RGBA{0, 200, 0, 255})

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	res, err := f.Fetch(context.Background(), FetchRequest{
		Key: "k1", URL: srv.URL, Target: image.Pt(20, 20),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Img.Bounds().Dx() != 20 || res.Img.Bounds().Dy() != 20 {
		t.Errorf("expected 20x20 downscale, got %v", res.Img.Bounds())
	}
	if hits != 1 {
		t.Errorf("expected 1 hit, got %d", hits)
	}

	// Second fetch hits the cache, no HTTP.
	res2, err := f.Fetch(context.Background(), FetchRequest{
		Key: "k1", URL: srv.URL, Target: image.Pt(20, 20),
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("expected cache hit, got %d HTTP hits", hits)
	}
	_ = res2
}

func TestFetcher_SingleFlightDedupes(t *testing.T) {
	pngBytes := tinyPNG(t, 50, 50, imgcolor.RGBA{0, 0, 200, 255})

	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.Fetch(context.Background(), FetchRequest{
				Key: "same", URL: srv.URL, Target: image.Pt(10, 10),
			})
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("singleflight should dedupe: hits=%d", hits)
	}
}

func TestFetcher_CachedReturnsFalseWhenMissing(t *testing.T) {
	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	if _, ok := f.Cached("never-stored", image.Pt(10, 10)); ok {
		t.Errorf("expected Cached to return false for unknown key")
	}
}

func TestFetcher_CachedReturnsImageWhenPresent(t *testing.T) {
	pngBytes := tinyPNG(t, 60, 40, imgcolor.RGBA{255, 0, 0, 255})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	// Prime the cache via Fetch. Cached now performs a memo-only
	// lookup keyed on (key, target), so Fetch and the subsequent
	// Cached must use the SAME target -- the old disk-decode
	// fallback that would synthesize a (key, target) memo entry
	// on demand has been removed (it was a 156ms sync-on-Update
	// regression; callers now drive an async re-fetch instead).
	target := image.Pt(20, 20)
	if _, err := f.Fetch(context.Background(), FetchRequest{
		Key: "primed", URL: srv.URL, Target: target,
	}); err != nil {
		t.Fatal(err)
	}

	img, ok := f.Cached("primed", target)
	if !ok {
		t.Fatalf("expected Cached to return true after Fetch at the same target")
	}
	if img == nil {
		t.Fatalf("expected non-nil image")
	}
	if img.Bounds().Dx() != 20 || img.Bounds().Dy() != 20 {
		t.Errorf("expected 20x20 downscale, got %v", img.Bounds())
	}
}

// TestFetcher_CachedReturnsFalseOnMemoMissEvenWithDiskHit asserts the
// contract change: Cached no longer falls back to disk decode. Even
// when the on-disk cache holds the file, a cold (key, target) memo
// must return ok=false so the caller can spawn an async Fetch to
// populate the memo instead of paying the 100-200ms decode cost on
// the UI thread.
func TestFetcher_CachedReturnsFalseOnMemoMissEvenWithDiskHit(t *testing.T) {
	pngBytes := tinyPNG(t, 100, 100, imgcolor.RGBA{0, 0, 200, 255})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	// Fetch into the disk cache and the (key, target=0x0) memo.
	if _, err := f.Fetch(context.Background(), FetchRequest{
		Key: "k", URL: srv.URL, Target: image.Pt(0, 0),
	}); err != nil {
		t.Fatal(err)
	}

	// Cached at a DIFFERENT target -- disk has the bytes, but no memo
	// entry exists for (k, 30x30). Old behavior: open + decode + downscale
	// to 30x30 synchronously, return (img, true). New behavior: return
	// (nil, false) so the caller spawns a background Fetch.
	if img, ok := f.Cached("k", image.Pt(30, 30)); ok {
		t.Errorf("expected Cached to return false on memo miss; got ok=true img=%v", img.Bounds())
	}
}

// BenchmarkFetcher_CachedColdMemoDiskHit measures the worst case the
// fix targets: 21 distinct (key, target) memo misses where every file
// is already on disk (cold-start visit to a previously-rendered
// channel). Pre-fix each call paid open + image.Decode + downscale on
// the UI thread (~100-200ms per image at typical Slack thumb sizes,
// summing to multi-second freezes). Post-fix each call is a pure
// sync.Map miss returning (nil, false) instantly; the caller spawns
// a background goroutine to do the work.
func BenchmarkFetcher_CachedColdMemoDiskHit(b *testing.B) {
	pngBytes := tinyPNG(b, 600, 400, imgcolor.RGBA{255, 0, 0, 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	cache, _ := NewCache(b.TempDir(), 100)
	f := NewFetcher(cache, http.DefaultClient)

	// Pre-populate the disk cache for N distinct keys and FLUSH the
	// in-memory decoded memo so every Cached() lookup hits the cold
	// branch the benchmark exercises.
	const N = 21
	target := image.Pt(60*8, 16*16) // typical Slack legacy attachment thumb size
	for i := 0; i < N; i++ {
		key := keyN(i)
		if _, err := f.Fetch(context.Background(), FetchRequest{
			Key: key, URL: srv.URL, Target: target,
		}); err != nil {
			b.Fatal(err)
		}
	}
	// We want to measure the COLD path (memo miss + disk decode +
	// downscale) per iteration, not just the first call. Wipe the
	// memo before every iteration so each measured call exercises
	// the cold branch. b.StopTimer / b.StartTimer is used to keep
	// the wipe out of the measured time.
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		f.decoded.Range(func(k, _ any) bool {
			f.decoded.Delete(k)
			return true
		})
		key := keyN(i % N)
		b.StartTimer()
		_, _ = f.Cached(key, target)
	}
}

func keyN(i int) string { return "bench-" + string(rune('a'+i%26)) + string(rune('0'+i/26)) }

func TestFetcher_CachedNeverStartsDownload(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		w.Write(tinyPNG(t, 10, 10, imgcolor.RGBA{0, 255, 0, 255}))
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	if _, ok := f.Cached("never", image.Pt(10, 10)); ok {
		t.Errorf("expected ok=false")
	}
	if hits != 0 {
		t.Errorf("Cached should not start a download; got %d hits", hits)
	}
}

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

	cellTarget := image.Pt(10, 5)
	pixelTarget := image.Pt(20, 10)

	f.ConfigurePrerender(ProtoHalfBlock)

	if _, err := f.Fetch(context.Background(), FetchRequest{
		Key: "k1", URL: srv.URL, Target: pixelTarget, CellTarget: cellTarget,
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

// TestFetcher_DecodesWebP exercises the WebP decode path that Slack's
// avatar CDN now serves for many users. Without a registered WebP
// decoder the download succeeds but image.Decode fails with "image:
// unknown format" and the avatar is silently evicted from the cache —
// the user just sees no picture. This regression test guards the blank
// import of golang.org/x/image/webp in fetcher.go.
func TestFetcher_DecodesWebP(t *testing.T) {
	if len(tinyWebP) == 0 {
		t.Fatal("testdata/tiny.webp missing or empty")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		w.Write(tinyWebP)
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	res, err := f.Fetch(context.Background(), FetchRequest{
		Key: "avatar-webp", URL: srv.URL,
	})
	if err != nil {
		t.Fatalf("Fetch failed: %v — webp decoder not registered?", err)
	}
	if res.Img == nil {
		t.Fatal("decoded image is nil")
	}
	if res.Img.Bounds().Dx() <= 0 || res.Img.Bounds().Dy() <= 0 {
		t.Errorf("unreasonable dims: %v", res.Img.Bounds())
	}
}

// TestWebPDecoderRegistered is a narrower companion to
// TestFetcher_DecodesWebP: it bypasses the HTTP/cache path and asserts
// directly that image.Decode recognizes WebP. If this fails, the blank
// import in fetcher.go is missing or the fixture is corrupt.
func TestWebPDecoderRegistered(t *testing.T) {
	img, format, err := image.Decode(bytes.NewReader(tinyWebP))
	if err != nil {
		t.Fatalf("image.Decode failed: %v — golang.org/x/image/webp blank import missing?", err)
	}
	if format != "webp" {
		t.Errorf("format = %q, want %q", format, "webp")
	}
	if img == nil {
		t.Error("decoded image is nil")
	}
}

func TestFetcher_HTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 10)
	f := NewFetcher(cache, http.DefaultClient)

	_, err := f.Fetch(context.Background(), FetchRequest{
		Key: "missing", URL: srv.URL, Target: image.Pt(10, 10),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestConfigurePrerender_SameProtocolKeepsCache(t *testing.T) {
	// SetImageContext runs ConfigurePrerender for every window model it
	// configures, and a workspace switch builds a fresh window model,
	// so the same-protocol re-configure is the common case: it must not
	// drop entries the fetch goroutine already encoded, or every
	// switch re-encodes each visible image on the UI thread.
	f := NewFetcher(nil, nil)
	f.ConfigurePrerender(ProtoSixel)
	key := prerenderKey("F123-480", image.Pt(60, 12), ProtoSixel)
	f.prerendered.Store(key, Render{Cells: image.Pt(60, 12)})

	f.ConfigurePrerender(ProtoSixel)

	if _, ok := f.Prerendered("F123-480", image.Pt(60, 12), ProtoSixel); !ok {
		t.Error("re-configure with the same protocol dropped the cached render")
	}
}

func TestConfigurePrerender_ProtocolChangeResetsCache(t *testing.T) {
	// The encoded output is protocol-specific, so switching protocol
	// (terminal capability re-probe) must drop it.
	f := NewFetcher(nil, nil)
	f.ConfigurePrerender(ProtoSixel)
	f.prerendered.Store(prerenderKey("F123-480", image.Pt(60, 12), ProtoSixel), Render{Cells: image.Pt(60, 12)})

	f.ConfigurePrerender(ProtoHalfBlock)

	if _, ok := f.Prerendered("F123-480", image.Pt(60, 12), ProtoSixel); ok {
		t.Error("protocol change kept a render encoded for the old protocol")
	}
	if _, ok := f.Prerendered("F123-480", image.Pt(60, 12), ProtoHalfBlock); ok {
		t.Error("protocol change kept an entry under the new protocol that was never stored for it")
	}
}
