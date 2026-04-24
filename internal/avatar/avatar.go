// Package avatar downloads Slack user avatars, caches them locally,
// and renders them as half-block pixel art for terminal display.
package avatar

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/draw"
)

const (
	// AvatarCols is the width of the rendered avatar in terminal columns.
	AvatarCols = 4
	// AvatarPixelW is the pixel width we resize to (1 pixel per column).
	AvatarPixelW = AvatarCols
	// AvatarPixelH is the pixel height (2 pixels per row via half-blocks).
	AvatarPixelH = 4 // 2 rows × 2 pixels per row
)

// Cache manages downloading and caching avatar images.
type Cache struct {
	dir     string
	mu      sync.RWMutex
	renders map[string]string // userID -> rendered half-block string
}

// NewCache creates an avatar cache that stores images in the given directory.
func NewCache(dir string) *Cache {
	os.MkdirAll(dir, 0700)
	return &Cache{
		dir:     dir,
		renders: make(map[string]string),
	}
}

// Preload downloads and renders an avatar for the given user in the background.
func (c *Cache) Preload(userID, avatarURL string) {
	if avatarURL == "" {
		return
	}
	go func() {
		rendered := c.loadAndRender(userID, avatarURL)
		c.mu.Lock()
		c.renders[userID] = rendered
		c.mu.Unlock()
	}()
}

// PreloadSync downloads and renders an avatar synchronously.
func (c *Cache) PreloadSync(userID, avatarURL string) {
	if avatarURL == "" {
		return
	}
	rendered := c.loadAndRender(userID, avatarURL)
	c.mu.Lock()
	c.renders[userID] = rendered
	c.mu.Unlock()
}

// Get returns the rendered half-block avatar for a user, or empty string if not cached.
func (c *Cache) Get(userID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.renders[userID]
}

func (c *Cache) loadAndRender(userID, avatarURL string) string {
	// Check disk cache first
	cachePath := filepath.Join(c.dir, userID+".img")
	img, err := loadFromDisk(cachePath)
	if err != nil {
		// Download
		img, err = downloadImage(avatarURL)
		if err != nil {
			return ""
		}
		// Save to disk (best effort)
		saveToDisk(cachePath, img, avatarURL)
	}

	// Resize to tiny avatar
	resized := resizeImage(img, AvatarPixelW, AvatarPixelH)

	// Render as half-block art
	return renderHalfBlock(resized)
}

func downloadImage(url string) (image.Image, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	img, _, err := image.Decode(resp.Body)
	return img, err
}

func loadFromDisk(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

func saveToDisk(path string, img image.Image, originalURL string) {
	// We'll save the original downloaded data, but since we already decoded it,
	// just save a marker file. Re-download if needed.
	// For simplicity, download again to save raw bytes.
	resp, err := http.Get(originalURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	io.Copy(f, resp.Body)
}

func resizeImage(img image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

// renderHalfBlock converts a tiny image into a string using Unicode half-block characters.
// Each character cell represents 2 vertical pixels:
//   - Foreground color = top pixel
//   - Background color = bottom pixel
//   - Character = ▀ (upper half block)
//
// This gives 2× vertical resolution compared to full block characters.
func renderHalfBlock(img image.Image) string {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	var rows []string
	for y := 0; y < h; y += 2 {
		var row strings.Builder
		for x := 0; x < w; x++ {
			topR, topG, topB, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			top := color.RGBA{uint8(topR >> 8), uint8(topG >> 8), uint8(topB >> 8), 255}

			var bot color.RGBA
			if y+1 < h {
				botR, botG, botB, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y+1).RGBA()
				bot = color.RGBA{uint8(botR >> 8), uint8(botG >> 8), uint8(botB >> 8), 255}
			} else {
				bot = color.RGBA{0, 0, 0, 255}
			}

			// ▀ with foreground=top, background=bottom
			fg := fmt.Sprintf("\033[38;2;%d;%d;%dm", top.R, top.G, top.B)
			bg := fmt.Sprintf("\033[48;2;%d;%d;%dm", bot.R, bot.G, bot.B)
			row.WriteString(fg + bg + "▀")
		}
		row.WriteString("\033[0m") // reset
		rows = append(rows, row.String())
	}

	return strings.Join(rows, "\n")
}
