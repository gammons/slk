package ui

import (
	"context"
	stdimage "image"
	"testing"

	"github.com/gammons/slk/internal/core"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
)

// sixelThreadApp is an App with the thread pane open on a reply that
// carries a sixel image, rendered once at width x height. The channel
// pane shows only the image-free parent, so every placement comes from
// the thread.
func sixelThreadApp(t *testing.T, width, height int) (*App, messages.MessageItem) {
	t.Helper()
	channelID, _, fileID, reply := imageBearingMessage(t)
	reply.TS = "1700000000.000200"
	parent := messages.MessageItem{TS: "1700000000.000100", UserID: "U2", UserName: "bob", Text: "parent", Timestamp: "10:29 AM"}

	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	if _, err := cache.Put(fileID+"-720", "png", makeTestPNGBytes(720, 720)); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	// Prime the decoded memo so the first View renders the image itself
	// rather than its loading placeholder (Fetcher.Cached is memo-only).
	if _, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key: fileID + "-720", URL: "unused://disk-cache-hit", Target: stdimage.Pt(320, 320),
	}); err != nil {
		t.Fatalf("Fetch (memo prime): %v", err)
	}
	a := buildTestApp(withSize(width, height))
	a.imgProtocol = imgpkg.ProtoSixel
	a.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoSixel,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	})
	a.activeChannelID = channelID
	a.messagepane.SetMessages([]messages.MessageItem{parent})
	a.threadVisible = true
	a.focusedPanel = PanelThread
	a.threadPanel.SetThread(parent, []messages.MessageItem{reply}, channelID, parent.TS)
	_ = a.View()
	return a, reply
}

// firstImageCell returns the screen cell (0-based) of the top-left of
// the reply's image, from the thread pane's own image hit map and the
// screen mapping the click router uses.
func firstImageCell(t *testing.T, a *App, ts string) (x, y int) {
	t.Helper()
	for r := 0; r < a.height; r++ {
		for c := 0; c < a.width; c++ {
			if got, _, ok := a.threadPanel.HitTestImage(r, c); ok && got == ts {
				return a.layout.MsgEnd() + 1 + c, 1 + a.threadPanel.ChromeHeight() + r
			}
		}
	}
	t.Fatal("thread pane recorded no image hit for the reply")
	return 0, 0
}

// A sixel image in the thread pane is published as a placement at the
// image's own cells on screen, beside the messages pane.
func TestCollectSixelPlacements_ThreadPaneBesideMessages(t *testing.T) {
	a, reply := sixelThreadApp(t, 200, 60)
	frame := a.computeFrame()
	if frame.MsgWidth == 0 || frame.ThreadWidth == 0 {
		t.Fatalf("precondition: panes side by side, got %+v", frame)
	}
	got := a.collectSixelPlacements(frame)
	if len(got) != 1 {
		t.Fatalf("placements = %+v, want the thread image's one", got)
	}
	x, y := firstImageCell(t, a, reply.TS)
	if got[0].Col-1 != x || got[0].Row-1 != y {
		t.Errorf("placement at screen (%d,%d), want the image's top-left cell (%d,%d)", got[0].Col-1, got[0].Row-1, x, y)
	}
	if right := a.layout.ThreadEnd() - 1; got[0].Col-1+got[0].Cols > right {
		t.Errorf("placement %+v reaches past the thread pane's right border at %d", got[0], right)
	}
}

// When the panes stack, the thread in front is drawn alone, and its
// images are placed there too.
func TestCollectSixelPlacements_ThreadPaneStackedInFront(t *testing.T) {
	a, reply := sixelThreadApp(t, 100, 60)
	frame := a.computeFrame()
	if frame.MsgWidth != 0 || frame.ThreadWidth == 0 {
		t.Fatalf("precondition: thread stacked in front, got %+v", frame)
	}
	got := a.collectSixelPlacements(frame)
	if len(got) != 1 {
		t.Fatalf("placements = %+v, want the thread image's one", got)
	}
	x, y := firstImageCell(t, a, reply.TS)
	if got[0].Col-1 != x || got[0].Row-1 != y {
		t.Errorf("placement at screen (%d,%d), want the image's top-left cell (%d,%d)", got[0].Col-1, got[0].Row-1, x, y)
	}
}

// The thread's placements stop at the compose box: an image that would
// reach it is clipped out, never painted over the composer.
func TestThreadSixelRect_StopsAtTheCompose(t *testing.T) {
	a, _ := sixelThreadApp(t, 200, 60)
	frame := a.computeFrame()
	rect := a.threadSixelRect(frame)
	composeRow := 1 + a.layout.PageHeight(PanelThread) // first row under the replies
	if bottom := rect.Y + rect.H - 1; bottom != composeRow {
		t.Errorf("thread rect bottom = %d, want the compose region's first row %d", bottom, composeRow)
	}
}

// A sixel avatar service reaches every pane: the channel window, the
// thread pane, and a window split off later. Each places its avatars
// in its own band of the screen.
func TestSetAvatarService_SixelAvatarsReachEveryPane(t *testing.T) {
	a := buildTestApp(withSize(200, 60))
	a.imgProtocol = imgpkg.ProtoSixel
	a.SetThreadAvatars(true)
	a.SetAvatarService(core.NewSixelAvatarService(
		func(string) string { return "AVAV\nAVAV" }, nil,
		func(userID string) (string, []byte, bool) {
			return "avatar-" + userID, []byte("\x1bPq#0~-\x1b\\"), true
		},
	))
	parent := messages.MessageItem{TS: "1700000000.000100", UserID: "U2", UserName: "bob", Text: "parent", Timestamp: "10:29 AM"}
	reply := messages.MessageItem{TS: "1700000000.000200", UserID: "U3", UserName: "cat", Text: "reply", Timestamp: "10:30 AM"}
	a.activeChannelID = "C123"
	a.messagepane.SetMessages([]messages.MessageItem{parent})
	a.threadVisible = true
	a.threadPanel.SetThread(parent, []messages.MessageItem{reply}, "C123", parent.TS)
	_ = a.View()

	var inMessages, inThread int
	for _, p := range a.collectSixelPlacements(a.computeFrame()) {
		switch x := p.Col - 1; {
		case x >= a.layout.SidebarEnd() && x < a.layout.MsgEnd():
			inMessages++
		case x >= a.layout.MsgEnd() && x < a.layout.ThreadEnd():
			inThread++
		}
	}
	if inMessages != 1 || inThread != 2 {
		t.Errorf("avatar placements: %d in the messages pane (want 1), %d in the thread pane (want the parent's and the reply's 2)", inMessages, inThread)
	}

	w := a.newWindowModel("general")
	w.SetMessages([]messages.MessageItem{parent})
	_ = w.View(30, 100)
	if len(w.SixelPlacements()) != 1 {
		t.Errorf("a window split off later placed %d avatars, want 1", len(w.SixelPlacements()))
	}
}
