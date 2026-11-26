package activityview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
)

func linkModel() Model {
	it := core.ActivityItem{
		Key: "k1", Type: "at_user", FeedTS: "1700000000.000100",
		ChannelID: "C1", TS: "1700000000.000100", AuthorID: "U1",
	}
	m := New(map[string]string{"U1": "alice"}, "U2")
	m.SetItems([]core.ActivityItem{it})
	m.SetBodies(map[string]core.ActivityMessage{
		core.ActivityMsgKey("C1", it.TS): {Text: "see <https://example.com/x|label> here", UserID: "U1"},
	})
	return m
}

// A link in a card preview is clickable: the hit test finds it by the
// column it was drawn in.
func TestCardLinkHit(t *testing.T) {
	m := linkModel()
	out := m.View(10, 80)
	row, col := -1, -1
	for y, line := range strings.Split(ansi.Strip(out), "\n") {
		if i := strings.Index(line, "label"); i >= 0 {
			row, col = y, ansi.StringWidth(line[:i])
			break
		}
	}
	if row < 0 {
		t.Fatalf("the preview does not draw the link label:\n%s", ansi.Strip(out))
	}
	if url, ok := m.HitTestLink(row, col); !ok || url != "https://example.com/x" {
		t.Errorf("HitTestLink(%d,%d) = %q, %v, want the link", row, col, url, ok)
	}
	if _, ok := m.HitTestLink(row, col+len("label")); ok {
		t.Error("a cell past the label hit the link")
	}
}

// Clipping a card line keeps escape sequences whole: the hyperlink stays
// one opener and one closer, not a half-written sequence.
func TestClipToWidthKeepsOSC8(t *testing.T) {
	link := "\x1b]8;;https://example.com/x\x1b\\a-very-long-label\x1b]8;;\x1b\\"
	got := clipToWidth("prefix "+link, 20)
	if n := strings.Count(got, "\x1b]8;;"); n != 2 {
		t.Errorf("clipped line has %d OSC 8 markers, want an opener and a closer: %q", n, got)
	}
	if !strings.HasSuffix(ansi.Strip(got), "…") {
		t.Errorf("clipped line = %q, want an ellipsis", ansi.Strip(got))
	}
}

// An emptied feed clears the previous cards' link hits.
func TestEmptyFeedClearsLinkHits(t *testing.T) {
	m := linkModel()
	out := m.View(10, 80)
	row, col := -1, -1
	for y, line := range strings.Split(ansi.Strip(out), "\n") {
		if i := strings.Index(line, "label"); i >= 0 {
			row, col = y, ansi.StringWidth(line[:i])
			break
		}
	}
	if row < 0 {
		t.Fatal("no link drawn")
	}
	m.SetItems(nil)
	m.View(10, 80)
	if _, ok := m.HitTestLink(row, col); ok {
		t.Error("a link hit survived an empty feed")
	}
}
