# Phase 1: `internal/image` Package Foundation

> Index: `00-overview.md`. Next: `02-avatar-refactor.md`.

**Goal:** Stand up the `internal/image` package with capability detection, cell-metrics, an LRU disk cache, an HTTP fetcher with single-flight, the half-block renderer, and the thumb-picker. Pure-package only — no UI integration.

**At the end of this phase:** `go test ./internal/image/...` passes. The package is unused by the rest of the codebase (will be wired in Phase 2).

**Spec sections covered:** Architecture, Capability Detection, Cell Metrics, Image Cache, Fetcher, Renderer (half-block).

---

## Task 1.1: Create package skeleton with `Protocol` enum and `Renderer` interface

**Files:**
- Create: `internal/image/renderer.go`

- [ ] **Step 1: Create `internal/image/renderer.go`**

```go
// Package image renders bitmap images for terminal display via three
// protocols: kitty graphics (preferred), sixel, and unicode half-block.
// The package also owns image fetching, decoding, downscaling, and the
// on-disk cache shared with the avatar subsystem.
package image

import (
	"image"
	"io"
)

// Protocol enumerates the rendering protocols this package can emit.
type Protocol int

const (
	// ProtoOff disables image rendering; consumers should fall back to text.
	ProtoOff Protocol = iota
	// ProtoHalfBlock uses the ▀ upper-half-block character with 24-bit color.
	ProtoHalfBlock
	// ProtoSixel uses the DEC sixel protocol.
	ProtoSixel
	// ProtoKitty uses the kitty graphics protocol with unicode placeholders.
	ProtoKitty
)

// String returns a human-readable protocol name (used in logs and config).
func (p Protocol) String() string {
	switch p {
	case ProtoOff:
		return "off"
	case ProtoHalfBlock:
		return "halfblock"
	case ProtoSixel:
		return "sixel"
	case ProtoKitty:
		return "kitty"
	default:
		return "unknown"
	}
}

// Render is one renderer's output for a single image at a single target size.
// Lines and Fallback are always exactly Cells.Y rows long and each row is
// Cells.X cells wide (per lipgloss.Width). The messages-pane render cache
// treats Lines like any other text content.
type Render struct {
	// Cells is the (cols, rows) footprint in terminal cells.
	Cells image.Point

	// Lines is the per-row text/escape content baked into the message cache.
	Lines []string

	// Fallback is the half-block equivalent used when partial visibility
	// prevents the primary protocol from emitting (sixel only). For
	// half-block and kitty renders, Fallback equals Lines.
	Fallback []string

	// OnFlush is an optional pre-frame side effect (kitty image upload).
	// Called at most once per frame across all rendered images. Idempotent.
	OnFlush func(io.Writer) error

	// ID is a protocol-specific image ID. Zero when the protocol has no
	// notion of a stable image identifier.
	ID uint32
}

// Renderer encodes an in-memory image into a Render at a target cell footprint.
type Renderer interface {
	Render(img image.Image, target image.Point) Render
}
```

- [ ] **Step 2: Verify the package builds**

Run: `go build ./internal/image/...`
Expected: success, no output.

- [ ] **Step 3: Commit**

```bash
git add internal/image/renderer.go
git commit -m "feat(image): add internal/image package skeleton with Protocol and Renderer"
```

---

## Task 1.2: Capability detection

**Files:**
- Create: `internal/image/capability.go`
- Create: `internal/image/capability_test.go`

The `Detect` function maps environment variables and a config string to a `Protocol`. Pure function — no side effects, no terminal queries (the kitty version probe is a separate function called from main).

- [ ] **Step 1: Write the failing test**

Create `internal/image/capability_test.go`:

```go
package image

import "testing"

func TestDetect_ConfigOverrides(t *testing.T) {
	cases := []struct {
		cfg  string
		want Protocol
	}{
		{"off", ProtoOff},
		{"halfblock", ProtoHalfBlock},
		{"sixel", ProtoSixel},
		{"kitty", ProtoKitty},
	}
	for _, tc := range cases {
		got := Detect(Env{}, tc.cfg)
		if got != tc.want {
			t.Errorf("cfg=%q: got %v, want %v", tc.cfg, got, tc.want)
		}
	}
}

func TestDetect_TmuxForcesHalfBlock(t *testing.T) {
	env := Env{TMUX: "/tmp/tmux", KittyWindowID: "1", Term: "xterm-kitty"}
	if got := Detect(env, "auto"); got != ProtoHalfBlock {
		t.Errorf("expected halfblock under tmux, got %v", got)
	}
}

func TestDetect_KittyByEnvVar(t *testing.T) {
	cases := []Env{
		{KittyWindowID: "1"},
		{Term: "xterm-kitty"},
		{TermProgram: "ghostty"},
		{TermProgram: "WezTerm"},
	}
	for i, env := range cases {
		if got := Detect(env, "auto"); got != ProtoKitty {
			t.Errorf("case %d (%+v): want kitty, got %v", i, env, got)
		}
	}
}

func TestDetect_Sixel(t *testing.T) {
	cases := []Env{
		{Term: "foot"},
		{Term: "mlterm"},
	}
	for _, env := range cases {
		if got := Detect(env, "auto"); got != ProtoSixel {
			t.Errorf("env=%+v: want sixel, got %v", env, got)
		}
	}
}

func TestDetect_FallbackHalfBlock(t *testing.T) {
	env := Env{Term: "xterm-256color", Colorterm: "truecolor"}
	if got := Detect(env, "auto"); got != ProtoHalfBlock {
		t.Errorf("want halfblock fallback, got %v", got)
	}
}

func TestDetect_AutoUnknownConfigDefaultsToAuto(t *testing.T) {
	// Empty cfg or unknown values are treated as "auto".
	if got := Detect(Env{Term: "xterm-kitty"}, ""); got != ProtoKitty {
		t.Errorf("empty cfg should be auto, got %v", got)
	}
	if got := Detect(Env{Term: "xterm-kitty"}, "bogus"); got != ProtoKitty {
		t.Errorf("unknown cfg should be auto, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestDetect`
Expected: build error — `Detect`/`Env` undefined.

- [ ] **Step 3: Implement capability detection**

Create `internal/image/capability.go`:

```go
package image

import "strings"

// Env is a snapshot of terminal-related environment variables.
// Captured separately so tests can inject values without touching os.Getenv.
type Env struct {
	TMUX          string
	KittyWindowID string
	Term          string
	TermProgram   string
	Colorterm     string
}

// CaptureEnv reads the relevant environment variables from the OS.
func CaptureEnv() Env {
	return Env{
		TMUX:          getenv("TMUX"),
		KittyWindowID: getenv("KITTY_WINDOW_ID"),
		Term:          getenv("TERM"),
		TermProgram:   getenv("TERM_PROGRAM"),
		Colorterm:     getenv("COLORTERM"),
	}
}

// Detect picks the rendering protocol for the current terminal.
// cfg is the user's config value (e.g. "auto", "kitty", "sixel", "halfblock", "off").
// Anything other than the four explicit values is treated as "auto".
func Detect(env Env, cfg string) Protocol {
	switch strings.ToLower(strings.TrimSpace(cfg)) {
	case "off":
		return ProtoOff
	case "halfblock":
		return ProtoHalfBlock
	case "sixel":
		return ProtoSixel
	case "kitty":
		return ProtoKitty
	}
	// auto
	if env.TMUX != "" {
		return ProtoHalfBlock
	}
	if env.KittyWindowID != "" || env.Term == "xterm-kitty" {
		return ProtoKitty
	}
	switch env.TermProgram {
	case "ghostty", "WezTerm":
		return ProtoKitty
	}
	if env.Term == "foot" || env.Term == "mlterm" {
		return ProtoSixel
	}
	return ProtoHalfBlock
}
```

We need a `getenv` helper that's overridable in tests. Add to the same file:

```go
// getenv is overridable in tests. Production reads os.Getenv.
var getenv = func(key string) string {
	return osGetenv(key)
}
```

And reference `osGetenv` from a separate small file so we can stub `getenv` cleanly:

Add `osGetenv` directly:

```go
import "os"

// (replace the previous getenv block with this)
var getenv = os.Getenv
```

The full file imports become:

```go
import (
	"os"
	"strings"
)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/image/ -run TestDetect -v`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/image/capability.go internal/image/capability_test.go
git commit -m "feat(image): add capability detection (kitty/sixel/halfblock/off)"
```

---

## Task 1.3: Cell metrics

**Files:**
- Create: `internal/image/cellmetrics.go`
- Create: `internal/image/cellmetrics_test.go`

`CellPixels` returns `(pxW, pxH)` — pixels per terminal cell. Used to size image render targets in pixels.

- [ ] **Step 1: Add `golang.org/x/sys` if missing**

Run: `go list -m golang.org/x/sys 2>/dev/null`
If empty: `go get golang.org/x/sys/unix && go mod tidy`

- [ ] **Step 2: Write the failing test**

Create `internal/image/cellmetrics_test.go`:

```go
package image

import "testing"

func TestCellPixels_EnvOverride(t *testing.T) {
	saved := getenv
	defer func() { getenv = saved }()
	getenv = func(k string) string {
		switch k {
		case "COLORTERM_CELL_WIDTH":
			return "10"
		case "COLORTERM_CELL_HEIGHT":
			return "20"
		}
		return ""
	}

	w, h := CellPixels(0)
	if w != 10 || h != 20 {
		t.Errorf("got (%d,%d), want (10,20)", w, h)
	}
}

func TestCellPixels_FallbackWhenNoEnvAndNoFD(t *testing.T) {
	saved := getenv
	defer func() { getenv = saved }()
	getenv = func(k string) string { return "" }

	// fd = -1 forces ioctl to fail.
	w, h := CellPixels(-1)
	if w != 8 || h != 16 {
		t.Errorf("got (%d,%d), want (8,16) fallback", w, h)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestCellPixels`
Expected: build error — `CellPixels` undefined.

- [ ] **Step 4: Implement**

Create `internal/image/cellmetrics.go`:

```go
package image

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// CellPixels returns the (width, height) of a terminal cell in pixels.
// It honors $COLORTERM_CELL_WIDTH/$COLORTERM_CELL_HEIGHT, then attempts
// TIOCGWINSZ on the given fd, then falls back to (8, 16).
//
// fd is typically int(os.Stdout.Fd()). Pass -1 to skip the ioctl path.
func CellPixels(fd int) (pxW, pxH int) {
	if w, ok := atoi(getenv("COLORTERM_CELL_WIDTH")); ok {
		if h, ok := atoi(getenv("COLORTERM_CELL_HEIGHT")); ok {
			return w, h
		}
	}
	if fd >= 0 {
		if ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); err == nil {
			if ws.Xpixel > 0 && ws.Ypixel > 0 && ws.Col > 0 && ws.Row > 0 {
				return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
			}
		}
	}
	return 8, 16
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/image/ -run TestCellPixels -v`
Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/image/cellmetrics.go internal/image/cellmetrics_test.go go.mod go.sum
git commit -m "feat(image): add CellPixels metric helper"
```

---

## Task 1.4: LRU disk cache

**Files:**
- Create: `internal/image/cache.go`
- Create: `internal/image/cache_test.go`

The cache stores raw image bytes on disk, indexed by an opaque key. Eviction is LRU by atime, capped by total bytes. Per the spec, oversize-single-entry is allowed (logged, evicted on next sweep).

- [ ] **Step 1: Write the failing test**

Create `internal/image/cache_test.go`:

```go
package image

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_PutGet(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 10) // 10 MB cap
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("hello-png-bytes")
	path, err := c.Put("k1", "png", data)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := c.Get("k1")
	if !ok {
		t.Fatal("expected hit")
	}
	if got != path {
		t.Errorf("Get path %q != Put path %q", got, path)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("path does not exist: %v", err)
	}
}

func TestCache_Miss(t *testing.T) {
	c, _ := NewCache(t.TempDir(), 10)
	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss")
	}
}

func TestCache_LRUEvictsOldest(t *testing.T) {
	dir := t.TempDir()
	// 1 MB cap; entries ~ 600KB each => 2nd Put fits, 3rd evicts oldest.
	c, _ := NewCache(dir, 1)
	bigA := bytes.Repeat([]byte{'a'}, 600*1024)
	bigB := bytes.Repeat([]byte{'b'}, 600*1024)
	bigC := bytes.Repeat([]byte{'c'}, 600*1024)

	if _, err := c.Put("a", "bin", bigA); err != nil {
		t.Fatal(err)
	}
	// Make 'a' older than 'b' by tweaking mtime.
	older := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "a.bin"), older, older)

	if _, err := c.Put("b", "bin", bigB); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put("c", "bin", bigC); err != nil {
		t.Fatal(err)
	}

	if _, ok := c.Get("a"); ok {
		t.Errorf("expected 'a' evicted")
	}
	if _, ok := c.Get("b"); !ok {
		t.Errorf("expected 'b' still present")
	}
	if _, ok := c.Get("c"); !ok {
		t.Errorf("expected 'c' present")
	}
}

func TestCache_OversizeEntryAllowed(t *testing.T) {
	c, _ := NewCache(t.TempDir(), 1) // 1 MB cap
	huge := bytes.Repeat([]byte{'x'}, 2*1024*1024)
	if _, err := c.Put("huge", "bin", huge); err != nil {
		t.Fatalf("oversize Put should succeed: %v", err)
	}
	if _, ok := c.Get("huge"); !ok {
		t.Error("expected oversize entry served from cache for this session")
	}
}

func TestCache_GetUpdatesATime(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewCache(dir, 10)
	c.Put("k", "bin", []byte("x"))

	older := time.Now().Add(-time.Hour)
	path := filepath.Join(dir, "k.bin")
	os.Chtimes(path, older, older)

	c.Get("k")

	st, _ := os.Stat(path)
	if time.Since(st.ModTime()) > time.Minute {
		t.Errorf("Get should refresh mtime, got %v old", time.Since(st.ModTime()))
	}
}

func TestCache_LoadIndexAtStartup(t *testing.T) {
	dir := t.TempDir()
	c1, _ := NewCache(dir, 10)
	c1.Put("preexisting", "bin", []byte("data"))

	// New cache instance: should pick up the existing file.
	c2, err := NewCache(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c2.Get("preexisting"); !ok {
		t.Error("expected pre-existing entry to be indexed at startup")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestCache`
Expected: build error — types undefined.

- [ ] **Step 3: Implement the cache**

Create `internal/image/cache.go`:

```go
package image

import (
	"container/list"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache is a size-bounded LRU disk cache for image bytes.
//
// Files are stored at <dir>/<key>.<ext>. Eviction is LRU by mtime: on Put,
// oldest entries are deleted until total disk usage <= cap. A single entry
// larger than cap is admitted but flagged for eviction on the next sweep.
type Cache struct {
	dir   string
	capB  int64
	mu    sync.Mutex
	items map[string]*item
	lru   *list.List // front = most recently used
	total int64
}

type item struct {
	key   string
	path  string
	size  int64
	atime time.Time
	elem  *list.Element
}

// NewCache creates (and indexes) a cache at dir with capMB megabyte cap.
// Existing files in dir are picked up at startup.
func NewCache(dir string, capMB int64) (*Cache, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	c := &Cache{
		dir:   dir,
		capB:  capMB * 1024 * 1024,
		items: map[string]*item{},
		lru:   list.New(),
	}
	if err := c.loadIndex(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cache) loadIndex() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	type entryInfo struct {
		name  string
		size  int64
		atime time.Time
	}
	var infos []entryInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		infos = append(infos, entryInfo{e.Name(), st.Size(), st.ModTime()})
	}
	// Sort oldest first so PushFront yields a list with newest at front.
	sort.Slice(infos, func(i, j int) bool { return infos[i].atime.Before(infos[j].atime) })
	for _, info := range infos {
		key := strings.TrimSuffix(info.name, filepath.Ext(info.name))
		it := &item{
			key:   key,
			path:  filepath.Join(c.dir, info.name),
			size:  info.size,
			atime: info.atime,
		}
		it.elem = c.lru.PushFront(it)
		c.items[key] = it
		c.total += info.size
	}
	return nil
}

// Get returns the path to a cached entry and refreshes its LRU position.
func (c *Cache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok {
		return "", false
	}
	now := time.Now()
	_ = os.Chtimes(it.path, now, now)
	it.atime = now
	c.lru.MoveToFront(it.elem)
	return it.path, true
}

// Put writes data to the cache under key with the given extension (no dot)
// and returns the on-disk path. Triggers LRU eviction to stay under cap.
func (c *Cache) Put(key, ext string, data []byte) (string, error) {
	if ext == "" {
		ext = "bin"
	}
	path := filepath.Join(c.dir, key+"."+ext)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Replace existing entry if present.
	if old, ok := c.items[key]; ok {
		c.total -= old.size
		c.lru.Remove(old.elem)
		delete(c.items, key)
	}
	now := time.Now()
	it := &item{key: key, path: path, size: int64(len(data)), atime: now}
	it.elem = c.lru.PushFront(it)
	c.items[key] = it
	c.total += it.size

	c.evictLocked()
	return path, nil
}

func (c *Cache) evictLocked() {
	for c.total > c.capB && c.lru.Len() > 1 {
		// Take the LRU (back).
		back := c.lru.Back()
		if back == nil {
			return
		}
		it := back.Value.(*item)
		// Don't evict the newest (front) entry, even if it's oversize —
		// this admits a single-entry-over-cap. It will be evicted next sweep.
		if it.elem == c.lru.Front() {
			return
		}
		c.lru.Remove(back)
		delete(c.items, it.key)
		c.total -= it.size
		_ = os.Remove(it.path)
	}
}

// Stats returns current totals (for logging).
func (c *Cache) Stats() (entries int, totalBytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len(), c.total
}

// String formats cache stats for logs.
func (c *Cache) String() string {
	n, b := c.Stats()
	return fmt.Sprintf("image.Cache{dir=%s entries=%d bytes=%d cap=%d}", c.dir, n, b, c.capB)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/image/ -run TestCache -v`
Expected: all pass. If `TestCache_LRUEvictsOldest` fails, double-check that the chtimes-based ordering is being honored — the test relies on mtime-driven LRU.

- [ ] **Step 5: Commit**

```bash
git add internal/image/cache.go internal/image/cache_test.go
git commit -m "feat(image): add LRU disk cache for image bytes"
```

---

## Task 1.5: Half-block renderer

**Files:**
- Create: `internal/image/halfblock.go`
- Create: `internal/image/halfblock_test.go`

Generalizes the avatar half-block algorithm to any cell footprint.

- [ ] **Step 1: Write the failing test**

Create `internal/image/halfblock_test.go`:

```go
package image

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// makeSolid returns a w×h image filled with c.
func makeSolid(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestHalfBlock_OutputShape(t *testing.T) {
	hb := HalfBlockRenderer{}
	src := makeSolid(8, 16, color.RGBA{255, 0, 0, 255})
	r := hb.Render(src, image.Pt(4, 2))

	if r.Cells != image.Pt(4, 2) {
		t.Errorf("Cells: got %v, want (4,2)", r.Cells)
	}
	if len(r.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(r.Lines))
	}
	for i, line := range r.Lines {
		if !strings.Contains(line, "▀") {
			t.Errorf("line %d missing ▀: %q", i, line)
		}
		if !strings.Contains(line, "\x1b[38;2;255;0;0m") {
			t.Errorf("line %d missing red fg ANSI: %q", i, line)
		}
	}
	if r.OnFlush != nil {
		t.Error("halfblock should not have OnFlush")
	}
	if r.ID != 0 {
		t.Error("halfblock should have ID=0")
	}
	// Fallback equals Lines for halfblock.
	if len(r.Fallback) != len(r.Lines) {
		t.Error("halfblock Fallback should equal Lines")
	}
}

func TestHalfBlock_TopBottomColors(t *testing.T) {
	// Top half red, bottom half blue. After half-block rendering, fg=red, bg=blue.
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	for y := 2; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	hb := HalfBlockRenderer{}
	r := hb.Render(src, image.Pt(4, 2))

	// First line = top sub-row (rows 0-1 of pixels). After the renderer
	// downscales 4x4 → 4x4 and maps two pixel rows per cell row, the first
	// cell row uses pixel row 0 (top, red) and pixel row 1 (also red).
	// So fg=red, bg=red on row 1. Row 2 uses pixel rows 2-3 (both blue).
	if !strings.Contains(r.Lines[0], "\x1b[38;2;255;0;0m") {
		t.Error("row 0 fg should be red")
	}
	if !strings.Contains(r.Lines[1], "\x1b[38;2;0;0;255m") {
		t.Error("row 1 fg should be blue")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestHalfBlock`
Expected: build error — `HalfBlockRenderer` undefined.

- [ ] **Step 3: Implement**

Create `internal/image/halfblock.go`:

```go
package image

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/draw"
)

// HalfBlockRenderer encodes images via Unicode upper-half-block characters
// (▀) with 24-bit fg/bg colors. Two pixel rows per terminal cell row.
type HalfBlockRenderer struct{}

// Render satisfies the Renderer interface.
func (HalfBlockRenderer) Render(img image.Image, target image.Point) Render {
	if target.X <= 0 || target.Y <= 0 {
		return Render{Cells: target}
	}
	pxW, pxH := target.X, target.Y*2
	resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Over, nil)

	lines := make([]string, target.Y)
	for cellY := 0; cellY < target.Y; cellY++ {
		var b strings.Builder
		for x := 0; x < pxW; x++ {
			top := rgbaAt(resized, x, cellY*2)
			bot := rgbaAt(resized, x, cellY*2+1)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bot.R, bot.G, bot.B)
		}
		b.WriteString("\x1b[0m")
		lines[cellY] = b.String()
	}
	return Render{
		Cells:    target,
		Lines:    lines,
		Fallback: lines, // half-block is its own fallback
	}
}

func rgbaAt(img image.Image, x, y int) color.RGBA {
	r, g, b, _ := img.At(x, y).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 255}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/image/ -run TestHalfBlock -v`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/image/halfblock.go internal/image/halfblock_test.go
git commit -m "feat(image): add half-block renderer (generalized from avatar)"
```

---

## Task 1.6: Fetcher with HTTP, decode, downscale, single-flight

**Files:**
- Create: `internal/image/fetcher.go`
- Create: `internal/image/fetcher_test.go`

- [ ] **Step 1: Add `golang.org/x/sync/singleflight` dep**

Run: `go get golang.org/x/sync/singleflight && go mod tidy`

- [ ] **Step 2: Write the failing test**

Create `internal/image/fetcher_test.go`:

```go
package image

import (
	"bytes"
	"context"
	"image"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	imgcolor "image/color"
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestFetcher`
Expected: build error.

- [ ] **Step 4: Implement**

Create `internal/image/fetcher.go`:

```go
package image

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/sync/singleflight"
)

// FetchRequest describes one image fetch.
type FetchRequest struct {
	Key    string      // cache key (e.g. "F0123ABCD-720" or "avatar-U123")
	URL    string      // remote URL
	Target image.Point // target downscale size in pixels (0 = no downscale)
}

// FetchResult is the decoded, downscaled image plus on-disk metadata.
type FetchResult struct {
	Img    image.Image
	Source string // path on disk
	Mime   string
}

// Fetcher downloads images, stores raw bytes in Cache, decodes, and
// downscales. Concurrent fetches for the same Key are deduplicated.
type Fetcher struct {
	cache *Cache
	http  *http.Client
	sf    singleflight.Group
}

// NewFetcher constructs a Fetcher. If client is nil, a default with a
// 10-second timeout is used.
func NewFetcher(cache *Cache, client *http.Client) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Fetcher{cache: cache, http: client}
}

// Fetch returns the decoded image, downloading and caching if needed.
func (f *Fetcher) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	v, err, _ := f.sf.Do(req.Key, func() (any, error) {
		return f.fetchInner(ctx, req)
	})
	if err != nil {
		return FetchResult{}, err
	}
	return v.(FetchResult), nil
}

func (f *Fetcher) fetchInner(ctx context.Context, req FetchRequest) (FetchResult, error) {
	path, hit := f.cache.Get(req.Key)
	if !hit {
		// Download.
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
		if err != nil {
			return FetchResult{}, err
		}
		httpReq.Header.Set("User-Agent", "slk/inline-image-fetcher")
		resp, err := f.http.Do(httpReq)
		if err != nil {
			return FetchResult{}, fmt.Errorf("fetch %s: %w", req.URL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return FetchResult{}, fmt.Errorf("fetch %s: HTTP %d", req.URL, resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return FetchResult{}, err
		}
		ext := extFromMime(resp.Header.Get("Content-Type"), req.URL)
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
		return FetchResult{}, fmt.Errorf("decode %s: %w", path, err)
	}

	if req.Target.X > 0 && req.Target.Y > 0 {
		img = downscale(img, req.Target)
	}

	mime := mimeFromExt(filepath.Ext(path))
	return FetchResult{Img: img, Source: path, Mime: mime}, nil
}

// downscale fits img within target preserving aspect; never upscales.
func downscale(img image.Image, target image.Point) image.Image {
	srcW, srcH := img.Bounds().Dx(), img.Bounds().Dy()
	if srcW <= target.X && srcH <= target.Y {
		// Re-render to RGBA at exact target size (small, identity-ish scale).
		dst := image.NewRGBA(image.Rect(0, 0, target.X, target.Y))
		draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		return dst
	}
	dst := image.NewRGBA(image.Rect(0, 0, target.X, target.Y))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

func extFromMime(contentType, url string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "image/png"):
		return "png"
	case strings.HasPrefix(ct, "image/jpeg"), strings.HasPrefix(ct, "image/jpg"):
		return "jpg"
	case strings.HasPrefix(ct, "image/gif"):
		return "gif"
	}
	// Fall back to URL extension.
	if i := strings.LastIndex(url, "."); i >= 0 {
		ext := strings.ToLower(strings.TrimPrefix(url[i:], "."))
		if ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "gif" {
			if ext == "jpeg" {
				return "jpg"
			}
			return ext
		}
	}
	return "png"
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	}
	return "application/octet-stream"
}

// Bytes reads the cached file's raw bytes.
func (f *Fetcher) Bytes(key string) ([]byte, error) {
	path, ok := f.cache.Get(key)
	if !ok {
		return nil, fmt.Errorf("not cached: %s", key)
	}
	return os.ReadFile(path)
}

var _ = bytes.NewReader // keep import if unused
```

Remove the `bytes` import if it's unused (`go vet` will catch it).

- [ ] **Step 5: Run tests**

Run: `go test ./internal/image/ -run TestFetcher -v`
Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add internal/image/fetcher.go internal/image/fetcher_test.go go.mod go.sum
git commit -m "feat(image): add HTTP fetcher with single-flight dedup and downscale"
```

---

## Task 1.7: Thumb-picker

**Files:**
- Modify: `internal/image/fetcher.go` (append `PickThumb`)
- Create: `internal/image/thumb_test.go`

`PickThumb` selects the smallest Slack thumb URL whose pixel dimensions are ≥ target on both axes.

- [ ] **Step 1: Write failing test**

Create `internal/image/thumb_test.go`:

```go
package image

import (
	"image"
	"testing"
)

func TestPickThumb_SmallestThatFits(t *testing.T) {
	thumbs := []ThumbSpec{
		{URL: "u-360", W: 360, H: 360},
		{URL: "u-720", W: 720, H: 720},
		{URL: "u-1024", W: 1024, H: 1024},
	}
	// Target 400x400 — should pick 720.
	url, suffix := PickThumb(thumbs, image.Pt(400, 400))
	if url != "u-720" {
		t.Errorf("got %q, want u-720", url)
	}
	if suffix != "720" {
		t.Errorf("suffix got %q, want 720", suffix)
	}
}

func TestPickThumb_FallsBackToLargest(t *testing.T) {
	thumbs := []ThumbSpec{
		{URL: "u-360", W: 360, H: 360},
	}
	url, _ := PickThumb(thumbs, image.Pt(800, 800))
	if url != "u-360" {
		t.Errorf("got %q, want u-360 (largest available)", url)
	}
}

func TestPickThumb_EmptyReturnsEmpty(t *testing.T) {
	url, _ := PickThumb(nil, image.Pt(100, 100))
	if url != "" {
		t.Errorf("expected empty, got %q", url)
	}
}

func TestPickThumb_RequiresBothAxes(t *testing.T) {
	thumbs := []ThumbSpec{
		{URL: "u-wide", W: 1000, H: 100}, // wide enough but too short
		{URL: "u-square", W: 500, H: 500},
	}
	url, _ := PickThumb(thumbs, image.Pt(400, 400))
	if url != "u-square" {
		t.Errorf("got %q, want u-square (only one that fits both axes)", url)
	}
}
```

- [ ] **Step 2: Implement**

Append to `internal/image/fetcher.go`:

```go
// ThumbSpec is one Slack thumbnail variant.
type ThumbSpec struct {
	URL string
	W   int
	H   int
}

// PickThumb selects the smallest thumb whose dimensions are >= target on
// both axes. Falls back to the largest available if none satisfy.
// suffix is a short string usable in cache keys (e.g. "720").
func PickThumb(thumbs []ThumbSpec, target image.Point) (url, suffix string) {
	if len(thumbs) == 0 {
		return "", ""
	}
	// Sort ascending by max(W, H).
	sorted := make([]ThumbSpec, len(thumbs))
	copy(sorted, thumbs)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if max(sorted[j].W, sorted[j].H) < max(sorted[i].W, sorted[i].H) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	for _, t := range sorted {
		if t.W >= target.X && t.H >= target.Y {
			return t.URL, fmt.Sprintf("%d", max(t.W, t.H))
		}
	}
	last := sorted[len(sorted)-1]
	return last.URL, fmt.Sprintf("%d", max(last.W, last.H))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

(If your Go version is 1.21+, `max` is a builtin and the helper is redundant — delete it. Check `go version`.)

- [ ] **Step 3: Run tests**

Run: `go test ./internal/image/ -run TestPickThumb -v`
Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add internal/image/fetcher.go internal/image/thumb_test.go
git commit -m "feat(image): add PickThumb for smallest-fits thumbnail selection"
```

---

## Task 1.8: Top-level `Render` dispatcher

**Files:**
- Modify: `internal/image/renderer.go`

A small free function that picks a renderer based on `Protocol`. Keeps callers protocol-agnostic.

- [ ] **Step 1: Append to `internal/image/renderer.go`**

```go
// RenderImage encodes img at target cells using the given protocol's renderer.
// Returns a zero Render if proto == ProtoOff.
func RenderImage(proto Protocol, img image.Image, target image.Point) Render {
	switch proto {
	case ProtoOff:
		return Render{Cells: target}
	case ProtoHalfBlock:
		return HalfBlockRenderer{}.Render(img, target)
	case ProtoSixel:
		return sixelRenderer.Render(img, target)
	case ProtoKitty:
		return kittyRenderer.Render(img, target)
	}
	return Render{}
}

// Singleton renderers — concrete instances appear in kitty.go / sixel.go.
// Until those exist, fall back to half-block so this file builds in isolation.
var (
	sixelRenderer Renderer = HalfBlockRenderer{}
	kittyRenderer Renderer = HalfBlockRenderer{}
)
```

(Phase 3 and Phase 4 will overwrite the `sixelRenderer` and `kittyRenderer` package vars with real implementations.)

- [ ] **Step 2: Verify build**

Run: `go build ./internal/image/...` → success.
Run: `go test ./internal/image/...` → all pass.

- [ ] **Step 3: Commit**

```bash
git add internal/image/renderer.go
git commit -m "feat(image): add RenderImage dispatcher"
```

---

## Phase 1 done

`internal/image` is a self-contained package with capability detection, cell metrics, an LRU cache, a single-flight fetcher with downscale, the half-block renderer, the thumb-picker, and a protocol dispatcher. All tests pass; nothing in the rest of the codebase imports it yet.

**Verify:**
```bash
go test ./internal/image/... -v
go vet ./internal/image/...
```

Continue to `02-avatar-refactor.md`.
