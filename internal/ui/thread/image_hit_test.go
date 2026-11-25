package thread

import (
	"bytes"
	"context"
	stdimage "image"
	imgcolor "image/color"
	imgpng "image/png"
	"testing"

	"github.com/gammons/slk/internal/bubbles/entrylist"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
)

const imageHitFileID = "F0123ABCD"

// imageThreadModel returns a thread pane whose image context already
// holds imageHitFileID's decoded bytes, so the first View renders the
// image rather than its loading placeholder. Mirrors
// setupImageMessageModel in the messages package.
func imageThreadModel(t *testing.T, proto imgpkg.Protocol) *Model {
	t.Helper()
	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	src := stdimage.NewRGBA(stdimage.Rect(0, 0, 720, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 720; x++ {
			src.Set(x, y, imgcolor.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := imgpng.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	key := imageHitFileID + "-720"
	if _, err := cache.Put(key, "png", buf.Bytes()); err != nil {
		t.Fatalf("cache.Put: %v", err)
	}
	fetcher := imgpkg.NewFetcher(cache, nil)
	if _, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key: key, URL: "unused://disk-cache-hit", Target: stdimage.Pt(320, 320),
	}); err != nil {
		t.Fatalf("Fetch (memo prime): %v", err)
	}
	m := New()
	m.SetImageContext(imgrender.ImageContext{
		Protocol:   proto,
		Fetcher:    fetcher,
		CellPixels: stdimage.Pt(8, 16),
		MaxRows:    20,
	})
	return m
}

func withImage(msg messages.MessageItem) messages.MessageItem {
	msg.Attachments = []messages.Attachment{{
		Kind:   "image",
		Name:   "screenshot.png",
		URL:    "https://example.com/perma",
		FileID: imageHitFileID,
		Mime:   "image/png",
		Thumbs: []messages.ThumbSpec{{URL: "https://example.com/720.png", W: 720, H: 720}},
	}}
	return msg
}

// imageHitCenter returns the centre cell of the last frame's only
// image hit rect.
func imageHitCenter(t *testing.T, m *Model) (row, col int) {
	t.Helper()
	var rects []entrylist.FrameHit
	for _, h := range m.list.Hits() {
		if h.Kind == entrylist.HitImage {
			rects = append(rects, h)
		}
	}
	if len(rects) != 1 {
		t.Fatalf("frame has %d image hit rects, want 1", len(rects))
	}
	h := rects[0]
	return (h.RowStart + h.RowEnd) / 2, (h.ColStart + h.ColEnd) / 2
}

// An image on a reply is clickable: issue #198, where clicking an image
// in the thread pane did nothing while `v` opened it.
func TestHitTestImage_Reply(t *testing.T) {
	m := imageThreadModel(t, imgpkg.ProtoHalfBlock)
	parent := messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"}
	reply := withImage(messages.MessageItem{TS: "101.0", UserName: "bob", Text: "see", Timestamp: "9:01 AM"})
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	_ = m.View(80, 80)

	row, col := imageHitCenter(t, m)
	ts, attIdx, ok := m.HitTestImage(row, col)
	if !ok || ts != reply.TS || attIdx != 0 {
		t.Fatalf("HitTestImage(%d,%d) = (%q, %d, %v), want (%q, 0, true)", row, col, ts, attIdx, ok, reply.TS)
	}
	if _, _, ok := m.HitTestImage(row, 0); ok {
		t.Error("HitTestImage on the left border column hit; want no hit")
	}
}

// The parent is entry 0 of the list, rendered every frame; its image is
// clickable too.
func TestHitTestImage_Parent(t *testing.T) {
	m := imageThreadModel(t, imgpkg.ProtoHalfBlock)
	parent := withImage(messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"})
	reply := messages.MessageItem{TS: "101.0", UserName: "bob", Text: "nice", Timestamp: "9:01 AM"}
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	_ = m.View(80, 80)

	row, col := imageHitCenter(t, m)
	ts, attIdx, ok := m.HitTestImage(row, col)
	if !ok || ts != parent.TS || attIdx != 0 {
		t.Fatalf("HitTestImage(%d,%d) = (%q, %d, %v), want (%q, 0, true)", row, col, ts, attIdx, ok, parent.TS)
	}
}

// A parent with no replies (or whose replies are still loading) is drawn
// through the zero-reply branch; its image is clickable there too.
func TestHitTestImage_ParentWithNoReplies(t *testing.T) {
	m := imageThreadModel(t, imgpkg.ProtoHalfBlock)
	parent := withImage(messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"})
	m.SetThread(parent, nil, "C1", parent.TS)
	_ = m.View(80, 80)

	row, col := imageHitCenter(t, m)
	ts, attIdx, ok := m.HitTestImage(row, col)
	if !ok || ts != parent.TS || attIdx != 0 {
		t.Fatalf("HitTestImage(%d,%d) = (%q, %d, %v), want (%q, 0, true)", row, col, ts, attIdx, ok, parent.TS)
	}
}

// Switching to a thread with no replies must not leave the previous
// thread's image hits answering clicks: they would open the new parent
// with the old thread's attachment index.
func TestHitTestImage_NoStaleHitsAfterSwitchingToAZeroReplyThread(t *testing.T) {
	m := imageThreadModel(t, imgpkg.ProtoHalfBlock)
	parentA := withImage(messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"})
	replyA := messages.MessageItem{TS: "101.0", UserName: "bob", Text: "nice", Timestamp: "9:01 AM"}
	m.SetThread(parentA, []messages.MessageItem{replyA}, "C1", parentA.TS)
	_ = m.View(80, 80)
	row, col := imageHitCenter(t, m)

	parentB := messages.MessageItem{TS: "200.0", UserName: "carol", Text: "no images here", Timestamp: "10:00 AM"}
	m.SetThread(parentB, nil, "C1", parentB.TS)
	_ = m.View(80, 80)
	if ts, attIdx, ok := m.HitTestImage(row, col); ok {
		t.Fatalf("HitTestImage(%d,%d) = (%q, %d) after switching threads; want no hit", row, col, ts, attIdx)
	}
}
