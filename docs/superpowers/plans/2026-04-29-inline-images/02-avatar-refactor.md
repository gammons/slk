# Phase 2: Avatar Refactor + Disk Migration

> Index: `00-overview.md`. Previous: `01-image-package-foundation.md`. Next: `03-kitty-renderer.md`.

**Goal:** Refactor `internal/avatar` to delegate storage and rendering to `internal/image`. Migrate existing `~/.cache/slk/avatars/<userID>.img` files to `~/.cache/slk/images/avatar-<userID>.png`. Verify pixel-identical output via golden test.

**Spec sections covered:** Avatar Rendering, Cache (migration).

---

## Task 2.1: Avatar disk migration

**Files:**
- Create: `internal/image/migrate.go`
- Create: `internal/image/migrate_test.go`

The migration is idempotent: skip if target exists; rename otherwise. Also detects the file's MIME via the first few bytes (PNG/JPEG sniff) so we pick the right extension.

- [ ] **Step 1: Write the failing test**

Create `internal/image/migrate_test.go`:

```go
package image

import (
	"bytes"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"os"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, path string) {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			src.Set(x, y, imgcolor.RGBA{1, 2, 3, 255})
		}
	}
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	os.WriteFile(path, buf.Bytes(), 0600)
}

func TestMigrateAvatars_RenamesFiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	writePNG(t, filepath.Join(src, "U123.img"))
	writePNG(t, filepath.Join(src, "U456.img"))

	n, err := MigrateAvatars(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("migrated %d, want 2", n)
	}
	if _, err := os.Stat(filepath.Join(dst, "avatar-U123.png")); err != nil {
		t.Errorf("expected avatar-U123.png in dst: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "U123.img")); !os.IsNotExist(err) {
		t.Errorf("expected source removed, err=%v", err)
	}
}

func TestMigrateAvatars_Idempotent(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writePNG(t, filepath.Join(src, "U1.img"))
	writePNG(t, filepath.Join(dst, "avatar-U1.png"))

	n, err := MigrateAvatars(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("migrated %d, want 0 (target existed)", n)
	}
	// Source still removed because target already had the same logical entry.
	if _, err := os.Stat(filepath.Join(src, "U1.img")); !os.IsNotExist(err) {
		t.Errorf("expected source removed even when target existed")
	}
}

func TestMigrateAvatars_MissingSourceIsNoOp(t *testing.T) {
	dst := t.TempDir()
	n, err := MigrateAvatars(filepath.Join(t.TempDir(), "does-not-exist"), dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/image/ -run TestMigrateAvatars`
Expected: build error — `MigrateAvatars` undefined.

- [ ] **Step 3: Implement**

Create `internal/image/migrate.go`:

```go
package image

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MigrateAvatars moves files from oldDir/<userID>.img to
// newDir/avatar-<userID>.<ext> exactly once. The extension is sniffed from
// the file's first bytes (PNG or JPEG); unknown content is renamed as .png.
//
// Returns the number of files moved. If oldDir does not exist, returns
// (0, nil).
func MigrateAvatars(oldDir, newDir string) (int, error) {
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if err := os.MkdirAll(newDir, 0700); err != nil {
		return 0, err
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".img") {
			continue
		}
		userID := strings.TrimSuffix(e.Name(), ".img")
		oldPath := filepath.Join(oldDir, e.Name())
		ext := sniffExt(oldPath)
		newPath := filepath.Join(newDir, "avatar-"+userID+"."+ext)
		if _, err := os.Stat(newPath); err == nil {
			// Already migrated; just remove the source.
			os.Remove(oldPath)
			continue
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			// Cross-device fallback: copy + remove.
			if err := copyFile(oldPath, newPath); err != nil {
				return moved, err
			}
			os.Remove(oldPath)
		}
		moved++
	}
	return moved, nil
}

func sniffExt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "png"
	}
	defer f.Close()
	var hdr [8]byte
	n, _ := f.Read(hdr[:])
	if n >= 4 && hdr[0] == 0xFF && hdr[1] == 0xD8 {
		return "jpg"
	}
	if n >= 8 && hdr[0] == 0x89 && hdr[1] == 'P' && hdr[2] == 'N' && hdr[3] == 'G' {
		return "png"
	}
	return "png"
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
```

Add the `io` import to the file. (The test file does not need `io`.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/image/ -run TestMigrateAvatars -v`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add internal/image/migrate.go internal/image/migrate_test.go
git commit -m "feat(image): add idempotent avatar disk migration"
```

---

## Task 2.2: Refactor `internal/avatar` to delegate to `internal/image`

**Files:**
- Modify: `internal/avatar/avatar.go`

Keep the public API (`Cache`, `NewCache`, `Preload`, `PreloadSync`, `Get`) but replace the internals.

- [ ] **Step 1: Capture a golden ANSI string from the current implementation**

Before changing anything, capture the existing rendering for a known PNG so we can verify pixel parity after the refactor.

Add a temporary test (we'll keep it):

Create `internal/avatar/parity_test.go`:

```go
package avatar

import (
	"bytes"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRender_ParityGolden(t *testing.T) {
	// Generate a deterministic 16x16 gradient.
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
	defer srv.Close()

	c := NewCache(t.TempDir())
	c.PreloadSync("U_GOLDEN", srv.URL)
	got := c.Get("U_GOLDEN")
	if got == "" {
		t.Fatal("avatar render is empty")
	}

	// Capture for comparison after refactor. Hardcode after first run.
	t.Logf("golden:\n%q", got)
	// After refactor, replace this with an exact-match assertion:
	// const want = "..."
	// if got != want { t.Errorf(...) }
}
```

Run: `go test ./internal/avatar/ -run TestRender_ParityGolden -v`
Expected: passes; logs the golden string. Copy the printed `golden:` value verbatim into a `const want = ...` and replace the t.Logf with `if got != want { t.Errorf("parity mismatch:\n got:%q\nwant:%q", got, want) }`. Re-run to confirm it still passes against itself.

- [ ] **Step 2: Refactor `avatar.go`**

Replace `internal/avatar/avatar.go` with:

```go
// Package avatar downloads Slack user avatars and renders them as
// half-block pixel art for terminal display. Storage and rendering are
// delegated to internal/image; this package preserves the existing API.
package avatar

import (
	"context"
	"image"
	"sync"

	imgpkg "github.com/gammons/slk/internal/image"
)

const (
	// AvatarCols is the width of the rendered avatar in terminal columns.
	AvatarCols = 4
	// AvatarRows is the height in terminal rows (each row = 2 pixel rows
	// via half-blocks).
	AvatarRows = 2
)

// Cache wraps an image.Fetcher and memoizes rendered ANSI strings per user.
type Cache struct {
	fetcher *imgpkg.Fetcher
	mu      sync.RWMutex
	renders map[string]string // userID -> rendered half-block string
}

// NewCache creates an avatar cache backed by the shared image.Fetcher.
func NewCache(fetcher *imgpkg.Fetcher) *Cache {
	return &Cache{
		fetcher: fetcher,
		renders: make(map[string]string),
	}
}

// Preload downloads and renders an avatar in the background.
func (c *Cache) Preload(userID, avatarURL string) {
	if avatarURL == "" {
		return
	}
	go c.PreloadSync(userID, avatarURL)
}

// PreloadSync downloads and renders synchronously.
func (c *Cache) PreloadSync(userID, avatarURL string) {
	if avatarURL == "" {
		return
	}
	res, err := c.fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:    "avatar-" + userID,
		URL:    avatarURL,
		Target: image.Pt(AvatarCols, AvatarRows*2), // half-block uses 2 px per row
	})
	if err != nil {
		return
	}
	rendered := imgpkg.HalfBlockRenderer{}.Render(res.Img, image.Pt(AvatarCols, AvatarRows))
	c.mu.Lock()
	c.renders[userID] = joinLines(rendered.Lines)
	c.mu.Unlock()
}

// Get returns the rendered half-block avatar, or empty string if not cached.
func (c *Cache) Get(userID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.renders[userID]
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := lines[0]
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out
}
```

**Note the API change:** `NewCache(dir string)` becomes `NewCache(fetcher *image.Fetcher)`. This is a breaking change to one external caller (`cmd/slk/main.go`). We'll update that in Step 4.

- [ ] **Step 3: Update the parity test**

The parity test from Step 1 still uses `NewCache(t.TempDir())`. Update it to construct an `image.Fetcher`:

```go
package avatar

import (
	"bytes"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	imgpkg "github.com/gammons/slk/internal/image"
)

func TestRender_ParityGolden(t *testing.T) {
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
	defer srv.Close()

	cache, _ := imgpkg.NewCache(t.TempDir(), 10)
	fetcher := imgpkg.NewFetcher(cache, http.DefaultClient)
	c := NewCache(fetcher)
	c.PreloadSync("U_GOLDEN", srv.URL)
	got := c.Get("U_GOLDEN")
	if got == "" {
		t.Fatal("avatar render is empty")
	}
	// Compare against the captured golden string (paste the value from Step 1):
	const want = `<PASTE GOLDEN STRING FROM STEP 1>`
	if got != want {
		t.Errorf("parity mismatch:\n got: %q\nwant: %q", got, want)
	}
}
```

⚠️ When the engineer runs this for the first time after the refactor, they MUST first run the test against the *old* avatar code (Step 1), capture the golden string, paste it into `want`, and only then refactor `avatar.go`. The test verifies that the new implementation produces byte-identical output.

If the parity test fails after refactor, the half-block output is not pixel-equivalent. Common causes:
- The `target` size mismatch (old code used `AvatarPixelW=4`, `AvatarPixelH=4`; new code passes `image.Pt(4, 2)` to the renderer which then resizes to `(4, 4)` internally — equivalent).
- Trailing reset escape ordering. Compare bytes carefully.

- [ ] **Step 4: Update `cmd/slk/main.go`**

Find the existing avatar wiring around `cmd/slk/main.go:275-277`:

```go
avatarDir := filepath.Join(cacheDir, "avatars")
avatarCache := avatar.NewCache(avatarDir)
```

Replace with:

```go
imagesDir := filepath.Join(cacheDir, "images")
imageCache, err := imgpkg.NewCache(imagesDir, cfg.Cache.MaxImageCacheMB) // cfg key added in Phase 5
if err != nil {
    log.Printf("image cache: %v", err)
    // Fallback: avatars without persistent cache.
    imageCache, _ = imgpkg.NewCache(t.TempDir(), 10) // for compile only; in real path, hard-fail
}
imageFetcher := imgpkg.NewFetcher(imageCache, nil)

// Migrate old avatar cache (one-time, idempotent).
oldAvatarDir := filepath.Join(cacheDir, "avatars")
if n, err := imgpkg.MigrateAvatars(oldAvatarDir, imagesDir); err != nil {
    log.Printf("avatar migration: %v", err)
} else if n > 0 {
    log.Printf("migrated %d avatars to %s", n, imagesDir)
}

avatarCache := avatar.NewCache(imageFetcher)
```

Add the import:

```go
imgpkg "github.com/gammons/slk/internal/image"
```

The `cfg.Cache.MaxImageCacheMB` config key is added in Phase 5 — for now, hardcode `200`:

```go
imageCache, err := imgpkg.NewCache(imagesDir, 200)
```

Replace this with `cfg.Cache.MaxImageCacheMB` when Phase 5 lands.

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: all pass, including the avatar parity test.

If any other test or callsite breaks because of the `avatar.NewCache` signature change, fix the call by passing the new `*image.Fetcher`.

- [ ] **Step 6: Commit**

```bash
git add internal/avatar/avatar.go internal/avatar/parity_test.go cmd/slk/main.go go.mod go.sum
git commit -m "refactor(avatar): delegate storage and rendering to internal/image

The avatar package keeps its public API but internally uses the shared
image.Fetcher and HalfBlockRenderer. A one-time disk migration moves
~/.cache/slk/avatars/<userID>.img to ~/.cache/slk/images/avatar-<userID>.<ext>.
Pixel parity verified by golden test."
```

---

## Task 2.3: Verify cache file paths after migration

- [ ] **Step 1: Manual verification (one-time)**

Run slk against a workspace, then inspect:

```bash
ls ~/.cache/slk/images/ | head
```

Expected: `avatar-U*.png` (and/or `.jpg`) files. The old `~/.cache/slk/avatars/` directory should be empty (we don't delete the directory itself, just its contents).

- [ ] **Step 2: No commit needed** unless something is wrong.

---

## Phase 2 done

`internal/avatar` now delegates to `internal/image`. Existing avatars on disk are migrated transparently on first launch. Pixel output is identical.

**Verify:**
```bash
go test ./internal/avatar/... -v
go test ./internal/image/... -v
go build ./...
```

Continue to `03-kitty-renderer.md`.
