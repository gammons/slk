// internal/ui/app_imagepreview_test.go
package ui

import (
	"bytes"
	stdimage "image"
	imgcolor "image/color"
	imgpng "image/png"
	"testing"

	tea "charm.land/bubbletea/v2"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
)

// makeTestPNGBytes returns a deterministic PNG of the given size for
// staging on-disk image cache fixtures. Mirrors the helper in the
// messages package (kept local to avoid an import cycle).
func makeTestPNGBytes(w, h int) []byte {
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

// imageBearingMessage builds a one-message MessageItem slice with a
// single image attachment whose bytes have been pre-staged in the
// caller-supplied cache. Returns the channel ID, ts, and fileID so
// tests can assert the OpenImagePreviewMsg payload.
func imageBearingMessage(t *testing.T) (channelID, ts, fileID string, msg messages.MessageItem) {
	t.Helper()
	channelID = "C123"
	ts = "1700000000.000100"
	fileID = "F0123ABCD"
	msg = messages.MessageItem{
		TS:        ts,
		UserID:    "U1",
		UserName:  "alice",
		Text:      "look at this",
		Timestamp: "10:30 AM",
		Attachments: []messages.Attachment{{
			Kind:   "image",
			Name:   "screenshot.png",
			URL:    "https://example.com/perma",
			FileID: fileID,
			Mime:   "image/png",
			Thumbs: []messages.ThumbSpec{{URL: "https://example.com/720.png", W: 720, H: 720}},
		}},
	}
	return
}

// TestOKey_DispatchesOpenImagePreviewMsg asserts that pressing `O` on
// a selected message with an image attachment dispatches an
// OpenImagePreviewMsg carrying the message's channel, TS, and the
// attachment index of the first image.
func TestOKey_DispatchesOpenImagePreviewMsg(t *testing.T) {
	channelID, ts, _, msg := imageBearingMessage(t)

	app := NewApp()
	app.activeChannelID = channelID
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{msg})

	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from O key")
	}
	out := cmd()
	op, ok := out.(messages.OpenImagePreviewMsg)
	if !ok {
		t.Fatalf("got %T, want messages.OpenImagePreviewMsg", out)
	}
	if op.Channel != channelID {
		t.Errorf("OpenImagePreviewMsg.Channel = %q, want %q", op.Channel, channelID)
	}
	if op.TS != ts {
		t.Errorf("OpenImagePreviewMsg.TS = %q, want %q", op.TS, ts)
	}
	if op.AttIdx != 0 {
		t.Errorf("OpenImagePreviewMsg.AttIdx = %d, want 0", op.AttIdx)
	}
}

// TestOKey_NoImageAttachmentNoop asserts that pressing `O` on a
// selected message without any image attachment is a no-op (returns
// nil) — the keybind only fires when there's something to preview.
func TestOKey_NoImageAttachmentNoop(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{
		{TS: "1.0", UserName: "alice", Text: "no images here"},
	})
	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd != nil {
		t.Errorf("expected nil cmd when selected message has no image attachment, got non-nil")
	}
}

// TestOKey_NothingSelectedNoop asserts that `O` with no messages in
// the pane is a clean no-op.
func TestOKey_NothingSelectedNoop(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	cmd := app.handleNormalMode(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd != nil {
		t.Errorf("expected nil cmd when nothing selected, got non-nil")
	}
}

// TestMouseClick_OnImageDispatchesOpenPreview wires the messages-pane
// inline-image rendering pipeline (cached bytes), drives one View() to
// populate the hit-rect cache, then issues a tea.MouseClickMsg at the
// center of the image's footprint and asserts the click produces an
// OpenImagePreviewMsg with the right payload — bypassing the existing
// drag-to-copy press handler.
func TestMouseClick_OnImageDispatchesOpenPreview(t *testing.T) {
	channelID, ts, fileID, msg := imageBearingMessage(t)

	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	pngBytes := makeTestPNGBytes(720, 720)
	if _, err := cache.Put(fileID+"-720", "png", pngBytes); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)

	app := NewApp()
	app.width = 120
	app.height = 60
	app.activeChannelID = channelID
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{msg})
	app.messagepane.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoHalfBlock,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	})

	// Drive a render so layout offsets and m.lastHits populate. We
	// call View() twice: first to pin layout, second to ensure the
	// image hit cache reflects the post-render geometry.
	_ = app.View()
	_ = app.View()

	rects := app.messagepane.LastHitsForTest()
	if len(rects) == 0 {
		t.Fatal("expected at least one image hit rect after View() with cached bytes")
	}
	h := rects[0]
	rowMid := (h.RowStart + h.RowEnd) / 2
	colMid := (h.ColStart + h.ColEnd) / 2

	// Convert the hit rect (which is keyed in messages-pane content
	// coordinates: rows past chrome, cols within the pane) to an
	// app-level (X, Y) terminal coordinate. Inverse of panelAt:
	//   X_terminal = layoutSidebarEnd + 1 (border) + paneCol
	//   Y_terminal = 1 (border) + chromeHeight + contentRow
	chrome := app.messagepane.ChromeHeight()
	x := app.layoutSidebarEnd + 1 + colMid
	y := 1 + chrome + rowMid

	_, cmd := app.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from click on image")
	}
	out := cmd()
	op, ok := out.(messages.OpenImagePreviewMsg)
	if !ok {
		t.Fatalf("got %T, want messages.OpenImagePreviewMsg", out)
	}
	if op.Channel != channelID {
		t.Errorf("Channel = %q, want %q", op.Channel, channelID)
	}
	if op.TS != ts {
		t.Errorf("TS = %q, want %q", op.TS, ts)
	}
	if op.AttIdx != 0 {
		t.Errorf("AttIdx = %d, want 0", op.AttIdx)
	}
}
