// Package avatar downloads Slack user avatars and renders them at a
// fixed 4×2 cell footprint. Storage delegates to internal/image's
// shared cache; rendering uses kitty graphics on capable terminals
// (sharper) and falls back to half-block (▀) elsewhere. Sixel is
// intentionally NOT used for avatars: re-emitting sixel byte streams
// per visible avatar per redraw would dominate the bandwidth budget.
package avatar

import (
	"context"
	"image"
	"strings"
	"sync"

	imgpkg "github.com/gammons/slk/internal/image"
)

const (
	// AvatarCols is the width of the rendered avatar in terminal columns.
	AvatarCols = 4
	// AvatarRows is the height in terminal rows. Half-block uses 2 pixel
	// rows per cell row; kitty fits the source image to AvatarCols×AvatarRows
	// cells and the terminal scales pixels appropriately.
	AvatarRows = 2
)

// Cache wraps an image.Fetcher and memoizes rendered ANSI strings per user.
//
// When the active rendering protocol is kitty, the avatar's "render"
// is a small block of unicode-placeholder cells; the actual image
// upload happens via the kitty side channel (image.KittyOutput) on
// first render of a given user, deduped by the kitty registry's
// per-(key,target) tracking. When the protocol is not kitty (sixel,
// half-block, off, ...), the avatar renders as half-block ANSI text.
type Cache struct {
	fetcher  *imgpkg.Fetcher
	kitty    *imgpkg.KittyRenderer // nil when not using kitty
	useKitty bool

	mu      sync.RWMutex
	renders map[string]string // userID -> rendered ANSI string
}

// NewCache creates an avatar cache backed by the shared image.Fetcher.
// kitty may be nil; in that case (or when useKitty is false) the cache
// renders avatars via half-block regardless of any kitty support
// elsewhere in the app.
func NewCache(fetcher *imgpkg.Fetcher, kitty *imgpkg.KittyRenderer, useKitty bool) *Cache {
	return &Cache{
		fetcher:  fetcher,
		kitty:    kitty,
		useKitty: useKitty && kitty != nil,
		renders:  make(map[string]string),
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
	// Source size differs by protocol:
	//   - half-block: (AvatarCols, AvatarRows*2) gives the renderer
	//     exactly the pixel grid it samples, matching the original
	//     pre-kitty pipeline byte-for-byte (parity test relies on this).
	//   - kitty: skip the fetcher's downscale (Target = zero point) so
	//     the renderer's own pixel-target resize starts from the highest
	//     available source resolution. With a 32×32 source (Slack's
	//     image_32) and kitty's internal target of ~32×32, this is
	//     effectively identity scaling — sharp pixels.
	target := image.Pt(AvatarCols, AvatarRows*2)
	if c.useKitty {
		target = image.Point{}
	}
	res, err := c.fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:    "avatar-" + userID,
		URL:    avatarURL,
		Target: target,
	})
	if err != nil {
		return
	}
	rendered := c.renderAvatar(userID, res.Img)
	c.mu.Lock()
	c.renders[userID] = rendered
	c.mu.Unlock()
}

// Get returns the rendered avatar string, or empty if not cached.
func (c *Cache) Get(userID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.renders[userID]
}

// renderAvatar produces the avatar's rendered string for the active
// protocol. Kitty path: SetSource + RenderKey, immediately drain the
// upload escape to the kitty side channel (the registry's fresh-flag
// guarantees this fires only once per user), and return the
// placeholder cells. Half-block path: encode and return.
func (c *Cache) renderAvatar(userID string, img image.Image) string {
	target := image.Pt(AvatarCols, AvatarRows)
	if c.useKitty {
		key := "avatar-" + userID
		c.kitty.SetSource(key, img)
		out := c.kitty.RenderKey(key, target)
		// Fire the upload escape NOW (single-threaded from
		// PreloadSync's perspective; the side-channel writer handles
		// concurrency). After this, the kitty registry returns
		// fresh=false for subsequent renders, so OnFlush is nil.
		if out.OnFlush != nil {
			_ = out.OnFlush(imgpkg.KittyOutput)
		}
		return joinLines(out.Lines)
	}
	out := imgpkg.HalfBlockRenderer{}.Render(img, target)
	return joinLines(out.Lines)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
