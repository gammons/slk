package messages

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"sort"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/config"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/styles"
)

// galleryFetcher serves one decoded image at every requested cell size,
// so a test can lay images out without a disk cache or a network.
type galleryFetcher struct {
	img   image.Image
	kitty *imgpkg.KittyRenderer
}

func newGalleryFetcher() *galleryFetcher {
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.Set(x, y, color.RGBA{R: 200, G: uint8(x * 4), B: uint8(y * 4), A: 255})
		}
	}
	return &galleryFetcher{img: src, kitty: imgpkg.NewKittyRenderer(imgpkg.NewRegistry())}
}

func (f *galleryFetcher) Fetch(context.Context, imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{}, nil
}

func (f *galleryFetcher) Cached(string, image.Point) (image.Image, bool) {
	return f.img, true
}

func (f *galleryFetcher) Prerendered(key string, cell image.Point, proto imgpkg.Protocol) (imgpkg.Render, bool) {
	if cell.X <= 0 || cell.Y <= 0 {
		return imgpkg.Render{}, false
	}
	if proto == imgpkg.ProtoKitty {
		// Keyed by the file id, the way the fetcher prerenders: two
		// attachments of one picture are still two images.
		f.kitty.SetSource(key, f.img)
		return f.kitty.RenderKey(key, cell), true
	}
	return imgpkg.RenderImage(proto, f.img, cell), true
}

func (f *galleryFetcher) ConfigurePrerender(imgpkg.Protocol)            {}
func (f *galleryFetcher) ConfigurePrerenderKitty(*imgpkg.KittyRenderer) {}

func (f *galleryFetcher) InvalidatePrerender() {}

func galleryMessage(n int, fileBetween bool) MessageItem {
	var atts []Attachment
	for i := 0; i < n; i++ {
		if fileBetween && i == 1 {
			atts = append(atts, Attachment{
				Kind: "file", Name: "notes.pdf", URL: "https://example.com/notes.pdf", FileID: "FD",
			})
		}
		atts = append(atts, Attachment{
			Kind:   "image",
			Name:   "img.png",
			URL:    "https://example.com/img",
			FileID: string(rune('A' + i)),
			Thumbs: []ThumbSpec{{URL: "https://example.com/t.png", W: 400, H: 400}},
		})
	}
	return MessageItem{
		TS: "1.000", UserID: "U1", UserName: "alice", Text: "pics",
		Timestamp: "10:30 AM", Attachments: atts,
	}
}

func galleryPane(t *testing.T, proto imgpkg.Protocol, msg MessageItem) *Model {
	t.Helper()
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	f := newGalleryFetcher()
	m := New([]MessageItem{msg}, "C1")
	ctx := imgrender.ImageContext{
		Protocol:   proto,
		Fetcher:    f,
		CellPixels: image.Pt(8, 16),
		MaxRows:    20,
	}
	if proto == imgpkg.ProtoKitty {
		ctx.KittyRender = f.kitty
	}
	m.SetImageContext(ctx)
	return m
}

func sortedImageHits(m *Model) []entrylist.FrameHit {
	hits := frameHits(m.list, entrylist.HitImage)
	sort.Slice(hits, func(i, j int) bool { return hits[i].Aux < hits[j].Aux })
	return hits
}

// A square thumb at MaxRows 20 and 8×16 cells is 40×20. View(60, 80)
// has no avatar, so the content width is 76 and that footprint fits.
func TestGallery_SingleImageKeepsItsSize(t *testing.T) {
	m := galleryPane(t, imgpkg.ProtoHalfBlock, galleryMessage(1, false))
	_ = m.View(60, 80)
	hits := sortedImageHits(m)
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	h := hits[0]
	if w, ht := h.ColEnd-h.ColStart, h.RowEnd-h.RowStart; w != 40 || ht != 20 {
		t.Fatalf("single image footprint = %dx%d, want 40x20", w, ht)
	}
	if h.Aux != 0 || h.Key == "" {
		t.Fatalf("hit identity = aux %d key %q", h.Aux, h.Key)
	}
}

func TestGallery_TwoImagesShareARow(t *testing.T) {
	m := galleryPane(t, imgpkg.ProtoHalfBlock, galleryMessage(2, false))
	_ = m.View(60, 80)
	hits := sortedImageHits(m)
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	a, b := hits[0], hits[1]
	if a.RowStart != b.RowStart || a.RowEnd != b.RowEnd {
		t.Fatalf("images stacked: a rows [%d,%d) b rows [%d,%d)", a.RowStart, a.RowEnd, b.RowStart, b.RowEnd)
	}
	if a.ColEnd > b.ColStart {
		t.Fatalf("images overlap horizontally: a cols [%d,%d) b cols [%d,%d)", a.ColStart, a.ColEnd, b.ColStart, b.ColEnd)
	}
	aw, bw := a.ColEnd-a.ColStart, b.ColEnd-b.ColStart
	ah := a.RowEnd - a.RowStart
	if aw >= 40 || bw >= 40 || ah >= 20 {
		t.Fatalf("images were not scaled down to share the row: %dx%d and %d wide", aw, ah, bw)
	}
	if aw != bw {
		t.Fatalf("equal thumbs rendered at %d and %d cols", aw, bw)
	}
	// The gap is one column. Clicking each image reports its own attachment.
	if _, att, id, ok := m.HitTest((a.RowStart+a.RowEnd)/2, (a.ColStart+a.ColEnd)/2); !ok || att != 0 || id != a.Key {
		t.Fatalf("click on first image = ok %v att %d id %q, want att 0 id %q", ok, att, id, a.Key)
	}
	if _, att, id, ok := m.HitTest((b.RowStart+b.RowEnd)/2, (b.ColStart+b.ColEnd)/2); !ok || att != 1 || id != b.Key {
		t.Fatalf("click on second image = ok %v att %d id %q, want att 1 id %q", ok, att, id, b.Key)
	}
	// The gap between them is not part of either image.
	if _, _, _, ok := m.HitTest(a.RowStart, a.ColEnd); ok {
		t.Fatalf("click on the gap column %d registered as an image", a.ColEnd)
	}
}

func TestGallery_ThirdImageWraps(t *testing.T) {
	m := galleryPane(t, imgpkg.ProtoHalfBlock, galleryMessage(3, false))
	_ = m.View(60, 80)
	hits := sortedImageHits(m)
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want 3", len(hits))
	}
	if hits[0].RowStart != hits[1].RowStart {
		t.Fatalf("first two images did not share a row: rows %d and %d", hits[0].RowStart, hits[1].RowStart)
	}
	if hits[2].RowStart < hits[0].RowEnd {
		t.Fatalf("third image row %d overlaps the first row ending %d", hits[2].RowStart, hits[0].RowEnd)
	}
	if hits[2].ColStart != hits[0].ColStart {
		t.Fatalf("wrapped image starts at col %d, want %d", hits[2].ColStart, hits[0].ColStart)
	}
}

func TestGallery_FileBetweenImagesDoesNotShareARow(t *testing.T) {
	m := galleryPane(t, imgpkg.ProtoHalfBlock, galleryMessage(2, true))
	_ = m.View(60, 80)
	hits := sortedImageHits(m)
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].RowStart == hits[1].RowStart {
		t.Fatalf("images on either side of a file shared a row")
	}
	// Each is alone, so each keeps the single-image size.
	for i, h := range hits {
		if w, ht := h.ColEnd-h.ColStart, h.RowEnd-h.RowStart; w != 40 || ht != 20 {
			t.Fatalf("image %d footprint = %dx%d, want 40x20", i, w, ht)
		}
	}
}

func TestGallery_MaxColsCapsEveryImageOnTheRow(t *testing.T) {
	m := galleryPane(t, imgpkg.ProtoHalfBlock, galleryMessage(2, false))
	ctx := m.imgRenderer.Context()
	ctx.MaxCols = 10
	m.SetImageContext(ctx)
	_ = m.View(60, 80)
	hits := sortedImageHits(m)
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].RowStart != hits[1].RowStart {
		t.Fatalf("capped images did not share a row")
	}
	for i, h := range hits {
		if w := h.ColEnd - h.ColStart; w > 10 {
			t.Fatalf("image %d is %d cols, wider than MaxCols 10", i, w)
		}
	}
}

func TestGallery_NarrowPanePacksWithinTheContentWidth(t *testing.T) {
	// Width 24 → content width 20 (the pane's floor). Two square images
	// still share the row, and neither hit extends past the pane.
	m := galleryPane(t, imgpkg.ProtoHalfBlock, galleryMessage(2, false))
	_ = m.View(30, 24)
	hits := sortedImageHits(m)
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].RowStart != hits[1].RowStart {
		t.Fatalf("narrow pane stacked the images: rows %d and %d", hits[0].RowStart, hits[1].RowStart)
	}
	if hits[1].ColEnd > 24 {
		t.Fatalf("second image ends at col %d, past the pane width 24", hits[1].ColEnd)
	}
	if w := hits[0].ColEnd - hits[0].ColStart; w >= 20 {
		t.Fatalf("image width %d was not scaled down for a 20-col content area", w)
	}
}

func TestGallery_ResizeReflowsTheRow(t *testing.T) {
	m := galleryPane(t, imgpkg.ProtoHalfBlock, galleryMessage(2, false))
	_ = m.View(60, 80)
	wide := sortedImageHits(m)
	_ = m.View(30, 24)
	narrow := sortedImageHits(m)
	if len(wide) != 2 || len(narrow) != 2 {
		t.Fatalf("hits wide %d narrow %d, want 2 and 2", len(wide), len(narrow))
	}
	ww := wide[0].ColEnd - wide[0].ColStart
	nw := narrow[0].ColEnd - narrow[0].ColStart
	if nw >= ww {
		t.Fatalf("resize to a narrower pane left the image at %d cols (was %d)", nw, ww)
	}
	if narrow[0].RowStart != narrow[1].RowStart {
		t.Fatalf("narrow resize stacked the images")
	}
}

func TestGallery_SixelPlacementsShareTheRow(t *testing.T) {
	m := galleryPane(t, imgpkg.ProtoSixel, galleryMessage(2, false))
	out := m.View(60, 80)
	if strings.Contains(out, "\x1bP") {
		t.Fatal("sixel bytes leaked into the frame")
	}
	places := m.SixelPlacements()
	if len(places) != 2 {
		t.Fatalf("placements = %d, want 2", len(places))
	}
	sort.Slice(places, func(i, j int) bool { return places[i].Col < places[j].Col })
	if places[0].Row != places[1].Row {
		t.Fatalf("sixel rows %d and %d, want the same row", places[0].Row, places[1].Row)
	}
	if places[0].Col+places[0].Cols > places[1].Col {
		t.Fatalf("sixel footprints overlap: %+v %+v", places[0], places[1])
	}
	hits := sortedImageHits(m)
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if places[0].Col != hits[0].ColStart || places[1].Col != hits[1].ColStart {
		t.Fatalf("placement cols (%d, %d) != hit cols (%d, %d)",
			places[0].Col, places[1].Col, hits[0].ColStart, hits[1].ColStart)
	}
	if places[0].Cols != hits[0].ColEnd-hits[0].ColStart || places[0].Rows != hits[0].RowEnd-hits[0].RowStart {
		t.Fatalf("placement size %dx%d != hit size %dx%d",
			places[0].Cols, places[0].Rows, hits[0].ColEnd-hits[0].ColStart, hits[0].RowEnd-hits[0].RowStart)
	}
}

func TestGallery_SixelPartialVisibilityKeepsBothFallbacks(t *testing.T) {
	m := galleryPane(t, imgpkg.ProtoSixel, galleryMessage(2, false))
	_ = m.View(60, 80)
	hits := sortedImageHits(m)
	if len(hits) != 2 || hits[0].RowEnd <= hits[0].RowStart {
		t.Fatalf("hits = %+v", hits)
	}
	// Cut the viewport through the shared row so neither image is fully
	// visible. Both must fall back to half-block on the same pane row.
	clip := m.chromeHeight + hits[0].RowStart + 2
	if clip >= m.chromeHeight+hits[0].RowEnd {
		clip = m.chromeHeight + hits[0].RowStart + 1
	}
	out := m.View(clip, 80)
	if strings.Contains(out, "\x1bP") {
		t.Fatal("partial sixel leaked into the frame")
	}
	if n := len(m.SixelPlacements()); n != 0 {
		t.Fatalf("partial view published %d placements, want 0", n)
	}
	blocks := 0
	for _, line := range strings.Split(out, "\n") {
		n := strings.Count(line, "▀")
		if n > blocks {
			blocks = n
		}
	}
	want := (hits[0].ColEnd - hits[0].ColStart) + (hits[1].ColEnd - hits[1].ColStart)
	if blocks < want {
		t.Fatalf("widest fallback row has %d half-blocks, want at least %d (both images)", blocks, want)
	}
}

func TestGallery_KittyPlaceholdersResolveSideBySide(t *testing.T) {
	t.Setenv("TMUX", "")
	saved := imgpkg.KittyOutput
	t.Cleanup(func() { imgpkg.KittyOutput = saved })
	var uploads bytes.Buffer
	imgpkg.KittyOutput = &uploads

	m := galleryPane(t, imgpkg.ProtoKitty, galleryMessage(2, false))
	out := m.View(60, 80)
	if strings.Count(uploads.String(), "a=T") < 2 {
		t.Fatalf("kitty uploads = %d transmits, want one per image", strings.Count(uploads.String(), "a=T"))
	}
	if !strings.ContainsRune(out, imgpkg.PlaceholderRune) {
		t.Fatal("view has no kitty placeholders")
	}
	lines := strings.Split(out, "\n")
	_, rects := imgpkg.ResolvePlaceholders(out, 80, len(lines), ansi.WcWidth)
	if len(rects) < 2 {
		t.Fatalf("resolved %d kitty rectangles, want 2", len(rects))
	}
	sort.Slice(rects, func(i, j int) bool { return rects[i].X < rects[j].X })
	if rects[0].Y != rects[1].Y {
		t.Fatalf("kitty images on rows %d and %d, want the same row", rects[0].Y, rects[1].Y)
	}
	if rects[0].X+rects[0].Cols > rects[1].X {
		t.Fatalf("kitty rectangles overlap: %+v %+v", rects[0], rects[1])
	}
	if rects[0].ID == 0 || rects[1].ID == 0 || rects[0].ID == rects[1].ID {
		t.Fatalf("kitty ids = %d and %d, want two distinct images", rects[0].ID, rects[1].ID)
	}
	// The resolved cells agree with the click rects.
	hits := sortedImageHits(m)
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	// Frame rows include the channel header; hit rows are below it.
	if rects[0].X != hits[0].ColStart || rects[0].Cols != hits[0].ColEnd-hits[0].ColStart {
		t.Fatalf("kitty rect x,cols = %d,%d; hit cols [%d,%d)",
			rects[0].X, rects[0].Cols, hits[0].ColStart, hits[0].ColEnd)
	}
}

func TestGallery_ThreadSplicesHalfBlockSideBySide(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	f := newGalleryFetcher()
	rc := &RenderContext{Images: imgrender.NewRenderer(), UserNames: map[string]string{"U1": "alice"}}
	rc.Images.SetContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoSixel,
		Fetcher:    f,
		CellPixels: image.Pt(8, 16),
		MaxRows:    20,
	})
	// The thread pane records hits and does not record sixel rows, so
	// the half-block fallback has to be in the text.
	got := RenderMessage(galleryMessage(2, false), 80, rc, RenderOptions{ImageHits: true})
	if len(got.SixelRows) != 0 {
		t.Fatalf("thread render captured %d sixel rows, want 0", len(got.SixelRows))
	}
	var widest int
	for _, line := range strings.Split(got.Content, "\n") {
		if n := strings.Count(line, "▀"); n > widest {
			widest = n
		}
	}
	if widest < 60 {
		t.Fatalf("widest half-block line has %d blocks, want both images on one line", widest)
	}
	if w := lipgloss.Width(strings.Split(got.Content, "\n")[0]); w < 0 {
		t.Fatalf("width %d", w)
	}
}
