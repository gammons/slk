package messages

import (
	goimage "image"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)

// Regression test for #133. A kitty image placement encodes the image
// ID in the foreground color and ends by resetting it (\e[39m), so the
// count printed after it renders uncolored unless the pill's own
// foreground is re-asserted in between.
func TestRenderReactionPill_ImageEmojiKeepsCountColor(t *testing.T) {
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })

	const placeholder = "\U0010EEEE"
	url := emojiutil.CDNBaseURL + "1f44d.png"
	ff := newFakePlaceFetcher()
	ff.setPrerendered(emojiutil.EmojiCacheKey(url), goimage.Pt(2, 1), imgpkg.Render{
		Cells:   goimage.Pt(2, 1),
		Lines:   []string{placeholder + placeholder},
		OnFlush: func(io.Writer) error { return nil },
	})

	m := New([]MessageItem{{
		TS:        "1.0",
		UserName:  "alice",
		UserID:    "U1",
		Text:      "hello",
		Timestamp: "10:30 AM",
		Reactions: []ReactionItem{{Emoji: "thumbsup", Count: 3, HasReacted: true}},
	}}, "general")
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emojiutil.PlaceContext{Fetcher: ff},
		Cells:    2,
		Customs:  nil,
	})

	out := m.View(24, 80)
	if !strings.Contains(out, placeholder) {
		t.Fatal("expected an image placement in the pill; the rest of this test proves nothing without one")
	}

	want := placeholder + ansi.Style{}.ForegroundColor(styles.ReactionPillOwn.GetForeground()).String()
	if !strings.Contains(out, want) {
		t.Error("count is not preceded by the pill fg — it renders uncolored " +
			"while the rest of the pill stays colored (#133)")
	}
}
