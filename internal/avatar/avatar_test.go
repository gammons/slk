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
	served := make(chan struct{})
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	pngBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			close(served)
		}
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

	// One worker, deep queue. The single worker is what makes the
	// sentinel drain below exact: preloadCh is FIFO, so a job enqueued
	// after every other job is also *completed* after every other job.
	// With the production 8-worker pool that ordering does not hold and
	// the counts below would be samples rather than totals.
	c := newCacheForTest(fetcher, nil, false, 1, 256)

	ready := make(chan string, 64) // > the 51 jobs a dedup failure can enqueue
	c.SetOnReady(func(userID string) { ready <- userID })

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

	// Preload never blocks — it makes its dedup decision under
	// sync.Map.LoadOrStore, enqueues at most one job and returns — so
	// this Wait does not deadlock against the server's hold. Once it
	// returns, every one of the 50 dedup decisions has been made and the
	// set of enqueued jobs is final.
	wg.Wait()

	// No timeout by design: `go test` already imposes one (default 10m),
	// and a wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "server saw 0 fetches; want 1".
	<-served // the job that won the dedup is at the server, still held

	// Enqueue a sentinel behind everything already queued. Because the
	// pool has one worker draining a FIFO channel, the sentinel's
	// onReady cannot fire until every job enqueued before it has run to
	// completion — so once we see it, hits and the per-user completion
	// counts are totals, not samples. This is what the old
	// `sleep(50ms); read hits` could not guarantee: it read the counter
	// while the duplicate jobs were still arriving.
	c.Preload("U_SENTINEL", srv.URL)
	close(release)

	dupCompletions := 0
	for {
		id := <-ready // onReady fires after preloadInner stored the render
		if id == "U_SENTINEL" {
			break
		}
		if id == "U_DUP" {
			dupCompletions++
		}
	}

	// The drain above also serves as cleanup: every job has completed, so
	// no worker is still writing into the fetcher's t.TempDir when this
	// test body returns and races TempDir's RemoveAll.
	if dupCompletions != 1 {
		t.Errorf("U_DUP was fetched and rendered %d times; want 1 (dedup failed)", dupCompletions)
	}
	if h := hits.Load(); h != 2 {
		t.Errorf("server saw %d requests (U_DUP + sentinel); want 2 (dedup failed)", h)
	}
	if got := c.Get("U_DUP"); got == "" {
		t.Error("avatar never rendered after dedup'd Preloads completed")
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
	// Buffered past the number of requests this test can possibly
	// generate (3 expected, 4 if the drop under test regresses) so the
	// handler never blocks on the send.
	serving := make(chan struct{}, 4)
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	pngBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serving <- struct{}{}
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

	ready := make(chan string, 4)
	c.SetOnReady(func(userID string) { ready <- userID })

	// Fill the worker (it'll block on the server) + the 2-slot queue.
	// That's 3 jobs. A 4th must be dropped.
	c.Preload("U_W1", srv.URL) // grabbed by worker

	// The handler is only entered after the lone worker dequeued U_W1
	// and drove preloadInner as far as the network, so this receive
	// establishes that the 2-slot queue is empty again before we fill
	// it. Guessing that 20ms was enough for the dequeue is what made
	// this test load-sensitive.
	//
	// No timeout by design: `go test` already imposes one (default 10m),
	// and a wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "dropped Preload left userID stuck in
	// inflight set" pointing at the wrong cause.
	<-serving

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

	// Drain. Exactly three jobs reach a worker; U_DROP was rejected by
	// backpressure and must never complete. onReady fires after
	// preloadInner stores the render, which is after the fetcher's last
	// write into t.TempDir, so this also keeps the workers from
	// outliving the test body and racing TempDir's RemoveAll.
	completed := map[string]bool{}
	for i := 0; i < 3; i++ {
		completed[<-ready] = true
	}
	for _, id := range []string{"U_W1", "U_Q1", "U_Q2"} {
		if !completed[id] {
			t.Errorf("onReady never fired for %s; the queued jobs did not all complete", id)
		}
	}
	if completed["U_DROP"] {
		t.Error("U_DROP was rejected by backpressure but still ran a fetch")
	}
}

// TestPreload_BoundedConcurrencyN1 asserts that with a 1-worker pool,
// two Preloads complete sequentially (not in parallel). The server
// records the high-water mark of in-flight requests; we expect 1.
func TestPreload_BoundedConcurrencyN1(t *testing.T) {
	var inflight atomic.Int32
	var peak atomic.Int32
	release := make(chan struct{})
	serving := make(chan struct{}, 4)

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
		serving <- struct{}{} // request landed, peak already updated
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

	ready := make(chan struct{}, 4)
	c.SetOnReady(func(string) { ready <- struct{}{} })

	// Fire 4 Preloads for distinct users. Only one should hit the
	// server at a time given workers=1.
	for i := 0; i < 4; i++ {
		c.Preload(fmt.Sprintf("U%d", i), srv.URL)
	}

	// No timeout by design: `go test` already imposes one (default 10m),
	// and a wall-clock budget inside the test is exactly what made this
	// load-sensitive. A hang here produces a goroutine dump naming the
	// stuck test, which beats "expected peak inflight=1; got 0" — which
	// is precisely how the old fixed 50ms sleep failed when it was not
	// long enough for even the first request to land.
	<-serving // request 0 reached the handler and is held at `release`

	// "At most one worker runs at a time" is a negative-existence claim:
	// no signal can ever prove that a *second* concurrent request is not
	// about to arrive, so this window cannot be replaced by a receive.
	// It is deliberately kept, and it is not a wall-clock budget — the
	// bias is one-sided. If the pool is correctly bounded, peak stays 1
	// no matter how long we wait, so a short window cannot false-fail;
	// it can only false-pass by missing a widened pool. The loop exits
	// the instant a violation appears, so the common (passing) case is
	// the only one that pays the full window. The assertion itself is
	// made after the drain below, where the value is exact.
	overlapWindow := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(overlapWindow) && peak.Load() == 1 {
		time.Sleep(time.Millisecond)
	}

	// Let the four requests through one at a time. With workers=1 the
	// (i+1)-th request cannot even start until the i-th handler has
	// returned, so each receive is guaranteed to have a sender.
	release <- struct{}{} // release request 0, observed above
	for i := 1; i < 4; i++ {
		<-serving             // request i reached the handler
		release <- struct{}{} // let it finish so the worker moves on
	}
	close(release)

	// Drain: all four renders stored, so no worker is still writing into
	// the fetcher's t.TempDir when this test body returns.
	for i := 0; i < 4; i++ {
		<-ready
	}

	// peak is a high-water mark and every job has now completed, so this
	// read is exact rather than a sample: there is no later moment at
	// which it could still rise. The old version sampled it after a
	// fixed 50ms, which read 0 whenever that was not long enough.
	if got := peak.Load(); got != 1 {
		t.Fatalf("peak inflight=%d with 1 worker, want 1", got)
	}
}
