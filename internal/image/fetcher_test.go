package image

import (
	"bytes"
	"context"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func tinyPNG(t *testing.T, w, h int, c imgcolor.RGBA) []byte {
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

	// Prime the cache via Fetch.
	if _, err := f.Fetch(context.Background(), FetchRequest{
		Key: "primed", URL: srv.URL, Target: image.Pt(0, 0),
	}); err != nil {
		t.Fatal(err)
	}

	img, ok := f.Cached("primed", image.Pt(20, 20))
	if !ok {
		t.Fatalf("expected Cached to return true after Fetch")
	}
	if img == nil {
		t.Fatalf("expected non-nil image")
	}
	if img.Bounds().Dx() != 20 || img.Bounds().Dy() != 20 {
		t.Errorf("expected 20x20 downscale, got %v", img.Bounds())
	}
}

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
