// internal/ui/messages/model_test.go
package messages

import (
	"bytes"
	stdimage "image"
	imgcolor "image/color"
	imgpng "image/png"
	"strings"
	"testing"

	imgpkg "github.com/gammons/slk/internal/image"
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
		chType   string
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

	// Render again WITHOUT changing selection. yOffset must NOT snap back.
	_ = m.View(20, 80)
	if m.yOffset != scrolled {
		t.Errorf("yOffset snapped back after render: want %d, got %d", scrolled, m.yOffset)
	}

	// Now move selection -- yOffset should re-snap to keep selection visible.
	m.MoveUp()
	_ = m.View(20, 80)
	if m.yOffset == scrolled {
		t.Error("expected yOffset to re-snap after selection change, but it did not")
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
	m.SetImageContext(ImageContext{
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
	m.HandleImageReady(channel, ts)

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
// message whose bytes are pre-staged in the on-disk cache. Returns the
// model configured with the given protocol; the image is fully cached so
// the first View() will take the rendered (not placeholder) path.
func setupImageMessageModel(t *testing.T, protocol imgpkg.Protocol) *Model {
	t.Helper()
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	const fileID = "F0123ABCD"
	pngBytes := makeTestPNG(720, 720)
	if _, err := cache.Put(fileID+"-720", "png", pngBytes); err != nil {
		t.Fatalf("cache.Put: %v", err)
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
	ctx := ImageContext{
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

// TestView_SixelFullVisibility_EmitsBytes asserts that when a sixel-
// rendered image fits entirely within the visible viewport, the View
// output contains the actual sixel byte stream (the OnFlush bytes
// captured into sixelRows during buildCache), not just the sentinel
// placeholder line. The sixel byte stream begins with the DCS escape
// "\x1bP" (ESC P) per the DEC standard.
func TestView_SixelFullVisibility_EmitsBytes(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoSixel)
	// A tall viewport ensures the entire image (~20 rows + chrome + body)
	// fits without clipping.
	out := m.View(60, 80)
	if !strings.Contains(out, "\x1bP") {
		t.Errorf("expected View() output to contain a sixel DCS escape (\\x1bP) when the image is fully visible; got %d bytes without it", len(out))
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
}

// TestView_KittyEmitsUploadEscape asserts that when a kitty-rendered
// image is in the visible window, View() emits the kitty
// transmit-and-display escape (begins with "\x1b_G") via the captured
// per-frame flushes. The kitty registry's first-render-of-id contract
// guarantees this happens exactly once per image per frame.
func TestView_KittyEmitsUploadEscape(t *testing.T) {
	m := setupImageMessageModel(t, imgpkg.ProtoKitty)
	// Capture the kitty side-channel output. The upload escape is
	// written directly to kittyOutput (not embedded in View()'s
	// return string) because bubbletea/lipgloss strip APC sequences.
	saved := kittyOutput
	defer func() { kittyOutput = saved }()
	var buf bytes.Buffer
	kittyOutput = &buf

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
