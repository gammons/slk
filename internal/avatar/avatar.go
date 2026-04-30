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
		Target: image.Pt(AvatarCols, AvatarRows*2),
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
