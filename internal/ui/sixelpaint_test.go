// internal/ui/sixelpaint_test.go
//
// absoluteWindowSixelPlacements had zero test coverage despite being flagged (in
// devdocs/image-problem.md) as the prime suspect for slk's still-open sixel
// positioning bug. These tests pin its row/col math against the SAME
// formula already proven correct for real click round-trips in
// TestMouseClick_OnImageDispatchesOpenPreview (app_imagepreview_test.go):
//
//	X_terminal = layout.sidebarEnd + 1 (border) + paneCol
//	Y_terminal = 1 (border) + chromeHeight + contentRow
//
// absoluteWindowSixelPlacements is the same inverse-of-PanelAt mapping, plus the
// CUP 1-based conversion and the right/bottom clip checks. If these ever
// disagree with panellayout.go's PanelAt, every sixel image is displaced —
// exactly the symptom on file.
package ui

import (
	"context"
	stdimage "image"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/wintree"
)

// primeChromeHeight drives one ViewBare call so messagepane.ChromeHeight()
// reports a real value. absoluteWindowSixelPlacements is only meaningful
// after a render, same precondition as ChromeHeight's own doc comment.
func primeChromeHeight(a *App, width, height int) {
	a.messagepane.SetChannel("general", "")
	_ = a.messagepane.ViewBare(height, width)
}

// fullRegionRect mirrors the bounds renderWindowsRegion passes to
// ComputeRects for the single-window / messages-region case.
func fullRegionRect(a *App, height int) wintree.Rect {
	return wintree.Rect{
		X: 0,
		Y: 0,
		W: a.layout.MsgEnd() - a.layout.SidebarEnd(),
		H: height - 1, // content height: status bar occupies the last row
	}
}

// TestAbsoluteWindowSixelPlacements_SingleWindowMatchesPanelAtInverse
// pins the leaf-relative math for the single full-region window against
// the SAME formula already proven correct for real click round-trips in
// TestMouseClick_OnImageDispatchesOpenPreview (app_imagepreview_test.go):
//
//	X_terminal = layout.sidebarEnd + 1 (border) + paneCol
//	Y_terminal = 1 (border) + chromeHeight + contentRow
//
// absoluteWindowSixelPlacements is the same inverse-of-PanelAt mapping,
// plus the CUP 1-based conversion and the leaf-border clip checks.
func TestAbsoluteWindowSixelPlacements_SingleWindowMatchesPanelAtInverse(t *testing.T) {
	a := newPanelAtApp()
	a.height = 24
	primeChromeHeight(a, 50, 20)
	chrome := a.messagepane.ChromeHeight()
	if chrome < 1 {
		t.Fatalf("precondition: expected chromeHeight >= 1, got %d", chrome)
	}

	got := absoluteWindowSixelPlacements(
		[]imgpkg.SixelPaint{{Key: "k", Row: 3, Rows: 4, Cols: 6}},
		chrome, fullRegionRect(a, a.height), a.layout.SidebarEnd(),
	)
	if len(got) != 1 {
		t.Fatalf("placement rejected for the single full-region window; got %d", len(got))
	}
	pl := got[0]

	// Independently derive the expected CUP coordinates via the same
	// inverse-of-PanelAt formula the mouse-click round-trip test uses,
	// then convert 0-based -> 1-based CUP.
	wantX := a.layout.sidebarEnd + 1 + 0 // p.Col is always 0
	wantY := 1 + chrome + 3
	wantCol := wantX + 1
	wantRow := wantY + 1

	if pl.Row != wantRow {
		t.Errorf("Row = %d, want %d (sidebarEnd=%d chrome=%d)", pl.Row, wantRow, a.layout.sidebarEnd, chrome)
	}
	if pl.Col != wantCol {
		t.Errorf("Col = %d, want %d", pl.Col, wantCol)
	}
	if pl.Rows != 4 || pl.Cols != 6 {
		t.Errorf("footprint = %dx%d, want 6x4", pl.Cols, pl.Rows)
	}
}

func TestAbsoluteWindowSixelPlacements_SingleWindowRejectsPastRightEdge(t *testing.T) {
	a := newPanelAtApp()
	a.height = 24
	primeChromeHeight(a, 50, 20)

	// Messages band is [sidebarEnd=25, msgEnd=77), i.e. 52 cols wide
	// including both border columns, so 50 content cols. A 51-wide
	// image cannot fit without touching the right border.
	got := absoluteWindowSixelPlacements(
		[]imgpkg.SixelPaint{{Key: "k", Row: 0, Rows: 2, Cols: 51}},
		a.messagepane.ChromeHeight(), fullRegionRect(a, a.height), a.layout.SidebarEnd(),
	)
	if len(got) != 0 {
		t.Fatalf("expected rejection for a placement wider than the pane content")
	}
}

func TestAbsoluteWindowSixelPlacements_SingleWindowRejectsPastBottomEdge(t *testing.T) {
	a := newPanelAtApp()
	a.height = 24
	primeChromeHeight(a, 50, 20)

	got := absoluteWindowSixelPlacements(
		[]imgpkg.SixelPaint{{Key: "k", Row: 0, Rows: 100, Cols: 4}},
		a.messagepane.ChromeHeight(), fullRegionRect(a, a.height), a.layout.SidebarEnd(),
	)
	if len(got) != 0 {
		t.Fatalf("expected rejection for a placement taller than the pane")
	}
}

func TestAbsoluteWindowSixelPlacements_SingleWindowRejectsZeroFootprint(t *testing.T) {
	a := newPanelAtApp()
	a.height = 24
	primeChromeHeight(a, 50, 20)

	for _, p := range []imgpkg.SixelPaint{
		{Key: "k", Row: 0, Rows: 0, Cols: 4},
		{Key: "k", Row: 0, Rows: 4, Cols: 0},
	} {
		got := absoluteWindowSixelPlacements(
			[]imgpkg.SixelPaint{p},
			a.messagepane.ChromeHeight(), fullRegionRect(a, a.height), a.layout.SidebarEnd(),
		)
		if len(got) != 0 {
			t.Fatalf("expected rejection for zero-footprint placement %+v", p)
		}
	}
}

// A right split's placements are offset by the leaf's own origin, not
// the unsplit messages-pane origin.
func TestAbsoluteWindowSixelPlacements_RightSplitUsesLeafOrigin(t *testing.T) {
	rect := wintree.Rect{X: 40, Y: 0, W: 40, H: 24}
	got := absoluteWindowSixelPlacements(
		[]imgpkg.SixelPaint{{Key: "right", Row: 3, Col: 0, Rows: 4, Cols: 6}},
		2, rect, 25,
	)
	if len(got) != 1 || got[0].Col != 67 || got[0].Row != 7 {
		t.Fatalf("placement = %+v, want CUP row=7 col=67", got)
	}
}

// A bottom split's placements are offset by the leaf's own vertical
// origin.
func TestAbsoluteWindowSixelPlacements_BottomSplitUsesLeafOrigin(t *testing.T) {
	rect := wintree.Rect{X: 0, Y: 12, W: 80, H: 12}
	got := absoluteWindowSixelPlacements(
		[]imgpkg.SixelPaint{{Key: "bottom", Row: 1, Col: 0, Rows: 2, Cols: 5}},
		2, rect, 25,
	)
	if len(got) != 1 || got[0].Col != 27 || got[0].Row != 17 {
		t.Fatalf("placement = %+v, want CUP row=17 col=27", got)
	}
}

// Each leaf clips against its OWN border: a placement inside the global
// messages band but past a narrow leaf's right edge is rejected.
func TestAbsoluteWindowSixelPlacements_ClipsAgainstLeafBorders(t *testing.T) {
	rect := wintree.Rect{X: 40, Y: 0, W: 10, H: 6}
	// contentLeft = 25+40+1 = 66; right = 25+40+10-1 = 74. Cols=9 ->
	// col+cols = 75 > 74, so the placement reaches past the leaf border.
	got := absoluteWindowSixelPlacements(
		[]imgpkg.SixelPaint{{Key: "wide", Row: 0, Col: 0, Rows: 1, Cols: 9}},
		2, rect, 25,
	)
	if len(got) != 0 {
		t.Fatalf("wide placement not clipped to the leaf's right border: %+v", got)
	}
	// contentTop = 0+1+2 = 3; bottom = 0+6-1 = 5. Rows=5 -> row+rows =
	// 8 > 5, so the placement reaches past the leaf's bottom border.
	got = absoluteWindowSixelPlacements(
		[]imgpkg.SixelPaint{{Key: "tall", Row: 0, Col: 0, Rows: 5, Cols: 1}},
		2, rect, 25,
	)
	if len(got) != 0 {
		t.Fatalf("tall placement not clipped to the leaf's bottom border: %+v", got)
	}
}

// TestCollectSixelPlacements_TwoWindowsFocusedAndUnfocused drives a
// two-window app where BOTH windows render an inline sixel image and
// asserts the collector returns one placement inside each leaf
// rectangle, and that switching focus neither drops the unfocused
// placement nor moves either origin.
func TestCollectSixelPlacements_TwoWindowsFocusedAndUnfocused(t *testing.T) {
	a, w1, w2 := twoWindowApp(t)
	a.imgProtocol = imgpkg.ProtoSixel
	setupTwoWindowSixelImages(t, a)

	frame := a.layout.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(), a.sidebarVisible, a.threadVisible)
	bounds := wintree.Rect{X: 0, Y: 0, W: frame.MsgWidth + frame.MsgBorder, H: frame.ContentHeight}
	rects := a.wins.ComputeRects(bounds)

	// Render each window's model at its own leaf's size so placements
	// land in each model's content area.
	for _, id := range a.wins.Leaves() {
		rect := rects[id]
		if rect.W < 1 || rect.H < 1 {
			t.Fatalf("window %v has a degenerate rect: %+v", id, rect)
		}
		_ = a.winModels[id].View(rect.H, rect.W)
	}

	placements := a.collectSixelPlacements(frame)
	if len(placements) != 2 {
		t.Fatalf("placements = %d, want 2 (one per visible window)", len(placements))
	}

	sidebarEnd := a.layout.SidebarEnd()
	covered := map[wintree.LeafID]bool{}
	for _, pl := range placements {
		col0 := pl.Col - 1
		row0 := pl.Row - 1
		inside := false
		for _, id := range a.wins.Leaves() {
			r := rects[id]
			if col0 >= sidebarEnd+r.X && col0 < sidebarEnd+r.X+r.W && row0 >= r.Y && row0 < r.Y+r.H {
				covered[id] = true
				inside = true
				break
			}
		}
		if !inside {
			t.Fatalf("placement %+v lands outside every leaf rect", pl)
		}
	}
	if len(covered) != 2 {
		t.Fatalf("placements cover %d windows, want both %v/%v (covered=%v)", len(covered), w1, w2, covered)
	}

	// Changing focus must not remove the unfocused window's placement
	// or move either origin.
	a.focusedWin = w1
	after := a.collectSixelPlacements(frame)
	if len(after) != 2 {
		t.Fatalf("after focus switch placements = %d, want 2", len(after))
	}
	for i := range placements {
		if placements[i].Row != after[i].Row || placements[i].Col != after[i].Col {
			t.Fatalf("focus switch moved a placement: before %+v after %+v", placements[i], after[i])
		}
	}
}

// setupTwoWindowSixelImages wires a shared sixel image pipeline into
// every window model of a, with one image-bearing message each, using
// the same image-cache staging pattern as the messages package's
// setupImageMessageModel: on-disk PNG, memo prime via Fetch, then a
// sixel ImageContext with the test cell metrics.
func setupTwoWindowSixelImages(t *testing.T, a *App) {
	t.Helper()
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	const fileID = "F0123ABCD"
	const key = fileID + "-720"
	if _, err := cache.Put(key, "png", makeTestPNGBytes(720, 720)); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	// Prime the in-memory decoded memo (Fetcher.Cached is memo-only).
	if _, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:    key,
		URL:    "unused://disk-cache-hits-skip-network",
		Target: stdimage.Pt(320, 320),
	}); err != nil {
		t.Fatalf("Fetch (memo prime): %v", err)
	}
	ctx := imgrender.ImageContext{
		Protocol:   imgpkg.ProtoSixel,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	}
	a.SetImageContext(ctx)
	_, _, _, msg := imageBearingMessage(t)
	for _, m := range a.allWinModels() {
		m.SetMessages([]messages.MessageItem{msg})
	}
}

// TestAbsolutePreviewSixelPlacement_NoBorderOffset pins the one
// difference from the messages-pane formula: the preview panel has no
// border of its own (renderPreviewPanel wraps it with exactSize, not a
// bordered*Pane helper), so unlike absoluteWindowSixelPlacements there is no
// "+1" for a top/left border — only the sidebar's left edge offset.
func TestAbsolutePreviewSixelPlacement_NoBorderOffset(t *testing.T) {
	a := newPanelAtApp()
	a.height = 24

	p := imgpkg.SixelPaint{Key: "k", Row: 4, Col: 10, Rows: 6, Cols: 20}
	pl, ok := a.absolutePreviewSixelPlacement(p)
	if !ok {
		t.Fatalf("absolutePreviewSixelPlacement rejected a placement that should fit")
	}

	wantCol := a.layout.sidebarEnd + p.Col + 1 // no border offset, then 1-based CUP
	wantRow := p.Row + 1

	if pl.Col != wantCol {
		t.Errorf("Col = %d, want %d (sidebarEnd=%d p.Col=%d)", pl.Col, wantCol, a.layout.sidebarEnd, p.Col)
	}
	if pl.Row != wantRow {
		t.Errorf("Row = %d, want %d (p.Row=%d)", pl.Row, wantRow, p.Row)
	}
	if pl.Rows != p.Rows || pl.Cols != p.Cols {
		t.Errorf("footprint = %dx%d, want %dx%d", pl.Cols, pl.Rows, p.Cols, p.Rows)
	}
	if pl.Key != p.Key {
		t.Errorf("Key = %q, want %q", pl.Key, p.Key)
	}
}

func TestAbsolutePreviewSixelPlacement_RejectsZeroFootprint(t *testing.T) {
	a := newPanelAtApp()
	a.height = 24

	for _, p := range []imgpkg.SixelPaint{
		{Key: "k", Row: 0, Col: 0, Rows: 0, Cols: 4},
		{Key: "k", Row: 0, Col: 0, Rows: 4, Cols: 0},
	} {
		if _, ok := a.absolutePreviewSixelPlacement(p); ok {
			t.Fatalf("expected rejection for zero-footprint placement %+v", p)
		}
	}
}

func TestAbsoluteSixelPlacement_RejectsZeroFootprint(t *testing.T) {
	a := newPanelAtApp()
	a.height = 24
	primeChromeHeight(a, 50, 20)

	for _, p := range []imgpkg.SixelPaint{
		{Key: "k", Row: 0, Rows: 0, Cols: 4},
		{Key: "k", Row: 0, Rows: 4, Cols: 0},
	} {
		got := absoluteWindowSixelPlacements(
			[]imgpkg.SixelPaint{p},
			a.messagepane.ChromeHeight(), fullRegionRect(a, a.height), a.layout.SidebarEnd(),
		)
		if len(got) != 0 {
			t.Fatalf("expected rejection for zero-footprint placement %+v", p)
		}
	}
}

// sixelTestApp returns an App configured for sixel frame publication:
// protocol sixel plus a fresh frame store, at the zero-size state so the
// early-fallback path is reachable.
func sixelTestApp() *App {
	a := NewApp()
	a.imgProtocol = imgpkg.ProtoSixel
	a.sixelFrames = imgpkg.NewSixelFrameStore()
	return a
}

// frameIDFromTitle parses the internal frame ID out of a marked
// WindowTitle. The marker literal lives only in tests.
func frameIDFromTitle(t *testing.T, title string) uint64 {
	t.Helper()
	const marker = "\x1fslk-sixel-frame="
	idx := strings.Index(title, marker)
	if idx < 0 {
		t.Fatalf("title %q carries no frame marker", title)
	}
	id, err := strconv.ParseUint(title[idx+len(marker):], 10, 64)
	if err != nil {
		t.Fatalf("unparsable frame id in title %q: %v", title, err)
	}
	return id
}

func TestFinalizeSixelView_PublishesPlacementAndMarksTitle(t *testing.T) {
	a := sixelTestApp()
	v := tea.NewView("screen\ncontent\nlines")
	placements := []imgpkg.SixelPlacement{{
		Key: "k", Row: 2, Col: 3, Rows: 1, Cols: 1,
		Bytes: []byte("\x1bPqk\x1b\\"),
	}}
	out := a.finalizeSixelView(v, placements)

	id := frameIDFromTitle(t, out.WindowTitle)
	frame, ok := a.sixelFrames.Take(id)
	if !ok {
		t.Fatalf("frame %d not published", id)
	}
	if len(frame.Placements) != 1 {
		t.Fatalf("Placements = %d, want 1", len(frame.Placements))
	}
	if frame.Placements[0].Key != "k" {
		t.Fatalf("placement key = %q, want k", frame.Placements[0].Key)
	}
	// The guard must be attached from the complete screen string.
	if frame.Placements[0].Guard == "" {
		t.Fatal("guard not attached from the frame content")
	}
	// The marked title must strip back to the real title.
	if !strings.HasPrefix(out.WindowTitle, a.windowTitle) {
		t.Fatalf("marked title %q does not carry real title %q", out.WindowTitle, a.windowTitle)
	}
}

// A frame with no visible sixel surface must publish an empty frame so
// the painter erases any previously painted pixels.
func TestFinalizeSixelView_EmptyFrameErasesPriorSurface(t *testing.T) {
	a := sixelTestApp()
	a.finalizeSixelView(tea.NewView("one"), []imgpkg.SixelPlacement{{
		Key: "k", Row: 2, Col: 3, Rows: 1, Cols: 1, Bytes: []byte("\x1bPqk\x1b\\"),
	}})

	out := a.finalizeSixelView(tea.NewView("two"), nil)
	id := frameIDFromTitle(t, out.WindowTitle)
	frame, ok := a.sixelFrames.Take(id)
	if !ok {
		t.Fatal("second frame not published")
	}
	if len(frame.Placements) != 0 {
		t.Fatalf("empty surface must publish zero placements, got %d", len(frame.Placements))
	}
}

// Non-sixel protocols keep the plain real title and never publish a
// frame or touch the store.
func TestFinalizeSixelView_NonSixelKeepsRealTitleAndPublishesNothing(t *testing.T) {
	a := sixelTestApp()
	a.imgProtocol = imgpkg.ProtoHalfBlock

	out := a.finalizeSixelView(tea.NewView("content"), []imgpkg.SixelPlacement{{
		Key: "k", Row: 2, Col: 3, Rows: 1, Cols: 1,
	}})
	if out.WindowTitle != a.windowTitle {
		t.Fatalf("title = %q, want real %q", out.WindowTitle, a.windowTitle)
	}
	if strings.Contains(out.WindowTitle, "slk-sixel-frame=") {
		t.Fatalf("non-sixel title carries a frame marker")
	}
	if _, ok := a.sixelFrames.Take(1); ok {
		t.Fatal("non-sixel finalizer must not publish a frame")
	}
}

// A real window-size change forces exactly the next published frame;
// duplicate size messages do not, and the flag is consumed by use.
func TestWindowSizeChangeForcesExactlyNextSixelFrame(t *testing.T) {
	a := sixelTestApp()

	first := a.finalizeSixelView(tea.NewView("a"), nil)
	f1, ok := a.sixelFrames.Take(frameIDFromTitle(t, first.WindowTitle))
	if !ok || f1.Force {
		t.Fatalf("initial frame: ok=%v Force=%v, want ok=true Force=false", ok, f1.Force)
	}

	// A real size change marks the next published frame for repaint.
	_, _ = a.Update(tea.WindowSizeMsg{Width: 101, Height: 25})
	forced := a.finalizeSixelView(tea.NewView("b"), nil)
	f2, ok := a.sixelFrames.Take(frameIDFromTitle(t, forced.WindowTitle))
	if !ok || !f2.Force {
		t.Fatalf("frame after real resize: ok=%v Force=%v, want forced", ok, f2.Force)
	}

	// The flag was consumed: the following frame is not forced.
	next := a.finalizeSixelView(tea.NewView("c"), nil)
	f3, ok := a.sixelFrames.Take(frameIDFromTitle(t, next.WindowTitle))
	if !ok || f3.Force {
		t.Fatalf("subsequent frame: ok=%v Force=%v, want not forced", ok, f3.Force)
	}

	// A duplicate size message (same dimensions) must not force again.
	_, _ = a.Update(tea.WindowSizeMsg{Width: 101, Height: 25})
	if a.forceSixelRepaint {
		t.Fatal("duplicate WindowSizeMsg must not set forceSixelRepaint")
	}
}

// The pre-measurement fallback view publishes an empty frame so a
// previous surface is erased even before the first real layout.
func TestEarlyFallbackPublishesEmptySixelFrame(t *testing.T) {
	a := sixelTestApp()
	if a.width != 0 || a.height != 0 {
		t.Fatal("precondition: early fallback needs zero size")
	}

	v := a.View()
	id := frameIDFromTitle(t, v.WindowTitle)
	frame, ok := a.sixelFrames.Take(id)
	if !ok {
		t.Fatal("early fallback must publish a frame")
	}
	if len(frame.Placements) != 0 {
		t.Fatalf("early fallback frame must be empty, got %d placements", len(frame.Placements))
	}
}
