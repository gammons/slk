package ui

import (
	"context"
	stdimage "image"
	"testing"

	tea "charm.land/bubbletea/v2"

	imgpkg "github.com/gammons/slk/internal/image"
)

// recordingFetcher implements core.ImageFetcher and counts the
// InvalidatePrerender calls, so the reducer test can observe the
// encoded-image cache dropping.
type recordingFetcher struct {
	invalidations int
}

func (f *recordingFetcher) Fetch(_ context.Context, _ imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{}, nil
}

func (f *recordingFetcher) Cached(string, stdimage.Point) (stdimage.Image, bool) {
	return nil, false
}

func (f *recordingFetcher) Prerendered(string, stdimage.Point, imgpkg.Protocol) (imgpkg.Render, bool) {
	return imgpkg.Render{}, false
}

func (f *recordingFetcher) ConfigurePrerender(imgpkg.Protocol) {}
func (f *recordingFetcher) ConfigurePrerenderKitty(*imgpkg.KittyRenderer) {
}
func (f *recordingFetcher) InvalidatePrerender() { f.invalidations++ }

// CellMetricsChangedMsg re-points the image context's cell pixels and
// re-pushes it (the pane renderers hold copies, and their
// SetImageContext drops their render caches), drops the fetcher's
// prerendered cache and the preview memo, and arms the sixel painter's
// next plan. It deliberately does NOT touch the window size: the full
// text redraw that restores the cells the erase blanks is cmd/slk's
// tea.ClearScreen, sent right after this message. A re-delivered
// WindowSizeMsg would race the real resize the cell change usually
// arrives with, and the resize arm would accept the stale dimensions as
// the terminal's.
func TestCellMetricsChangedMsg_InvalidatesWithoutTouchingSize(t *testing.T) {
	a := newTestApp(t, withSize(120, 40), withRender())
	// A real resize has happened before any metrics change can: it is
	// what sets a.width/a.height.
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	fetcher := &recordingFetcher{}
	a.SetImageFetcher(fetcher)
	a.forceSixelRepaint = false

	cmd, handled := reduceIO(a, CellMetricsChangedMsg{CellW: 9, CellH: 20})
	if !handled {
		t.Fatal("reduceIO did not handle CellMetricsChangedMsg")
	}
	if cmd != nil {
		t.Errorf("reduceIO returned %T, want no command (the redraw is cmd/slk's tea.ClearScreen)", cmd)
	}
	if !a.forceSixelRepaint {
		t.Error("forceSixelRepaint not set; the painter would keep the pixels painted at the old size")
	}
	if a.width != 120 || a.height != 40 {
		t.Errorf("the App's size moved to %dx%d, want it untouched at 120x40", a.width, a.height)
	}
	if a.imageCtx.CellPixels.X != 9 || a.imageCtx.CellPixels.Y != 20 {
		t.Errorf("imageCtx.CellPixels = %v, want (9,20): image layout would keep the startup measurement", a.imageCtx.CellPixels)
	}
	if fetcher.invalidations != 1 {
		t.Errorf("InvalidatePrerender called %d times, want 1: rebuilt panes would reuse sixel bytes encoded at the old size", fetcher.invalidations)
	}
}
