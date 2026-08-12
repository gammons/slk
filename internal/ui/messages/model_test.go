// internal/ui/messages/model_test.go
package messages

import (
	"bytes"
	"context"
	stdimage "image"
	imgcolor "image/color"
	imgpng "image/png"
	"io"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestMessagePaneView(t *testing.T) {
	msgs := []MessageItem{
		{UserName: "alice", Text: "Hello world", Timestamp: "10:30 AM"},
		{UserName: "bob", Text: "Hey there!", Timestamp: "10:31 AM"},
	}

	m := New(msgs, "general")
	view := m.View(20, 60) // height=20, width=60

	if !strings.Contains(view, "alice") {
		t.Error("expected 'alice' in view")
	}
	if !strings.Contains(view, "Hello world") {
		t.Error("expected 'Hello world' in view")
	}
	if !strings.Contains(view, "general") {
		t.Error("expected channel name in header")
	}
}

func TestMessagePaneNavigation(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserName: "alice", Text: "msg 1"},
		{TS: "2.0", UserName: "bob", Text: "msg 2"},
		{TS: "3.0", UserName: "carol", Text: "msg 3"},
	}

	m := New(msgs, "general")
	// Should start at bottom (newest message)
	if m.SelectedIndex() != 2 {
		t.Errorf("expected selected index 2, got %d", m.SelectedIndex())
	}

	m.MoveUp()
	if m.SelectedIndex() != 1 {
		t.Errorf("expected index 1 after move up, got %d", m.SelectedIndex())
	}
}

func TestMessagePaneAppend(t *testing.T) {
	m := New(nil, "general")

	m.AppendMessage(MessageItem{TS: "1.0", UserName: "alice", Text: "new message"})
	if len(m.Messages()) != 1 {
		t.Errorf("expected 1 message, got %d", len(m.Messages()))
	}
}

// TestHeaderGlyph_ByChannelType asserts that the message-pane header
// uses a type-aware glyph: # for public channels (default), \u25c6
// for private, \u25cf for dm/group_dm. The channel name itself
// follows the glyph verbatim.
func TestHeaderGlyph_ByChannelType(t *testing.T) {
	cases := []struct {
		chType    string
		wantGlyph string
	}{
		{"channel", "#"},
		{"", "#"}, // unspecified defaults to #
		{"private", "\u25c6"},
		{"dm", "\u25cf"},
		{"group_dm", "\u25cf"},
	}
	for _, tc := range cases {
		t.Run(tc.chType, func(t *testing.T) {
			m := New(nil, "general")
			m.SetChannel("Grant, Myles, Ray", "")
			m.SetChannelType(tc.chType)
			out := m.View(20, 60)
			// View output is ANSI-styled; just look for the glyph + space + name.
			want := tc.wantGlyph + " Grant, Myles, Ray"
			if !strings.Contains(out, want) {
				t.Errorf("type=%q: expected %q in header, got:\n%s", tc.chType, want, out)
			}
		})
	}
}

// TestAppendMessage_AlwaysScrollsToBottom asserts that an incoming
// message scrolls the view to the bottom even when the user has
// scrolled up (selection is not at the last index). This matches
// chat-client expectations: new messages should always be visible.
func TestAppendMessage_AlwaysScrollsToBottom(t *testing.T) {
	msgs := make([]MessageItem, 5)
	for i := range msgs {
		msgs[i] = MessageItem{
			TS:        "1.0",
			UserName:  "alice",
			Text:      "old",
			Timestamp: "10:00 AM",
		}
	}
	m := New(msgs, "general")

	// Move selection up so we're explicitly NOT at the bottom.
	m.MoveUp()
	m.MoveUp()
	if m.SelectedIndex() == len(msgs)-1 {
		t.Fatalf("test setup: expected selection above bottom, got %d", m.SelectedIndex())
	}

	m.AppendMessage(MessageItem{TS: "2.0", UserName: "bob", Text: "incoming", Timestamp: "10:01 AM"})

	wantIdx := len(m.Messages()) - 1
	if got := m.SelectedIndex(); got != wantIdx {
		t.Errorf("AppendMessage should scroll to bottom: SelectedIndex=%d want=%d", got, wantIdx)
	}
	if !m.IsAtBottom() {
		t.Error("AppendMessage should leave model IsAtBottom() == true")
	}
}

// TestScrollPreservedAcrossRenders asserts that mouse-wheel-style scrolling
// (ScrollUp / ScrollDown) is not undone by the next View() call. Without the
// snap-decoupling logic, every render would pull yOffset back to the line
// containing the selected message.
func TestScrollPreservedAcrossRenders(t *testing.T) {
	msgs := make([]MessageItem, 60)
	for i := range msgs {
		msgs[i] = MessageItem{
			TS:        "1.0",
			UserName:  "alice",
			Text:      "msg body",
			Timestamp: "10:00 AM",
		}
	}
	m := New(msgs, "general")
	// Render once so selection is snapped to bottom, then scroll up.
	_ = m.View(20, 80)
	startOffset := m.yOffset
	m.ScrollUp(10)
	scrolled := m.yOffset
	if scrolled >= startOffset {
		t.Fatalf("ScrollUp did not decrease yOffset: before=%d after=%d", startOffset, scrolled)
	}

	// Render again. The viewport scroll position (yOffset) must NOT snap back.
	// The cursor follows the scroll and clamps to the bottommost visible
	// message, but the scroll position itself is preserved.
	_ = m.View(20, 80)
	if m.yOffset != scrolled {
		t.Errorf("yOffset snapped back after render: want %d, got %d", scrolled, m.yOffset)
	}

	// Moving selection DOWN past the bottom edge of the (scrolled) viewport
	// should re-snap the viewport down to keep the new selection visible.
	m.MoveDown()
	_ = m.View(20, 80)
	if m.yOffset <= scrolled {
		t.Errorf("expected yOffset to re-snap down after moving selection below the fold: scrolled=%d got=%d", scrolled, m.yOffset)
	}
}

// selectionVisible reports whether the currently selected message's line range
// (as computed by the most recent View()) has at least one line inside the
// visible viewport window.
func selectionVisible(m *Model) bool {
	top := m.yOffset
	bottom := m.yOffset + m.lastViewHeight
	return m.selectedStartLine < bottom && m.selectedEndLine > top
}

// TestCursorFollowsScrollUp asserts that scrolling the viewport up far enough
// to push the selected message off the bottom edge drags the cursor with it,
// clamping selection to the bottommost still-visible message.
func TestCursorFollowsScrollUp(t *testing.T) {
	msgs := make([]MessageItem, 60)
	for i := range msgs {
		msgs[i] = MessageItem{TS: "1.0", UserName: "alice", Text: "msg body", Timestamp: "10:00 AM"}
	}
	m := New(msgs, "general")
	_ = m.View(20, 80)
	startSel := m.SelectedIndex() // bottom
	if startSel != len(msgs)-1 {
		t.Fatalf("setup: expected selection at bottom %d, got %d", len(msgs)-1, startSel)
	}

	m.ScrollUp(10)
	_ = m.View(20, 80)

	if m.SelectedIndex() >= startSel {
		t.Errorf("cursor did not follow scroll up: before=%d after=%d", startSel, m.SelectedIndex())
	}
	if !selectionVisible(&m) {
		t.Errorf("selection not visible after scroll up: sel=[%d,%d) window=[%d,%d)",
			m.selectedStartLine, m.selectedEndLine, m.yOffset, m.yOffset+m.lastViewHeight)
	}
}

// TestCursorFollowsScrollDown asserts that scrolling the viewport down far
// enough to push the selected message off the top edge drags the cursor with
// it, clamping selection to the topmost still-visible message.
func TestCursorFollowsScrollDown(t *testing.T) {
	msgs := make([]MessageItem, 60)
	for i := range msgs {
		msgs[i] = MessageItem{TS: "1.0", UserName: "alice", Text: "msg body", Timestamp: "10:00 AM"}
	}
	m := New(msgs, "general")
	m.GoToTop()
	_ = m.View(20, 80)
	startSel := m.SelectedIndex() // top
	if startSel != 0 {
		t.Fatalf("setup: expected selection at top 0, got %d", startSel)
	}

	m.ScrollDown(20)
	_ = m.View(20, 80)

	if m.SelectedIndex() <= startSel {
		t.Errorf("cursor did not follow scroll down: before=%d after=%d", startSel, m.SelectedIndex())
	}
	if !selectionVisible(&m) {
		t.Errorf("selection not visible after scroll down: sel=[%d,%d) window=[%d,%d)",
			m.selectedStartLine, m.selectedEndLine, m.yOffset, m.yOffset+m.lastViewHeight)
	}
}

// TestCursorUnchangedWhenScrollKeepsSelectionVisible asserts that a small
// scroll that leaves the selected message on-screen does not move the cursor.
func TestCursorUnchangedWhenScrollKeepsSelectionVisible(t *testing.T) {
	msgs := make([]MessageItem, 60)
	for i := range msgs {
		msgs[i] = MessageItem{TS: "1.0", UserName: "alice", Text: "msg body", Timestamp: "10:00 AM"}
	}
	m := New(msgs, "general")
	_ = m.View(20, 80)
	startSel := m.SelectedIndex()

	// Scroll up by a single line; the selected (bottom) message should still
	// have lines on-screen, so the cursor must not move.
	m.ScrollUp(1)
	_ = m.View(20, 80)

	if m.SelectedIndex() != startSel {
		t.Errorf("cursor moved on a scroll that kept selection visible: before=%d after=%d",
			startSel, m.SelectedIndex())
	}
	if !selectionVisible(&m) {
		t.Errorf("selection unexpectedly off-screen after 1-line scroll")
	}
}

func TestUpdateMessageInPlace_Found(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserName: "alice", Text: "old"},
		{TS: "2.0", UserName: "bob", Text: "hello"},
	}
	m := New(msgs, "general")
	got := m.UpdateMessageInPlace("2.0", "hello edited")
	if !got {
		t.Fatalf("expected UpdateMessageInPlace to return true for existing TS")
	}
	all := m.messages
	if all[1].Text != "hello edited" {
		t.Errorf("text not updated: %q", all[1].Text)
	}
	if !all[1].IsEdited {
		t.Error("IsEdited not set")
	}
	if all[0].Text != "old" {
		t.Error("other messages should be untouched")
	}
}

func TestUpdateMessageInPlace_NotFound(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", Text: "a"}}, "general")
	got := m.UpdateMessageInPlace("does-not-exist", "x")
	if got {
		t.Error("expected false when TS missing")
	}
}

func TestRemoveMessageByTS_Middle(t *testing.T) {
	m := New([]MessageItem{
		{TS: "1.0", Text: "a"},
		{TS: "2.0", Text: "b"},
		{TS: "3.0", Text: "c"},
	}, "general")
	// Selection starts at bottom (index 2 = "c").
	got := m.RemoveMessageByTS("2.0")
	if !got {
		t.Fatal("expected true")
	}
	all := m.messages
	if len(all) != 2 || all[0].TS != "1.0" || all[1].TS != "3.0" {
		t.Errorf("unexpected messages after remove: %+v", all)
	}
	// Removed index 1 was <= selected (2) → selected decrements to 1.
	if m.SelectedIndex() != 1 {
		t.Errorf("expected selected=1 after removing earlier message, got %d", m.SelectedIndex())
	}
}

func TestRemoveMessageByTS_RemovesSelected(t *testing.T) {
	m := New([]MessageItem{
		{TS: "1.0", Text: "a"},
		{TS: "2.0", Text: "b"},
		{TS: "3.0", Text: "c"},
	}, "general")
	// Selection starts at index 2; remove TS "3.0" (the selected one).
	got := m.RemoveMessageByTS("3.0")
	if !got {
		t.Fatal("expected true")
	}
	if m.SelectedIndex() != 1 {
		t.Errorf("expected selected clamped to 1, got %d", m.SelectedIndex())
	}
}

func TestRemoveMessageByTS_NotFound(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", Text: "a"}}, "general")
	if m.RemoveMessageByTS("nope") {
		t.Error("expected false when TS missing")
	}
	if len(m.messages) != 1 {
		t.Error("messages should be unchanged when TS missing")
	}
}

func TestRemoveMessageByTS_LastBecomesEmpty(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", Text: "a"}}, "general")
	if !m.RemoveMessageByTS("1.0") {
		t.Fatal("expected true")
	}
	if len(m.messages) != 0 {
		t.Error("expected empty after removing last")
	}
	if _, ok := m.SelectedMessage(); ok {
		t.Error("SelectedMessage should be (_, false) when empty")
	}
}

// makeTestPNG synthesizes a w×h RGBA PNG. Used as fixture bytes
// for the inline-image cache so tests don't need network access.
func makeTestPNG(w, h int) []byte {
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			src.Set(x, y, imgcolor.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := imgpng.Encode(&buf, src); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// TestImageReady_DoesNotChangeMessageHeight is the headline behavioral
// guarantee of the inline-image pipeline: the user's scroll position
// must not jump when an image transitions from the loading placeholder
// to the rendered bytes. The placeholder block reserves exactly the
// same number of rows as the eventual image, so the cached viewEntry
// height for the message must be identical across the two renders.
//
// The test injects the image bytes directly into the on-disk cache (no
// HTTP), then simulates the goroutine completion via HandleImageReady
// (which is what App.Update wires to ImageReadyMsg in Phase 5.6).
func TestImageReady_DoesNotChangeMessageHeight(t *testing.T) {
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)

	const channel = "C123"
	const ts = "1700000000.000100"
	const fileID = "F0123ABCD"

	msg := MessageItem{
		TS:        ts,
		UserID:    "U1",
		UserName:  "alice",
		Text:      "look at this",
		Timestamp: "10:30 AM",
		Attachments: []Attachment{{
			Kind:   "image",
			Name:   "screenshot.png",
			URL:    "https://example.com/perma",
			FileID: fileID,
			Mime:   "image/png",
			Thumbs: []ThumbSpec{{URL: "https://example.com/720.png", W: 720, H: 720}},
		}},
	}

	m := New([]MessageItem{msg}, channel)
	m.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoHalfBlock,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
		// SendMsg deliberately nil: we drive the "image arrived"
		// transition synchronously via HandleImageReady below
		// rather than relying on the prefetcher goroutine.
		SendMsg: nil,
	})

	const width = 80
	const height = 30

	// First render: cache is empty → placeholder is emitted at the
	// reserved size. (A fetch goroutine is spawned but its result
	// races with the second render; we don't depend on it.)
	_ = m.View(height, width)

	heightBefore := -1
	for _, e := range m.cache {
		if e.msgIdx == 0 {
			heightBefore = e.height
			break
		}
	}
	if heightBefore < 0 {
		t.Fatal("could not find message entry in cache after first render")
	}

	// Inject the image bytes directly. Key format from
	// renderAttachmentBlock is "<FileID>-<suffix>" where suffix is
	// max(thumb.W, thumb.H) of the picked thumb. PickThumb chooses
	// the smallest thumb satisfying the pixel target; for a single
	// 720×720 thumb that's always the one picked, with suffix "720".
	pngBytes := makeTestPNG(720, 720)
	if _, err := cache.Put(fileID+"-720", "png", pngBytes); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}

	// Simulate the prefetcher goroutine completion. This is what
	// App.Update calls when ImageReadyMsg lands.
	m.HandleImageReady(channel, ts, "")

	// Second render: bytes are now cached → real image render.
	_ = m.View(height, width)

	heightAfter := -1
	for _, e := range m.cache {
		if e.msgIdx == 0 {
			heightAfter = e.height
			break
		}
	}
	if heightAfter < 0 {
		t.Fatal("could not find message entry in cache after second render")
	}

	if heightAfter != heightBefore {
		t.Errorf("message height changed across image load: before=%d after=%d (placeholder must reserve exactly the rendered image's height)", heightBefore, heightAfter)
	}
}

// setupImageMessageModel builds a Model with a single image-bearing
// message whose bytes are pre-staged in the on-disk cache AND the
// in-memory decoded memo. The memo prime is required because
// Fetcher.Cached is now memo-only (it no longer falls back to disk
// decode -- see fetcher.go:Cached docstring); without priming the
// memo, the first View() would render the loading placeholder
// instead of the actual image and the byte-presence assertions
// downstream would fail.
//
// Memo prime is done via a synchronous Fetch call: Fetch's
// disk-cache check (fetcher.go:226) sees the byte we just put,
// skips the HTTP semaphore, decodes + downscales + memoizes. The
// URL is unused on the disk-hit path so any placeholder URL works.
//
// pixelTarget is derived from the same computation the renderer
// will use at View(60, 80) given the test's static parameters
// (CellPixels=(8,16), MaxRows=20, no MaxCols, single 720x720 thumb,
// contentWidth = 80 - 4 = 76). computeImageTarget yields
// (40, 20) cells -> (320, 320) pixels for a square thumb.
func setupImageMessageModel(t *testing.T, protocol imgpkg.Protocol) *Model {
	t.Helper()
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	const fileID = "F0123ABCD"
	const key = fileID + "-720"
	pngBytes := makeTestPNG(720, 720)
	if _, err := cache.Put(key, "png", pngBytes); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	// Prime the in-memory decoded memo via Fetch. Fetch's singleflight
	// + disk-cache check skips the network when the bytes are already
	// on disk; we just need the decoded image to live in fetcher.decoded
	// so the renderer's Cached() lookup hits.
	if _, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:    key,
		URL:    "unused://disk-cache-hits-skip-network",
		Target: stdimage.Pt(320, 320),
	}); err != nil {
		t.Fatalf("Fetch (memo prime): %v", err)
	}
	msg := MessageItem{
		TS:        "1700000000.000100",
		UserID:    "U1",
		UserName:  "alice",
		Text:      "look at this",
		Timestamp: "10:30 AM",
		Attachments: []Attachment{{
			Kind:   "image",
			Name:   "screenshot.png",
			URL:    "https://example.com/perma",
			FileID: fileID,
			Mime:   "image/png",
			Thumbs: []ThumbSpec{{URL: "https://example.com/720.png", W: 720, H: 720}},
		}},
	}
	m := New([]MessageItem{msg}, "C123")
	ctx := imgrender.ImageContext{
		Protocol:   protocol,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	}
	if protocol == imgpkg.ProtoKitty {
		ctx.KittyRender = imgpkg.NewKittyRenderer(imgpkg.NewRegistry())
	}
	m.SetImageContext(ctx)
	return &m
}

// TestView_SixelFullVisibility_PublishesPlacement asserts that a fully
// visible sixel image is published as a placement rather than embedded in
// the frame string. Bytes in the frame would have to be re-emitted every
// render (bubbletea rewrites a line whenever its content changes) and
// could never be erased; the App paints placements out-of-band instead.
func TestView_SixelFullVisibility_PublishesPlacement(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoSixel)
	// A tall viewport ensures the entire image (~20 rows + chrome + body)
	// fits without clipping.
	out := m.View(60, 80)
	if strings.Contains(out, "\x1bP") {
		t.Error("sixel bytes leaked into the frame string; they must be painted out-of-band")
	}
	placements := m.SixelPlacements()
	if len(placements) != 1 {
		t.Fatalf("SixelPlacements() = %d, want 1 for a fully visible image", len(placements))
	}
	p := placements[0]
	if len(p.Bytes) == 0 || !strings.HasPrefix(string(p.Bytes), "\x1bP") {
		t.Error("placement carries no sixel DCS payload")
	}
	if p.Rows <= 0 || p.Cols <= 0 {
		t.Errorf("placement footprint = %dx%d, want a positive erase region", p.Cols, p.Rows)
	}
	if p.Key == "" {
		t.Error("placement has no identity key; the painter cannot detect changes")
	}
}

// The published placement's Col must be the message content column
// (contentColBase), not 0: with Col 0 the painter places the image at
// the pane's content-left edge, shifting it left under the message
// border / avatar and away from its placeholder.
func TestView_SixelPlacementColumnMatchesContentColBase(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoSixel)
	_ = m.View(80, 60)
	placements := m.SixelPlacements()
	if len(placements) != 1 {
		t.Fatalf("SixelPlacements() = %d, want 1", len(placements))
	}
	if placements[0].Col != 1 {
		t.Fatalf("placement Col = %d, want 1 (contentColBase without avatar)", placements[0].Col)
	}
}

// With an avatar the message content moves 5 columns right, so the
// placement column must shift with it.
func TestView_SixelPlacementColumnWithAvatar(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoSixel)
	m.SetAvatarFunc(func(string) string { return "▀▀▀▀\n▄▄▄▄" })
	_ = m.View(80, 60)
	placements := m.SixelPlacements()
	if len(placements) != 1 {
		t.Fatalf("SixelPlacements() = %d, want 1", len(placements))
	}
	if placements[0].Col != 6 {
		t.Fatalf("placement Col = %d, want 6 (contentColBase with avatar)", placements[0].Col)
	}
}

// TestView_SixelPartialVisibility_UsesFallback asserts that when the
// sixel image straddles the bottom edge of the viewport (some rows
// clipped), View() does NOT emit the sixel byte stream — it must
// substitute the per-row halfblock fallback for the visible rows of
// the image. Sixel terminals can't render a partial image; emitting
// the bytes anyway would push pixels below the pane.
func TestView_SixelPartialVisibility_UsesFallback(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoSixel)
	// First, render with enough room to know how tall the entry is and
	// where the image starts. Then choose a viewport height that cuts
	// the image in the middle.
	_ = m.View(60, 80)
	var entryHeight int
	for _, e := range m.cache {
		if e.msgIdx == 0 {
			entryHeight = e.height
			break
		}
	}
	if entryHeight == 0 {
		t.Fatal("could not measure entry height")
	}
	// Halve the height so the image is clipped at the bottom.
	clipped := m.View(entryHeight/2+m.chromeHeight, 80)
	if strings.Contains(clipped, "\x1bP") {
		t.Errorf("expected View() output to OMIT the sixel DCS escape under partial visibility; bytes leaked anyway")
	}
	if n := len(m.SixelPlacements()); n != 0 {
		t.Errorf("SixelPlacements() = %d under partial visibility, want 0 (halfblock fallback covers it)", n)
	}
}

// TestView_KittyEmitsUploadEscape asserts that when a kitty-rendered
// image is in the visible window, View() emits the kitty
// transmit-and-display escape (begins with "\x1b_G") via the captured
// per-frame flushes. The kitty registry's first-render-of-id contract
// guarantees this happens exactly once per image per frame.
func TestView_KittyEmitsUploadEscape(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoKitty)
	// Capture the kitty side-channel output. The upload escape is
	// written directly to imgpkg.KittyOutput (not embedded in View()'s
	// return string) because bubbletea/lipgloss strip APC sequences.
	saved := imgpkg.KittyOutput
	defer func() { imgpkg.KittyOutput = saved }()
	var buf bytes.Buffer
	imgpkg.KittyOutput = &buf

	_ = m.View(60, 80)
	if !strings.Contains(buf.String(), "\x1b_G") {
		t.Errorf("expected kitty side-channel to receive a graphics escape (\\x1b_G) for the visible image's upload; got %d bytes without it", buf.Len())
	}
}

// TestHitTest_OnImageRegion asserts that View() captures a click-
// detection hit rect for each inline image attachment and that the
// public HitTest method returns the correct (msgIdx, attIdx, fileID)
// triple for coordinates inside the rect — and ok=false elsewhere.
//
// We pre-stage the image bytes via setupImageMessageModel so the
// FIRST View() takes the rendered (non-placeholder) path. The
// expected fileID is the same fixture ID setupImageMessageModel
// hard-codes ("F0123ABCD").
func TestHitTest_OnImageRegion(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoHalfBlock)

	// Drive a render to populate m.lastHits.
	_ = m.View(60, 80)

	if len(m.lastHits) == 0 {
		t.Fatal("expected at least one hit rect after View() with a cached image attachment")
	}

	h := m.lastHits[0]

	// Sanity: the rect must be non-empty in both dimensions.
	if h.rowEnd <= h.rowStart || h.colEnd <= h.colStart {
		t.Fatalf("hit rect is degenerate: rows=[%d,%d) cols=[%d,%d)", h.rowStart, h.rowEnd, h.colStart, h.colEnd)
	}

	// Hit-test the center of the image footprint. We expect the
	// fixture's fileID and (msgIdx=0, attIdx=0) since the test
	// model has exactly one message with exactly one attachment.
	rowMid := (h.rowStart + h.rowEnd) / 2
	colMid := (h.colStart + h.colEnd) / 2
	msgIdx, attIdx, fileID, ok := m.HitTest(rowMid, colMid)
	if !ok {
		t.Fatalf("HitTest(%d,%d) returned ok=false for a coordinate inside the recorded hit rect", rowMid, colMid)
	}
	if fileID != "F0123ABCD" {
		t.Errorf("HitTest fileID got %q want %q", fileID, "F0123ABCD")
	}
	if msgIdx != 0 || attIdx != 0 {
		t.Errorf("HitTest got (msgIdx=%d, attIdx=%d), want (0, 0)", msgIdx, attIdx)
	}

	// A coordinate to the LEFT of the image (column 0 — landing on
	// the thick left border) must not register as a hit: the border
	// is chrome, not part of the image footprint.
	if _, _, _, ok := m.HitTest(rowMid, 0); ok {
		t.Error("HitTest(_, 0) should not hit (column 0 is the thick-left-border)")
	}

	// A coordinate ABOVE the image (row 0 — username/timestamp row,
	// which precedes the image inside the message body) must not
	// register either.
	if _, _, _, ok := m.HitTest(0, colMid); ok {
		t.Error("HitTest(0, _) should not hit (row 0 is above the image footprint)")
	}

	// A coordinate just past the right edge must not register.
	if _, _, _, ok := m.HitTest(rowMid, h.colEnd); ok {
		t.Errorf("HitTest at colEnd=%d (exclusive boundary) should not hit", h.colEnd)
	}
}

// TestHitTest_NoHitsBeforeView guards against the trivial bug of a
// stale hit slice surviving across a model with no rendered images.
// A fresh Model that has never been View()'d must return ok=false
// for any coordinate.
func TestHitTest_NoHitsBeforeView(t *testing.T) {
	m := New(nil, "C123")
	if _, _, _, ok := m.HitTest(0, 0); ok {
		t.Error("HitTest on a never-rendered Model should return ok=false")
	}
}

// TestModel_HandleImageReady_PerEntryInvalidation asserts that when
// an ImageReadyMsg lands for a single message, the model invalidates
// only that message's cached row -- sibling entries must survive
// untouched. This is the perf invariant that prevents N back-to-back
// full-cache walks when N images arrive in a burst (channel switch
// into unseen history; scroll-up to a region with many attachments).
//
// Today's HandleImageReady sets m.cache = nil and forces buildCache
// to walk every message in the channel on the next View(). The test
// captures a sibling entry's rendered lines BEFORE the call and
// asserts they are still present (and identical) AFTER.
func TestModel_HandleImageReady_PerEntryInvalidation(t *testing.T) {
	const channel = "C-perf"
	msgs := []MessageItem{
		{TS: "1700000001.000000", UserID: "U1", UserName: "alice", Text: "first", Timestamp: "10:30 AM"},
		{TS: "1700000002.000000", UserID: "U2", UserName: "bob", Text: "second (target)", Timestamp: "10:31 AM"},
		{TS: "1700000003.000000", UserID: "U3", UserName: "carol", Text: "third", Timestamp: "10:32 AM"},
	}
	m := New(msgs, channel)

	// Drive a render to populate the cache.
	_ = m.View(20, 60)
	if m.cache == nil {
		t.Fatal("expected cache populated after View()")
	}

	// Snapshot the sibling entries (indices for the first and third
	// messages) so we can compare them after the invalidation.
	firstIdx, ok := m.messageIDToEntryIdx["1700000001.000000"]
	if !ok {
		t.Fatal("could not find first message entry index")
	}
	thirdIdx, ok := m.messageIDToEntryIdx["1700000003.000000"]
	if !ok {
		t.Fatal("could not find third message entry index")
	}
	firstBefore := append([]string(nil), m.cache[firstIdx].linesNormal...)
	thirdBefore := append([]string(nil), m.cache[thirdIdx].linesNormal...)
	firstHeightBefore := m.cache[firstIdx].height
	thirdHeightBefore := m.cache[thirdIdx].height

	// Simulate an image arriving for the SECOND message only. The
	// non-empty key is what the prefetcher dispatches in production
	// (legacy "" key still nils the whole cache for safety; this
	// test exercises the fast path).
	m.HandleImageReady(channel, "1700000002.000000", "F222-720")

	if m.cache == nil {
		t.Fatal("HandleImageReady with non-empty key should not nil the entire cache; sibling rebuilds defeat the perf optimization")
	}

	// Drive a second render so any pending partial rebuild lands.
	// Sibling entries must be present and identical after the render.
	_ = m.View(20, 60)

	firstIdxAfter, ok := m.messageIDToEntryIdx["1700000001.000000"]
	if !ok {
		t.Fatal("first message entry missing from index after HandleImageReady")
	}
	thirdIdxAfter, ok := m.messageIDToEntryIdx["1700000003.000000"]
	if !ok {
		t.Fatal("third message entry missing from index after HandleImageReady")
	}

	if got := m.cache[firstIdxAfter].height; got != firstHeightBefore {
		t.Errorf("sibling (first) entry height changed: before=%d after=%d", firstHeightBefore, got)
	}
	if got := m.cache[thirdIdxAfter].height; got != thirdHeightBefore {
		t.Errorf("sibling (third) entry height changed: before=%d after=%d", thirdHeightBefore, got)
	}

	if !equalLines(firstBefore, m.cache[firstIdxAfter].linesNormal) {
		t.Errorf("sibling (first) entry linesNormal changed across HandleImageReady -- sibling was rebuilt unnecessarily")
	}
	if !equalLines(thirdBefore, m.cache[thirdIdxAfter].linesNormal) {
		t.Errorf("sibling (third) entry linesNormal changed across HandleImageReady -- sibling was rebuilt unnecessarily")
	}
}

// TestModel_HandleAvatarReady_PerUserStaleInvalidation asserts that
// when an AvatarReadyMsg lands for a userID, the model marks ONLY the
// cache slots authored by that user as stale -- siblings (entries from
// other users) must survive untouched. This is the perf invariant that
// prevents N back-to-back full-cache rebuilds when a busy channel's
// scrollback brings in N unique authors and their avatars arrive in a
// burst over many bubbletea ticks.
//
// Before the fix HandleAvatarReady did m.cache = nil, forcing
// buildCache to walk every message and re-run the markdown / wordwrap
// / blockkit pipeline. Now it mirrors HandleImageReady's per-TS stale
// path: only the affected messages rebuild on the next View(), via
// partialRebuild.
func TestModel_HandleAvatarReady_PerUserStaleInvalidation(t *testing.T) {
	const channel = "C-avatar-perf"
	msgs := []MessageItem{
		{TS: "1700000001.000000", UserID: "U1", UserName: "alice", Text: "first by alice", Timestamp: "10:30 AM"},
		{TS: "1700000002.000000", UserID: "U2", UserName: "bob", Text: "by bob (target)", Timestamp: "10:31 AM"},
		{TS: "1700000003.000000", UserID: "U3", UserName: "carol", Text: "by carol", Timestamp: "10:32 AM"},
		{TS: "1700000004.000000", UserID: "U2", UserName: "bob", Text: "second by bob (target)", Timestamp: "10:33 AM"},
	}
	m := New(msgs, channel)
	_ = m.View(20, 60)
	if m.cache == nil {
		t.Fatal("expected cache populated after View()")
	}

	aliceIdx := m.messageIDToEntryIdx["1700000001.000000"]
	carolIdx := m.messageIDToEntryIdx["1700000003.000000"]
	aliceBefore := append([]string(nil), m.cache[aliceIdx].linesNormal...)
	carolBefore := append([]string(nil), m.cache[carolIdx].linesNormal...)

	m.HandleAvatarReady("U2")

	if m.cache == nil {
		t.Fatal("HandleAvatarReady must not nil the entire cache; siblings get rebuilt unnecessarily")
	}
	if _, ok := m.staleEntries["1700000002.000000"]; !ok {
		t.Error("expected bob's first message marked stale")
	}
	if _, ok := m.staleEntries["1700000004.000000"]; !ok {
		t.Error("expected bob's second message marked stale")
	}
	if _, ok := m.staleEntries["1700000001.000000"]; ok {
		t.Error("alice's message must NOT be marked stale (different author)")
	}
	if _, ok := m.staleEntries["1700000003.000000"]; ok {
		t.Error("carol's message must NOT be marked stale (different author)")
	}

	// Drive the partial rebuild and confirm siblings are byte-identical.
	_ = m.View(20, 60)

	if !equalLines(aliceBefore, m.cache[m.messageIDToEntryIdx["1700000001.000000"]].linesNormal) {
		t.Error("alice's cached lines changed across HandleAvatarReady -- sibling rebuilt unnecessarily")
	}
	if !equalLines(carolBefore, m.cache[m.messageIDToEntryIdx["1700000003.000000"]].linesNormal) {
		t.Error("carol's cached lines changed across HandleAvatarReady -- sibling rebuilt unnecessarily")
	}
}

// TestModel_HandleAvatarReady_UnknownUserIDNoOp covers the case where
// AvatarReadyMsg arrives for a user whose messages aren't loaded in
// the active channel (e.g. they're in another channel's history that
// happens to share the workspace). Must not dirty the cache.
func TestModel_HandleAvatarReady_UnknownUserIDNoOp(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1700000001.000000", UserID: "U1", UserName: "alice", Text: "hi", Timestamp: "10:30 AM"},
	}
	m := New(msgs, "C")
	_ = m.View(20, 60)

	beforeVersion := m.version
	m.HandleAvatarReady("U_NOT_IN_CHANNEL")

	if len(m.staleEntries) != 0 {
		t.Errorf("staleEntries should remain empty; got %d", len(m.staleEntries))
	}
	if m.version != beforeVersion {
		t.Error("AvatarReady for non-present user should not dirty the version")
	}
}

// TestModel_HandleAvatarReady_EmptyUserIDNoOp guards the no-op path so
// a stray empty event from the host wiring doesn't pointlessly dirty
// the cache.
func TestModel_HandleAvatarReady_EmptyUserIDNoOp(t *testing.T) {
	m := New([]MessageItem{{TS: "1", UserID: "U1", UserName: "a", Text: "hi"}}, "C")
	_ = m.View(20, 60)
	beforeVersion := m.version
	m.HandleAvatarReady("")
	if m.version != beforeVersion {
		t.Error("HandleAvatarReady(\"\") should not dirty the version")
	}
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestUpsertSelfSent_ReplacesRacingWSEcho asserts that when the WS
// echo of our own message races ahead of the HTTP response and
// AppendMessage stores the WS-echo MessageItem first, calling
// UpsertSelfSent with the optimistic version replaces the existing
// entry's content. This ensures the converted-mrkdwn text from
// internal/slack/mrkdwn always wins for self-sent messages.
func TestUpsertSelfSent_ReplacesRacingWSEcho(t *testing.T) {
	m := New(nil, "general")

	// Simulate WS echo arriving first with text Slack normalized.
	wsEcho := MessageItem{
		TS:        "1.0",
		UserID:    "U1",
		UserName:  "grant",
		Text:      "Hello World",
		Timestamp: "12:21 PM",
	}
	m.AppendMessage(wsEcho)
	if got := m.Messages()[0].Text; got != "Hello World" {
		t.Fatalf("setup: WS-echo text not stored, got %q", got)
	}

	// Now the optimistic MessageSentMsg arrives with our converted mrkdwn.
	optimistic := MessageItem{
		TS:        "1.0",
		UserID:    "U1",
		UserName:  "grant",
		Text:      "Hello\nWorld",
		Timestamp: "12:21 PM",
	}
	m.UpsertSelfSent(optimistic)

	if n := len(m.Messages()); n != 1 {
		t.Fatalf("got %d messages, want 1 (upsert should not duplicate)", n)
	}
	if got := m.Messages()[0].Text; got != "Hello\nWorld" {
		t.Errorf("Text = %q, want %q (optimistic should replace WS-echo)", got, "Hello\nWorld")
	}
}

// TestUpsertSelfSent_AppendsWhenNoExisting asserts that when no
// message with the given TS exists yet (the common case where the
// HTTP response arrives before the WS echo), UpsertSelfSent simply
// appends the message.
func TestUpsertSelfSent_AppendsWhenNoExisting(t *testing.T) {
	m := New(nil, "general")
	msg := MessageItem{
		TS:        "1.0",
		UserID:    "U1",
		UserName:  "grant",
		Text:      "Hello\nWorld",
		Timestamp: "12:21 PM",
	}
	m.UpsertSelfSent(msg)

	if n := len(m.Messages()); n != 1 {
		t.Fatalf("got %d messages, want 1", n)
	}
	if got := m.Messages()[0].Text; got != "Hello\nWorld" {
		t.Errorf("Text = %q", got)
	}
	// Selection should follow the new message.
	if got := m.SelectedIndex(); got != 0 {
		t.Errorf("SelectedIndex = %d, want 0", got)
	}
}

// TestUpsertSelfSent_ReplaceTriggersResnap asserts that replacing an
// existing message via UpsertSelfSent invalidates the snap-to-selected
// anchor so View() re-pins yOffset to the now-larger entry. Without
// this, the optimistic version's expanded body (e.g. multi-line list)
// extends below the fold and the user has to scroll manually.
func TestUpsertSelfSent_ReplaceTriggersResnap(t *testing.T) {
	m := New(nil, "general")

	// First call: append a 1-line WS-echo placeholder.
	m.AppendMessage(MessageItem{TS: "1.0", UserName: "grant", Text: "Hello World", Timestamp: "12:21 PM"})

	// Render once to establish snap state.
	_ = m.View(20, 80)
	if !m.hasSnapped {
		t.Fatal("setup: expected hasSnapped=true after first View")
	}

	// Now upsert with the optimistic, taller body. The snap anchor
	// should be invalidated so the next View re-pins to the bottom.
	m.UpsertSelfSent(MessageItem{TS: "1.0", UserName: "grant", Text: "Hello\nWorld\nMore\nLines", Timestamp: "12:21 PM"})
	if m.hasSnapped {
		t.Errorf("UpsertSelfSent replace should clear hasSnapped; got hasSnapped=true")
	}
}

// itoaU8 / fmtRGBBg are local helpers used by the tint-background tests.
// They build the SGR fragment lipgloss/v2 emits for an RGB background
// ("48;2;R;G;B"), so the test can substring-match against rendered
// output without depending on terminal dimensions or layout.
func itoaU8(v uint8) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func fmtRGBBg(r, g, b uint8) string {
	return "48;2;" + itoaU8(r) + ";" + itoaU8(g) + ";" + itoaU8(b)
}

// TestSelectedRowContainsTintBackground asserts that the rendered output
// for the selected message includes the SelectionTintColor as an ANSI
// background, while a non-selected row does not. Guards against
// regressions in cacheStyles' borderSelect construction.
func TestSelectedRowContainsTintBackground(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	msgs := []MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "first message"},
		{TS: "2.0", UserID: "U2", UserName: "bob", Text: "second message"},
	}
	m := New(msgs, "general")
	m.SetFocused(true)
	// Selection starts at the newest message (index 1) so we don't
	// even need MoveDown — but call it explicitly to be deterministic.
	if m.SelectedIndex() != 1 {
		t.Fatalf("expected initial selection at last message, got %d", m.SelectedIndex())
	}

	out := m.View(20 /*height*/, 60 /*width*/)

	r, g, b, _ := styles.SelectionTintColor(true).RGBA()
	want := fmtRGBBg(uint8(r>>8), uint8(g>>8), uint8(b>>8))
	if !strings.Contains(out, want) {
		t.Fatalf("expected selected row to contain tint bg %q\nout=%q", want, out)
	}
}

// TestEmptyStateRendersSpinnerWhenLoading asserts the empty-state
// branch in View() prefixes the "Loading messages..." text with the
// current braille spinner glyph (frame 0 = "⠋"), matching the
// workspace overlay spinner for visual consistency.
func TestEmptyStateRendersSpinnerWhenLoading(t *testing.T) {
	m := New(nil, "general")
	m.SetLoading(true)
	m.SetSpinnerFrame(0)
	out := m.View(20 /*height*/, 80 /*width*/)
	want := string(styles.SpinnerChars[0]) + " Loading messages..."
	if !strings.Contains(out, want) {
		t.Errorf("expected %q in empty-state output:\n%s", want, out)
	}
}

// TestEmptyStateRendersPlainTextWhenNotLoading asserts the empty-state
// branch falls back to the plain "No messages yet" string when the
// pane is not in loading state.
func TestEmptyStateRendersPlainTextWhenNotLoading(t *testing.T) {
	m := New(nil, "general")
	m.SetLoading(false)
	out := m.View(20 /*height*/, 80 /*width*/)
	if !strings.Contains(out, "No messages yet") {
		t.Errorf("expected 'No messages yet' fallback in output:\n%s", out)
	}
}

// TestLoadingOlderMessagesHintAnimatesAcrossFrames asserts that the
// "Loading older messages..." hint glyph reflects the CURRENT
// m.spinnerFrame on every View() call, not whatever frame was set
// when the cache was last built. Regression for: spinner glyph
// computed inside buildCache and stored on m.cacheLoadingHint, which
// only changes on cache-invalidating events (width/messages), so
// SetSpinnerFrame's m.dirty() call had no visible effect on the
// hint between cache rebuilds.
func TestLoadingOlderMessagesHintAnimatesAcrossFrames(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserName: "alice", UserID: "U1", Text: "hello", Timestamp: "1:00 PM"},
		{TS: "2.0", UserName: "bob", UserID: "U2", Text: "world", Timestamp: "1:01 PM"},
	}
	m := New(msgs, "general")
	m.SetLoading(true)
	// Build cache at frame 0 -- this would bake "⠋" into m.cacheLoadingHint
	// under the old behaviour.
	m.SetSpinnerFrame(0)
	_ = m.View(20 /*height*/, 60 /*width*/)
	// Now advance the spinner. Cache stays valid (width and message
	// count unchanged), so the next View() must compute the hint
	// glyph fresh from the current spinnerFrame, not reuse a cached
	// frame-0 glyph.
	m.SetSpinnerFrame(3)
	out := m.View(20, 60)
	wantGlyph := string(styles.SpinnerChars[3])
	want := wantGlyph + " Loading older messages..."
	if !strings.Contains(out, want) {
		t.Errorf("expected hint with frame-3 glyph %q in output; got:\n%s", want, out)
	}
	// Belt-and-suspenders: the frame-0 glyph must NOT be present in
	// a "Loading older messages..." hint. (It can legitimately appear
	// elsewhere in styled output, so we check the full hint string.)
	frame0Hint := string(styles.SpinnerChars[0]) + " Loading older messages..."
	if strings.Contains(out, frame0Hint) {
		t.Errorf("output still contains stale frame-0 hint %q:\n%s", frame0Hint, out)
	}
}

func TestPatchUserName_UpdatesMatchingRowsAndUserNamesMap(t *testing.T) {
	m := New([]MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "U1", Text: "hi"},
		{TS: "2.0", UserID: "U2", UserName: "alice", Text: "hey"},
		{TS: "3.0", UserID: "U1", UserName: "U1", Text: "again"},
	}, "general")

	verBefore := m.Version()

	m.PatchUserName("U1", "bob")

	if m.messages[0].UserName != "bob" {
		t.Errorf("msg[0].UserName = %q, want bob", m.messages[0].UserName)
	}
	if m.messages[1].UserName != "alice" {
		t.Errorf("msg[1].UserName should not have changed; got %q", m.messages[1].UserName)
	}
	if m.messages[2].UserName != "bob" {
		t.Errorf("msg[2].UserName = %q, want bob", m.messages[2].UserName)
	}
	if got := m.ResolveUserName("U1"); got != "bob" {
		t.Errorf("ResolveUserName(U1) = %q, want bob", got)
	}
	if m.Version() <= verBefore {
		t.Error("Version should bump after PatchUserName")
	}
}

func TestPatchUserName_NoOpWhenUnchanged(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", UserID: "U1", UserName: "bob"}}, "general")
	m.PatchUserName("U1", "bob") // prime the userNames map
	verBefore := m.Version()

	m.PatchUserName("U1", "bob") // second call, identical

	if m.Version() != verBefore {
		t.Error("Version should NOT bump on no-op PatchUserName")
	}
}

func TestPatchUserName_NoMatchingMessagesStillUpdatesMap(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0", UserID: "U1", UserName: "alice"}}, "general")

	m.PatchUserName("U99", "carol")

	if got := m.ResolveUserName("U99"); got != "carol" {
		t.Errorf("ResolveUserName(U99) = %q, want carol (mention map should update even with no message match)", got)
	}
}

func TestPatchUserName_InvalidatesCacheEvenWithNoMatchingMessages(t *testing.T) {
	// Renders happen with userNames consulted at render time; mention
	// text in other-authored messages goes stale when userNames
	// changes. PatchUserName must invalidate the cache even if no
	// MessageItem.UserID == userID.
	m := New([]MessageItem{
		{TS: "1.0", UserID: "alice", UserName: "alice", Text: "hello <@U99>"},
	}, "general")
	// Prime the render cache by calling View.
	_ = m.View(80, 10)
	if m.cache == nil {
		t.Fatal("expected cache populated after View(); harness assumption failed")
	}
	m.PatchUserName("U99", "carol")
	if m.cache != nil {
		t.Error("PatchUserName should have invalidated m.cache so the mention re-resolves")
	}
}

// TestHitTestReaction_OnPill asserts that View() captures a hit
// rect for each reaction pill on the rendered message and that
// HitTestReaction returns the correct (msgIdx, emoji) pair when a
// click lands inside the pill, and ok=false elsewhere.
//
// Reaction pills are rendered on a line below the message body (see
// renderMessagePlain). The exact column extents depend on
// emojiutil.Width() for the emoji glyph, so we don't assert specific
// coordinates — we just confirm the rect is non-empty and the center
// of the rect maps back to the expected emoji name.
func TestHitTestReaction_OnPill(t *testing.T) {
	msgs := []MessageItem{
		{
			TS:        "1700000001.000000",
			UserID:    "U1",
			UserName:  "alice",
			Text:      "hello",
			Timestamp: "10:30 AM",
			Reactions: []ReactionItem{
				{Emoji: "thumbsup", Count: 1, HasReacted: false},
				{Emoji: "tada", Count: 2, HasReacted: true},
			},
		},
	}
	m := New(msgs, "general")

	// Drive a render to populate hit rects.
	_ = m.View(30, 60)

	if len(m.lastReactionHits) != 2 {
		t.Fatalf("expected 2 reaction hit rects, got %d", len(m.lastReactionHits))
	}

	// First pill is :thumbsup:.
	h := m.lastReactionHits[0]
	if h.rowEnd <= h.rowStart || h.colEnd <= h.colStart {
		t.Fatalf("reaction hit rect is degenerate: rows=[%d,%d) cols=[%d,%d)", h.rowStart, h.rowEnd, h.colStart, h.colEnd)
	}
	rowMid := (h.rowStart + h.rowEnd) / 2
	colMid := (h.colStart + h.colEnd) / 2
	msgIdx, emoji, ok := m.HitTestReaction(rowMid, colMid)
	if !ok {
		t.Fatalf("HitTestReaction(%d,%d) returned ok=false for a coordinate inside the recorded rect", rowMid, colMid)
	}
	if emoji != "thumbsup" {
		t.Errorf("HitTestReaction emoji got %q want %q", emoji, "thumbsup")
	}
	if msgIdx != 0 {
		t.Errorf("HitTestReaction msgIdx got %d want 0", msgIdx)
	}

	// Second pill is :tada:.
	h2 := m.lastReactionHits[1]
	row2 := (h2.rowStart + h2.rowEnd) / 2
	col2 := (h2.colStart + h2.colEnd) / 2
	_, emoji2, ok := m.HitTestReaction(row2, col2)
	if !ok {
		t.Fatalf("HitTestReaction did not register inside second pill")
	}
	if emoji2 != "tada" {
		t.Errorf("second pill emoji got %q want %q", emoji2, "tada")
	}

	// A row clearly above the reaction line (row 0 is the username
	// row) must not register as a hit.
	if _, _, ok := m.HitTestReaction(0, colMid); ok {
		t.Error("HitTestReaction at username row should return ok=false")
	}
}

// TestHitTestReaction_NoHitsWithoutReactions asserts that a model
// rendering a message with NO reactions records zero reaction hits.
func TestHitTestReaction_NoHitsWithoutReactions(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "no reactions", Timestamp: "10:30 AM"},
	}
	m := New(msgs, "general")
	_ = m.View(20, 60)
	if len(m.lastReactionHits) != 0 {
		t.Errorf("expected 0 reaction hits, got %d", len(m.lastReactionHits))
	}
	if _, _, ok := m.HitTestReaction(0, 0); ok {
		t.Error("HitTestReaction with no reactions should always return ok=false")
	}
}

func TestModel_SetEmojiContext_InvalidatesCache(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "hello", Timestamp: "10:30 AM"},
	}
	m := New(msgs, "general")
	// Force a render to populate m.cache.
	_ = m.View(20, 60)
	if m.cache == nil {
		t.Fatalf("cache should be populated after View()")
	}

	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emojiutil.PlaceContext{},
		Cells:    2,
		Customs:  nil,
	})
	if m.cache != nil {
		t.Errorf("cache should be nil after SetEmojiContext (forces re-render)")
	}
}

func TestModel_RenderMessageWithImageEmoji_WarmCache(t *testing.T) {
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })

	thumbURL := emojiutil.CDNBaseURL + "1f44d.png"
	heartURL := emojiutil.CDNBaseURL + "2764-fe0f.png"

	ff := newFakePlaceFetcher() // defined in render_test.go
	ff.setPrerendered(emojiutil.EmojiCacheKey(thumbURL), stdimage.Pt(2, 1), imgpkg.Render{
		Cells: stdimage.Pt(2, 1),
		Lines: []string{"\U0010EEEE\U0010EEEE"},
	})
	ff.setPrerendered(emojiutil.EmojiCacheKey(heartURL), stdimage.Pt(2, 1), imgpkg.Render{
		Cells: stdimage.Pt(2, 1),
		Lines: []string{"\U0010EEEE\U0010EEEE"},
	})

	msgs := []MessageItem{{
		TS:        "1.0",
		UserName:  "alice",
		UserID:    "U1",
		Text:      "hi :thumbsup: and \u2764\uFE0F",
		Timestamp: "10:30 AM",
		Reactions: []ReactionItem{
			{Emoji: "thumbsup", Count: 3, HasReacted: false},
		},
	}}
	m := New(msgs, "general")
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emojiutil.PlaceContext{Fetcher: ff},
		Cells:    2,
		Customs:  nil,
	})

	out := m.View(24, 80)

	// The rendered output should contain kitty placeholder runes
	// (from the warm-path Place calls), NOT the literal ":thumbsup:"
	// text or the bare unicode glyph.
	if !strings.Contains(out, "\U0010EEEE") {
		t.Errorf("rendered view does not contain kitty placeholder runes; image mode appears inactive\noutput=%q", out)
	}
	if strings.Contains(out, ":thumbsup:") {
		t.Errorf("rendered view contains literal :thumbsup: text; image mode did not replace it\noutput=%q", out)
	}
}

// TestModel_RenderMessageWithImageEmoji_FlushesThreaded guards against a
// regression where renderMessagePlain dropped the named-return `flushes`
// slice (carrying body-text + reaction-pill emoji kitty upload callbacks)
// in favor of returning only `allFlushes` (carrying blockkit/attachment
// callbacks). When that happens kitty placeholder runes appear in the
// rendered output but the image bytes are never transmitted, producing
// blank cells in the terminal.
//
// Asserts: after a View() with image mode + a reaction emoji, at least one
// cached viewEntry must carry a non-empty flushes slice.
func TestModel_RenderMessageWithImageEmoji_FlushesThreaded(t *testing.T) {
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })

	thumbURL := emojiutil.CDNBaseURL + "1f44d.png"

	// Sentinel OnFlush makes the produced flush callback observable.
	// The fake fetcher's prerender memo carries the same callback that
	// Place returns to the caller; if renderMessagePlain drops the
	// named-return `flushes` slice, len(e.flushes) will be 0 across the
	// cache and the assertion below will fire.
	flushCalled := false
	ff := newFakePlaceFetcher()
	ff.setPrerendered(emojiutil.EmojiCacheKey(thumbURL), stdimage.Pt(2, 1), imgpkg.Render{
		Cells: stdimage.Pt(2, 1),
		Lines: []string{"\U0010EEEE\U0010EEEE"},
		OnFlush: func(_ io.Writer) error {
			flushCalled = true
			return nil
		},
	})

	msgs := []MessageItem{{
		TS:        "1.0",
		UserName:  "alice",
		UserID:    "U1",
		Text:      "hi :thumbsup:",
		Timestamp: "10:30 AM",
		Reactions: []ReactionItem{
			{Emoji: "thumbsup", Count: 3, HasReacted: false},
		},
	}}
	m := New(msgs, "general")
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emojiutil.PlaceContext{Fetcher: ff},
		Cells:    2,
		Customs:  nil,
	})

	_ = m.View(24, 80)

	var totalFlushes int
	for _, e := range m.cache {
		totalFlushes += len(e.flushes)
	}
	if totalFlushes == 0 {
		t.Errorf("expected at least one flush callback in cached entries, got 0 — emoji upload not threaded through renderMessagePlain return")
	}

	// Invoking the gathered flushes should reach the sentinel OnFlush.
	// (Defends against a future "we accumulate but never invoke" regression.)
	for _, e := range m.cache {
		for _, f := range e.flushes {
			_ = f(io.Discard)
		}
	}
	if totalFlushes > 0 && !flushCalled {
		t.Errorf("flush callback present but OnFlush sentinel never fired; the slice may hold the wrong callback")
	}
}

func TestUpdateReactionMaintainsUserIDs(t *testing.T) {
	// messages.New(msgs, channelName) returns a value Model; m is addressable
	// so the pointer-receiver methods below work.
	m := New([]MessageItem{{TS: "100.0", Text: "hi"}}, "general")

	// Add a reaction by user U1 -> creates the group with U1.
	m.UpdateReaction("100.0", "thumbsup", "U1", false)
	msg, _ := m.SelectedMessage()
	if len(msg.Reactions) != 1 {
		t.Fatalf("want 1 reaction, got %d", len(msg.Reactions))
	}
	if got := msg.Reactions[0].UserIDs; len(got) != 1 || got[0] != "U1" {
		t.Fatalf("want UserIDs [U1], got %v", got)
	}

	// Add same emoji by U2 -> appends to the same group.
	m.UpdateReaction("100.0", "thumbsup", "U2", false)
	msg, _ = m.SelectedMessage()
	if got := msg.Reactions[0].UserIDs; len(got) != 2 || got[1] != "U2" {
		t.Fatalf("want UserIDs [U1 U2], got %v", got)
	}

	// Remove U1 -> group remains with U2.
	m.UpdateReaction("100.0", "thumbsup", "U1", true)
	msg, _ = m.SelectedMessage()
	if got := msg.Reactions[0].UserIDs; len(got) != 1 || got[0] != "U2" {
		t.Fatalf("want UserIDs [U2] after remove, got %v", got)
	}

	// Remove U2 -> group disappears (count hits 0).
	m.UpdateReaction("100.0", "thumbsup", "U2", true)
	msg, _ = m.SelectedMessage()
	if len(msg.Reactions) != 0 {
		t.Fatalf("want 0 reactions after all removed, got %d", len(msg.Reactions))
	}
}

// TestUpdateReactionTruncatedRemoval covers the PR #68 review follow-up:
// Slack truncates reactions[].users for popular reactions, so a removal of a
// user absent from the (truncated) list must still decrement when Count >
// len(UserIDs) — while a duplicate echo on a non-truncated list must not.
func TestUpdateReactionTruncatedRemoval(t *testing.T) {
	// Truncated reactor list: Count(5) > len(UserIDs)(2).
	m := New([]MessageItem{{TS: "100.0", Reactions: []ReactionItem{
		{Emoji: "tada", Count: 5, UserIDs: []string{"A", "B"}},
	}}}, "general")
	m.UpdateReaction("100.0", "tada", "Z", true) // Z not listed, but list is truncated
	msg, _ := m.SelectedMessage()
	if got := msg.Reactions[0].Count; got != 4 {
		t.Errorf("truncated removal must decrement: Count=%d, want 4", got)
	}

	// Non-truncated list: Count(2) == len(UserIDs)(2). A removal of an
	// un-listed user is a duplicate echo — must NOT under-count.
	m2 := New([]MessageItem{{TS: "200.0", Reactions: []ReactionItem{
		{Emoji: "tada", Count: 2, UserIDs: []string{"A", "B"}},
	}}}, "general")
	m2.UpdateReaction("200.0", "tada", "Z", true)
	msg2, _ := m2.SelectedMessage()
	if got := msg2.Reactions[0].Count; got != 2 {
		t.Errorf("duplicate-echo removal must not under-count: Count=%d, want 2", got)
	}
}

// TestUpdateReactionIdempotentAndHasReacted covers the live-reaction fix:
// an optimistic self-update plus its WS echo must collapse to one count,
// reactions made by the current user from another device still apply, and
// HasReacted reflects only the current user.
func TestUpdateReactionIdempotentAndHasReacted(t *testing.T) {
	m := New([]MessageItem{{TS: "100.0", Text: "hi"}}, "general")
	m.SetCurrentUser("ME")

	// Self reaction (optimistic) then the WS echo of the same reaction:
	// must NOT double-count.
	m.UpdateReaction("100.0", "tada", "ME", false)
	m.UpdateReaction("100.0", "tada", "ME", false)
	msg, _ := m.SelectedMessage()
	if len(msg.Reactions) != 1 || msg.Reactions[0].Count != 1 {
		t.Fatalf("self add + echo: want one reaction count 1, got %+v", msg.Reactions)
	}
	if !msg.Reactions[0].HasReacted {
		t.Errorf("want HasReacted=true for current user's reaction")
	}

	// Another user adds the same emoji: count 2, HasReacted stays true.
	m.UpdateReaction("100.0", "tada", "OTHER", false)
	msg, _ = m.SelectedMessage()
	if msg.Reactions[0].Count != 2 {
		t.Errorf("want count 2 after other user adds, got %d", msg.Reactions[0].Count)
	}
	if !msg.Reactions[0].HasReacted {
		t.Errorf("HasReacted should remain true (current user still reacted)")
	}

	// Reaction by another user only: must not be flagged HasReacted.
	m.UpdateReaction("100.0", "eyes", "OTHER", false)
	msg, _ = m.SelectedMessage()
	for _, r := range msg.Reactions {
		if r.Emoji == "eyes" {
			if r.HasReacted {
				t.Errorf("other-user-only reaction must have HasReacted=false")
			}
			// Absent-user remove must not under-count.
			m.UpdateReaction("100.0", "eyes", "ME", true)
			msg2, _ := m.SelectedMessage()
			for _, r2 := range msg2.Reactions {
				if r2.Emoji == "eyes" && r2.Count != 1 {
					t.Errorf("absent-user remove changed count to %d, want 1", r2.Count)
				}
			}
		}
	}
}

// TestAppendRemoveUserIDDoNotMutateInput proves the helpers are pure: the
// UserIDs slices are aliased across the message pane and thread panel, so the
// helpers must never write into the input's backing array.
func TestAppendRemoveUserIDDoNotMutateInput(t *testing.T) {
	orig := []string{"U1", "U2", "U3"}
	shared := orig[:3:3] // same backing array, capped

	got := RemoveUserID(shared, "U1")
	if len(got) != 2 || got[0] != "U2" || got[1] != "U3" {
		t.Fatalf("RemoveUserID result wrong: %v", got)
	}
	if orig[0] != "U1" || orig[1] != "U2" || orig[2] != "U3" {
		t.Fatalf("RemoveUserID mutated input backing array: %v", orig)
	}

	base := []string{"U1"}
	base2 := base[:1:1] // force append to allocate
	got2 := AppendUserID(base2, "U2")
	if len(got2) != 2 || got2[0] != "U1" || got2[1] != "U2" {
		t.Fatalf("AppendUserID result wrong: %v", got2)
	}
	if len(base) != 1 || base[0] != "U1" {
		t.Fatalf("AppendUserID mutated input: %v", base)
	}
}

func TestSetSearchTerms_ClonesInput(t *testing.T) {
	m := New(nil, "general")
	terms := []string{"deploy"}
	m.SetSearchTerms(terms)
	terms[0] = "mutated"
	if len(m.searchTerms) != 1 || m.searchTerms[0] != "deploy" {
		t.Fatalf("SetSearchTerms aliased caller slice: %v", m.searchTerms)
	}
}

func TestSetSearchTerms_RedundantCallKeepsCache(t *testing.T) {
	m := New(nil, "general")
	m.SetSearchTerms([]string{"deploy"})
	m.cache = []viewEntry{{}} // sentinel: a populated render cache
	m.SetSearchTerms([]string{"deploy"})
	if m.cache == nil {
		t.Fatal("redundant SetSearchTerms invalidated the render cache")
	}
}

// TestSixelIdentity_DifferentStableKeysNeverCollide pins the collision
// regression: two different images whose payloads encode to the same
// byte length must not share an identity when their stable cache keys
// differ. The old length-based key made red and blue 80x64 images both
// "128/80x64", so a slot swap between them was treated as unchanged and
// never repainted.
func TestSixelIdentity_DifferentStableKeysNeverCollide(t *testing.T) {
	a := sixelEntry{key: "FRED-720", bytes: []byte("same-size-a"), width: 10, height: 5}
	b := sixelEntry{key: "FBLUE-720", bytes: []byte("same-size-b"), width: 10, height: 5}
	if len(a.bytes) != len(b.bytes) {
		t.Fatal("precondition: payload lengths must match")
	}
	if imgpkg.PlacementKey(a.key, a.bytes, a.width, a.height) == imgpkg.PlacementKey(b.key, b.bytes, b.width, b.height) {
		t.Fatalf("different stable keys collided: %q", imgpkg.PlacementKey(a.key, a.bytes, a.width, a.height))
	}
}

// TestSixelIdentity_EmptyKeysHashPayload asserts the digest fallback:
// when no stable key exists, equal-length different payloads still get
// different identities (payload hashed once at construction time).
func TestSixelIdentity_EmptyKeysHashPayload(t *testing.T) {
	a := sixelEntry{bytes: []byte("same-size-a"), width: 10, height: 5}
	b := sixelEntry{bytes: []byte("same-size-b"), width: 10, height: 5}
	if len(a.bytes) != len(b.bytes) {
		t.Fatal("precondition: payload lengths must match")
	}
	if imgpkg.PlacementKey(a.key, a.bytes, a.width, a.height) == imgpkg.PlacementKey(b.key, b.bytes, b.width, b.height) {
		t.Fatalf("equal-length different payloads with empty keys collided: %q", imgpkg.PlacementKey(a.key, a.bytes, a.width, a.height))
	}
}

// TestSixelIdentity_GeometryParticipates asserts that a key collision is
// still broken by a footprint difference.
func TestSixelIdentity_GeometryParticipates(t *testing.T) {
	a := sixelEntry{key: "F1-720", bytes: []byte("payload"), width: 10, height: 5}
	b := sixelEntry{key: "F1-720", bytes: []byte("payload"), width: 11, height: 5}
	if imgpkg.PlacementKey(a.key, a.bytes, a.width, a.height) == imgpkg.PlacementKey(b.key, b.bytes, b.width, b.height) {
		t.Fatalf("different target geometry collided: %q", imgpkg.PlacementKey(a.key, a.bytes, a.width, a.height))
	}
}

// TestView_SixelIntersectingMoreBelowUsesFallbackAndPublishesNoPlacement
// asserts that an image whose footprint includes the row later replaced
// by the "-- more below --" indicator is treated as not fully paintable:
// no sixel placement is published, the visible rows use half-block
// fallback, and the indicator stays visible.
func TestView_SixelIntersectingMoreBelowUsesFallbackAndPublishesNoPlacement(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoSixel)
	// First render at the same height pins lastViewHeight and marks the
	// selection as snapped; ScrollUp then moves the viewport to the top
	// (yOffset 0) so the image is fully visible.
	_ = m.View(23, 60)
	m.ScrollUp(1)

	// msgAreaHeight = 22; the image spans window rows 2..21, and the
	// more-below indicator replaces row 21 — the image's last row.
	out := m.View(23, 60)
	if strings.Contains(out, "\x1bP") {
		t.Errorf("sixel DCS leaked under an indicator-intersecting frame")
	}
	if n := len(m.SixelPlacements()); n != 0 {
		t.Errorf("SixelPlacements() = %d, want 0 when the image intersects the more-below indicator", n)
	}
	if !strings.Contains(out, "▀") {
		t.Errorf("expected half-block fallback rows for the non-indicator rows")
	}
	if !strings.Contains(out, "more below") {
		t.Errorf("more-below indicator missing from output")
	}
}

// TestView_SixelIntersectingLoadingRowUsesFallbackAndPublishesNoPlacement
// is the loading-hint twin: with m.loading true, the loading row replaces
// window row 0, and an image starting on that row must fall back to
// half-block rather than publish a placement that would sit under the
// authoritative hint.
func TestView_SixelIntersectingLoadingRowUsesFallbackAndPublishesNoPlacement(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoSixel)
	// First render pins the geometry at this height; the selection snap
	// bottoms the viewport at yOffset=3 (maxOffset). ScrollUp moves the
	// image's first row onto window row 0 (yOffset=2).
	_ = m.View(21, 60)
	m.SetLoading(true)
	m.ScrollUp(1)

	out := m.View(21, 60)
	if strings.Contains(out, "\x1bP") {
		t.Errorf("sixel DCS leaked under a loading-indicator-intersecting frame")
	}
	if n := len(m.SixelPlacements()); n != 0 {
		t.Errorf("SixelPlacements() = %d, want 0 when the image intersects the loading row", n)
	}
	if !strings.Contains(out, "▀") {
		t.Errorf("expected half-block fallback rows for the non-indicator rows")
	}
	if !strings.Contains(out, "Loading older messages") {
		t.Errorf("loading indicator missing from output")
	}
}
