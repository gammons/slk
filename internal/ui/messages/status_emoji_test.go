package messages

import (
	"bytes"
	goimage "image"
	"io"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	bubblesansi "github.com/gammons/slk/internal/bubbles/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
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

// A custom status emoji shows the generic glyph (drawn through the image
// path, like every status emoji) until the workspace's custom emoji
// arrive, then its artwork.
func TestAuthorStatus_CustomEmojiAfterSetEmojiCustoms(t *testing.T) {
	statusEmojiImages(t)
	customs := map[string]string{"company-logo": "https://emoji.slack-edge.com/T1/company-logo/abc.png"}
	f := newFakePlaceFetcher()
	placement := serveStatusEmoji(t, f, "company-logo", customs, "53", "")

	fallback := serveStatusEmoji(t, f, "speech_balloon", nil, "56", "")

	m := statusPane(peerstatus.Status{Emoji: ":company-logo:"}, f)
	if out := m.View(20, 80); !strings.Contains(out, fallback) || strings.Contains(out, placement) {
		t.Fatalf("before customs load the header should draw the fallback image, not the custom artwork:\n%q", out)
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
	// call returns the emoji, whether it is an image, and how many
	// uploads it queued on the body's collector.
	call := func(statuses map[string]peerstatus.Status, f emojiutil.PlaceFetcher) (string, bool, int) {
		var flushes []func(io.Writer) error
		opts := &RenderSlackMarkdownOpts{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, EmojiCells: 2, EmojiFlushes: &flushes}
		got, image := authorStatusEmoji(statuses, "U1", now, opts)
		return got, image, len(flushes)
	}

	t.Run("image mode off is the glyph", func(t *testing.T) {
		f := newFakePlaceFetcher()
		serveStatusEmoji(t, f, "calendar", nil, "54", "")
		for _, st := range []peerstatus.Status{{Emoji: ":calendar:"}, {Huddle: peerstatus.HuddleActive}, {Text: "Lunch"}, {}} {
			if got, image, n := call(statusOf(st), f); got != st.Glyph(now) || image || n != 0 {
				t.Errorf("%+v: authorStatusEmoji = %q (image %v, %d uploads), want %q", st, got, image, n, st.Glyph(now))
			}
		}
	})

	t.Run("no fetcher is the glyph", func(t *testing.T) {
		statusEmojiImages(t)
		st := peerstatus.Status{Emoji: ":calendar:"}
		if got, image, _ := call(statusOf(st), nil); got != st.Glyph(now) || image {
			t.Errorf("authorStatusEmoji = %q (image %v), want %q", got, image, st.Glyph(now))
		}
	})

	t.Run("huddle", func(t *testing.T) {
		statusEmojiImages(t)
		f := newFakePlaceFetcher()
		placement := serveStatusEmoji(t, f, "headphones", nil, "55", "")
		if got, image, n := call(statusOf(peerstatus.Status{Huddle: peerstatus.HuddleActive}), f); !strings.HasPrefix(got, placement) || !image || n == 0 {
			t.Errorf("authorStatusEmoji = %q (image %v, %d uploads), want it to start with %q", got, image, n, placement)
		}
	})

	t.Run("cold image reserves its cells", func(t *testing.T) {
		statusEmojiImages(t)
		f := newFakePlaceFetcher() // nothing warm: Place reserves blanks and fetches
		if got, image, n := call(statusOf(peerstatus.Status{Emoji: ":hourglass_flowing_sand:"}), f); got != "  " || !image || n != 0 {
			t.Errorf("authorStatusEmoji = %q (image %v, %d uploads), want two reserved cells", got, image, n)
		}
	})

	t.Run("no status", func(t *testing.T) {
		statusEmojiImages(t)
		if got, image, n := call(nil, newFakePlaceFetcher()); got != "" || image || n != 0 {
			t.Errorf("authorStatusEmoji = %q (image %v, %d uploads), want empty", got, image, n)
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

func dmHeaderPane(f *fakePlaceFetcher) *Model {
	m := New(nil, "alice")
	m.SetChannelType("dm")
	if f != nil {
		m.SetEmojiContext(EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
	}
	return m
}

var dmTopicNow = time.Unix(1700000000, 0)

func TestDMTopic_EmojiAsImagesWithTheTopicColourRestored(t *testing.T) {
	kitty := statusEmojiImages(t)
	f := newFakePlaceFetcher()
	huddle := serveStatusEmoji(t, f, "headphones", nil, "81", "<upload headphones>")
	cal := serveStatusEmoji(t, f, "calendar", nil, "82", "<upload calendar>")

	m := dmHeaderPane(f)
	m.SetDMTopic(peerstatus.Status{Emoji: ":calendar:", Text: "Meeting", Huddle: peerstatus.HuddleActive}, dmTopicNow, "15:04")
	out := m.View(10, 80)
	restore := bubblesansi.ForegroundSGR(styles.Timestamp.GetForeground())
	if !strings.Contains(out, huddle+restore+" In a huddle") {
		t.Errorf("huddle not drawn as an image with the topic colour restored after it:\n%q", out)
	}
	if !strings.Contains(out, cal+restore+" Meeting") {
		t.Errorf("status not drawn as an image with the topic colour restored after it:\n%q", out)
	}
	if strings.Contains(out, peerstatus.HuddleGlyph) || strings.Contains(out, "📆") {
		t.Errorf("header still draws a glyph:\n%q", out)
	}
	for _, u := range []string{"<upload headphones>", "<upload calendar>"} {
		if n := strings.Count(kitty.String(), u); n != 1 {
			t.Errorf("%s fired %d times from View, want 1", u, n)
		}
	}
	// Every chrome line is exactly the pane width (ui.borderedTopPane
	// assembles the border by concatenation).
	for i, l := range strings.Split(m.chromeCache, "\n") {
		if w := lipgloss.Width(l); w != 80 {
			t.Errorf("chrome line %d is %d cells, want 80", i, w)
		}
	}
}

// A cold image keeps its blank cells: nothing trims them, so the line
// does not shift when the image arrives.
func TestDMTopic_ColdReservationKept(t *testing.T) {
	statusEmojiImages(t)
	f := newFakePlaceFetcher() // nothing warm
	m := dmHeaderPane(f)
	m.SetDMTopic(peerstatus.Status{Emoji: ":hourglass_flowing_sand:"}, dmTopicNow, "15:04")
	_ = m.View(10, 80)
	topicLine := strings.Split(m.chromeCache, "\n")[1]
	if got := ansi.Strip(topicLine); !strings.HasPrefix(got, "  ") || strings.TrimSpace(got) != "" {
		t.Errorf("cold topic = %q, want a 2-cell blank reservation", got)
	}
}

// A status text too long for the pane wraps the topic. The images stay
// as wide through the wrap as they are drawn: the cold header is the
// warm one with blanks where the images are, so nothing moves when they
// land, and every chrome line keeps the pane width.
func TestDMTopic_WrappedTopicKeepsTheImageCells(t *testing.T) {
	statusEmojiImages(t)
	st := peerstatus.Status{Emoji: ":calendar:", Text: "abcdefghijklmnopqrs", Huddle: peerstatus.HuddleActive}
	render := func(f *fakePlaceFetcher) *Model {
		m := dmHeaderPane(f)
		m.SetDMTopic(st, dmTopicNow, "15:04")
		_ = m.View(10, 20)
		return m
	}
	f := newFakePlaceFetcher()
	cold := render(f)
	huddle := serveStatusEmoji(t, f, "headphones", nil, "83", "")
	cal := serveStatusEmoji(t, f, "calendar", nil, "84", "")
	warm := render(f)
	if !strings.Contains(warm.chromeCache, huddle) || !strings.Contains(warm.chromeCache, cal) {
		t.Fatalf("warm topic has no images:\n%q", warm.chromeCache)
	}
	blanked := strings.NewReplacer(huddle, "  ", cal, "  ").Replace(warm.chromeCache)
	if c, w := ansi.Strip(cold.chromeCache), ansi.Strip(blanked); c != w {
		t.Errorf("cold header is not the warm one with blank images:\ncold:\n%s\nwarm:\n%s", c, w)
	}
	for name, m := range map[string]*Model{"cold": cold, "warm": warm} {
		for i, l := range strings.Split(m.chromeCache, "\n") {
			if w := lipgloss.Width(l); w != 20 {
				t.Errorf("%s: chrome line %d is %d cells, want 20: %q", name, i, w, l)
			}
		}
	}
}

// The header is rebuilt when an emoji fetch lands, when custom emoji
// arrive, and when the status changes even though its text does not
// (two custom emoji draw the same fallback glyph).
func TestDMTopic_HeaderRebuilds(t *testing.T) {
	statusEmojiImages(t)
	customs := map[string]string{
		"logo-a": "https://emoji.slack-edge.com/T1/logo-a/a.png",
		"logo-b": "https://emoji.slack-edge.com/T1/logo-b/b.png",
	}
	f := newFakePlaceFetcher()
	a := serveStatusEmoji(t, f, "logo-a", customs, "83", "")
	b := serveStatusEmoji(t, f, "logo-b", customs, "84", "")

	m := dmHeaderPane(f)
	m.SetDMTopic(peerstatus.Status{Emoji: ":logo-a:", Text: "Busy"}, dmTopicNow, "15:04")
	if out := m.View(10, 80); !strings.Contains(out, peerstatus.FallbackGlyph+" Busy") {
		t.Fatalf("before customs load the header should draw the fallback glyph:\n%q", out)
	}
	m.SetEmojiCustoms(customs)
	if out := m.View(10, 80); !strings.Contains(out, a) {
		t.Errorf("SetEmojiCustoms did not rebuild the header with the image:\n%q", out)
	}

	m.SetDMTopic(peerstatus.Status{Emoji: ":logo-b:", Text: "Busy"}, dmTopicNow, "15:04")
	if out := m.View(10, 80); !strings.Contains(out, b) {
		t.Errorf("a new status with the same text did not rebuild the header:\n%q", out)
	}

	// A fetch that lands (here: a different placement now warm for the
	// same URL) reaches the header through HandleEmojiImageReady.
	b2 := serveStatusEmoji(t, f, "logo-b", customs, "85", "")
	_ = m.View(10, 80)
	m.HandleEmojiImageReady("")
	if out := m.View(10, 80); !strings.Contains(out, b2) {
		t.Errorf("HandleEmojiImageReady did not rebuild the header:\n%q", out)
	}
}

// With image mode off, or for a plain topic, the header is what
// SetChannelTopic draws.
func TestDMTopic_ImageModeOffIsThePlainTopic(t *testing.T) {
	st := peerstatus.Status{Emoji: ":calendar:", Text: "Meeting", Huddle: peerstatus.HuddleActive, DND: true}
	f := newFakePlaceFetcher()
	serveStatusEmoji(t, f, "calendar", nil, "86", "")

	plain := dmHeaderPane(nil)
	plain.SetChannelTopic(st.Summary(dmTopicNow, "15:04"))
	want := plain.View(10, 80)

	m := dmHeaderPane(f)
	m.SetDMTopic(st, dmTopicNow, "15:04")
	if got := m.View(10, 80); got != want {
		t.Errorf("image mode off changed the DM header:\n got %q\nwant %q", got, want)
	}

	// SetChannelTopic drops the kept status: a plain topic never draws images.
	statusEmojiImages(t)
	m.SetChannelTopic("Release day")
	if got := m.View(10, 80); strings.Contains(got, "\U0010EEEE") {
		t.Errorf("plain topic drew an image:\n%q", got)
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
