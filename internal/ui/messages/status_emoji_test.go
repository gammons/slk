package messages

import (
	"bytes"
	goimage "image"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

// statusEmojiImages turns image mode on for one test and points the
// kitty side channel at a buffer.
func statusEmojiImages(t *testing.T) *bytes.Buffer {
	t.Helper()
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })
	var buf bytes.Buffer
	prev := imgpkg.KittyOutput
	imgpkg.KittyOutput = &buf
	t.Cleanup(func() { imgpkg.KittyOutput = prev })
	return &buf
}

// serveStatusEmoji makes the emoji a shortcode names warm in f and
// returns its placement: realistic, with the image ID in the foreground
// and the "\x1b[39m" a real placement ends with. Its upload writes
// upload to the kitty side channel.
func serveStatusEmoji(t *testing.T, f *fakePlaceFetcher, name string, customs map[string]string, id, upload string) string {
	t.Helper()
	url, ok := emojiutil.URLForShortcode(name, customs)
	if !ok {
		t.Fatalf("precondition: :%s: has no URL", name)
	}
	placement := "\x1b[38;2;0;0;" + id + "m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"
	f.setPrerendered(emojiutil.EmojiCacheKey(url), goimage.Pt(2, 1), imgpkg.Render{
		Cells: goimage.Pt(2, 1),
		Lines: []string{placement},
		OnFlush: func(w io.Writer) error {
			_, err := io.WriteString(w, upload)
			return err
		},
	})
	return placement
}

func statusPane(st peerstatus.Status, f *fakePlaceFetcher) *Model {
	m := New([]MessageItem{{TS: "1700000000.000100", UserID: "U1", UserName: "alice", Text: "hello", Timestamp: "10:00 AM"}}, "general")
	m.SetUserStatuses(map[string]peerstatus.Status{"U1": st})
	if f != nil {
		m.SetEmojiContext(EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
	}
	return m
}

func TestAuthorStatus_ImageInTheHeaderAndUploadedFromView(t *testing.T) {
	kitty := statusEmojiImages(t)
	f := newFakePlaceFetcher()
	placement := serveStatusEmoji(t, f, "calendar", nil, "51", "<upload calendar>")

	m := statusPane(peerstatus.Status{Emoji: ":calendar:", Text: "Meeting"}, f)
	out := m.View(20, 80)
	if !strings.Contains(out, "alice") {
		t.Fatalf("author missing:\n%s", out)
	}
	if !strings.Contains(out, placement) {
		t.Errorf("status emoji not drawn as an image in the author header:\n%q", out)
	}
	if strings.Contains(out, "📆") {
		t.Errorf("header still draws the glyph:\n%q", out)
	}
	if got := strings.Count(kitty.String(), "<upload calendar>"); got != 1 {
		t.Errorf("status image uploaded %d times from View, want 1", got)
	}
}

// The selected row repaints the background for the selection tint; the
// image ID in the placement's foreground must come through untouched.
func TestAuthorStatus_SelectedRowKeepsTheImage(t *testing.T) {
	statusEmojiImages(t)
	f := newFakePlaceFetcher()
	placement := serveStatusEmoji(t, f, "calendar", nil, "52", "")

	m := statusPane(peerstatus.Status{Emoji: ":calendar:"}, f)
	m.SetFocused(true)
	_ = m.View(20, 80)
	var sel string
	for i, e := range m.list.Entries() {
		if e.Selectable {
			sel = strings.Join(m.selectedLines(i), "\n")
		}
	}
	if !strings.Contains(sel, "alice") {
		t.Fatalf("no selected message row found:\n%q", sel)
	}
	if !strings.Contains(sel, placement) {
		t.Errorf("selected row lost the placement:\n%q", sel)
	}
}

// A custom status emoji shows the generic glyph until the workspace's
// custom emoji arrive, then its artwork.
func TestAuthorStatus_CustomEmojiAfterSetEmojiCustoms(t *testing.T) {
	statusEmojiImages(t)
	customs := map[string]string{"company-logo": "https://emoji.slack-edge.com/T1/company-logo/abc.png"}
	f := newFakePlaceFetcher()
	placement := serveStatusEmoji(t, f, "company-logo", customs, "53", "")

	m := statusPane(peerstatus.Status{Emoji: ":company-logo:"}, f)
	if out := m.View(20, 80); !strings.Contains(out, peerstatus.FallbackGlyph) {
		t.Fatalf("before customs load the header should draw the fallback glyph:\n%q", out)
	}
	m.SetEmojiCustoms(customs)
	if out := m.View(20, 80); !strings.Contains(out, placement) {
		t.Errorf("custom status emoji not drawn as an image after SetEmojiCustoms:\n%q", out)
	}
}

func TestAuthorStatusEmoji(t *testing.T) {
	now := time.Unix(1700000000, 0)
	statusOf := func(st peerstatus.Status) map[string]peerstatus.Status {
		return map[string]peerstatus.Status{"U1": st}
	}

	t.Run("image mode off is the glyph", func(t *testing.T) {
		f := newFakePlaceFetcher()
		serveStatusEmoji(t, f, "calendar", nil, "54", "")
		for _, st := range []peerstatus.Status{{Emoji: ":calendar:"}, {Huddle: peerstatus.HuddleActive}, {Text: "Lunch"}, {}} {
			got, _, flush := AuthorStatusEmoji(statusOf(st), "U1", now, EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
			if got != st.Glyph(now) || flush != nil {
				t.Errorf("%+v: AuthorStatusEmoji = %q (flush %v), want %q", st, got, flush != nil, st.Glyph(now))
			}
		}
	})

	t.Run("no fetcher is the glyph", func(t *testing.T) {
		statusEmojiImages(t)
		st := peerstatus.Status{Emoji: ":calendar:"}
		if got, _, _ := AuthorStatusEmoji(statusOf(st), "U1", now, EmojiContext{}); got != st.Glyph(now) {
			t.Errorf("AuthorStatusEmoji = %q, want %q", got, st.Glyph(now))
		}
	})

	t.Run("huddle", func(t *testing.T) {
		statusEmojiImages(t)
		f := newFakePlaceFetcher()
		placement := serveStatusEmoji(t, f, "headphones", nil, "55", "")
		got, _, flush := AuthorStatusEmoji(statusOf(peerstatus.Status{Huddle: peerstatus.HuddleActive}), "U1", now, EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
		if got != placement || flush == nil {
			t.Errorf("AuthorStatusEmoji = %q (flush %v), want %q", got, flush != nil, placement)
		}
	})

	t.Run("cold image reserves its cells", func(t *testing.T) {
		statusEmojiImages(t)
		f := newFakePlaceFetcher() // nothing warm: Place reserves blanks and fetches
		got, _, flush := AuthorStatusEmoji(statusOf(peerstatus.Status{Emoji: ":hourglass_flowing_sand:"}), "U1", now, EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
		if got != "  " || flush != nil {
			t.Errorf("AuthorStatusEmoji = %q (flush %v), want two reserved cells", got, flush != nil)
		}
	})

	t.Run("no status", func(t *testing.T) {
		statusEmojiImages(t)
		f := newFakePlaceFetcher()
		if got, _, flush := AuthorStatusEmoji(nil, "U1", now, EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}}); got != "" || flush != nil {
			t.Errorf("AuthorStatusEmoji = %q (flush %v), want empty", got, flush != nil)
		}
	})
}

// A header too wide for its pane wraps. The status emoji wraps as one
// unit, like the timestamp: a cold image's reserved cells survive the
// wrap, so the header has the same shape cold and warm. Here the second
// line holds 25 cells of name, the image and its space 3 more: the
// timestamp (10 with its gap) no longer fits after them in the 36-cell
// budget, warm or cold.
func TestAuthorStatus_WrappedHeaderKeepsTheImageCells(t *testing.T) {
	statusEmojiImages(t)
	long := strings.Repeat("alice", 6) + " " + strings.Repeat("smith", 5)
	render := func(f *fakePlaceFetcher) string {
		m := New([]MessageItem{{TS: "1700000000.000100", UserID: "U1", UserName: long, Text: "hello", Timestamp: "10:00 AM"}}, "general")
		m.SetUserStatuses(map[string]peerstatus.Status{"U1": {Emoji: ":calendar:"}})
		m.SetEmojiContext(EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
		return m.View(20, 40)
	}
	lineOf := func(view, s string) int {
		for i, l := range strings.Split(view, "\n") {
			if strings.Contains(ansi.Strip(l), s) {
				return i
			}
		}
		t.Fatalf("%q not drawn:\n%s", s, view)
		return -1
	}

	f := newFakePlaceFetcher()
	cold := render(f)
	placement := serveStatusEmoji(t, f, "calendar", nil, "57", "")
	warm := render(f)
	if !strings.Contains(warm, placement) {
		t.Fatalf("warm header has no image:\n%q", warm)
	}
	if lineOf(warm, "10:00 AM") == lineOf(warm, "smith") {
		t.Fatalf("precondition: the warm timestamp wraps under the name:\n%s", warm)
	}
	if lineOf(cold, "10:00 AM") != lineOf(warm, "10:00 AM") {
		t.Errorf("the timestamp moved when the image landed: the cold reservation was lost in the wrap\ncold:\n%s\nwarm:\n%s", cold, warm)
	}
}

// Without images the status glyph wraps with the name as it always
// has: an overlong name and glyph reflow together, whitespace
// normalized, so the glyph stays on the name's line.
func TestAuthorStatus_GlyphWrapsWithTheName(t *testing.T) {
	name := "Alexandria  Montgomery-Christensen"
	m := New([]MessageItem{{TS: "1700000000.000100", UserID: "U1", UserName: name, Text: "hello", Timestamp: "10:00 AM"}}, "general")
	m.SetUserStatuses(map[string]peerstatus.Status{"U1": {Emoji: ":calendar:"}})
	out := ansi.Strip(m.View(20, 40))
	if !strings.Contains(out, "Alexandria Montgomery-Christensen 📆") {
		t.Errorf("glyph not wrapped with the name:\n%s", out)
	}
}
