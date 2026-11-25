package demo

import (
	"context"
	"image"
	"strings"

	"github.com/gammons/slk/internal/core"
	imgpkg "github.com/gammons/slk/internal/image"
)

type storedImage struct {
	img  image.Image
	mime string
}

// imageStore is the demo's core.ImageFetcher. It holds every demo image in
// memory, keyed by attachment FileID, and reports them as already cached
// so the renderer never starts a fetch; every other key misses.
type imageStore struct{ byFileID map[string]storedImage }

var _ core.ImageFetcher = imageStore{}

func newImageStore() (imageStore, error) {
	meme, err := decodeMeme()
	if err != nil {
		return imageStore{}, err
	}
	return imageStore{byFileID: map[string]storedImage{
		chartFileID: {img: drawChart(), mime: "image/png"},
		memeFileID:  {img: meme, mime: "image/jpeg"},
	}}, nil
}

// lookup resolves a renderer cache key. The inline renderer keys by
// FileID + "-" + the chosen thumb's size, the full-screen preview by
// FileID + "-preview".
func (s imageStore) lookup(key string) (storedImage, bool) {
	id, _, ok := strings.Cut(key, "-")
	if !ok {
		return storedImage{}, false
	}
	img, found := s.byFileID[id]
	return img, found
}

func (s imageStore) Fetch(_ context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	img, ok := s.lookup(req.Key)
	if !ok {
		return imgpkg.FetchResult{}, errUnavailable
	}
	return imgpkg.FetchResult{Img: img.img, Mime: img.mime}, nil
}

func (s imageStore) Cached(key string, _ image.Point) (image.Image, bool) {
	img, ok := s.lookup(key)
	return img.img, ok
}

func (imageStore) Prerendered(string, image.Point, imgpkg.Protocol) (imgpkg.Render, bool) {
	return imgpkg.Render{}, false
}

func (imageStore) ConfigurePrerender(imgpkg.Protocol) {}

func (imageStore) ConfigurePrerenderKitty(*imgpkg.KittyRenderer) {}

func (imageStore) InvalidatePrerender() {}
