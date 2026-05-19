package avatar

import (
	"bytes"
	"fmt"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	imgpkg "github.com/gammons/slk/internal/image"
)

// testCache builds a Cache wired to a local HTTP server serving a tiny
// PNG. Returns the cache and a teardown closure.
func testCache(t *testing.T) (*Cache, string, func()) {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.Set(x, y, imgcolor.RGBA{uint8(x * 16), uint8(y * 16), 128, 255})
		}
	}
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	pngBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))

	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	fetcher := imgpkg.NewFetcher(cache, http.DefaultClient)
	c := NewCache(fetcher, nil, false)
	return c, srv.URL, srv.Close
}

// TestPreload_OnReadyCallbackFires asserts that Cache invokes its
// onReady callback exactly once after a successful Preload, carrying
// the userID. Required so the bubbletea host can invalidate the
// messages-pane render cache when an avatar lands.
func TestPreload_OnReadyCallbackFires(t *testing.T) {
	c, url, done := testCache(t)
	defer done()

	var called atomic.Int32
	var gotUserID atomic.Value
	c.SetOnReady(func(userID string) {
		called.Add(1)
		gotUserID.Store(userID)
	})

	c.PreloadSync("U_READY", url)

	if n := called.Load(); n != 1 {
		t.Fatalf("onReady fired %d times, want 1", n)
	}
	if got, _ := gotUserID.Load().(string); got != "U_READY" {
		t.Fatalf("onReady received userID=%q, want U_READY", got)
	}
}

// TestPreload_OnReadyNotFiredOnFetchError asserts onReady stays silent
// when the fetch fails. Avoids invalidation storms for users whose
// avatars 404.
func TestPreload_OnReadyNotFiredOnFetchError(t *testing.T) {
	c, _, done := testCache(t)
	defer done()

	var called atomic.Int32
	c.SetOnReady(func(string) { called.Add(1) })

	// Unreachable URL — Fetcher should return an error and skip render.
	c.PreloadSync("U_404", "http://127.0.0.1:1/missing")

	if n := called.Load(); n != 0 {
		t.Fatalf("onReady fired %d times for failed fetch, want 0", n)
	}
}

// TestPreload_DedupesInflight asserts that calling Preload many times
// for the same userID before the fetch completes results in exactly
// one fetch (and one onReady fire), not N. The lazy AvatarFunc on the
// hot render path will call Preload on every miss, so without dedup
// every redraw would stampede.
func TestPreload_DedupesInflight(t *testing.T) {
	// Build a server we can hold open so we can observe inflight state.
	var hits atomic.Int32
	release := make(chan struct{})
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	pngBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	imgCache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := imgpkg.NewFetcher(imgCache, http.DefaultClient)
	c := NewCache(fetcher, nil, false)

	var ready atomic.Int32
	c.SetOnReady(func(string) { ready.Add(1) })

	// Fire 50 concurrent Preloads for the same user. With dedup, only
	// one should reach the server.
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c.Preload("U_DUP", srv.URL)
		}()
	}

	// Give goroutines a moment to enqueue and hit the server's hold.
	time.Sleep(50 * time.Millisecond)
	if h := hits.Load(); h != 1 {
		close(release)
		wg.Wait()
		t.Fatalf("server saw %d fetches for one userID; want 1 (dedup failed)", h)
	}

	close(release)
	wg.Wait()

	// Allow async PreloadSync goroutines spawned by Preload to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Get("U_DUP") != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := c.Get("U_DUP"); got == "" {
		t.Fatal("avatar never rendered after dedup'd Preloads completed")
	}
	if r := ready.Load(); r != 1 {
		t.Fatalf("onReady fired %d times for one userID; want 1", r)
	}
}

// TestPreload_QueueBackpressureReleasesInflightSlot asserts that when
// the worker pool's queue is full, Preload drops the job AND clears
// the userID from the inflight dedup set. Without the cleanup a
// dropped userID would be permanently stuck "in flight" with no work
// pending, and its avatar would never appear regardless of how many
// times AvatarFunc retried.
//
// Strategy: build a Cache whose worker queue is artificially small (we
// expose this via newCacheForTest), fill the queue with jobs blocked
// at the server, then assert a subsequent Preload for a different user
// observes inflight clearance even though it never reached a worker.
func TestPreload_QueueBackpressureReleasesInflightSlot(t *testing.T) {
	release := make(chan struct{})
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	pngBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	imgCache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := imgpkg.NewFetcher(imgCache, http.DefaultClient)

	// 1 worker, queue depth 2: easy to saturate.
	c := newCacheForTest(fetcher, nil, false, 1, 2)

	// Fill the worker (it'll block on the server) + the 2-slot queue.
	// That's 3 jobs. A 4th must be dropped.
	c.Preload("U_W1", srv.URL) // grabbed by worker
	time.Sleep(20 * time.Millisecond)
	c.Preload("U_Q1", srv.URL) // queued
	c.Preload("U_Q2", srv.URL) // queued
	c.Preload("U_DROP", srv.URL)

	// U_DROP should have been dropped; its inflight slot must be cleared
	// so a future Preload would try again.
	if _, present := c.inflight.Load("U_DROP"); present {
		close(release)
		t.Fatal("dropped Preload left userID stuck in inflight set")
	}

	// Sanity: U_W1/U_Q1/U_Q2 are still inflight (work pending).
	if _, present := c.inflight.Load("U_W1"); !present {
		close(release)
		t.Fatal("inflight set lost U_W1 prematurely")
	}

	close(release)

	// Drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Get("U_Q2") != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPreload_BoundedConcurrencyN1 asserts that with a 1-worker pool,
// two Preloads complete sequentially (not in parallel). The server
// records the high-water mark of in-flight requests; we expect 1.
func TestPreload_BoundedConcurrencyN1(t *testing.T) {
	var inflight atomic.Int32
	var peak atomic.Int32
	release := make(chan struct{})

	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	pngBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := inflight.Add(1)
		for {
			p := peak.Load()
			if now <= p || peak.CompareAndSwap(p, now) {
				break
			}
		}
		<-release
		inflight.Add(-1)
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	imgCache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := imgpkg.NewFetcher(imgCache, http.DefaultClient)

	c := newCacheForTest(fetcher, nil, false, 1, 8)

	// Fire 4 Preloads for distinct users. Only one should hit the
	// server at a time given workers=1.
	for i := 0; i < 4; i++ {
		c.Preload(fmt.Sprintf("U%d", i), srv.URL)
	}

	// Allow at least one to land at the server.
	time.Sleep(50 * time.Millisecond)
	if got := peak.Load(); got != 1 {
		close(release)
		t.Fatalf("expected peak inflight=1 with 1 worker; got %d", got)
	}

	// Drain.
	for i := 0; i < 4; i++ {
		release <- struct{}{}
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Get("U3") != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
