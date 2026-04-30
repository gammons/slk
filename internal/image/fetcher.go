package image

import (
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
	"sort"
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

// downscale fits img within target preserving the renderer's expectation;
// the renderer always wants exactly target pixels — so we always scale.
// (Avoids an extra branch and image-copy path.)
func downscale(img image.Image, target image.Point) image.Image {
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

// Cached returns the decoded image and true if it's already in the on-disk
// cache. Never starts a network download. When target is positive on both
// axes, the returned image is downscaled to those pixel dimensions; pass
// image.Point{} (zero) to skip downscale.
func (f *Fetcher) Cached(key string, target image.Point) (image.Image, bool) {
	path, ok := f.cache.Get(key)
	if !ok {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, false
	}
	if target.X > 0 && target.Y > 0 {
		img = downscale(img, target)
	}
	return img, true
}

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
	sort.Slice(sorted, func(i, j int) bool {
		return max(sorted[i].W, sorted[i].H) < max(sorted[j].W, sorted[j].H)
	})
	for _, t := range sorted {
		if t.W >= target.X && t.H >= target.Y {
			return t.URL, fmt.Sprintf("%d", max(t.W, t.H))
		}
	}
	last := sorted[len(sorted)-1]
	return last.URL, fmt.Sprintf("%d", max(last.W, last.H))
}
