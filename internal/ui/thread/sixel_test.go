package thread

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/entrylist"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
)

// imageHit returns the last frame's only image hit rect.
func imageHit(t *testing.T, m *Model) entrylist.FrameHit {
	t.Helper()
	for _, h := range m.list.Hits() {
		if h.Kind == entrylist.HitImage {
			return h
		}
	}
	t.Fatal("frame has no image hit rect")
	return entrylist.FrameHit{}
}

// A fully visible sixel image on a reply is placed, not drawn as text:
// one placement where the image's own cells are, carrying its bytes.
func TestSixel_ReplyImageIsPlaced(t *testing.T) {
	m := imageThreadModel(t, imgpkg.ProtoSixel)
	parent := messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"}
	reply := withImage(messages.MessageItem{TS: "101.0", UserName: "bob", Text: "see", Timestamp: "9:01 AM"})
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	_ = m.View(80, 80)

	got := m.SixelPlacements()
	if len(got) != 1 {
		t.Fatalf("SixelPlacements() = %d placements, want 1", len(got))
	}
	p, h := got[0], imageHit(t, m)
	if p.Row != h.RowStart || p.Col != h.ColStart || p.Rows != h.RowEnd-h.RowStart || p.Cols != h.ColEnd-h.ColStart {
		t.Errorf("placement at row %d col %d, %dx%d; want the image's cells: row %d col %d, %dx%d",
			p.Row, p.Col, p.Cols, p.Rows, h.RowStart, h.ColStart, h.ColEnd-h.ColStart, h.RowEnd-h.RowStart)
	}
	if len(p.Bytes) == 0 || p.Key == "" {
		t.Errorf("placement carries no sixel bytes or key: %+v", p)
	}
}

// The parent is entry 0, rendered every frame; its image is placed too,
// including through the zero-reply branch.
func TestSixel_ParentImageIsPlaced(t *testing.T) {
	for name, replies := range map[string][]messages.MessageItem{
		"with replies": {{TS: "101.0", UserName: "bob", Text: "nice", Timestamp: "9:01 AM"}},
		"no replies":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			m := imageThreadModel(t, imgpkg.ProtoSixel)
			parent := withImage(messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"})
			m.SetThread(parent, replies, "C1", parent.TS)
			_ = m.View(80, 80)
			if got := m.SixelPlacements(); len(got) != 1 {
				t.Fatalf("SixelPlacements() = %d placements, want 1", len(got))
			}
		})
	}
}

// An image the window cuts is not placed: sixel cannot draw part of an
// image, so its visible rows show the halfblock fallback instead of the
// blank placeholder.
func TestSixel_PartlyVisibleImageFallsBackToHalfblock(t *testing.T) {
	m := imageThreadModel(t, imgpkg.ProtoSixel)
	parent := messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"}
	reply := withImage(messages.MessageItem{TS: "101.0", UserName: "bob", Text: "see", Timestamp: "9:01 AM"})
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	m.selected = parentSelected // keep the window at the top, so the reply's image is cut by the bottom edge
	out := m.View(12, 80)

	if got := m.SixelPlacements(); len(got) != 0 {
		t.Fatalf("SixelPlacements() = %+v, want none for a cut image", got)
	}
	if !strings.ContainsAny(ansi.Strip(out), "▀▄") {
		t.Errorf("cut image drew no halfblock fallback:\n%s", ansi.Strip(out))
	}
}

// Off sixel nothing is placed: half-block images are ordinary text.
func TestSixel_NoPlacementsOffSixel(t *testing.T) {
	m := imageThreadModel(t, imgpkg.ProtoHalfBlock)
	parent := messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"}
	reply := withImage(messages.MessageItem{TS: "101.0", UserName: "bob", Text: "see", Timestamp: "9:01 AM"})
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	_ = m.View(80, 80)
	if got := m.SixelPlacements(); len(got) != 0 {
		t.Fatalf("SixelPlacements() = %+v, want none off sixel", got)
	}
}

// Placements belong to one frame: switching to a thread without images
// clears them, or the App would keep painting the old image.
func TestSixel_PlacementsClearWhenTheImageGoes(t *testing.T) {
	m := imageThreadModel(t, imgpkg.ProtoSixel)
	parent := withImage(messages.MessageItem{TS: "100.0", UserName: "alice", Text: "parent", Timestamp: "9:00 AM"})
	m.SetThread(parent, nil, "C1", parent.TS)
	_ = m.View(80, 80)
	if len(m.SixelPlacements()) != 1 {
		t.Fatal("precondition: the parent's image is placed")
	}
	other := messages.MessageItem{TS: "200.0", UserName: "carol", Text: "no images", Timestamp: "10:00 AM"}
	m.SetThread(other, nil, "C1", other.TS)
	_ = m.View(80, 80)
	if got := m.SixelPlacements(); len(got) != 0 {
		t.Fatalf("SixelPlacements() = %+v after switching threads, want none", got)
	}
}
