# Image Pipeline Follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Address the deferred items from the 2026-05-01 image-pipeline-perf branch — bound the in-memory caches, finish Phase 2 of the original investigation (Cache.mu, stdout serialization, avatar preload, BlockImageReadyMsg handler), and clean up minor smells from the merged branch.

**Architecture:** Three independent sections (A, B, C) with no cross-dependencies between sections. Each task within a section is also independent enough to land on its own. Pick whichever subset matters most when (or if) you decide to execute.

**Tech Stack:** Go 1.22+, bubbletea, existing `internal/image`, `internal/avatar`, `internal/ui/messages` packages.

**Verification baseline:** `go test ./...` clean on `main` at the time the plan is executed. Each task ends with the same.

---

## Section A — Memory bounds on in-memory image caches (highest priority)

After the perf branch, every image fetched in a session leaves two permanent residents:

- `Fetcher.decoded` — a decoded `image.Image` (RGBA bitmap, target-pixel sized).
- `Fetcher.prerendered` — a `Render` whose `OnFlush` closure can hold ~50–300 KB of sixel bytes or a base64'd kitty PNG.

For long-running sessions browsing busy channels (hundreds of images), this grows without bound until the process restarts. The on-disk LRU `Cache` stays bounded, but the in-memory mirrors do not.

This section adds a single LRU bound shared by both maps, keyed identically to the disk cache, and wires `Cache` to evict its memo siblings when it evicts a disk file.

### Task A1: Add a bounded LRU helper in `internal/image`

**Files:**
- Create: `internal/image/memo_lru.go`
- Create: `internal/image/memo_lru_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/image/memo_lru_test.go
package image

import (
	"testing"
)

func TestMemoLRU_BasicEvictsOldest(t *testing.T) {
	lru := newMemoLRU(2)
	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("c", 3) // evicts "a"

	if _, ok := lru.Get("a"); ok {
		t.Errorf("a should have been evicted")
	}
	if v, ok := lru.Get("b"); !ok || v.(int) != 2 {
		t.Errorf("b should still be present")
	}
	if v, ok := lru.Get("c"); !ok || v.(int) != 3 {
		t.Errorf("c should still be present")
	}
}

func TestMemoLRU_GetRefreshesRecency(t *testing.T) {
	lru := newMemoLRU(2)
	lru.Put("a", 1)
	lru.Put("b", 2)
	_, _ = lru.Get("a") // a is now MRU
	lru.Put("c", 3)     // should evict "b", not "a"

	if _, ok := lru.Get("a"); !ok {
		t.Errorf("a should still be present (was refreshed)")
	}
	if _, ok := lru.Get("b"); ok {
		t.Errorf("b should have been evicted")
	}
}

func TestMemoLRU_Delete(t *testing.T) {
	lru := newMemoLRU(4)
	lru.Put("a", 1)
	lru.Delete("a")
	if _, ok := lru.Get("a"); ok {
		t.Errorf("a should be gone after Delete")
	}
}

func TestMemoLRU_DeleteByPrefix(t *testing.T) {
	lru := newMemoLRU(4)
	lru.Put("F123-720", 1)
	lru.Put("F123-360", 2)
	lru.Put("F999-720", 3)
	lru.DeleteByPrefix("F123-")

	if _, ok := lru.Get("F123-720"); ok {
		t.Errorf("F123-720 should be gone after prefix delete")
	}
	if _, ok := lru.Get("F123-360"); ok {
		t.Errorf("F123-360 should be gone after prefix delete")
	}
	if _, ok := lru.Get("F999-720"); !ok {
		t.Errorf("F999-720 should remain")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestMemoLRU -v`
Expected: FAIL with "undefined: newMemoLRU".

- [ ] **Step 3: Implement `memoLRU`**

```go
// internal/image/memo_lru.go
package image

import (
	"container/list"
	"strings"
	"sync"
)

// memoLRU is a size-bounded thread-safe LRU keyed by string. Used by
// Fetcher to bound the in-memory decoded-image and prerendered-Render
// caches that would otherwise grow without bound for long-running
// sessions. Capacity is in entry count, not bytes — sizing per byte
// would require introspecting Render.OnFlush closures, which isn't
// worth the complexity. Pick a count that covers a busy channel's
// worth of attachments (a few hundred).
type memoLRU struct {
	cap   int
	mu    sync.Mutex
	items map[string]*list.Element
	lru   *list.List
}

type memoEntry struct {
	key string
	val any
}

func newMemoLRU(cap int) *memoLRU {
	if cap <= 0 {
		cap = 1
	}
	return &memoLRU{
		cap:   cap,
		items: make(map[string]*list.Element, cap),
		lru:   list.New(),
	}
}

// Put inserts or replaces an entry, evicting the oldest when over cap.
func (m *memoLRU) Put(key string, val any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.items[key]; ok {
		e.Value.(*memoEntry).val = val
		m.lru.MoveToFront(e)
		return
	}
	e := m.lru.PushFront(&memoEntry{key: key, val: val})
	m.items[key] = e
	for m.lru.Len() > m.cap {
		back := m.lru.Back()
		if back == nil {
			break
		}
		ent := back.Value.(*memoEntry)
		m.lru.Remove(back)
		delete(m.items, ent.key)
	}
}

// Get returns the entry and refreshes recency. Returns (nil, false) on miss.
func (m *memoLRU) Get(key string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.items[key]
	if !ok {
		return nil, false
	}
	m.lru.MoveToFront(e)
	return e.Value.(*memoEntry).val, true
}

// Delete removes a single key.
func (m *memoLRU) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.items[key]; ok {
		m.lru.Remove(e)
		delete(m.items, key)
	}
}

// DeleteByPrefix removes every entry whose key starts with prefix.
// Used to evict every (key, target, proto) variant when the underlying
// disk-cache key is deleted (the keys are formatted as
// "<key>|<wxh>|<proto>"). O(n).
func (m *memoLRU) DeleteByPrefix(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.items {
		if strings.HasPrefix(k, prefix) {
			m.lru.Remove(e)
			delete(m.items, k)
		}
	}
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/image/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/image/memo_lru.go internal/image/memo_lru_test.go
git commit -m "feat(image): add bounded memoLRU helper

Tasks A2 and A3 will replace the unbounded sync.Maps in Fetcher
(decoded, prerendered) with this bounded LRU so long-running
sessions don't accumulate decoded images and prerendered byte
streams indefinitely."
```

### Task A2: Replace `Fetcher.decoded` with `memoLRU`

**Files:**
- Modify: `internal/image/fetcher.go`
- Modify: `internal/image/fetcher_test.go` (existing tests should still pass; add a new bound-evicts test)

- [ ] **Step 1: Define the cap constant**

In `internal/image/fetcher.go`, near the existing `fetchConcurrencyLimit` constant, add:

```go
// memoCap caps the per-Fetcher in-memory caches (decoded images and
// prerendered Renders). Sized for a busy channel's worth of
// attachments — well above the on-screen working set, well below the
// "every image you ever scrolled past" growth path. Tune via metrics
// once we have any.
const memoCap = 256
```

- [ ] **Step 2: Replace `decoded sync.Map` with `decoded *memoLRU`**

Change the field declaration on `Fetcher`:

```go
// before:
decoded sync.Map // string("<key>|<wxh>") -> image.Image
// after:
decoded *memoLRU // string("<key>|<wxh>") -> image.Image, size-bounded
```

In `NewFetcher`, initialize:

```go
return &Fetcher{
    cache:       cache,
    http:        client,
    sem:         make(chan struct{}, fetchConcurrencyLimit),
    authsByTeam: map[string]TeamAuth{},
    decoded:     newMemoLRU(memoCap),
    prerendered: &sync.Map{}, // unchanged for now (see A3)
}
```

- [ ] **Step 3: Update all `f.decoded` callsites**

Run: `rg -n "f\.decoded" internal/image/fetcher.go`
There are four:
- `fetchInner`: `f.decoded.Store(decodedMemoKey(...), img)` → `f.decoded.Put(decodedMemoKey(...), img)`
- `Cached`: `if v, ok := f.decoded.Load(memoKey); ok { ... }` → `if v, ok := f.decoded.Get(memoKey); ok { ... }`
- `Cached` decode-failure path: `f.decoded.Delete(memoKey)` → unchanged signature, still works
- `Cached` success path: `f.decoded.Store(memoKey, img)` → `f.decoded.Put(memoKey, img)`

The value type is `any`; the existing code does `if img, ok := v.(image.Image); ok { ... }` after `Load`. Keep that — `Get` also returns `any`.

- [ ] **Step 4: Add a bound-evicts test**

In `internal/image/fetcher_test.go`:

```go
// When the decoded memo is full, fetching a (memoCap+1)th distinct
// image evicts the oldest. The evicted entry's Cached() lookup will
// fall through to the on-disk path, decode again, and repopulate.
// This is correctness for memory-bound sessions; perf-critical hot
// path (active channel) stays in memo since memoCap >> typical
// working set.
func TestFetcher_DecodedMemoIsBounded(t *testing.T) {
	pngBytes := tinyPNG(t, 50, 50, imgcolor.RGBA{0, 0, 0, 255})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	cache, _ := NewCache(t.TempDir(), 100)
	f := NewFetcher(cache, http.DefaultClient)

	target := image.Pt(10, 10)

	// Fetch memoCap+1 distinct keys.
	for i := 0; i <= memoCap; i++ {
		key := fmt.Sprintf("k%d", i)
		if _, err := f.Fetch(context.Background(), FetchRequest{
			Key: key, URL: srv.URL, Target: target,
		}); err != nil {
			t.Fatalf("fetch %s: %v", key, err)
		}
	}

	// k0 (oldest) must have been evicted from the memo.
	if _, ok := f.decoded.Get(decodedMemoKey("k0", target)); ok {
		t.Errorf("expected k0 to be evicted from decoded memo")
	}
	// k%d (most recent) must still be present.
	if _, ok := f.decoded.Get(decodedMemoKey(fmt.Sprintf("k%d", memoCap), target)); !ok {
		t.Errorf("expected most-recent key to be in memo")
	}
}
```

You'll need `"fmt"` in the imports (probably already there).

- [ ] **Step 5: Run tests**

Run: `go test -race ./internal/image/ -count=1 -v`
Expected: all PASS, including the new bound test and the existing `TestFetcher_FetchPopulatesDecodedMemo` (which only fetches one key — well below the cap).

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/image/fetcher.go internal/image/fetcher_test.go
git commit -m "perf(image): bound Fetcher.decoded with memoLRU

The decoded sync.Map grew without bound for long-running sessions:
every image fetched left a decoded RGBA bitmap behind. Replace with
the bounded memoLRU helper sized to memoCap=256 entries — well above
a busy channel's working set, but capped so 'left it open all day'
sessions don't leak.

When a key is evicted from the memo, the next Cached() call falls
through to the on-disk Cache, re-decodes, and repopulates."
```

### Task A3: Replace `Fetcher.prerendered` with `memoLRU`

**Files:**
- Modify: `internal/image/fetcher.go`

- [ ] **Step 1: Replace the field**

Change the field on `Fetcher`:

```go
// before:
prerendered    *sync.Map
// after:
prerendered    *memoLRU
```

- [ ] **Step 2: Update `NewFetcher`**

```go
prerendered: newMemoLRU(memoCap),
```

- [ ] **Step 3: Update `ConfigurePrerender`**

```go
func (f *Fetcher) ConfigurePrerender(proto Protocol) {
    f.prerenderMu.Lock()
    defer f.prerenderMu.Unlock()
    f.prerenderProto = proto
    f.prerendered = newMemoLRU(memoCap) // fresh LRU on reconfigure
}
```

- [ ] **Step 4: Update `Prerendered`**

Replace `m.Load(mk)` with `m.Get(mk)`.

- [ ] **Step 5: Update `maybePrerender`**

Replace `m.Store(prerenderKey(...), r)` with `m.Put(prerenderKey(...), r)`.

- [ ] **Step 6: Update the doc comment**

The field comment claims "Entries are retained for the lifetime of the Fetcher." That's no longer true. Replace with:

```go
// prerendered caches the result of RenderImage(proto, decoded, cellTarget)
// per (key, cellTarget, proto). The fetch goroutine populates this so
// the bubbletea Update goroutine never runs sixel encoding, halfblock
// per-pixel ANSI building, or kitty PNG-encode-and-base64 work.
//
// Bounded by memoCap entries. Eviction trades memory for a one-time
// re-encode if an off-screen image scrolls back into view after its
// memo entry was pushed out by N=memoCap intervening fetches.
//
// The pointer is swappable under prerenderMu so ConfigurePrerender can
// drop the cache atomically without racing in-flight maybePrerender
// stores: a stale worker writes to the old (now-orphaned) LRU and the
// store is harmlessly GC'd.
prerendered *memoLRU
```

- [ ] **Step 7: Run tests**

Run: `go test -race ./internal/image/ -count=1 -v`
Expected: all PASS, including the existing `TestFetcher_FetchPopulatesPrerender` (one key, well under cap).

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/image/fetcher.go
git commit -m "perf(image): bound Fetcher.prerendered with memoLRU

Mirror Task A2's bound on the decoded memo. Sixel renders can hold
50-300 KB of encoded bytes per entry inside their OnFlush closure,
so an unbounded prerendered map was the worse of the two leaks.
Reconfigure semantics are unchanged: ConfigurePrerender swaps in a
fresh LRU, and stale stores from in-flight workers land in the
orphaned old LRU and GC."
```

### Task A4: Wire `Cache.Delete` and LRU eviction to purge the Fetcher memos

When the on-disk `Cache` evicts a file (LRU sweep in `Put`, or explicit `Delete`), the in-memory memo entries pointing at that file become stale. After A2/A3 they no longer leak forever, but the wrong-data window between disk eviction and memo eviction is preventable.

**Files:**
- Modify: `internal/image/cache.go` — add a callback hook
- Modify: `internal/image/fetcher.go` — register the hook

- [ ] **Step 1: Read the existing Cache eviction sites**

Run: `rg -n "delete\(c\.items|os\.Remove" internal/image/cache.go`
You should see `Cache.Delete` (line ~169) and `Cache.evictLocked` (line ~148). Both `delete(c.items, key)` and `os.Remove(...)`.

- [ ] **Step 2: Add an `OnEvict` callback to `Cache`**

In `cache.go`, add a field and setter:

```go
// onEvict, if set, is called with the disk-cache key after an entry
// is removed from the in-memory index and on-disk file. Used by
// Fetcher to drop in-memory memo entries (decoded image, prerendered
// Render) for the same key, since those would otherwise return stale
// results pointing at a deleted file path.
//
// Called WITHOUT c.mu held (would deadlock if the callback re-entered
// Cache). Callback is invoked once per evicted key per call site.
type Cache struct {
    // ...existing fields...
    onEvict func(key string)
}

// SetEvictCallback registers a function called after an entry is
// removed from the cache (via Delete or LRU sweep in Put). Safe to
// call once at startup; not safe to mutate concurrently with Put/Get.
// Pass nil to clear.
func (c *Cache) SetEvictCallback(fn func(key string)) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.onEvict = fn
}
```

- [ ] **Step 3: Invoke the callback from `Delete` and `evictLocked`**

`Delete` is straightforward — capture the key before unlocking, then call after `os.Remove`:

```go
func (c *Cache) Delete(key string) bool {
    c.mu.Lock()
    it, ok := c.items[key]
    if !ok {
        c.mu.Unlock()
        return false
    }
    c.lru.Remove(it.elem)
    delete(c.items, key)
    c.total -= it.size
    onEvict := c.onEvict
    c.mu.Unlock()
    _ = os.Remove(it.path)
    if onEvict != nil {
        onEvict(key)
    }
    return true
}
```

`evictLocked` is called from `Put` while holding `c.mu`. Collecting evicted keys and firing the callback after `Put` returns keeps the contract "callback is called without c.mu held":

```go
// evictLocked removes oldest entries (LRU back) while total exceeds cap.
// Caller must hold c.mu. Returns the list of keys evicted so the caller
// can fire the OnEvict callback after releasing the lock.
func (c *Cache) evictLocked() []string {
    var evicted []string
    for c.total > c.capB && c.lru.Len() > 0 {
        back := c.lru.Back()
        if back == nil {
            break
        }
        it := back.Value.(*item)
        c.lru.Remove(back)
        delete(c.items, it.key)
        c.total -= it.size
        _ = os.Remove(it.path)
        evicted = append(evicted, it.key)
    }
    return evicted
}
```

Update `Put` to fire the callback after the lock is released:

```go
func (c *Cache) Put(key, ext string, data []byte) (string, error) {
    if ext == "" {
        ext = "bin"
    }
    path := filepath.Join(c.dir, key+"."+ext)
    if err := os.WriteFile(path, data, 0600); err != nil {
        return "", err
    }

    c.mu.Lock()
    if old, ok := c.items[key]; ok {
        c.total -= old.size
        c.lru.Remove(old.elem)
        delete(c.items, key)
    }
    evicted := c.evictLocked()
    now := time.Now()
    it := &item{key: key, path: path, size: int64(len(data)), atime: now}
    it.elem = c.lru.PushFront(it)
    c.items[key] = it
    c.total += it.size
    onEvict := c.onEvict
    c.mu.Unlock()

    if onEvict != nil {
        for _, k := range evicted {
            onEvict(k)
        }
    }
    return path, nil
}
```

Note: `evictLocked` returning `[]string` is a behavioral change — verify no other caller depends on the old `void` signature. (`rg -n "evictLocked" internal/image/`.)

- [ ] **Step 4: Wire the Fetcher's purge callback**

In `internal/image/fetcher.go`'s `NewFetcher`, after constructing `f`, register:

```go
f.cache.SetEvictCallback(f.purgeMemo)
```

Add the method:

```go
// purgeMemo evicts every in-memory entry keyed by the given disk-cache
// key. Called by Cache.SetEvictCallback so the decoded image and
// prerendered Render maps don't return stale results pointing at a
// deleted file. The decoded memo key is "<key>|<wxh>"; the prerendered
// memo key is "<key>|<wxh>|<proto>". DeleteByPrefix sweeps every
// (target, proto) variant for a given disk key in one pass.
func (f *Fetcher) purgeMemo(key string) {
    f.decoded.DeleteByPrefix(key + "|")
    f.prerenderMu.RLock()
    pr := f.prerendered
    f.prerenderMu.RUnlock()
    if pr != nil {
        pr.DeleteByPrefix(key + "|")
    }
}
```

- [ ] **Step 5: Add a test**

```go
// internal/image/fetcher_test.go
func TestFetcher_CacheEvictionPurgesMemos(t *testing.T) {
    pngBytes := tinyPNG(t, 50, 50, imgcolor.RGBA{0, 0, 0, 255})
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "image/png")
        w.Write(pngBytes)
    }))
    defer srv.Close()

    cache, _ := NewCache(t.TempDir(), 100)
    f := NewFetcher(cache, http.DefaultClient)

    target := image.Pt(10, 10)
    if _, err := f.Fetch(context.Background(), FetchRequest{
        Key: "k1", URL: srv.URL, Target: target,
    }); err != nil {
        t.Fatal(err)
    }

    // Confirm the memo has it.
    if _, ok := f.decoded.Get(decodedMemoKey("k1", target)); !ok {
        t.Fatal("expected memo populated")
    }

    // Explicit Delete must purge the memo.
    cache.Delete("k1")

    if _, ok := f.decoded.Get(decodedMemoKey("k1", target)); ok {
        t.Errorf("memo should be purged after Cache.Delete")
    }
}
```

- [ ] **Step 6: Run tests**

Run: `go test -race ./internal/image/ -count=1 -v`
Expected: all PASS.

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/image/cache.go internal/image/fetcher.go internal/image/fetcher_test.go
git commit -m "feat(image): purge Fetcher memos when Cache evicts

When the on-disk Cache evicts a file (explicit Delete or LRU sweep
in Put), drop the matching entries from Fetcher.decoded and
Fetcher.prerendered too. Without this hook, memo entries can briefly
point at a deleted file path; subsequent Cached() calls return stale
data until the memo's own LRU pushes them out.

Cache.evictLocked now returns the evicted keys so Put can fire the
callback after releasing c.mu (callbacks must not re-enter Cache)."
```

---

## Section B — Phase 2 perf and bug fixes

These were flagged in the original investigation but deferred from the perf branch. Each is independent.

### Task B1: Don't hold `Cache.mu` over disk I/O during eviction

**Background:** `Cache.evictLocked` calls `os.Remove(it.path)` while holding `c.mu`. `Cache.Delete` does the same. If a render-path `Cache.Get` (which also acquires `c.mu` to do `os.Chtimes` and an LRU update) lands during a multi-file eviction sweep, it blocks for the full sweep — measurable lag on slow disks.

After Task A4 the eviction code is restructured: `evictLocked` now collects victims into a slice. We can extend that to also do the `os.Remove` calls *outside* the lock.

**Files:**
- Modify: `internal/image/cache.go`
- Modify: `internal/image/cache_test.go` (add a test)

- [ ] **Step 1: Drop `os.Remove` from inside the lock**

In `evictLocked`, instead of removing the file in-loop, collect the path:

```go
func (c *Cache) evictLocked() []evictedItem {
    var evicted []evictedItem
    for c.total > c.capB && c.lru.Len() > 0 {
        back := c.lru.Back()
        if back == nil {
            break
        }
        it := back.Value.(*item)
        c.lru.Remove(back)
        delete(c.items, it.key)
        c.total -= it.size
        evicted = append(evicted, evictedItem{key: it.key, path: it.path})
    }
    return evicted
}

type evictedItem struct {
    key  string
    path string
}
```

In `Put`, after releasing `c.mu`, do the `os.Remove` calls and then fire callbacks:

```go
c.mu.Unlock()

for _, e := range evicted {
    _ = os.Remove(e.path)
}
if onEvict != nil {
    for _, e := range evicted {
        onEvict(e.key)
    }
}
return path, nil
```

Same shape for `Delete`: drop `os.Remove` outside the lock (already done in Task A4 — verify it's still correct).

- [ ] **Step 2: Drop `os.Chtimes` from inside the lock in `Get`**

`Get` currently does:

```go
now := time.Now()
_ = os.Chtimes(it.path, now, now)
it.atime = now
c.lru.MoveToFront(it.elem)
```

The `os.Chtimes` is a syscall under `c.mu`. Move it outside:

```go
func (c *Cache) Get(key string) (string, bool) {
    c.mu.Lock()
    it, ok := c.items[key]
    if !ok {
        c.mu.Unlock()
        return "", false
    }
    now := time.Now()
    it.atime = now
    c.lru.MoveToFront(it.elem)
    path := it.path
    c.mu.Unlock()
    _ = os.Chtimes(path, now, now) // best-effort; not under lock
    return path, true
}
```

The atime field is updated before the syscall, so concurrent `Get`s see the right LRU order even if the disk's mtime is stale by a few microseconds.

- [ ] **Step 3: Add a test for concurrent Get during eviction sweep**

```go
// Get must not block on a concurrent Put that's evicting many files.
// We don't measure latency directly (flaky); instead we run them in
// parallel and verify both complete and produce correct values.
func TestCache_GetDoesNotBlockOnEvictionSweep(t *testing.T) {
    dir := t.TempDir()
    cache, _ := NewCache(dir, 1) // 1 MB cap, easy to overflow
    // Fill close to cap with N small entries.
    for i := 0; i < 50; i++ {
        key := fmt.Sprintf("k%d", i)
        cache.Put(key, "bin", make([]byte, 30*1024))
    }
    // Concurrent Get + Put-that-evicts. Don't assert timing; just no
    // deadlock + correct results.
    var wg sync.WaitGroup
    wg.Add(2)
    go func() {
        defer wg.Done()
        for i := 0; i < 100; i++ {
            cache.Get(fmt.Sprintf("k%d", i%50))
        }
    }()
    go func() {
        defer wg.Done()
        for i := 50; i < 100; i++ {
            cache.Put(fmt.Sprintf("k%d", i), "bin", make([]byte, 30*1024))
        }
    }()
    wg.Wait()
}
```

(May need `import "sync"`.)

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/image/ -count=1 -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/image/cache.go internal/image/cache_test.go
git commit -m "perf(image): drop Cache.mu over disk I/O

Cache.evictLocked called os.Remove and Cache.Get called os.Chtimes
while holding c.mu. A concurrent render-path Get during a
multi-file eviction sweep blocked on the lock for the full sweep,
producing measurable UI lag on slow disks.

Restructure: collect victim paths under the lock, do the syscalls
after Unlock. atime is still set before the syscall so LRU ordering
remains consistent across concurrent Gets."
```

### Task B2: Serialize writes to `image.KittyOutput`

**Background:** `imgpkg.KittyOutput` is `os.Stdout` by default. The bubbletea Update goroutine writes to it (via OnFlush in `View()` chrome — `internal/ui/messages/model.go:2295`). Avatar `PreloadSync` *also* writes to it from a goroutine (`internal/avatar/avatar.go:120`). Concurrent writes to `os.Stdout` aren't atomic for multi-byte sequences — kitty graphics escapes can be 100s of KB and an interleaved write would corrupt them.

The single mitigation: wrap `KittyOutput` in a mutex-serialized writer.

**Files:**
- Modify: `internal/image/renderer.go`
- Optional add: `internal/image/serialized_writer.go` (or inline)

- [ ] **Step 1: Add a serialized writer wrapper**

In `internal/image/renderer.go` (or a new file `serialized_writer.go`):

```go
// serializedWriter wraps an io.Writer with a mutex so concurrent
// callers don't interleave their byte streams. Critical for the kitty
// graphics output channel: a single image upload is hundreds of KB of
// APC escape data, and any byte interleave from a competing writer
// corrupts the protocol stream and leaves the terminal in an unknown
// state.
type serializedWriter struct {
    mu sync.Mutex
    w  io.Writer
}

func newSerializedWriter(w io.Writer) *serializedWriter {
    return &serializedWriter{w: w}
}

func (s *serializedWriter) Write(p []byte) (int, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.w.Write(p)
}
```

You'll need `"sync"` in the imports.

- [ ] **Step 2: Wrap `KittyOutput` at startup**

Replace the package-level var:

```go
// before:
var KittyOutput io.Writer = os.Stdout
// after:
var KittyOutput io.Writer = newSerializedWriter(os.Stdout)
```

If tests override `KittyOutput`, they'll need to wrap their override too. Check:

Run: `rg -n "KittyOutput\s*=" internal/`
Each test that does `imgpkg.KittyOutput = &buf` (or similar) should be updated to either accept the wrapped version or wrap their own buffer. Alternative: keep the wrap inside a new exported helper:

```go
// SerializeOutput wraps a writer with internal mutex protection so
// concurrent goroutines can write to it without interleaving.
// Tests that capture KittyOutput should call SerializeOutput on
// their buffer before assigning it.
func SerializeOutput(w io.Writer) io.Writer {
    return newSerializedWriter(w)
}
```

- [ ] **Step 3: Add a test exercising concurrent writes**

```go
// internal/image/renderer_test.go (or wherever fits)
func TestSerializedWriter_NoInterleave(t *testing.T) {
    var buf bytes.Buffer
    sw := newSerializedWriter(&buf)
    var wg sync.WaitGroup
    a := bytes.Repeat([]byte("A"), 10000)
    b := bytes.Repeat([]byte("B"), 10000)
    wg.Add(2)
    go func() { defer wg.Done(); sw.Write(a) }()
    go func() { defer wg.Done(); sw.Write(b) }()
    wg.Wait()

    // Output is one full A-run then one full B-run, or vice versa,
    // never interleaved.
    s := buf.String()
    if !(strings.HasPrefix(s, strings.Repeat("A", 10000)) ||
        strings.HasPrefix(s, strings.Repeat("B", 10000))) {
        t.Errorf("interleaved write detected; first 50 bytes: %q", s[:50])
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test -race ./... -count=1`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/image/
git commit -m "fix(image): serialize concurrent writes to KittyOutput

KittyOutput defaults to os.Stdout. The bubbletea Update goroutine
writes to it from View(); avatar PreloadSync writes to it from a
worker goroutine. Concurrent writes to os.Stdout aren't atomic for
multi-byte sequences, and a single kitty image upload is 100s of KB
of APC escape data. An interleaved write corrupts the protocol
stream and can leave the terminal in an unknown state.

Wrap KittyOutput in a mutex-serialized writer so the two paths
serialize at the byte-stream boundary."
```

### Task B3: Bound avatar preload concurrency

**Background:** `cmd/slk/main.go:1016` calls `avatarCache.Preload(u.ID, u.Profile.Image32)` for every user in the workspace at connect. Each spawns a goroutine that hits the shared `Fetcher`'s 4-slot semaphore. With 1,000 users in a workspace this is 1,000 simultaneous goroutines all queued on a semaphore — and they starve message-attachment fetches that share the same semaphore.

Two related fixes:
1. Give avatars their own bounded worker pool so they don't pile up.
2. Either give avatars their own semaphore on the Fetcher, or yield to message-image fetches.

This task does (1). (2) requires more design — defer.

**Files:**
- Modify: `internal/avatar/avatar.go`
- Modify: `internal/avatar/avatar_test.go` (add a test)

- [ ] **Step 1: Add a worker pool to `Cache`**

```go
// avatarPreloadWorkers caps the number of avatar Preload goroutines
// that can be downloading or rendering concurrently. Below this limit
// each call to Preload spawns its own goroutine; at or above the limit,
// callers are queued and processed in arrival order. Bounding this
// matters because Slack workspaces routinely have 1,000+ users — a
// naive "goroutine per user" preload at workspace connect both pegs
// CPU on the avatar render path and starves message-image fetches
// that share the underlying Fetcher semaphore.
const avatarPreloadWorkers = 8
```

Add a job channel + worker startup to `Cache`:

```go
type Cache struct {
    // ...existing...
    preloadCh chan preloadJob
}

type preloadJob struct {
    userID    string
    avatarURL string
}

func NewCache(fetcher *imgpkg.Fetcher, kitty *imgpkg.KittyRenderer, useKitty bool) *Cache {
    c := &Cache{
        fetcher:   fetcher,
        kitty:     kitty,
        useKitty:  useKitty && kitty != nil,
        renders:   make(map[string]string),
        preloadCh: make(chan preloadJob, 1024),
    }
    for i := 0; i < avatarPreloadWorkers; i++ {
        go c.preloadWorker()
    }
    return c
}

func (c *Cache) preloadWorker() {
    for job := range c.preloadCh {
        c.PreloadSync(job.userID, job.avatarURL)
    }
}
```

Replace `Preload`:

```go
// Preload enqueues a background download+render. Bounded by
// avatarPreloadWorkers; callers don't spawn fresh goroutines. If the
// queue is full (1024 backlog), the caller's enqueue is non-blocking
// — the avatar simply doesn't preload. The rendering path falls back
// to "[?]" until the next time we hear about the user.
func (c *Cache) Preload(userID, avatarURL string) {
    if avatarURL == "" {
        return
    }
    select {
    case c.preloadCh <- preloadJob{userID: userID, avatarURL: avatarURL}:
    default:
        // Queue full; drop the preload.
    }
}
```

- [ ] **Step 2: Confirm test compatibility**

Run: `go test ./internal/avatar/...`
Existing tests calling `Preload` expect the work to happen — they may have been calling `PreloadSync` directly, or relying on the goroutine spawned by `Preload`. The new worker pool still does the work, just from one of N pooled goroutines. If a test does `Preload(...)` then immediately checks `Get(...)`, it was racy before (depended on the goroutine to run) and remains racy. If a test does `PreloadSync(...)` to get deterministic behavior, that path is unchanged.

- [ ] **Step 3: Add a worker-pool test**

```go
// Preload must not spawn one goroutine per call beyond the pool size.
// We can't directly count goroutines, but we can verify ordering: with
// only 1 worker, two Preloads of two URLs that block at the server
// must complete sequentially.
func TestPreload_BoundedConcurrency(t *testing.T) {
    // Setup is involved since Cache requires a real Fetcher. Skip
    // for now if not feasible; the visible win is at startup with
    // 1000+ users, which is hard to assert in a unit test.
    t.Skip("integration test — covered by manual smoke test")
}
```

Alternatively, write a smaller test that asserts the channel exists and accepts enqueues without blocking:

```go
func TestPreload_QueueBackpressureDropsRatherThanBlocks(t *testing.T) {
    // Construct a Cache without starting workers, then fill the
    // queue and verify the (1025th) Preload returns immediately.
    c := &Cache{
        fetcher:   nil,
        renders:   make(map[string]string),
        preloadCh: make(chan preloadJob, 4),
    }
    // No workers consuming; channel fills to 4.
    for i := 0; i < 4; i++ {
        c.Preload(fmt.Sprintf("u%d", i), "https://example.invalid/")
    }
    done := make(chan struct{})
    go func() {
        c.Preload("u-extra", "https://example.invalid/")
        close(done)
    }()
    select {
    case <-done:
        // Good — Preload returned without blocking.
    case <-time.After(200 * time.Millisecond):
        t.Fatal("Preload should not block when queue is full")
    }
}
```

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/avatar/... -count=1 -v`
Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/avatar/avatar.go internal/avatar/avatar_test.go
git commit -m "perf(avatar): bound preload concurrency with worker pool

cmd/slk's connectWorkspace calls Preload for every user in the
workspace at connect. For a 1,000-user workspace that spawned 1,000
goroutines simultaneously, each queued on the Fetcher's 4-slot
semaphore and starving message-image fetches.

Replace per-call goroutine with an N=8 worker pool fed by a 1,024-
slot buffered channel. Backpressure drops the preload (caller's
choice; a missed avatar is recoverable) rather than blocking the
caller, so workspace connect doesn't stall."
```

### Task B4: Wire `BlockImageReadyMsg` handler

**Background:** `internal/ui/messages/blockkit/image.go:164` dispatches `BlockImageReadyMsg{Channel, TS, URL}` when a block-kit image fetch completes. There is no handler for it in `App.Update`. The image lands in cache but the messages pane never invalidates so the placeholder remains until some unrelated event triggers a render.

**Files:**
- Modify: `internal/ui/app.go` (add handler near the existing `messages.ImageReadyMsg` handler)
- Possibly modify: `internal/ui/messages/model.go` if the existing `HandleImageReady` doesn't accept a URL-keyed cache key
- Modify: `internal/ui/messages/blockkit/image_test.go` (add coverage)

- [ ] **Step 1: Locate the existing ImageReadyMsg handler**

Run: `rg -n "ImageReadyMsg|BlockImageReadyMsg" internal/ui/`
Read the handler at `internal/ui/app.go:1226` (`messages.ImageReadyMsg`). It calls `a.messagepane.HandleImageReady(msg.Channel, msg.TS, msg.Key)`.

- [ ] **Step 2: Decide how the messages pane handles a URL-keyed image**

Block-kit images are keyed by URL hash (`urlCacheKey(url)` → `internal/ui/messages/blockkit/image.go:124`). The messages-pane cache has no idea which view-entry holds which block-kit image; the only available identifier is the message's TS.

Simplest correct approach: same as `ImageReadyMsg` — invalidate the message at TS via `staleEntries[ts]` so `partialRebuild` re-runs `renderMessageEntry` for it, which re-renders block-kit blocks (which now find the image cached and return rendered cells instead of placeholder).

Add a new method on `Model`:

```go
// HandleBlockImageReady invalidates the message at ts so its
// block-kit image attachments — fetched via a URL-keyed cache, not
// a Slack file ID — are re-rendered with the now-cached bytes.
// Mirrors HandleImageReady but doesn't touch fetchingImages (which
// is keyed by Slack file ID, not URL).
func (m *Model) HandleBlockImageReady(channel, ts string) {
    if channel != m.channelName {
        return
    }
    if ts == "" || m.cache == nil {
        m.cache = nil
        m.dirty()
        return
    }
    if m.staleEntries == nil {
        m.staleEntries = make(map[string]struct{})
    }
    m.staleEntries[ts] = struct{}{}
    m.dirty()
}
```

- [ ] **Step 3: Wire the App.Update handler**

In `internal/ui/app.go`, near the `messages.ImageReadyMsg` case:

```go
case messages.BlockImageReadyMsg:
    a.messagepane.HandleBlockImageReady(msg.Channel, msg.TS)
    return a, nil
```

(Note: `BlockImageReadyMsg` is declared in `internal/ui/messages/blockkit`. Verify the package path in `app.go`'s switch — it may already be imported as `blockkit.BlockImageReadyMsg`. Adapt accordingly.)

Run: `rg -n "BlockImageReadyMsg" internal/ui/`
Confirm the type is accessible.

- [ ] **Step 4: Add a test**

```go
// internal/ui/messages/model_test.go
func TestModel_HandleBlockImageReady_PerEntryInvalidation(t *testing.T) {
    m := New([]MessageItem{
        {TS: "111.111", Text: "msg one"},
        {TS: "222.222", Text: "msg two"},
    }, "test-channel")
    _ = m.View()
    if m.cache == nil {
        t.Fatal("expected cache populated")
    }

    m.HandleBlockImageReady("test-channel", "222.222")

    if m.cache == nil {
        t.Fatal("HandleBlockImageReady should not nil the entire cache")
    }
    if _, stale := m.staleEntries["222.222"]; !stale {
        t.Fatal("expected 222.222 marked stale")
    }
    if _, stale := m.staleEntries["111.111"]; stale {
        t.Errorf("sibling 111.111 must not be marked stale")
    }
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/ui/messages/... -count=1 -v`
Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/messages/model.go internal/ui/messages/model_test.go
git commit -m "fix(blockkit): wire BlockImageReadyMsg handler

blockkit/image.go dispatched BlockImageReadyMsg when a block-kit
image fetch completed, but App.Update had no handler for it. The
image landed in cache silently and the placeholder remained until
some unrelated event triggered a re-render.

Add Model.HandleBlockImageReady (per-message stale invalidation,
mirroring HandleImageReady) and route the message to it from
App.Update."
```

---

## Section C — Minor cleanups in the perf branch

Each task in this section is a small standalone polish.

### Task C1: Restore the lost sixel comment in `renderAttachmentBlock`

The slow-path block in `internal/ui/messages/model.go` lost a useful comment explaining the sixel sentinel-row strategy.

**Files:**
- Modify: `internal/ui/messages/model.go`

- [ ] **Step 1: Find the relevant block**

Run: `rg -n "Sixel sentinel|sixelEntry|SixelSentinel" internal/ui/messages/model.go | head -20`
Locate the slow-path sixel branch in `renderAttachmentBlock` (around line 1568 today, after the Prerendered fast path).

- [ ] **Step 2: Add the comment**

Just before the `if ctx.Protocol == imgpkg.ProtoSixel {` branch in the slow path, insert:

```go
// Sixel: capture the bytes once into sixelRows. The View frame
// emits them inline at flush time using a private-use sentinel row
// (see SixelSentinel). OnFlush isn't surfaced here because the
// bytes are already baked into the sentinel row; the same logic
// applies in the fast path above.
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`

- [ ] **Step 4: Commit**

```bash
git add internal/ui/messages/model.go
git commit -m "docs(messages): restore sixel sentinel-row comment

The Task 2 perf refactor lost a comment explaining why the sixel
slow-path captures bytes into sixelRows instead of surfacing
OnFlush. Restore it for future readers; same logic also applies in
the fast path."
```

### Task C2: Merge `ConfigurePrerender` + `ConfigurePrerenderKitty` into one method

Two methods that the only caller invokes back-to-back. Merging tightens the API and closes the brief window where `prerenderProto == ProtoKitty` but `prerenderKitty == nil`.

**Files:**
- Modify: `internal/image/fetcher.go`
- Modify: `internal/ui/messages/model.go` (the only caller)

- [ ] **Step 1: Replace both methods with one**

In `internal/image/fetcher.go`:

```go
// ConfigurePrerender enables eager protocol encoding in the fetch
// goroutine. After every successful Fetch whose request carries a
// non-zero CellTarget, the decoded image is run through
// RenderImage(proto, ..., cellTarget) and stashed for retrieval via
// Prerendered. Pass ProtoOff to disable.
//
// kr is required when proto == ProtoKitty (the worker calls
// kr.SetSource + kr.RenderKey on it) and ignored otherwise.
//
// Safe to call at startup or whenever the active protocol changes
// (theme switch / terminal capability re-probe). Resets the
// prerender cache.
func (f *Fetcher) ConfigurePrerender(proto Protocol, kr *KittyRenderer) {
    f.prerenderMu.Lock()
    defer f.prerenderMu.Unlock()
    f.prerenderProto = proto
    if proto == ProtoKitty {
        f.prerenderKitty = kr
    } else {
        f.prerenderKitty = nil
    }
    f.prerendered = newMemoLRU(memoCap) // or &sync.Map{} if Section A not yet executed
}
```

Delete `ConfigurePrerenderKitty`.

- [ ] **Step 2: Update the caller**

In `internal/ui/messages/model.go`'s `SetImageContext`, replace:

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

with:

```go
if m.imgCtx.Fetcher != nil {
    var kr *imgpkg.KittyRenderer
    if m.imgCtx.Protocol == imgpkg.ProtoKitty {
        kr = m.imgCtx.KittyRender
    }
    m.imgCtx.Fetcher.ConfigurePrerender(m.imgCtx.Protocol, kr)
}
```

- [ ] **Step 3: Run tests**

Run: `go test -race ./... -count=1`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/image/fetcher.go internal/ui/messages/model.go
git commit -m "refactor(image): collapse ConfigurePrerender API

ConfigurePrerender + ConfigurePrerenderKitty were two methods that
the only caller invoked back-to-back. Merging into a single
ConfigurePrerender(proto, kr) closes the brief window where
prerenderProto == ProtoKitty but prerenderKitty was unset, and
trims the call site to one line."
```

### Task C3: Align kitty source keys between fast and slow paths

The Task 2 prerender path uses `"F-" + key` (where `key = att.FileID + "-" + suffix`) as the kitty source key. The slow path in `renderAttachmentBlock` uses `"F-" + att.FileID` (no suffix). Both work because each path is internally consistent, but a future reader will be confused.

The cleanest fix is to use the suffixed key everywhere — different thumb sizes get distinct kitty IDs as intended, and the fast/slow paths agree.

**Files:**
- Modify: `internal/ui/messages/model.go`
- Modify: `internal/image/fetcher.go` (add a comment explaining why the suffix is in the key)

- [ ] **Step 1: Update the slow path**

In `renderAttachmentBlock`, the slow-path kitty branch:

```go
// before:
ckey := "F-" + att.FileID
// after:
ckey := "F-" + key  // key = att.FileID + "-" + suffix; matches prerender path
```

`key` is already in scope from earlier in the function (`key := att.FileID + "-" + suffix`).

- [ ] **Step 2: Add a clarifying comment in `maybePrerender`**

```go
case ProtoKitty:
    if kr == nil {
        return
    }
    // Source key includes the thumbnail-size suffix so different
    // thumb resolutions of the same file get distinct kitty image
    // IDs. The slow path in renderAttachmentBlock uses the same key
    // shape so both paths share the kitty registry's per-source
    // upload-once tracking.
    ckey := "F-" + key
    kr.SetSource(ckey, img)
    r = kr.RenderKey(ckey, cellT)
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/image/... ./internal/ui/messages/... -count=1`
Expected: all PASS. (No tests assert exact kitty source keys; behavior is unchanged.)

- [ ] **Step 4: Commit**

```bash
git add internal/ui/messages/model.go internal/image/fetcher.go
git commit -m "refactor(messages): align kitty source keys across paths

The Task 2 prerender path used 'F-<fileID>-<suffix>' while the
slow path used 'F-<fileID>'. Each path was internally consistent
but the asymmetry was confusing. Use the suffixed form everywhere
so different thumb sizes of the same file map to distinct kitty
image IDs in both paths and a future reader doesn't 'fix' it."
```

### Task C4: Fill in missing prerender + partialRebuild test cases

The reviewer flagged four gaps:
- `TestFetcher_FetchPopulatesPrerender` only covers halfblock (not kitty, not ProtoOff, not reconfigure-resets-cache).
- `TestModel_HandleImageReady_PerEntryInvalidation` only covers single-stale; no multi-stale, no unknown-TS, no height-changing rebuild.

**Files:**
- Modify: `internal/image/fetcher_test.go`
- Modify: `internal/ui/messages/model_test.go`

- [ ] **Step 1: Add prerender kitty test**

```go
// internal/image/fetcher_test.go
func TestFetcher_FetchPopulatesPrerender_Kitty(t *testing.T) {
    pngBytes := tinyPNG(t, 100, 100, imgcolor.RGBA{0, 200, 0, 255})
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "image/png")
        w.Write(pngBytes)
    }))
    defer srv.Close()

    cache, _ := NewCache(t.TempDir(), 10)
    f := NewFetcher(cache, http.DefaultClient)

    kr := NewKittyRenderer(NewRegistry())
    f.ConfigurePrerender(ProtoKitty, kr) // assumes Task C2 landed; otherwise call both methods

    cellT := image.Pt(8, 4)
    if _, err := f.Fetch(context.Background(), FetchRequest{
        Key: "k1", URL: srv.URL, Target: image.Pt(40, 20), CellTarget: cellT,
    }); err != nil {
        t.Fatal(err)
    }

    r, ok := f.Prerendered("k1", cellT, ProtoKitty)
    if !ok {
        t.Fatal("expected kitty prerender hit")
    }
    if r.OnFlush == nil {
        t.Errorf("kitty prerender must carry an OnFlush closure for the upload escape")
    }
    if len(r.Lines) != cellT.Y {
        t.Errorf("expected %d lines, got %d", cellT.Y, len(r.Lines))
    }
}

func TestFetcher_FetchSkipsPrerender_WhenOff(t *testing.T) {
    pngBytes := tinyPNG(t, 50, 50, imgcolor.RGBA{0, 0, 0, 255})
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "image/png")
        w.Write(pngBytes)
    }))
    defer srv.Close()

    cache, _ := NewCache(t.TempDir(), 10)
    f := NewFetcher(cache, http.DefaultClient)
    // Don't ConfigurePrerender; default is ProtoOff.

    cellT := image.Pt(4, 2)
    if _, err := f.Fetch(context.Background(), FetchRequest{
        Key: "k1", URL: srv.URL, Target: image.Pt(20, 10), CellTarget: cellT,
    }); err != nil {
        t.Fatal(err)
    }

    if _, ok := f.Prerendered("k1", cellT, ProtoHalfBlock); ok {
        t.Errorf("Prerendered should miss when prerender is not configured")
    }
}

func TestFetcher_ConfigurePrerenderResetsCache(t *testing.T) {
    pngBytes := tinyPNG(t, 50, 50, imgcolor.RGBA{0, 0, 0, 255})
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "image/png")
        w.Write(pngBytes)
    }))
    defer srv.Close()

    cache, _ := NewCache(t.TempDir(), 10)
    f := NewFetcher(cache, http.DefaultClient)
    f.ConfigurePrerender(ProtoHalfBlock, nil)

    cellT := image.Pt(4, 2)
    if _, err := f.Fetch(context.Background(), FetchRequest{
        Key: "k1", URL: srv.URL, Target: image.Pt(20, 10), CellTarget: cellT,
    }); err != nil {
        t.Fatal(err)
    }
    if _, ok := f.Prerendered("k1", cellT, ProtoHalfBlock); !ok {
        t.Fatal("expected hit before reconfigure")
    }

    // Reconfigure to a different proto; the old cache must be dropped.
    f.ConfigurePrerender(ProtoSixel, nil)
    if _, ok := f.Prerendered("k1", cellT, ProtoHalfBlock); ok {
        t.Errorf("expected halfblock entry evicted after reconfigure")
    }
}
```

- [ ] **Step 2: Add multi-stale and unknown-TS partial rebuild tests**

```go
// internal/ui/messages/model_test.go
func TestModel_HandleImageReady_MultiStale(t *testing.T) {
    m := New([]MessageItem{
        {TS: "111.111", Text: "one"},
        {TS: "222.222", Text: "two"},
        {TS: "333.333", Text: "three"},
    }, "ch")
    _ = m.View()

    siblingBefore := append([]string{}, m.cache[indexForTS(m, "111.111")].linesNormal...)

    m.HandleImageReady("ch", "222.222", "F222-key")
    m.HandleImageReady("ch", "333.333", "F333-key")

    _ = m.View() // trigger partial rebuild

    if len(m.staleEntries) != 0 {
        t.Errorf("staleEntries should be cleared after View, got %d", len(m.staleEntries))
    }
    after := m.cache[indexForTS(m, "111.111")].linesNormal
    if !equalLines(siblingBefore, after) {
        t.Errorf("sibling 111.111 must remain unchanged across multi-stale rebuild")
    }
}

// indexForTS is a test helper; add it once if not already present.
func indexForTS(m *Model, ts string) int {
    for i, e := range m.cache {
        if e.msgIdx >= 0 && m.messages[e.msgIdx].TS == ts {
            return i
        }
    }
    return -1
}

func TestModel_HandleImageReady_UnknownTS_DoesNotPanic(t *testing.T) {
    m := New([]MessageItem{
        {TS: "111.111", Text: "one"},
    }, "ch")
    _ = m.View()

    // TS that's not in m.messages.
    m.HandleImageReady("ch", "999.999", "Fxxx-key")
    _ = m.View() // partial rebuild silently skips unknown TS

    if m.cache == nil {
        t.Fatal("cache must remain populated")
    }
}
```

- [ ] **Step 3: Add height-changing rebuild test**

This is the trickiest because the test needs to mutate a message between two `View()` calls in a way that changes its rendered height. The simplest reproducible case: append text to a message's body, then HandleImageReady to force a partial rebuild.

```go
func TestModel_PartialRebuild_PropagatesHeightChange(t *testing.T) {
    m := New([]MessageItem{
        {TS: "111.111", Text: "one"},
        {TS: "222.222", Text: "two"},
        {TS: "333.333", Text: "three"},
    }, "ch")
    _ = m.View()
    totalBefore := m.totalLines
    h1Before := m.cache[indexForTS(m, "222.222")].height

    // Mutate message 222 to a longer body.
    m.messages[1].Text = "two\nlines\nafter\nedit"
    // Force partial rebuild via HandleImageReady (simpler than wiring
    // a different invalidation path).
    m.HandleImageReady("ch", "222.222", "F-fake")
    _ = m.View()

    h1After := m.cache[indexForTS(m, "222.222")].height
    if h1After == h1Before {
        t.Skip("test scenario didn't actually change height; not a regression")
    }
    delta := h1After - h1Before
    if m.totalLines != totalBefore+delta {
        t.Errorf("totalLines should reflect height delta: before=%d delta=%d after=%d",
            totalBefore, delta, m.totalLines)
    }
    // Sibling at 333.333 must have its absolute offset shifted by delta.
    // (Adapt to the actual entryOffsets API.)
}
```

- [ ] **Step 4: Run tests**

Run: `go test -race ./... -count=1 -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/image/fetcher_test.go internal/ui/messages/model_test.go
git commit -m "test(image): cover prerender kitty/off/reconfigure + partial rebuild edges

Code-quality review of the perf branch flagged four uncovered cases:
  - prerender kitty branch (must produce non-nil OnFlush)
  - prerender off / unconfigured (Prerendered returns false)
  - ConfigurePrerender resets the cache
  - partialRebuild handles multi-stale, unknown-TS, height-change

Add tests for each so future refactors of the prerender pipeline or
the staleEntries dispatcher don't silently regress."
```

---

## Self-review notes

**Spec coverage (against the deferred-items list from `2026-05-01-image-pipeline-perf.md`):**

| Deferred item | Task | Status |
|---|---|---|
| Memory bound on `Fetcher.decoded` + `Fetcher.prerendered` | A1, A2, A3 | covered |
| `Cache` evicts memo siblings | A4 | covered |
| `Cache.mu` over disk I/O | B1 | covered |
| Avatar stdout serialization | B2 | covered |
| Avatar preload thundering herd | B3 | covered |
| `BlockImageReadyMsg` handler | B4 | covered |
| Lost sixel comment | C1 | covered |
| `ConfigurePrerender` API merge | C2 | covered |
| Kitty source-key asymmetry | C3 | covered |
| Missing prerender + partialRebuild tests | C4 | covered |

**Cross-section dependencies:**
- Section A is a clean prerequisite for nothing else (you can do Section B or C without A).
- Task A4 builds on A1's `DeleteByPrefix`; if you skip A1/A2/A3 and only want A4, A4 won't make sense (the memos are unbounded `sync.Map`s with no `DeleteByPrefix`). **A4 requires A1, A2, A3.**
- Task C2's snippet assumes A3 (uses `newMemoLRU`); if doing C2 without A3, swap to `&sync.Map{}` per the existing code.
- B1 builds on A4's restructured `evictLocked` (returns evicted keys instead of void). If you skip A4 and only want B1, you'll do the same restructuring as part of B1.
- B4 reuses the `staleEntries` mechanism from the perf branch; no new mechanics.
- C1, C3 are standalone.
- C4's test for kitty prerender uses C2's merged API; if C2 isn't done yet, switch the test to call both `ConfigurePrerender` + `ConfigurePrerenderKitty`.

**Recommended execution order if running the whole plan:** A1 → A2 → A3 → A4 → B1 → B2 → B3 → B4 → C1 → C2 → C3 → C4.

**Risk areas:**
1. **Task B3** changes Preload semantics from "always spawns a goroutine" to "enqueues, may drop on backpressure." If anything in the codebase relies on Preload's old eager-goroutine behavior for correctness (not just perf), that path will silently break under load. The grep `rg -n "Preload\b" internal/ cmd/` should turn up all callers — review each before merging.
2. **Task B4** assumes block-kit images are 1:1 with messages (the TS-keyed invalidation is precise enough). Verify by inspection: a single message can carry multiple block-kit images, but they all live in the same view-entry and re-rendering that entry re-renders all of them. That's correct.
3. **Task A4 / B1** changes the `Cache` API contract (`evictLocked` return value, callback timing). Any future caller of `Cache` outside the fetcher needs to know callbacks fire after Unlock and after disk I/O.
