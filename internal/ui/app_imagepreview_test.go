// internal/ui/app_imagepreview_test.go
package ui

import (
	"bytes"
	stdimage "image"
	imgcolor "image/color"
	imgpng "image/png"
	"testing"
	"time"

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
	x := app.layout.sidebarEnd + 1 + colMid
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

// TestMouseClick_OnThreadImageDispatchesOpenPreview is issue #198:
// clicking an image in the thread pane must open the preview, as it
// does in the channel pane (and as `v` already did in the thread).
func TestMouseClick_OnThreadImageDispatchesOpenPreview(t *testing.T) {
	channelID, _, fileID, img := imageBearingMessage(t)
	parent := messages.MessageItem{TS: "1700000000.000001", UserID: "U2", UserName: "bob", Text: "thread", Timestamp: "10:00 AM"}
	reply := img
	reply.TS = "1700000000.000200"

	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	if _, err := cache.Put(fileID+"-720", "png", makeTestPNGBytes(720, 720)); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)

	app := NewApp()
	app.width = 200
	app.height = 60
	app.activeChannelID = channelID
	app.messagepane.SetMessages([]messages.MessageItem{parent})
	app.threadVisible = true
	app.focusedPanel = PanelThread
	app.threadPanel.SetThread(parent, []messages.MessageItem{reply}, channelID, parent.TS)
	app.threadPanel.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoHalfBlock,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	})
	_ = app.View()
	_ = app.View()

	// Find a cell the thread pane reports as the reply's image, in
	// its content coordinates, then click it on screen.
	row, col, found := -1, -1, false
	for r := 0; r < app.height && !found; r++ {
		for c := 0; c < app.width; c++ {
			if ts, _, ok := app.threadPanel.HitTestImage(r, c); ok && ts == reply.TS {
				row, col, found = r, c, true
				break
			}
		}
	}
	if !found {
		t.Fatal("thread pane recorded no image hit for the reply after View()")
	}
	x := app.layout.msgEnd + 1 + col
	y := 1 + app.threadPanel.ChromeHeight() + row

	_, cmd := app.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("expected non-nil cmd from click on thread image")
	}
	op, ok := cmd().(messages.OpenImagePreviewMsg)
	if !ok {
		t.Fatalf("got %T, want messages.OpenImagePreviewMsg", cmd())
	}
	if op.Channel != channelID || op.TS != reply.TS || op.AttIdx != 0 {
		t.Errorf("OpenImagePreviewMsg = %+v, want {Channel:%q TS:%q AttIdx:0}", op, channelID, reply.TS)
	}
}

// heldSiblingApp puts a two-image message on C123 where the first
// image is Slack-hosted (the policy loads it) and the second is not (in
// "trusted" mode the policy holds it back), with the preview already
// open on the first.
func heldSiblingApp(t *testing.T) (*App, string) {
	t.Helper()
	const ts = "1700000000.000100"
	msg := messages.MessageItem{
		TS: ts, UserID: "U1", UserName: "alice",
		Attachments: []messages.Attachment{
			{
				Kind: "image", Name: "slack.png", FileID: "F0SLACK", Mime: "image/png",
				Thumbs: []messages.ThumbSpec{{URL: "https://files.slack.com/a-720.png", W: 720, H: 720}},
			},
			{
				Kind: "image", Name: "elsewhere.png", FileID: "F0ELSE", Mime: "image/png",
				Thumbs: []messages.ThumbSpec{{URL: "https://images.example.com/b-720.png", W: 720, H: 720}},
			},
		},
	}

	app := NewApp()
	app.activeChannelID = "C123"
	app.focusedPanel = PanelMessages
	app.messagepane.SetMessages([]messages.MessageItem{msg})
	app.SetImageFetcher(stubPreviewFetcher{})
	app.imageCtx.Policy = imgpkg.NewLoadPolicy(imgpkg.LoadTrusted, nil)

	// Preview already open on the Slack-hosted sibling.
	overlay := imgpkg.NewLoadingPreview("slack.png", 2, 0)
	app.preview.Open(&overlay, "C123", ts, 0)
	return app, ts
}

// TestCyclePreview_HeldSiblingAsksInsteadOfFailingSilently covers the
// second bug Greptile found on PR #467. Only the initial open consulted
// previewHeld, so cycling onto a sibling the policy holds back issued a
// Gated fetch that was refused; previewErrorMsg dismisses only a
// *loading* overlay, so the image already on screen stayed up and the
// keypress looked like it did nothing.
func TestCyclePreview_HeldSiblingAsksInsteadOfFailingSilently(t *testing.T) {
	app, ts := heldSiblingApp(t)

	cmd := app.cycleImagePreviewCmd("C123", ts, 0, +1)

	if app.preview.Active() {
		t.Error("cycling onto a held sibling left the preview open; it should close before prompting")
	}
	if !dialogShows(app, "Load images?") {
		t.Error("cycling onto a held sibling did not open the load prompt")
	}
	if cmd != nil {
		// promptLoadImages returns nil once it has opened the dialog;
		// a non-nil cmd here would mean a fetch was still issued.
		if _, isFetch := cmd().(previewLoadedMsg); isFetch {
			t.Error("a held sibling was fetched anyway")
		}
	}
}

// TestCyclePreview_AllowedSiblingStillFetches guards the fix against
// over-reach: a sibling the policy permits must still cycle normally,
// with the preview left open.
func TestCyclePreview_AllowedSiblingStillFetches(t *testing.T) {
	app, ts := heldSiblingApp(t)
	// Allow the second image's host for this session, as `I` would.
	app.imageCtx.Policy.AllowHosts("images.example.com")

	cmd := app.cycleImagePreviewCmd("C123", ts, 0, +1)

	if !app.preview.Active() {
		t.Error("cycling onto an allowed sibling closed the preview")
	}
	if dialogShows(app, "Load images?") {
		t.Error("cycling onto an allowed sibling opened the load prompt")
	}
	if cmd == nil {
		t.Fatal("cycling onto an allowed sibling issued no fetch")
	}
	if _, ok := cmd().(previewLoadedMsg); !ok {
		t.Errorf("got %#v, want previewLoadedMsg", cmd())
	}
}

// openPreviewMsgIn returns the reopen request among msgs, if any.
func openPreviewMsgIn(msgs []tea.Msg) (messages.OpenImagePreviewMsg, bool) {
	for _, m := range msgs {
		if op, ok := m.(messages.OpenImagePreviewMsg); ok {
			return op, true
		}
	}
	return messages.OpenImagePreviewMsg{}, false
}

// TestOpenPreview_HeldImageOpensAfterApproval covers the third bug
// Greptile found on PR #467. Opening the preview of a held image asked
// first, but approving only recorded the policy and redrew the panes:
// the preview the user asked for never opened, so `O` looked like it had
// done nothing and had to be pressed again.
func TestOpenPreview_HeldImageOpensAfterApproval(t *testing.T) {
	app, ts := heldSiblingApp(t)
	// The open path, not the cycle path: nothing on screen yet.
	app.preview.Close()

	app.Update(messages.OpenImagePreviewMsg{Channel: "C123", TS: ts, AttIdx: 1})
	if app.preview.Active() {
		t.Fatal("a held image opened its preview without asking")
	}
	if !dialogShows(app, "Load images?") {
		t.Fatal("opening a held image did not open the load prompt")
	}

	out := pressKey(t, app, keyPress('y'))

	op, ok := openPreviewMsgIn(out)
	if !ok {
		t.Fatalf("approving did not re-request the preview; msgs = %#v", out)
	}
	if op.Channel != "C123" || op.TS != ts || op.AttIdx != 1 {
		t.Errorf("reopen = %+v, want {Channel:C123 TS:%s AttIdx:1}", op, ts)
	}
	app.Update(op)
	if !app.preview.Active() {
		t.Fatal("the approved preview did not open")
	}
	if got := app.preview.AttIdx(); got != 1 {
		t.Errorf("preview opened on attachment %d, want 1", got)
	}
}

// TestCyclePreview_HeldSiblingOpensAfterApproval is the same for the
// cycle path, which closes the preview before prompting: approving must
// bring it back on the sibling the user cycled to, not leave the screen
// empty and make them reopen and navigate back.
func TestCyclePreview_HeldSiblingOpensAfterApproval(t *testing.T) {
	app, ts := heldSiblingApp(t)

	app.cycleImagePreviewCmd("C123", ts, 0, +1)
	if app.preview.Active() {
		t.Fatal("cycling onto a held sibling left the preview open")
	}

	out := pressKey(t, app, keyPress('y'))

	op, ok := openPreviewMsgIn(out)
	if !ok {
		t.Fatalf("approving did not re-request the preview; msgs = %#v", out)
	}
	if op.AttIdx != 1 {
		t.Errorf("reopen = %+v, want the sibling AttIdx 1", op)
	}
	app.Update(op)
	if !app.preview.Active() {
		t.Fatal("the approved preview did not come back")
	}
	if got := app.preview.AttIdx(); got != 1 {
		t.Errorf("preview came back on attachment %d, want the sibling 1", got)
	}
}

// TestLoadPrompt_CancelOpensNoPreview guards the other direction: the
// prompt's Cancel button records nothing and must not open a preview.
func TestLoadPrompt_CancelOpensNoPreview(t *testing.T) {
	app, ts := heldSiblingApp(t)
	app.preview.Close()

	app.Update(messages.OpenImagePreviewMsg{Channel: "C123", TS: ts, AttIdx: 1})
	out := pressKey(t, app, keyPress('n'))

	if op, ok := openPreviewMsgIn(out); ok {
		t.Errorf("cancelling re-requested the preview: %+v", op)
	}
	if app.preview.Active() {
		t.Error("cancelling opened a preview")
	}
}

// TestLoadImagesKey_RequestsNoPreview keeps `I` (the keybinding, not a
// preview) from opening a preview of its own on approval: it only
// allows the message's images so the panes draw them inline.
func TestLoadImagesKey_RequestsNoPreview(t *testing.T) {
	app, ts := heldSiblingApp(t)
	app.preview.Close()

	app.promptLoadImages(app.messagepane.Messages()[0])
	if !dialogShows(app, "Load images?") {
		t.Fatalf("I did not open the load prompt (ts %s)", ts)
	}

	out := pressKey(t, app, keyPress('y'))

	if op, ok := openPreviewMsgIn(out); ok {
		t.Errorf("I opened a preview on approval: %+v", op)
	}
	if app.preview.Active() {
		t.Error("I opened a preview on approval")
	}
}

// TestOpenPreview_TrustHostAlsoOpensPreview covers the prompt's other
// allowing button: "Trust this host this session" carries the deferred
// preview too, so it is not a quieter way to end up with nothing shown.
func TestOpenPreview_TrustHostAlsoOpensPreview(t *testing.T) {
	app, ts := heldSiblingApp(t)
	app.preview.Close()

	app.Update(messages.OpenImagePreviewMsg{Channel: "C123", TS: ts, AttIdx: 1})

	out := pressKey(t, app, keyPress('a'))

	op, ok := openPreviewMsgIn(out)
	if !ok {
		t.Fatalf("trusting the host did not re-request the preview; msgs = %#v", out)
	}
	app.Update(op)
	if !app.preview.Active() || app.preview.AttIdx() != 1 {
		t.Errorf("preview active=%v attIdx=%d, want true/1", app.preview.Active(), app.preview.AttIdx())
	}
}

// TestOpenPreview_ApprovalWaitsForAQueuedDialog covers the regression the
// replay introduced (Greptile's fourth find on PR #467). A dialog that
// queued behind the load prompt is opened by afterConfirmPrompt the step
// before the replayed request lands, so the preview went up over it. The
// preview swallows every key (app.go's pre-switch interception), so the
// dialog stayed visible but unanswerable until it was dismissed.
func TestOpenPreview_ApprovalWaitsForAQueuedDialog(t *testing.T) {
	// The skip toasts; don't make the test wait out its clear tick.
	oldTick := toastTick
	toastTick = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	t.Cleanup(func() { toastTick = oldTick })

	app, ts := heldSiblingApp(t)
	app.preview.Close()

	const ephTS = "1700000000.000200"
	app.messagepane.SetMessages([]messages.MessageItem{
		app.messagepane.Messages()[0], mentionEphemeral(ephTS),
	})

	app.Update(messages.OpenImagePreviewMsg{Channel: "C123", TS: ts, AttIdx: 1})
	if !dialogShows(app, "Load images?") {
		t.Fatal("opening a held image did not open the load prompt")
	}
	// A message with buttons arrives while the prompt is up: it queues.
	app.enqueueActionPrompt(actionPromptRef{channelID: "C123", ts: ephTS})
	if app.preview.Active() {
		t.Fatal("queueing a dialog opened a preview")
	}

	out := pressKey(t, app, keyPress('y'))

	// The queued dialog is now the overlay in front of the user.
	if !dialogShows(app, "Add Them") {
		t.Fatal("the queued dialog did not open when the load prompt closed")
	}
	if op, ok := openPreviewMsgIn(out); ok {
		t.Errorf("approving replayed the preview over the queued dialog: %+v", op)
	}
	// Feed anything the approval produced: still no preview on top.
	for _, m := range out {
		app.Update(m)
	}
	if app.preview.Active() {
		t.Error("a preview opened over the queued dialog and will swallow its keys")
	}
	if !dialogShows(app, "Add Them") {
		t.Error("the queued dialog was lost")
	}
	// The approval itself must not be lost: the image is permitted, so
	// the pane draws it inline even though no preview opened.
	if got := app.imageCtx.Policy.Decide("https://images.example.com/b-720.png"); got != imgpkg.Load {
		t.Errorf("Decide = %v, want Load: the approval was dropped", got)
	}
}
