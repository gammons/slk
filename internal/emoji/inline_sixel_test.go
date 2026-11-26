package emoji

import (
	"context"
	goimage "image"
	"io"
	"testing"
	"time"

	imgpkg "github.com/gammons/slk/internal/image"
)

// inlineFetcher serves prerendered renders by cache key and records the
// fetches a cold Place starts.
type inlineFetcher struct {
	rendered map[string]imgpkg.Render
	fetched  chan string
}

func (f *inlineFetcher) Fetch(_ context.Context, req imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	if f.fetched != nil {
		f.fetched <- req.URL
	}
	return imgpkg.FetchResult{}, nil
}

func (f *inlineFetcher) Prerendered(key string, _ goimage.Point, _ imgpkg.Protocol) (imgpkg.Render, bool) {
	r, ok := f.rendered[key]
	return r, ok
}

func warmSixelRender() imgpkg.Render {
	return imgpkg.Render{
		Cells:    goimage.Pt(2, 1),
		Fallback: []string{"  "},
		OnFlush: func(w io.Writer) error {
			_, err := io.WriteString(w, "\x1bPqpayload")
			return err
		},
	}
}

func withSixelInline(t *testing.T) {
	t.Helper()
	SetSixelInlineMode(true, 2)
	t.Cleanup(func() { SetSixelInlineMode(false, 2) })
}

func TestInlineSixelPlaceWarm(t *testing.T) {
	withSixelInline(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	f := &inlineFetcher{rendered: map[string]imgpkg.Render{EmojiCacheKey(url): warmSixelRender()}}
	sink := NewInlineSixel(PlaceContext{Fetcher: f}, 2, imgpkg.ProtoSixel)
	if sink == nil {
		t.Fatal("NewInlineSixel returned nil with sixel enabled")
	}

	marker, ok := sink.Place(url)
	if !ok {
		t.Fatal("Place returned ok=false for a warm image")
	}
	if want := inlineMarker(0, 2); marker != want {
		t.Errorf("marker = %q, want %q", marker, want)
	}
	if idx, ok := InlineMarkerIndex([]rune(marker)[1]); !ok || idx != 0 {
		t.Errorf("marker index rune = %q, %v, want index 0", string([]rune(marker)[1]), ok)
	}
	if got := Width(marker); got != 2 {
		t.Errorf("marker width = %d, want 2 (the image cells)", got)
	}
	imgs := sink.Images()
	if len(imgs) != 1 {
		t.Fatalf("images = %d, want one", len(imgs))
	}
	if imgs[0].Key != EmojiCacheKey(url) || imgs[0].Width != 2 {
		t.Errorf("image = %+v, want the sink's key and 2 cells", imgs[0])
	}
	if string(imgs[0].Bytes) != "\x1bPqpayload" {
		t.Errorf("image bytes = %q, want the sixel payload", imgs[0].Bytes)
	}
}

func TestInlineSixelPlaceColdStartsAFetch(t *testing.T) {
	withSixelInline(t)
	const url = "https://emoji.slack-edge.com/T1/cold/abc.png"
	f := &inlineFetcher{fetched: make(chan string, 1)}
	sink := NewInlineSixel(PlaceContext{Fetcher: f}, 2, imgpkg.ProtoSixel)

	if _, ok := sink.Place(url); !ok {
		t.Fatal("a cold image must still reserve its cells")
	}
	imgs := sink.Images()
	if len(imgs) != 1 || len(imgs[0].Bytes) != 0 {
		t.Fatalf("images = %+v, want one without bytes", imgs)
	}
	select {
	case got := <-f.fetched:
		if got != url {
			t.Errorf("fetched %q, want %q", got, url)
		}
	case <-time.After(time.Second):
		t.Fatal("Place did not start a fetch for a cold image")
	}
}

func TestInlineSixelFailedURLStaysText(t *testing.T) {
	withSixelInline(t)
	const url = "https://emoji.slack-edge.com/T1/broken/abc.png"
	markEmojiURLFailed(url)
	t.Cleanup(resetNegativeEmojiCache)
	sink := NewInlineSixel(PlaceContext{Fetcher: &inlineFetcher{}}, 2, imgpkg.ProtoSixel)
	if _, ok := sink.Place(url); ok {
		t.Error("a URL that failed to fetch must not be placed")
	}
	if len(sink.Images()) != 0 {
		t.Error("a failed URL recorded an image")
	}
}

func TestNewInlineSixelGuards(t *testing.T) {
	SetSixelInlineMode(false, 2)
	if NewInlineSixel(PlaceContext{Fetcher: &inlineFetcher{}}, 2, imgpkg.ProtoSixel) != nil {
		t.Error("disabled sixel inline mode returned a sink")
	}
	withSixelInline(t)
	if NewInlineSixel(PlaceContext{Fetcher: &inlineFetcher{}}, 2, imgpkg.ProtoKitty) != nil {
		t.Error("a kitty terminal returned a sixel sink")
	}
	if NewInlineSixel(PlaceContext{}, 2, imgpkg.ProtoSixel) != nil {
		t.Error("no fetcher returned a sink")
	}
}
