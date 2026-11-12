package emoji

import (
	"context"
	goimage "image"
	"io"
	"testing"

	imgpkg "github.com/gammons/slk/internal/image"
)

// withImageMode turns image mode on for one test.
func withImageMode(t *testing.T, cells int) {
	t.Helper()
	SetImageMode(true, cells)
	t.Cleanup(resetImageMode)
}

func TestPlaceShortcode_WarmStandardEmoji(t *testing.T) {
	withImageMode(t, 2)
	url, ok := URLForShortcode("orange_book", nil)
	if !ok {
		t.Fatal("precondition: :orange_book: has a URL")
	}
	ff := newFakeFetcher()
	flushed := 0
	ff.setPrerendered(EmojiCacheKey(url), goimage.Pt(2, 1), imgpkg.Render{
		Lines:   []string{"<book>"},
		OnFlush: func(io.Writer) error { flushed++; return nil },
	})

	got, flush, ok := PlaceShortcode(PlaceContext{Fetcher: ff}, "orange_book", nil, 2)
	if !ok || got != "<book>" {
		t.Fatalf("PlaceShortcode = (%q, ok=%v), want the prerendered placement", got, ok)
	}
	if flush == nil {
		t.Fatal("warm path returned no flush")
	}
	_ = flush(discardWriter{})
	if flushed != 1 {
		t.Errorf("flush ran %d times, want 1", flushed)
	}
}

// A workspace custom emoji resolves through the customs map, aliases
// included, to its own artwork.
func TestPlaceShortcode_CustomEmojiThroughAlias(t *testing.T) {
	withImageMode(t, 2)
	customURL := "https://emoji.slack-edge.com/T1/partyparrot/abc.gif"
	customs := map[string]string{
		"partyparrot": customURL,
		"parrot":      "alias:partyparrot",
	}
	ff := newFakeFetcher()
	ff.setPrerendered(EmojiCacheKey(customURL), goimage.Pt(2, 1), imgpkg.Render{Lines: []string{"<parrot>"}})

	got, _, ok := PlaceShortcode(PlaceContext{Fetcher: ff}, "parrot", customs, 2)
	if !ok || got != "<parrot>" {
		t.Errorf("PlaceShortcode(parrot) = (%q, ok=%v), want the custom emoji's placement", got, ok)
	}
}

// Every reason not to place reports ok=false, so the caller falls back
// to its glyph or text: image mode off, no fetcher, unknown name, no
// cells.
func TestPlaceShortcode_NotPlaced(t *testing.T) {
	ff := newFakeFetcher()
	ctx := PlaceContext{Fetcher: ff}

	if s, _, ok := PlaceShortcode(ctx, "rocket", nil, 2); ok || s != "" {
		t.Errorf("image mode off: PlaceShortcode = (%q, ok=%v), want not placed", s, ok)
	}

	withImageMode(t, 2)
	cases := []struct {
		name  string
		ctx   PlaceContext
		code  string
		cells int
	}{
		{"no fetcher", PlaceContext{}, "rocket", 2},
		{"unknown name", ctx, "no_such_emoji_here", 2},
		{"empty name", ctx, "", 2},
		{"zero cells", ctx, "rocket", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if s, flush, ok := PlaceShortcode(c.ctx, c.code, nil, c.cells); ok || s != "" || flush != nil {
				t.Errorf("PlaceShortcode = (%q, flush=%v, ok=%v), want not placed", s, flush != nil, ok)
			}
		})
	}
	if n := len(ff.fetchCallsSnapshot()); n != 0 {
		t.Errorf("fetcher called %d times, want 0", n)
	}
}

// Cold path: the image is not fetched yet, so the reservation is cells
// spaces wide and a fetch starts.
func TestPlaceShortcode_ColdReservesCells(t *testing.T) {
	withImageMode(t, 2)
	url, _ := URLForShortcode("books", nil)
	t.Cleanup(func() { awaitInflightCleared(EmojiCacheKey(url)) })
	ff := newFakeFetcher()
	ff.fetchFn = func(_ context.Context, _ imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
		return imgpkg.FetchResult{}, nil
	}

	got, flush, ok := PlaceShortcode(PlaceContext{Fetcher: ff}, "books", nil, 2)
	if !ok || got != "  " || flush != nil {
		t.Errorf("cold PlaceShortcode = (%q, flush=%v, ok=%v), want (\"  \", nil, true)", got, flush != nil, ok)
	}
}
